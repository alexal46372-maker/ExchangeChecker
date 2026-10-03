package ipc

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// v2Handler adds the optional version-2 interfaces to fakeHandler.
type v2Handler struct {
	*fakeHandler
	testBlock chan struct{}
	testCtxOK chan struct{} // closed when a test saw its ctx canceled
	testReqs  chan model.IPCRequest
	hb        model.Heartbeat
}

func (h *v2Handler) TestProfile(ctx context.Context, req model.IPCRequest) (model.TestProfileResult, error) {
	h.testReqs <- req
	switch req.ProfileID {
	case "busy":
		return model.TestProfileResult{}, &model.IPCError{Code: model.ErrCodeBusy, Key: model.MsgTestBusy, Message: "busy"}
	case "limited":
		return model.TestProfileResult{}, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgMonitorRateLimited, Message: "slow down"}
	}
	if h.testBlock != nil {
		select {
		case <-h.testBlock:
		case <-ctx.Done():
			close(h.testCtxOK)
			return model.TestProfileResult{}, ctx.Err()
		}
	}
	return model.TestProfileResult{ProfileID: req.ProfileID, OK: true, Category: model.TestOK, ServerVersion: "10.50.1600.1", CompatLevel: 100}, nil
}

func (h *v2Handler) AckAlert(profileID, pointKey string) (int, error) {
	switch profileID {
	case "gone":
		return 0, &model.IPCError{Code: model.ErrCodeNotFound, Key: model.MsgMonitorUnknownProfile, Message: "monitor: unknown profile"}
	case "quiet":
		return 0, &model.IPCError{Code: model.ErrCodeNoActiveAlert, Key: model.MsgMonitorNoActiveAlert, Message: "nothing"}
	}
	if pointKey != "" {
		return 1, nil
	}
	return 2, nil
}

func (h *v2Handler) Heartbeat() model.Heartbeat { return h.hb }

func newV2(t *testing.T) (*v2Handler, *Server, *MemListener, *testLog) {
	t.Helper()
	started := time.Date(2026, 9, 29, 9, 0, 0, 0, model.DBZone)
	h := &v2Handler{fakeHandler: &fakeHandler{snap: testSnapshot()}, testReqs: make(chan model.IPCRequest, 8),
		hb: model.Heartbeat{Protocol: model.IPCVersion, Now: started.Add(time.Hour), ServiceStartedAt: started, HungAfterSec: 120, PollIntervalSec: 60}}
	lg := &testLog{}
	s := NewServer(h, lg)
	l := NewMemListener()
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })
	return h, s, l, lg
}

// v1Message is the service -> tray message as a version-1 client
// (phase 3 tray) declared it.
type v1Message struct {
	ID      uint64                `json:"id,omitempty"`
	Type    string                `json:"type"`
	Version int                   `json:"version,omitempty"`
	Error   string                `json:"error,omitempty"`
	Result  string                `json:"result,omitempty"`
	Status  *model.StatusSnapshot `json:"status,omitempty"`
	Alert   *model.AlertEvent     `json:"alert,omitempty"`
}

// TestCHK_IPC_05_ProtocolV2Compat — protocol version 2: Hello carries
// version 2, the capabilities and the heartbeat; Pong carries the
// heartbeat; unknown commands answer code unknown_command; a version-1
// client decodes every version-2 message (new fields are omitempty
// additions) and its requests work unchanged; a handler without the
// version-2 interfaces announces no capabilities and answers the new
// commands with unknown_command.
func TestCHK_IPC_05_ProtocolV2Compat(t *testing.T) {
	if model.IPCVersion != 2 {
		t.Fatalf("IPCVersion %d", model.IPCVersion)
	}
	_, _, l, _ := newV2(t)
	rw, _ := l.Dial()
	defer rw.Close()
	var raw json.RawMessage
	if err := ReadMessage(rw, &raw); err != nil {
		t.Fatal(err)
	}
	var hello model.IPCMessage
	json.Unmarshal(raw, &hello)
	// phase 6: cancel comes with the async test_profile (CHK-IPC-07)
	want := []string{model.CapTestProfile, model.CapAckAlert, model.CapHeartbeat, model.CapCancel}
	if hello.Type != model.MsgHello || hello.Version != 2 || !reflect.DeepEqual(hello.Capabilities, want) ||
		hello.Heartbeat == nil || hello.Heartbeat.HungAfterSec != 120 || hello.Heartbeat.Protocol != 2 {
		t.Fatalf("hello %s", raw)
	}
	var old v1Message
	if err := json.Unmarshal(raw, &old); err != nil || old.Type != model.MsgHello || old.Version != 2 {
		t.Errorf("v1 client cannot read hello: %v %+v", err, old)
	}
	// a version-1 request frame, byte for byte as the phase-3 tray sends it
	for i, cmd := range []string{`{"id":1,"cmd":"ping"}`, `{"id":2,"cmd":"get_status"}`, `{"id":3,"cmd":"poll_now","profileId":"db1"}`, `{"id":4,"cmd":"frobnicate"}`} {
		frame := append([]byte{byte(len(cmd)), 0, 0, 0}, cmd...)
		if _, err := rw.Write(frame); err != nil {
			t.Fatal(err)
		}
		if err := ReadMessage(rw, &raw); err != nil {
			t.Fatal(err)
		}
		var o v1Message
		var n model.IPCMessage
		if err := json.Unmarshal(raw, &o); err != nil || o.ID != uint64(i+1) {
			t.Errorf("v1 decode %s: %v", raw, err)
		}
		json.Unmarshal(raw, &n)
		switch i {
		case 0:
			if o.Type != model.MsgPong || n.Heartbeat == nil || n.Heartbeat.PollIntervalSec != 60 {
				t.Errorf("pong %s", raw)
			}
		case 1:
			if o.Status == nil || len(o.Status.Profiles) != 2 {
				t.Errorf("status %s", raw)
			}
		case 2:
			if o.Type != model.MsgOK || o.Result != model.ResultAccepted {
				t.Errorf("poll_now %s", raw)
			}
		case 3:
			if o.Type != model.MsgError || !strings.Contains(o.Error, "unknown command") || n.Code != model.ErrCodeUnknownCommand ||
				n.ErrorKey != model.MsgMonitorUnknownCommand {
				t.Errorf("unknown command %s", raw)
			}
		}
	}

	// version-1 handler: no capabilities, new commands unknown
	s1 := NewServer(&fakeHandler{snap: testSnapshot()}, nil)
	l1 := NewMemListener()
	go s1.Serve(l1)
	defer s1.Close()
	rw1, _ := l1.Dial()
	var h1 model.IPCMessage
	if err := ReadMessage(rw1, &h1); err != nil || len(h1.Capabilities) != 0 || h1.Heartbeat != nil {
		t.Errorf("v1 handler hello %+v %v", h1, err)
	}
	c1 := NewClient(rw1, nil)
	defer c1.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, req := range []model.IPCRequest{{Cmd: model.CmdTestProfile, ProfileID: "db1"}, {Cmd: model.CmdAckAlert, ProfileID: "db1"}} {
		m, err := c1.Do(ctx, req)
		if err != nil || m.Type != model.MsgError || m.Code != model.ErrCodeUnknownCommand {
			t.Errorf("%s on a v1 handler: %+v %v", req.Cmd, m, err)
		}
	}
	if m, _ := c1.Call(ctx, model.CmdPing); m.Heartbeat != nil || m.Version != 2 {
		t.Errorf("v1 handler pong %+v", m)
	}
}

// TestCHK_IPC_03_TestProfileOverPipe — test_profile runs on its own
// goroutine: the client's ping is answered while the test runs, the
// result comes with the request ID; busy / rate_limited errors carry
// their code and key; the password of the request is not logged even
// for a malformed request; closing the server cancels a running test.
func TestCHK_IPC_03_TestProfileOverPipe(t *testing.T) {
	h, s, l, lg := newV2(t)
	h.testBlock = make(chan struct{})
	h.testCtxOK = make(chan struct{})
	rw, _ := l.Dial()
	c := NewClient(rw, nil)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const pw = "Top-Secret-42"
	p := model.Profile{ID: "n", Server: "srv", Database: "db", AuthMode: model.AuthSQL, Login: "u"}
	type reply struct {
		m   model.IPCMessage
		err error
	}
	res := make(chan reply, 1)
	go func() { m, err := c.TestProfile(ctx, "", &p, pw); res <- reply{m, err} }()
	req := <-h.testReqs
	if req.Password != pw || req.Profile == nil || req.Profile.Server != "srv" {
		t.Fatalf("request at the handler %v", req)
	}
	if m, err := c.Call(ctx, model.CmdPing); err != nil || m.Type != model.MsgPong {
		t.Fatalf("ping while testing: %+v %v", m, err)
	}
	select {
	case r := <-res:
		t.Fatalf("test answered before release: %+v", r.m)
	default:
	}
	close(h.testBlock)
	r := <-res
	if r.err != nil || r.m.Type != model.MsgTestResult || r.m.Test == nil || !r.m.Test.OK || r.m.Test.CompatLevel != 100 {
		t.Fatalf("result %+v %v", r.m, r.err)
	}
	for id, code := range map[string]string{"busy": model.ErrCodeBusy, "limited": model.ErrCodeRateLimited} {
		m, err := c.TestProfile(ctx, id, nil, "")
		<-h.testReqs
		if err != nil || m.Type != model.MsgError || m.Code != code || m.ErrorKey == "" {
			t.Errorf("%s: %+v %v", id, m, err)
		}
	}
	// malformed request with the password in it: dropped, not logged
	rw2, _ := l.Dial()
	readHello(t, rw2)
	bad := `{"id":1,"cmd":"test_profile","profile":"` + pw + `","password":"` + pw + `"}`
	rw2.Write(append([]byte{byte(len(bad)), 0, 0, 0}, bad...))
	expectClosed(t, rw2, 5*time.Second)
	waitLog(t, lg, "bad message")
	lg.mu.Lock()
	for _, line := range lg.lines {
		if strings.Contains(line, pw) {
			t.Errorf("password in the ipc log: %s", line)
		}
	}
	lg.mu.Unlock()
	// a running test is canceled by Close
	h.testBlock = make(chan struct{})
	go func() { m, err := c.TestProfile(ctx, "x", nil, ""); res <- reply{m, err} }()
	<-h.testReqs
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked by a running test")
	}
	<-h.testCtxOK
	if r := <-res; r.err == nil {
		t.Errorf("test reply after close: %+v", r.m)
	}
}

// TestCHK_IPC_02_AckOverPipe — ack_alert: MsgOK "acknowledged" with the
// count; not_found / no_active_alert errors carry code and key.
func TestCHK_IPC_02_AckOverPipe(t *testing.T) {
	_, _, l, _ := newV2(t)
	rw, _ := l.Dial()
	c := NewClient(rw, nil)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := c.AckAlert(ctx, "db1", "")
	if err != nil || m.Type != model.MsgOK || m.Result != model.ResultAcknowledged || m.Acked != 2 {
		t.Errorf("ack %+v %v", m, err)
	}
	if m, _ := c.AckAlert(ctx, "db1", "_Node1/A"); m.Acked != 1 {
		t.Errorf("point ack %+v", m)
	}
	for id, code := range map[string]string{"gone": model.ErrCodeNotFound, "quiet": model.ErrCodeNoActiveAlert} {
		m, err := c.AckAlert(ctx, id, "")
		if err != nil || m.Type != model.MsgError || m.Code != code || m.ErrorKey == "" || m.Error == "" {
			t.Errorf("%s: %+v %v", id, m, err)
		}
	}
}
