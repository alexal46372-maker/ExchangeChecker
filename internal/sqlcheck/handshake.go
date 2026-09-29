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
	return conn, nil
}

// hsError keeps the driver's message and adds the reason of the kill.
type hsError struct{ err, cause error }

func (e *hsError) Error() string   { return e.err.Error() + " (" + e.cause.Error() + ")" }
func (e *hsError) Unwrap() []error { return []error{e.err, e.cause} }
