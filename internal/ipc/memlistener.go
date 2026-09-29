package ipc

import (
	"io"
	"net"
	"sync"
)

// MemListener is an in-process Listener over net.Pipe (tests, and the
// non-Windows console build of the service). No network is used.
type MemListener struct {
	ch   chan net.Conn
	once sync.Once
	done chan struct{}
}

// NewMemListener returns an open MemListener.
func NewMemListener() *MemListener {
	return &MemListener{ch: make(chan net.Conn), done: make(chan struct{})}
}

// Accept implements Listener.
func (l *MemListener) Accept() (io.ReadWriteCloser, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, ErrListenerClosed
	}
}

// Dial connects a new client.
func (l *MemListener) Dial() (io.ReadWriteCloser, error) {
	srv, cli := net.Pipe()
	select {
	case l.ch <- srv:
		return cli, nil
	case <-l.done:
		srv.Close()
		cli.Close()
		return nil, ErrListenerClosed
	}
}

// Close implements Listener.
func (l *MemListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}
