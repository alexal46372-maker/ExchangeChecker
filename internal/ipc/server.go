package ipc

import (
	"bytes"
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

	mu     sync.Mutex
	l      Listener
	conns  map[*conn]struct{}
	closed bool
	stop   chan struct{}
	slots  chan struct{}
	wg     sync.WaitGroup
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
		ReadTimeout: model.IPCReadTimeout, IdleTimeout: model.IPCIdleTimeout}
}

// acceptBackoff is the pause after a failed Accept (e.g. a transient
// CreateNamedPipe failure) before the next attempt.
const acceptBackoff = 200 * time.Millisecond

// Serve accepts clients until Close; it returns nil after Close.
func (s *Server) Serve(l Listener) error {
	s.mu.Lock()
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
			s.Log.Printf("ipc: accept: %v", err)
			select {
			case <-time.After(acceptBackoff):
				continue
			case <-s.stop:
				return nil
			}
		}
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
	c.send(model.IPCMessage{Type: model.MsgHello, Version: model.IPCVersion})
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
		if !c.send(s.handle(req)) {
			s.Log.Printf("ipc: client too slow, disconnected")
			return
		}
	}
}

// handle executes one command.
func (s *Server) handle(req model.IPCRequest) model.IPCMessage {
	switch req.Cmd {
	case model.CmdPing:
		return model.IPCMessage{ID: req.ID, Type: model.MsgPong, Version: model.IPCVersion}
	case model.CmdGetStatus:
		snap := s.H.Snapshot()
		return model.IPCMessage{ID: req.ID, Type: model.MsgStatus, Status: &snap}
	case model.CmdReloadConfig:
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: result(s.H.ReloadConfig())}
	case model.CmdPollNow:
		coalesced, err := s.H.PollNow(req.ProfileID)
		if err != nil {
			return model.IPCMessage{ID: req.ID, Type: model.MsgError, Error: err.Error()}
		}
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK, Result: result(coalesced)}
	}
	return model.IPCMessage{ID: req.ID, Type: model.MsgError, Error: "unknown command: " + req.Cmd}
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
	s.closed = true
	close(s.stop)
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
