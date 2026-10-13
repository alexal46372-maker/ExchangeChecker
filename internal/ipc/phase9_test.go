package ipc

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type p9Handler struct {
	fakeHandler
	checks int32
}

func (h *p9Handler) CheckNow(id string) (model.CheckResult, error) {
	if id == "limited" {
		return model.CheckResult{}, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgCheckRateLimited,
			Message: "limited", RetryAfterMs: 1234}
	}
	atomic.AddInt32(&h.checks, 1)
	return model.CheckResult{Coalesced: id == "busy", CheckSeq: 42}, nil
}

func (h *p9Handler) EventsSince(run string, since uint64, limit int) model.EventsPage {
	if run != "r1" {
		return model.EventsPage{RunID: "r1", LastID: 400, ResetRequired: true}
	}
	var evs []model.AlertEvent
	for i := since + 1; i <= 400; i++ {
		evs = append(evs, model.AlertEvent{ID: i, RunID: "r1", ProfileID: "db1", ProfileName: strings.Repeat("я", 150)})
	}
	return model.EventsPage{RunID: "r1", LastID: 400, Events: evs}
}

type p9History struct {
	block   chan struct{}
	started chan struct{}
}

func (s *p9History) History(ctx context.Context, req model.IPCRequest) (model.HistoryPage, error) {
	if req.ProfileID == "slow" {
		s.started <- struct{}{}
		select {
		case <-s.block:
		case <-ctx.Done():
			return model.HistoryPage{}, ctx.Err()
		}
	}
	if req.ProfileID == "bad" {
		return model.HistoryPage{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgHistoryBadRange, Message: "bad"}
	}
	return model.HistoryPage{Records: []model.HistoryRecord{{Kind: model.HistoryKindProfile, ProfileID: req.ProfileID}}}, nil
}

func p9Server(t *testing.T) (*MemListener, *p9Handler, *p9History, func() (*Client, model.IPCMessage)) {
	t.Helper()
	h := &p9Handler{fakeHandler: fakeHandler{snap: testSnapshot()}}
	hs := &p9History{block: make(chan struct{}), started: make(chan struct{}, 8)}
	srv := NewServer(h, nil)
	srv.History = hs
	l := NewMemListener()
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	dial := func() (*Client, model.IPCMessage) {
		rw, err := l.Dial()
		if err != nil {
			t.Fatal(err)
		}
		pushes := make(chan model.IPCMessage, 64)
		c := NewClient(rw, func(m model.IPCMessage) { pushes <- m })
		t.Cleanup(func() { c.Close() })
		select {
		case m := <-pushes:
			return c, m
		case <-time.After(5 * time.Second):
			t.Fatal("no hello")
		}
		return nil, model.IPCMessage{}
	}
	return l, h, hs, dial
}

func do(t *testing.T, c *Client, req model.IPCRequest) model.IPCMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := c.Do(ctx, req)
	if err != nil {
		t.Fatalf("%s: %v", req.Cmd, err)
	}
	return m
}

// TestCHK_SVC_04_CheckNowIPC — capabilities, accepted / coalesced with
// checkSeq, the 2 s per-connection limit, rate_limited + retryAfterMs.
func TestCHK_SVC_04_CheckNowIPC(t *testing.T) {
	_, h, _, dial := p9Server(t)
	c, hello := dial()
	caps := strings.Join(hello.Capabilities, ",")
	for _, k := range []string{model.CapHistory, model.CapCheckNow, model.CapEventsSince, model.CapCancel} {
		if !strings.Contains(caps, k) {
			t.Errorf("capability %s missing: %s", k, caps)
		}
	}
	m := do(t, c, model.IPCRequest{Cmd: model.CmdCheckNow})
	if m.Type != model.MsgOK || m.Result != model.ResultAccepted || m.CheckSeq == nil || *m.CheckSeq != 42 {
		t.Fatalf("accepted: %+v", m)
	}
	m = do(t, c, model.IPCRequest{Cmd: model.CmdCheckNow, ProfileID: "db1"})
	if m.Type != model.MsgError || m.Code != model.ErrCodeRateLimited || m.ErrorKey != model.MsgCheckRateLimited ||
		m.RetryAfterMs <= 0 || m.RetryAfterMs > 2000 {
		t.Fatalf("connection limit: %+v", m)
	}
	if atomic.LoadInt32(&h.checks) != 1 {
		t.Fatal("refused request reached the handler")
	}
	c2, _ := dial()
	m = do(t, c2, model.IPCRequest{Cmd: model.CmdCheckNow, ProfileID: "busy"})
	if m.Result != model.ResultCheckCoalesced || m.CheckSeq == nil {
		t.Fatalf("coalesced: %+v", m)
	}
	c3, _ := dial()
	m = do(t, c3, model.IPCRequest{Cmd: model.CmdCheckNow, ProfileID: "limited"})
	if m.Code != model.ErrCodeRateLimited || m.RetryAfterMs != 1234 || m.ErrorKey != model.MsgCheckRateLimited {
		t.Fatalf("handler limit: %+v", m)
	}
	// poll_now is unchanged
	if m := do(t, c3, model.IPCRequest{Cmd: model.CmdPollNow}); m.Type != model.MsgOK || m.CheckSeq != nil {
		t.Fatalf("poll_now: %+v", m)
	}
}

// TestCHK_SVC_05_EventsSinceIPC — reply within 64 KiB (truncated size,
// lastId = last event sent), reset for another run.
func TestCHK_SVC_05_EventsSinceIPC(t *testing.T) {
	_, _, _, dial := p9Server(t)
	c, _ := dial()
	m := do(t, c, model.IPCRequest{Cmd: model.CmdEventsSince, RunID: "r1", SinceID: 0})
	b, _ := json.Marshal(m)
	if m.Type != model.MsgEvents || !m.Truncated || m.TruncatedReason != model.TruncatedSize || len(b) > model.IPCMaxInteractiveReply ||
		len(m.Events) == 0 || m.LastID == nil || *m.LastID != m.Events[len(m.Events)-1].ID || m.RunID != "r1" {
		t.Fatalf("events: %d bytes, %d events, %+v", len(b), len(m.Events), m.LastID)
	}
	m = do(t, c, model.IPCRequest{Cmd: model.CmdEventsSince, RunID: "old", SinceID: 9})
	if !m.ResetRequired || len(m.Events) != 0 || m.LastID == nil || *m.LastID != 400 {
		t.Fatalf("reset: %+v", m)
	}
}

// TestCHK_SVC_03_HistoryIPC — async history, busy limits (1 per
// connection, 2 per service), error passthrough, cancel.
func TestCHK_SVC_03_HistoryIPC(t *testing.T) {
	_, _, hs, dial := p9Server(t)
	c1, _ := dial()
	m := do(t, c1, model.IPCRequest{Cmd: model.CmdHistory, ProfileID: "db1"})
	if m.Type != model.MsgHistory || m.History == nil || len(m.History.Records) != 1 {
		t.Fatalf("history: %+v", m)
	}
	if m := do(t, c1, model.IPCRequest{Cmd: model.CmdHistory, ProfileID: "bad"}); m.Code != model.ErrCodeInvalidRequest || m.ErrorKey != model.MsgHistoryBadRange {
		t.Fatalf("error: %+v", m)
	}
	c2, _ := dial()
	c3, _ := dial()
	res := make(chan model.IPCMessage, 2)
	for _, c := range []*Client{c1, c2} {
		go func(c *Client) {
			m, _ := c.Do(context.Background(), model.IPCRequest{Cmd: model.CmdHistory, ProfileID: "slow"})
			res <- m
		}(c)
		<-hs.started
	}
	if m := do(t, c1, model.IPCRequest{Cmd: model.CmdHistory, ProfileID: "db1"}); m.Code != model.ErrCodeBusy || m.ErrorKey != model.MsgHistoryBusy {
		t.Fatalf("per connection: %+v", m)
	}
	if m := do(t, c3, model.IPCRequest{Cmd: model.CmdHistory, ProfileID: "db1"}); m.Code != model.ErrCodeBusy {
		t.Fatalf("per service: %+v", m)
	}
	close(hs.block)
	for i := 0; i < 2; i++ {
		if m := <-res; m.Type != model.MsgHistory {
			t.Fatalf("slow: %+v", m)
		}
	}
	if m := do(t, c3, model.IPCRequest{Cmd: model.CmdHistory, ProfileID: "db1"}); m.Type != model.MsgHistory {
		t.Fatalf("after release: %+v", m)
	}
	// cancel: a canceled request frees its slot
	hs.block = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c3.DoCancel(ctx, model.IPCRequest{Cmd: model.CmdHistory, ProfileID: "slow"})
		done <- err
	}()
	<-hs.started
	cancel()
	<-done
	deadline := time.Now().Add(5 * time.Second)
	for {
		m := do(t, c3, model.IPCRequest{Cmd: model.CmdHistory, ProfileID: "db1"})
		if m.Type == model.MsgHistory {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot not released after cancel: %+v", m)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
