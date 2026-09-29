package ipc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Listener accepts client connections (PipeListener on Windows; in-memory
// listeners in tests).
type Listener interface {
	Accept() (io.ReadWriteCloser, error)
	Close() error
}

// ErrListenerClosed is returned by Accept after Close.
var ErrListenerClosed = errors.New("ipc: listener closed")

// Handler is the service side (the monitor). PollNow / ReloadConfig are
// rate-limited by the handler; coalesced == true is answered with
// model.ResultCoalesced (CHK-MON-12).
type Handler interface {
	Snapshot() model.StatusSnapshot
	// PollNow polls every profile (profileID "") or one profile.
	PollNow(profileID string) (coalesced bool, err error)
	ReloadConfig() (coalesced bool)
}

// Optional handler interfaces (protocol version 2). A handler without
// them serves version-1 commands only; Hello.Capabilities lists what it
// supports and the other version-2 commands answer unknown_command.

// Tester runs test_profile (CHK-IPC-03). Called on its own goroutine: a
// test may take up to its timeout and must not block the client's other
// requests (ping). Errors of type *model.IPCError carry the reply code.
type Tester interface {
	TestProfile(ctx context.Context, req model.IPCRequest) (model.TestProfileResult, error)
}

// Acker runs ack_alert (CHK-IPC-02).
type Acker interface {
	AckAlert(profileID, pointKey string) (int, error)
}

// HeartbeatSource fills the heartbeat of Hello / Pong (CHK-IPC-04).
type HeartbeatSource interface {
	Heartbeat() model.Heartbeat
}

// Logger is the line logger.
type Logger interface {
	Printf(format string, args ...interface{})
}

type nopLogger struct{}

func (nopLogger) Printf(string, ...interface{}) {}

// outQueue is the per-client push queue length; a client that does not
// read is disconnected instead of blocking the service.
const outQueue = 64

// Server serves the protocol to at most MaxClients clients at a time.
type Server struct {
	H   Handler
	Log Logger
	// Limits (CHK-MON-11); NewServer sets the model.IPC* defaults.
	MaxRequest  int           // request frame limit (bytes)
	MaxClients  int           // concurrent connections
	ReadTimeout time.Duration // rest of a started request frame
	IdleTimeout time.Duration // no request at all
	// Accept failure backoff / log period (defaults AcceptBackoff*,
	// AcceptLogEvery; tests shorten them).
	// Zero values mean the defaults, also for a Server built without
	// NewServer.
	BackoffMin, BackoffMax, LogEvery time.Duration

	// clock of the accept loop (tests inject fakes; nil = time.Now /
	// time.After)
	now   func() time.Time
	after func(time.Duration) <-chan time.Time

	mu     sync.Mutex
	l      Listener
	conns  map[*conn]struct{}
	closed bool
	stop   chan struct{}
	slots  chan struct{}
	wg     sync.WaitGroup
	// ctx is canceled by Close (running test_profile calls end)
	ctx    context.Context
	cancel context.CancelFunc
}

type conn struct {
	rw      io.ReadWriteCloser
	out     chan model.IPCMessage
	once    sync.Once
	done    chan struct{}
	relOnce sync.Once
	release func()
}

func (c *conn) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.rw.Close()
	})
}

// send queues m; false when the queue is full or the connection closed.
func (c *conn) send(m model.IPCMessage) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.out <- m:
		return true
	case <-c.done:
		return false
	default:
		return false
	}
}

// NewServer returns a server for h.
func NewServer(h Handler, log Logger) *Server {
	if log == nil {
		log = nopLogger{}
	}
	return &Server{H: h, Log: log, conns: map[*conn]struct{}{}, stop: make(chan struct{}),
		MaxRequest: model.IPCMaxRequest, MaxClients: model.IPCMaxClients,
		ReadTimeout: model.IPCReadTimeout, IdleTimeout: model.IPCIdleTimeout,
		BackoffMin: AcceptBackoffMin, BackoffMax: AcceptBackoffMax, LogEvery: AcceptLogEvery}
}

// Accept failure handling (CHK-MON-11): exponential backoff from
// AcceptBackoffMin, doubling up to AcceptBackoffMax, reset after a
// successful Accept; the failure is logged once, then only when the error
// text changes or at most every AcceptLogEvery (with the number of
// suppressed repeats).
const (
	AcceptBackoffMin = 200 * time.Millisecond
	AcceptBackoffMax = 30 * time.Second
	AcceptLogEvery   = 10 * time.Minute
)

// initLocked fills what NewServer would have set, so a Server literal
// works too (s.mu held).
func (s *Server) initLocked() {
	if s.Log == nil {
		s.Log = nopLogger{}
	}
	if s.conns == nil {
		s.conns = map[*conn]struct{}{}
	}
	if s.stop == nil {
		s.stop = make(chan struct{})
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.after == nil {
		s.after = time.After
	}
	if s.ctx == nil {
		s.ctx, s.cancel = context.WithCancel(context.Background())
	}
}

// Capabilities lists the version-2 features of the handler.
func (s *Server) Capabilities() []string {
	var caps []string
	if _, ok := s.H.(Tester); ok {
		caps = append(caps, model.CapTestProfile)
	}
	if _, ok := s.H.(Acker); ok {
		caps = append(caps, model.CapAckAlert)
	}
	if _, ok := s.H.(HeartbeatSource); ok {
		caps = append(caps, model.CapHeartbeat)
	}
	return caps
}

func (s *Server) heartbeat() *model.Heartbeat {
	if h, ok := s.H.(HeartbeatSource); ok {
		hb := h.Heartbeat()
		return &hb
	}
	return nil
}

// errorReply builds MsgError; *model.IPCError supplies Code / ErrorKey.
func errorReply(id uint64, err error, code string) model.IPCMessage {
	m := model.IPCMessage{ID: id, Type: model.MsgError, Error: err.Error(), Code: code}
	var ie *model.IPCError
	if errors.As(err, &ie) {
		m.Code, m.ErrorKey = ie.Code, ie.Key
	}
	return m
}

// Serve accepts clients until Close; it returns nil after Close.
func (s *Server) Serve(l Listener) error {
	s.mu.Lock()
	s.initLocked()
	if s.closed {
		s.mu.Unlock()
		_ = l.Close()
		return nil
	}
	s.l = l
	if s.slots == nil {
		n := s.MaxClients
		if n <= 0 {
			n = model.IPCMaxClients
		}
		s.slots = make(chan struct{}, n)
	}
	slots := s.slots
	s.mu.Unlock()
	var fail acceptFailures
	for {
		// a free slot first: with MaxClients connections no new pipe
		// instance is created, further clients get ERROR_PIPE_BUSY
		select {
		case slots <- struct{}{}:
		case <-s.stop:
			return nil
		}
		release := func() { <-slots }
		rw, err := l.Accept()
		if err != nil {
			release()
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || errors.Is(err, ErrListenerClosed) {
				return nil
			}
			select {
			case <-s.after(fail.next(s, err)):
				continue
			case <-s.stop:
				return nil
			}
		}
		fail.ok(s)
		c := &conn{rw: rw, out: make(chan model.IPCMessage, outQueue), done: make(chan struct{}), release: release}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			s.drop(c)
			return nil
		}
		s.conns[c] = struct{}{}
		s.wg.Add(2)
		s.mu.Unlock()
		go s.writeLoop(c)
		go s.readLoop(c)
	}
}

// acceptFailures is the backoff / log anti-spam state of the accept loop.
type acceptFailures struct {
	n          int // consecutive failures
	delay      time.Duration
	lastMsg    string
	lastLog    time.Time
	suppressed int // repeats not logged since the last log line
	skipped    int // repeats not logged in the whole failure series
}

// next records a failure, logs it (rate-limited) and returns the pause
// before the next attempt.
func (f *acceptFailures) next(s *Server, err error) time.Duration {
	min, max, every := s.BackoffMin, s.BackoffMax, s.LogEvery
	if min <= 0 {
		min = AcceptBackoffMin
	}
	if max <= 0 {
		max = AcceptBackoffMax
	}
	if max < min {
		max = min
	}
	if every <= 0 {
		every = AcceptLogEvery
	}
	f.n++
	if f.delay == 0 {
		f.delay = min
	} else if f.delay *= 2; f.delay > max {
		f.delay = max
	}
	now := s.now()
	msg := err.Error()
	if msg != f.lastMsg || now.Sub(f.lastLog) >= every {
		if f.suppressed > 0 {
			s.Log.Printf("ipc: accept: %v (failure #%d, %d repeat(s) not logged, retry in %s)", err, f.n, f.suppressed, f.delay)
		} else {
			s.Log.Printf("ipc: accept: %v (failure #%d, retry in %s)", err, f.n, f.delay)
		}
		f.lastMsg, f.lastLog, f.suppressed = msg, now, 0
	} else {
		f.suppressed++
		f.skipped++
	}
	return f.delay
}

// ok resets the state after a successful Accept.
func (f *acceptFailures) ok(s *Server) {
	if f.n > 0 {
		s.Log.Printf("ipc: accept works again after %d failure(s), %d of them not logged", f.n, f.skipped)
	}
	*f = acceptFailures{}
}

func (s *Server) drop(c *conn) {
	c.close()
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
	if c.release != nil {
		c.relOnce.Do(c.release)
	}
}

var (
	errIdleTimeout = errors.New("ipc: idle timeout")
	errReadTimeout = errors.New("ipc: request read timeout")
)

// readRequest reads one request under two watchdogs that close the
// connection (which unblocks the pending read on every transport): the
// idle timeout until the first byte, then the read timeout for the rest
// of the frame; the frame is limited to MaxRequest bytes (CHK-MON-11).
func (s *Server) readRequest(c *conn, req *model.IPCRequest) error {
	var fired int32
	watch := func(d time.Duration) *time.Timer {
		if d <= 0 {
			d = 1<<63 - 1
		}
		return time.AfterFunc(d, func() { atomic.StoreInt32(&fired, 1); c.close() })
	}
	wd := watch(s.IdleTimeout)
	var first [1]byte
	_, err := io.ReadFull(c.rw, first[:])
	if !wd.Stop() || atomic.LoadInt32(&fired) == 1 {
		return errIdleTimeout
	}
	if err != nil {
		return err
	}
	wd = watch(s.ReadTimeout)
	max := s.MaxRequest
	if max <= 0 {
		max = model.IPCMaxRequest
	}
	err = ReadFrame(io.MultiReader(bytes.NewReader(first[:]), c.rw), req, max)
	if !wd.Stop() || atomic.LoadInt32(&fired) == 1 {
		return errReadTimeout
	}
	return err
}

func (s *Server) writeLoop(c *conn) {
	defer s.wg.Done()
	defer s.drop(c)
	for {
		select {
		case <-c.done:
			return
		case m := <-c.out:
			if err := WriteMessage(c.rw, m); err != nil {
				return
			}
		}
	}
}

func (s *Server) readLoop(c *conn) {
	defer s.wg.Done()
	defer s.drop(c)
	c.send(model.IPCMessage{Type: model.MsgHello, Version: model.IPCVersion, Capabilities: s.Capabilities(), Heartbeat: s.heartbeat()})
	for {
		var req model.IPCRequest
		if err := s.readRequest(c, &req); err != nil {
			if errors.Is(err, errIdleTimeout) || errors.Is(err, errReadTimeout) || errors.Is(err, ErrTooLarge) {
				s.Log.Printf("ipc: client dropped: %v", err)
			} else if !errors.Is(err, io.EOF) {
				select {
				case <-c.done:
				default:
					s.Log.Printf("ipc: client read: %v", err)
				}
			}
			return
		}
		if t, ok := s.H.(Tester); ok && req.Cmd == model.CmdTestProfile {
			s.startTest(c, t, req)
			continue
		}
		if !c.send(s.handle(req)) {
			s.Log.Printf("ipc: client too slow, disconnected")
			return
		}
	}
}

// startTest runs test_profile on its own goroutine; the reply is matched
// by ID (the client may send ping meanwhile). The test ends at its
// timeout, when the client disconnects or when the server closes.
func (s *Server) startTest(c *conn, t Tester, req model.IPCRequest) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	ctx := s.ctx
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			select {
			case <-c.done:
				cancel()
			case <-ctx.Done():
			}
		}()
		res, err := t.TestProfile(ctx, req)
		var m model.IPCMessage
		if err != nil {
			m = errorReply(req.ID, err, "")
		} else {
			m = model.IPCMessage{ID: req.ID, Type: model.MsgTestResult, Test: &res}
		}
		if !c.send(m) {
			select {
			case <-c.done:
			default:
				s.Log.Printf("ipc: client too slow, disconnected")
				s.drop(c)
			}
		}
	}()
}

// handle executes one command.
func (s *Server) handle(req model.IPCRequest) model.IPCMessage {
	switch req.Cmd {
	case model.CmdPing:
		return model.IPCMessage{ID: req.ID, Type: model.MsgPong, Version: model.IPCVersion, Heartbeat: s.heartbeat()}
	case model.CmdGetStatus:
		snap := s.H.Snapshot()
		return model.IPCMessage{ID: req.ID, Type: model.MsgStatus, Status: &snap}
	case model.CmdReloadConfig:
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: result(s.H.ReloadConfig())}
	case model.CmdPollNow:
		coalesced, err := s.H.PollNow(req.ProfileID)
		if err != nil {
			return errorReply(req.ID, err, "")
		}
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: result(coalesced)}
	case model.CmdAckAlert:
		if a, ok := s.H.(Acker); ok {
			n, err := a.AckAlert(req.ProfileID, req.PointKey)
			if err != nil {
				return errorReply(req.ID, err, "")
			}
			return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: model.ResultAcknowledged, Acked: n}
		}
	}
	return errorReply(req.ID, errors.New("unknown command: "+req.Cmd), model.ErrCodeUnknownCommand)
}

func result(coalesced bool) string {
	if coalesced {
		return model.ResultCoalesced
	}
	return model.ResultAccepted
}

// broadcast queues m for every client; slow clients are disconnected.
func (s *Server) broadcast(m model.IPCMessage) {
	s.mu.Lock()
	cs := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		cs = append(cs, c)
	}
	s.mu.Unlock()
	for _, c := range cs {
		if !c.send(m) {
			s.Log.Printf("ipc: client queue full, disconnected")
			s.drop(c)
		}
	}
}

// PublishStatus pushes a snapshot to all clients (monitor OnSnapshot).
func (s *Server) PublishStatus(snap model.StatusSnapshot) {
	s.broadcast(model.IPCMessage{Type: model.MsgStatus, Status: &snap})
}

// Alert pushes an alert event to all clients (alert.Sink).
func (s *Server) Alert(e model.AlertEvent) {
	s.broadcast(model.IPCMessage{Type: model.MsgAlert, Alert: &e})
}

// Clients returns the number of connected clients.
func (s *Server) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// Close stops accepting, disconnects all clients and waits for their
// goroutines.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.initLocked()
	s.closed = true
	close(s.stop)
	s.cancel()
	l := s.l
	cs := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		cs = append(cs, c)
	}
	s.mu.Unlock()
	var err error
	if l != nil {
		err = l.Close()
	}
	for _, c := range cs {
		c.close()
	}
	s.wg.Wait()
	return err
}
