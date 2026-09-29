package ipc

import (
	"errors"
	"io"
	"sync"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Client is a minimal protocol client over any connection (the tray can
// use it with DialPipe; tests use it with in-memory pipes). Replies are
// matched by request ID; pushes (ID 0) go to the Push callback.
type Client struct {
	rw   io.ReadWriteCloser
	Push func(model.IPCMessage)

	wmu     sync.Mutex
	mu      sync.Mutex
	nextID  uint64
	waiting map[uint64]chan model.IPCMessage
	err     error
	done    chan struct{}
}

// ErrClientClosed is returned by Call after the connection ended.
var ErrClientClosed = errors.New("ipc: connection closed")

// NewClient starts reading from rw. push may be nil.
func NewClient(rw io.ReadWriteCloser, push func(model.IPCMessage)) *Client {
	c := &Client{rw: rw, Push: push, waiting: map[uint64]chan model.IPCMessage{}, done: make(chan struct{})}
	go c.readLoop()
	return c
}

func (c *Client) readLoop() {
	defer close(c.done)
	for {
		var m model.IPCMessage
		if err := ReadMessage(c.rw, &m); err != nil {
			c.mu.Lock()
			c.err = err
			for id, ch := range c.waiting {
				close(ch)
				delete(c.waiting, id)
			}
			c.mu.Unlock()
			return
		}
		if m.ID != 0 {
			c.mu.Lock()
			ch := c.waiting[m.ID]
			delete(c.waiting, m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
				continue
			}
		}
		if c.Push != nil {
			c.Push(m)
		}
	}
}

// Call sends cmd and waits for its reply.
func (c *Client) Call(cmd string) (model.IPCMessage, error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return model.IPCMessage{}, ErrClientClosed
	}
	c.nextID++
	id := c.nextID
	ch := make(chan model.IPCMessage, 1)
	c.waiting[id] = ch
	c.mu.Unlock()
	c.wmu.Lock()
	err := WriteMessage(c.rw, model.IPCRequest{ID: id, Cmd: cmd})
	c.wmu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
		return model.IPCMessage{}, err
	}
	m, ok := <-ch
	if !ok {
		return model.IPCMessage{}, ErrClientClosed
	}
	return m, nil
}

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close closes the connection.
func (c *Client) Close() error {
	err := c.rw.Close()
	<-c.done
	return err
}
