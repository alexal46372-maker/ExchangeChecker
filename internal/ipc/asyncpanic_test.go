package ipc

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

const (
	panicPW  = "Pa$$-secret-77"
	panicSQL = "SELECT secret_col FROM dbo.secret_tbl"
)

// panicHandler: test_profile and the Discoverer commands take a slot of a
// pool of 2 (busy when none is free, released by defer) and panic while
// panicking is set; gate (when set) delays the panic.
type panicHandler struct {
	*discHandler
	panicking int32
	slots     chan struct{}
	gate      chan struct{}
}

func (h *panicHandler) enter() error {
	select {
	case h.slots <- struct{}{}:
		return nil
	default:
		return &model.IPCError{Code: model.ErrCodeBusy, Key: model.MsgDiscoverBusy, Message: "busy"}
	}
}

func (h *panicHandler) leave() { <-h.slots }

func (h *panicHandler) maybePanic(req model.IPCRequest) {
	if h.gate != nil {
		<-h.gate
	}
	if atomic.LoadInt32(&h.panicking) == 1 {
		panic("boom in " + req.Cmd + " pw=" + req.Password + " sql=" + panicSQL)
	}
}

func (h *panicHandler) TestProfile(ctx context.Context, req model.IPCRequest) (model.TestProfileResult, error) {
	if err := h.enter(); err != nil {
		return model.TestProfileResult{}, err
	}
	defer h.leave()
	h.maybePanic(req)
	return model.TestProfileResult{ProfileID: req.ProfileID, OK: true, Category: model.TestOK}, nil
}

func (h *panicHandler) Discover(ctx context.Context, req model.IPCRequest) ([]model.DiscoveryCandidate, string, error) {
	if err := h.enter(); err != nil {
		return nil, "", err
	}
	defer h.leave()
	h.maybePanic(req)
	return []model.DiscoveryCandidate{{Table: "_Node1"}}, "", nil
}

func (h *panicHandler) CheckMapping(ctx context.Context, req model.IPCRequest) (model.MappingCheck, error) {
	if err := h.enter(); err != nil {
		return model.MappingCheck{}, err
	}
	defer h.leave()
	h.maybePanic(req)
	return model.MappingCheck{OK: true}, nil
}

func newPanicServer(t *testing.T) (*panicHandler, *Server, *MemListener, *testLog) {
	t.Helper()
	v2, _, _, _ := newV2(t)
	h := &panicHandler{discHandler: &discHandler{v2Handler: v2}, slots: make(chan struct{}, 2)}
	lg := &testLog{}
	s := NewServer(h, lg)
	s.Authorize = func(io.ReadWriteCloser) (Peer, error) { return Peer{Admin: true, Account: `HOST\alex`}, nil }
	l := NewMemListener()
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })
	return h, s, l, lg
}

// TestCHK_IPC_08_AsyncHandlerPanic — a panic of the handler of an
// asynchronous request (test_profile and the discovery commands) does not
// bring the server down: the client gets error {code: internal,
// errorKey: monitor.internal} without the panic text or the stack, the
// service log has the stack (password and manual SQL scrubbed), the
// request's record for cancel {ref} is gone (the same id is accepted
// again) and the handler's slot is free: 4 panics in a row per command
// over a pool of 2, then the command succeeds (not busy) and ping works.
// A client that disconnected before the panic does not block the server.
func TestCHK_IPC_08_AsyncHandlerPanic(t *testing.T) {
	h, s, l, lg := newPanicServer(t)
	atomic.StoreInt32(&h.panicking, 1)
	mp := model.Mapping{Table: "_Node1", ManualSQL: panicSQL}
	p := model.Profile{ID: "p1", Server: "srv", Database: "db", Mapping: model.Mapping{ManualSQL: panicSQL}}
	reqs := []model.IPCRequest{
		{ID: 10, Cmd: model.CmdTestProfile, Profile: &p, Password: panicPW},
		{ID: 20, Cmd: model.CmdDiscover, Profile: &p, Password: panicPW},
		{ID: 30, Cmd: model.CmdCheckMapping, ProfileID: "p1", Mapping: &mp, Limit: 5},
	}
	c := dialRaw(t, l)
	for _, r := range reqs {
		for i := 0; i < 4; i++ { // same id: the record of the panicked request is gone
			m := c.call(r)
			if m.Type != model.MsgError || m.Code != model.ErrCodeInternal || m.ErrorKey != model.MsgMonitorInternal {
				t.Fatalf("%s #%d: %+v", r.Cmd, i, m)
			}
			for _, leak := range []string{"boom", "goroutine", "panic", ".go:", panicPW, "secret"} {
				if strings.Contains(m.Error, leak) {
					t.Fatalf("%s: reply leaks %q: %q", r.Cmd, leak, m.Error)
				}
			}
		}
		if m := c.call(model.IPCRequest{ID: r.ID + 5, Cmd: model.CmdPing}); m.Type != model.MsgPong {
			t.Fatalf("ping after %s panics: %+v", r.Cmd, m)
		}
	}
	if len(h.slots) != 0 {
		t.Fatalf("slots held: %d", len(h.slots))
	}
	if model.MonitorMessage(model.MsgMonitorInternal, "ru") == "" || model.MonitorMessage(model.MsgMonitorInternal, "en") == "" {
		t.Fatal("monitor.internal has no text")
	}
	// the log: stack and panic text, no password / manual SQL
	if !lg.has("internal error (recovered panic): boom in "+model.CmdTestProfile) || !lg.has("boom in "+model.CmdDiscover) ||
		!lg.has("boom in "+model.CmdCheckMapping) || !lg.has("goroutine ") || !lg.has("runtime/debug.Stack") {
		t.Fatalf("log: %q", lg.lines)
	}
	if lg.has(panicPW) || lg.has(panicSQL) || lg.has("secret_") {
		t.Fatalf("log leaks the password / SQL: %q", lg.lines)
	}
	// after the panics: the commands succeed (not busy)
	atomic.StoreInt32(&h.panicking, 0)
	if m := c.call(reqs[0]); m.Type != model.MsgTestResult || m.Test == nil || !m.Test.OK {
		t.Fatalf("test_profile after panics: %+v", m)
	}
	if m := c.call(reqs[1]); m.Type != model.MsgDiscovery || len(m.Candidates) != 1 {
		t.Fatalf("discover after panics: %+v", m)
	}
	if m := c.call(reqs[2]); m.Type != model.MsgMappingCheck || m.MappingCheck == nil || !m.MappingCheck.OK {
		t.Fatalf("check_mapping after panics: %+v", m)
	}

	// a client gone before the panic: the reply does not block, the
	// server keeps serving and closes promptly
	atomic.StoreInt32(&h.panicking, 1)
	h.gate = make(chan struct{})
	rw, err := l.Dial()
	if err != nil {
		t.Fatal(err)
	}
	readHello(t, rw)
	if err := WriteRequest(rw, model.IPCRequest{ID: 1, Cmd: model.CmdDiscover, ProfileID: "p1"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(h.slots) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	rw.Close()
	time.Sleep(20 * time.Millisecond)
	close(h.gate)
	for len(h.slots) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(h.slots) != 0 {
		t.Fatal("slot not released after the panic of a gone client")
	}
	atomic.StoreInt32(&h.panicking, 0)
	c2 := dialRaw(t, l)
	if m := c2.call(model.IPCRequest{ID: 2, Cmd: model.CmdPing}); m.Type != model.MsgPong {
		t.Fatalf("ping: %+v", m)
	}
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server Close blocked after a panic")
	}
}
