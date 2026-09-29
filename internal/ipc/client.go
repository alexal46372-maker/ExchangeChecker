package ipc

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

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

// Call sends cmd and waits for its reply until ctx is done (use
// context.WithTimeout; CHK-MON-11). A late reply is discarded.
func (c *Client) Call(ctx context.Context, cmd string) (model.IPCMessage, error) {
	return c.Do(ctx, model.IPCRequest{Cmd: cmd})
}

// CallTimeout is Call with a timeout.
func (c *Client) CallTimeout(cmd string, timeout time.Duration) (model.IPCMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.Call(ctx, cmd)
}

// Do sends req (its ID is assigned here) and waits for the reply until ctx
// is done. Requests above model.IPCMaxRequest are refused (ErrTooLarge).
func (c *Client) Do(ctx context.Context, req model.IPCRequest) (model.IPCMessage, error) {
	if err := ctx.Err(); err != nil {
		return model.IPCMessage{}, err
	}
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
	req.ID = id
	forget := func() {
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
	}
	c.wmu.Lock()
	err := WriteRequest(c.rw, req)
	c.wmu.Unlock()
	if err != nil {
		forget()
		return model.IPCMessage{}, err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return model.IPCMessage{}, ErrClientClosed
		}
		return m, nil
	case <-ctx.Done():
		forget()
		return model.IPCMessage{}, ctx.Err()
	}
}

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close closes the connection.
func (c *Client) Close() error {
	err := c.rw.Close()
	<-c.done
	return err
}
