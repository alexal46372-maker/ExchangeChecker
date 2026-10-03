package sqlcheck

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"sync"
	"time"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
)

// Bounded session setup (PLAN.md §12 п. 40).
//
// go-mssqldb v1.8.2 honors the context and the dial timeout only for the
// TCP dial; prelogin, TLS and login responses are read without a deadline
// and without watching the context. A host that accepts TCP and never
// answers therefore hung the caller, the socket and the sql.DB forever.
//
// guardedConnector wraps the driver connector. Every Connect call gets a
// handshake guard (passed to handshakeDialer through the context), which
// records the sockets the driver dials and runs a small state machine
// under a mutex:
//
//	handshaking --login ok--> done     (deadline cleared, watcher gone)
//	handshaking --ctx done / budget--> killed (sockets expired + closed)
//	handshaking --driver error--> failed      (sockets closed at once)
//
// The watcher reacts to the caller's context (Canceled or
// DeadlineExceeded) and to the handshake budget (Profile.ConnectTimeout)
// but acts only while the state is handshaking. Success is decided by the
// wrapper after the driver's Connect has returned, under the same lock, so
// the driver's own deferred cancel of its dial context cannot reach an
// established session, and a deadline firing right after login cannot
// poison it: the transition to done clears the socket deadline and the
// watcher can no longer act. Long poll queries are never cut by the guard.

type hsState int

const (
	hsHandshaking hsState = iota
	hsDone
	hsKilled
	hsFailed
)

type hsGuard struct {
	mu    sync.Mutex
	state hsState
	conns []net.Conn
	cause error         // why the handshake was killed
	stop  chan struct{} // closed when the handshake is over (any state)
}

func newHSGuard() *hsGuard { return &hsGuard{stop: make(chan struct{})} }

// add registers a dialed socket; it is closed at once when the handshake
// is no longer running.
func (h *hsGuard) add(c net.Conn) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state != hsHandshaking {
		_ = c.Close()
		if h.cause != nil {
			return h.cause
		}
		return errors.New("sqlcheck: handshake is over")
	}
	h.conns = append(h.conns, c)
	return nil
}

// kill expires and closes every socket if the handshake is still running.
func (h *hsGuard) kill(cause error) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state != hsHandshaking {
		return false
	}
	h.state, h.cause = hsKilled, cause
	for _, c := range h.conns {
		_ = c.SetDeadline(time.Unix(1, 0))
		_ = c.Close()
	}
	h.end()
	return true
}

// sockets returns the registered sockets (for a sessionHold).
func (h *hsGuard) sockets() []net.Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]net.Conn(nil), h.conns...)
}

// finish ends the handshake after the driver's Connect returned: on
// success the sockets lose any deadline (state done), on failure they are
// closed. It reports whether the session is usable (false if the watcher
// killed it first).
func (h *hsGuard) finish(err error) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state != hsHandshaking {
		return false
	}
	if err != nil {
		h.state = hsFailed
		for _, c := range h.conns {
			_ = c.Close()
		}
	} else {
		h.state = hsDone
		for _, c := range h.conns {
			_ = c.SetDeadline(time.Time{})
		}
	}
	h.conns = nil
	h.end()
	return err == nil
}

func (h *hsGuard) end() { close(h.stop) }

func (h *hsGuard) causeErr() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cause
}

// watch kills the handshake when ctx ends or the budget expires, whichever
// comes first, unless the handshake is over before.
func (h *hsGuard) watch(ctx context.Context, budget time.Duration) {
	var timeout <-chan time.Time
	if budget > 0 {
		t := time.NewTimer(budget)
		defer t.Stop()
		timeout = t.C
	}
	select {
	case <-h.stop:
	case <-ctx.Done():
		h.kill(ctx.Err())
	case <-timeout:
		h.kill(context.DeadlineExceeded)
	}
}

type hsKey struct{}

// handshakeDialer dials TCP like the driver's default dialer and registers
// the socket with the handshake guard of the Connect call.
type handshakeDialer struct{ nd net.Dialer }

func (d handshakeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := d.nd.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	if h, ok := ctx.Value(hsKey{}).(*hsGuard); ok {
		if err := h.add(conn); err != nil {
			return nil, err
		}
	}
	return conn, nil
}

// guardedConnector is the driver.Connector of Open.
type guardedConnector struct {
	inner  *mssql.Connector
	budget time.Duration
	hold   *sessionHold // nil: the guard ends at login (poll stores)
}

func newGuardedConnector(cfg msdsn.Config, budget time.Duration) *guardedConnector {
	c := mssql.NewConnectorConfig(cfg)
	c.Dialer = handshakeDialer{nd: net.Dialer{KeepAlive: cfg.KeepAlive}}
	return &guardedConnector{inner: c, budget: budget}
}

func (g *guardedConnector) Driver() driver.Driver { return g.inner.Driver() }

func (g *guardedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	h := newHSGuard()
	go h.watch(ctx, g.budget)
	conn, err := g.inner.Connect(context.WithValue(ctx, hsKey{}, h))
	var socks []net.Conn
	if err == nil && g.hold != nil {
		socks = h.sockets()
	}
	if !h.finish(err) && err == nil {
		// the watcher killed the sockets while the driver was returning
		_ = conn.Close()
		if err = h.causeErr(); err == nil {
			err = driver.ErrBadConn
		}
		return nil, err
	}
	if err != nil {
		if cause := h.causeErr(); cause != nil && !errors.Is(err, cause) {
			// expose the context error so classify / ConnCategory see a timeout
			return nil, &hsError{err: err, cause: cause}
		}
		return nil, err
	}
	if g.hold != nil && !g.hold.adopt(socks) {
		_ = conn.Close()
		return nil, g.hold.causeOr(driver.ErrBadConn)
	}
	return conn, nil
}

// hsError keeps the driver's message and adds the reason of the kill.
type hsError struct{ err, cause error }

func (e *hsError) Error() string   { return e.err.Error() + " (" + e.cause.Error() + ")" }
func (e *hsError) Unwrap() []error { return []error{e.err, e.cause} }

// sessionHold keeps the sockets of a short-lived store (TestConnectionInfo)
// under a watcher after login. go-mssqldb v1.8.2 reacts to the end of a
// query context by sending ATTENTION and then waits for the server's
// confirmation with a blocking read without deadline (tokenProcessor.
// nextToken -> readCancelConfirmation): on a server that went silent after
// login, Ping or the server-info query hung until TCP keepalive gave up
// (forever when the peer host still ACKs). While an operation of the hold
// is active, the end of its context expires and closes every adopted
// socket; between operations and after the store is closed it does
// nothing. Long-lived poll stores (Open) have no hold, so polling is never
// cut by it.
type sessionHold struct {
	mu     sync.Mutex
	conns  []net.Conn
	gen    int  // operation generation
	active bool // an operation of generation gen is running
	cause  error
}

// adopt takes over the sockets of a new session; false if the hold
// already killed its sessions (the sockets are closed then).
func (s *sessionHold) adopt(c []net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cause != nil {
		for _, x := range c {
			_ = x.Close()
		}
		return false
	}
	s.conns = append(s.conns, c...)
	return true
}

// op starts an operation bounded by ctx and returns its end function.
// When ctx ends while the operation runs, the sockets are expired and
// closed, so the blocked driver read returns at once.
func (s *sessionHold) op(ctx context.Context) (end func()) {
	s.mu.Lock()
	s.gen++
	gen := s.gen
	s.active = true
	s.mu.Unlock()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-stop:
		case <-ctx.Done():
			s.kill(gen, ctx.Err())
		}
	}()
	return func() {
		s.mu.Lock()
		if s.gen == gen {
			s.active = false
		}
		s.mu.Unlock()
		close(stop)
		<-done
	}
}

func (s *sessionHold) kill(gen int, cause error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || s.gen != gen || s.cause != nil {
		return
	}
	s.cause = cause
	for _, c := range s.conns {
		_ = c.SetDeadline(time.Unix(1, 0))
		_ = c.Close()
	}
	s.conns = nil
}

// causeOr returns why the hold killed its sessions, or def.
func (s *sessionHold) causeOr(def error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cause != nil {
		return s.cause
	}
	return def
}

// killed reports the kill reason (nil if the sessions were not killed).
func (s *sessionHold) killed() error { return s.causeOr(nil) }

// holdError turns an operation error into a timeout / cancel error when
// the hold cut the session (the driver then reports a closed socket or a
// missing cancel confirmation).
func holdError(op string, err error, hold *sessionHold) error {
	cause := hold.killed()
	if cause == nil {
		return err
	}
	kind := KindTimeout
	if errors.Is(cause, context.Canceled) {
		kind = KindCanceled
	}
	if err == nil {
		err = cause
	}
	return &Error{Kind: kind, Op: op, Err: &hsError{err: err, cause: cause}}
}
