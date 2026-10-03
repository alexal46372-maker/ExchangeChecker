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
	return c.do(ctx, req, false)
}

// DoCancel is Do for an asynchronous request (test_profile; protocol 2
// with capability cancel, CHK-IPC-07): when ctx ends first, the service is
// told to stop the request with cancel {ref: id} (fire and forget: the
// cancel reply and the request's canceled error are discarded as late
// replies) and ctx.Err() is returned. The connection stays open.
func (c *Client) DoCancel(ctx context.Context, req model.IPCRequest) (model.IPCMessage, error) {
	return c.do(ctx, req, true)
}

func (c *Client) do(ctx context.Context, req model.IPCRequest, cancelOnDone bool) (model.IPCMessage, error) {
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
		if cancelOnDone {
			c.mu.Lock()
			c.nextID++
			cid := c.nextID
			c.mu.Unlock()
			c.wmu.Lock()
			_ = WriteRequest(c.rw, model.IPCRequest{ID: cid, Cmd: model.CmdCancel, Ref: id})
			c.wmu.Unlock()
		}
		return model.IPCMessage{}, ctx.Err()
	}
}

// GetConfig sends get_config (admin only, capability config_read): reply
// MsgConfig (Config, Revision) or MsgError access_denied / save_failed.
func (c *Client) GetConfig(ctx context.Context) (model.IPCMessage, error) {
	return c.Do(ctx, model.IPCRequest{Cmd: model.CmdGetConfig})
}

// SaveProfile sends save_profile (admin only, capability config_write).
// password "" keeps the saved one when the server identity is unchanged
// (CanReuseSavedPassword); clearPassword deletes it; expectedRevision ""
// = last writer wins.
func (c *Client) SaveProfile(ctx context.Context, p model.Profile, password string, clearPassword bool, expectedRevision string) (model.IPCMessage, error) {
	return c.Do(ctx, model.IPCRequest{Cmd: model.CmdSaveProfile, Profile: &p, Password: password, ClearPassword: clearPassword,
		ExpectedRevision: expectedRevision})
}

// DeleteProfile sends delete_profile (admin only).
func (c *Client) DeleteProfile(ctx context.Context, profileID, expectedRevision string) (model.IPCMessage, error) {
	return c.Do(ctx, model.IPCRequest{Cmd: model.CmdDeleteProfile, ProfileID: profileID, ExpectedRevision: expectedRevision})
}

// SaveSettings sends save_settings (admin only).
func (c *Client) SaveSettings(ctx context.Context, st model.Settings, expectedRevision string) (model.IPCMessage, error) {
	return c.Do(ctx, model.IPCRequest{Cmd: model.CmdSaveSettings, Settings: &st, ExpectedRevision: expectedRevision})
}

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close closes the connection.
func (c *Client) Close() error {
	err := c.rw.Close()
	<-c.done
	return err
}

// TestProfile sends test_profile (protocol version 2, CHK-IPC-03) for the
// saved profile profileID (p nil) or the unsaved p with password. Use a
// ctx longer than the service's test timeout (15 s). The reply is
// MsgTestResult (Test) or MsgError (Code: busy, rate_limited, not_found,
// invalid_request, unknown_command on a version-1 service).
func (c *Client) TestProfile(ctx context.Context, profileID string, p *model.Profile, password string) (model.IPCMessage, error) {
	return c.Do(ctx, model.IPCRequest{Cmd: model.CmdTestProfile, ProfileID: profileID, Profile: p, Password: password})
}

// AckAlert sends ack_alert (protocol version 2, CHK-IPC-02): every active
// alert of profileID (pointKey "") or one point.
func (c *Client) AckAlert(ctx context.Context, profileID, pointKey string) (model.IPCMessage, error) {
	return c.Do(ctx, model.IPCRequest{Cmd: model.CmdAckAlert, ProfileID: profileID, PointKey: pointKey})
}
