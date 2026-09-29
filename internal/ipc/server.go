package ipc

import (
	"errors"
	"io"
	"sync"

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

// Handler is the service side (the monitor).
type Handler interface {
	Snapshot() model.StatusSnapshot
	PollNow()
	ReloadConfig()
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

// Server serves the protocol to any number of clients.
type Server struct {
	H   Handler
	Log Logger

	mu     sync.Mutex
	l      Listener
	conns  map[*conn]struct{}
	closed bool
	wg     sync.WaitGroup
}

type conn struct {
	rw   io.ReadWriteCloser
	out  chan model.IPCMessage
	once sync.Once
	done chan struct{}
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
	return &Server{H: h, Log: log, conns: map[*conn]struct{}{}}
}

// Serve accepts clients until Close; it returns nil after Close.
func (s *Server) Serve(l Listener) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = l.Close()
		return nil
	}
	s.l = l
	s.mu.Unlock()
	for {
		rw, err := l.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || errors.Is(err, ErrListenerClosed) {
				return nil
			}
			return err
		}
		c := &conn{rw: rw, out: make(chan model.IPCMessage, outQueue), done: make(chan struct{})}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			c.close()
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
		if err := ReadMessage(c.rw, &req); err != nil {
			if !errors.Is(err, io.EOF) {
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
		s.H.ReloadConfig()
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK}
	case model.CmdPollNow:
		s.H.PollNow()
		return model.IPCMessage{ID: req.ID, Type: model.MsgOK}
	}
	return model.IPCMessage{ID: req.ID, Type: model.MsgError, Error: "unknown command: " + req.Cmd}
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
