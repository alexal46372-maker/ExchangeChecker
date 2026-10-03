package trayipc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

const (
	tHeartbeat = 60 * time.Second
	tPing      = 10 * time.Second
	tHello     = 5 * time.Second
	tCall      = 11 * time.Second
)

func testOptions(clk *fakeClock, dial Dialer) Options {
	return Options{Dial: dial, After: clk.After, Heartbeat: tHeartbeat, PingTimeout: tPing,
		HelloTimeout: tHello, CallTimeout: tCall}
}

// TestCHK_IPC_01_HelloCapabilities — hello push (ID 0) gives version,
// capabilities and heartbeat.serviceStartedAt; get_status follows.
func TestCHK_IPC_01_HelloCapabilities(t *testing.T) {
	started := time.Date(2026, 10, 3, 9, 0, 0, 0, model.DBZone)
	h := &fakeHandler{snap: model.StatusSnapshot{Seq: 7, Level: model.LevelWarning}, hb: model.Heartbeat{Protocol: 2, ServiceStartedAt: started}}
	_, dial := startServer(t, h)
	clk := newFakeClock()
	var evMu sync.Mutex
	var kinds []EventKind
	opt := testOptions(clk, dial)
	opt.OnEvent = func(e Event) { evMu.Lock(); kinds = append(kinds, e.Kind); evMu.Unlock() }
	s := New(opt)
	runSession(t, s)
	waitFor(t, "connected", connected(s))
	st := s.Status()
	if st.Version != model.IPCVersion {
		t.Errorf("version %d", st.Version)
	}
	for _, c := range []string{model.CapTestProfile, model.CapAckAlert, model.CapHeartbeat} {
		if !st.Has(c) {
			t.Errorf("capability %s missing: %v", c, st.Capabilities)
		}
	}
	if !st.ServiceStartedAt.Equal(started) {
		t.Errorf("serviceStartedAt %v", st.ServiceStartedAt)
	}
	if st.Snapshot.Seq != 7 || st.Snapshot.Level != model.LevelWarning {
		t.Errorf("snapshot %+v", st.Snapshot)
	}
	if !s.Supported(model.CmdAckAlert) || !s.Supported(model.CmdTestProfile) {
		t.Error("v2 commands must be supported")
	}
	if m, err := s.Do(context.Background(), model.IPCRequest{Cmd: model.CmdAckAlert, ProfileID: "p"}); err != nil || m.Result != model.ResultAcknowledged {
		t.Errorf("ack: %+v %v", m, err)
	}
	waitFor(t, "status event", func() bool {
		evMu.Lock()
		defer evMu.Unlock()
		for _, k := range kinds {
			if k == EventStatus {
				return true
			}
		}
		return false
	})
}

// TestCHK_IPC_01_PingEvery60s — exactly one ping per heartbeat period
// (fake clock); the pong heartbeat (pollerHung) updates the status.
func TestCHK_IPC_01_PingEvery60s(t *testing.T) {
	h := &fakeHandler{hb: model.Heartbeat{Protocol: 2, ServiceStartedAt: time.Unix(1, 0)}}
	_, dial := startServer(t, h)
	clk := newFakeClock()
	s := New(testOptions(clk, dial))
	runSession(t, s)
	waitFor(t, "connected", connected(s))
	waitFor(t, "heartbeat timer", func() bool { return clk.pending(tHeartbeat) })
	if n := h.calls(); n != 1 { // hello only, no ping before 60 s
		t.Fatalf("heartbeat calls before the first period: %d", n)
	}
	h.mu.Lock()
	h.hb.PollerHung = true
	h.mu.Unlock()
	for i := 1; i <= 3; i++ {
		clk.fire(t, tHeartbeat)
		waitFor(t, fmt.Sprintf("ping %d", i), func() bool { return h.calls() == 1+i })
	}
	waitFor(t, "pollerHung from pong", func() bool { hb := s.Status().Heartbeat; return hb != nil && hb.PollerHung })
	if s.Status().State != StateConnected {
		t.Error("still connected")
	}
	if model.IPCHeartbeat != 60*time.Second || New(Options{}).opt.Heartbeat != model.IPCHeartbeat {
		t.Error("default ping period must be model.IPCHeartbeat (60 s)")
	}
}

// TestCHK_IPC_01_NotRespondingOnPongTimeout — no pong within PingTimeout
// -> StateNotResponding, then reconnect after the backoff.
func TestCHK_IPC_01_NotRespondingOnPongTimeout(t *testing.T) {
	ms := &manualServer{}
	hello := model.IPCMessage{Type: model.MsgHello, Version: 2, Heartbeat: &model.Heartbeat{Protocol: 2}}
	dial := ms.dialer(hello, func(r model.IPCRequest) *model.IPCMessage {
		if r.Cmd == model.CmdGetStatus {
			return &model.IPCMessage{Type: model.MsgStatus, Status: &model.StatusSnapshot{Seq: 1}}
		}
		return nil // ping is never answered
	})
	clk := newFakeClock()
	s := New(testOptions(clk, dial))
	runSession(t, s)
	waitFor(t, "connected", connected(s))
	clk.fire(t, tHeartbeat)
	clk.fire(t, tPing)
	waitFor(t, "not responding", func() bool { return s.Status().State == StateNotResponding })
	if _, err := s.Call(context.Background(), model.CmdPollNow); !errors.Is(err, ErrNotConnected) {
		t.Errorf("command while not responding: %v", err)
	}
	clk.fire(t, DefaultBackoffMin)
	waitFor(t, "reconnected", func() bool { return s.Status().State == StateConnected })
	if s.Status().Snapshot == nil {
		t.Error("last snapshot kept")
	}
	// hello timeout also counts as not responding
	ms2 := &manualServer{}
	silent := func(context.Context) (io.ReadWriteCloser, error) {
		d := ms2.dialer(model.IPCMessage{Type: "noise"}, func(model.IPCRequest) *model.IPCMessage { return nil })
		return d(context.Background())
	}
	clk2 := newFakeClock()
	s2 := New(testOptions(clk2, silent))
	runSession(t, s2)
	clk2.fire(t, tHello)
	waitFor(t, "no hello", func() bool { return s2.Status().State == StateNotResponding })
	if len(ms2.got()) != 0 {
		t.Errorf("nothing may be sent before hello: %v", ms2.got())
	}
}

// TestCHK_IPC_01_ReconnectBackoff — 1, 2, 4 ... 30 s while the pipe is
// missing (StateNotRunning), reset to 1 s after a successful hello.
func TestCHK_IPC_01_ReconnectBackoff(t *testing.T) {
	h := &fakeHandler{hb: model.Heartbeat{Protocol: 2}}
	srv, okDial := startServer(t, h)
	var mu sync.Mutex
	up := false
	dial := func(ctx context.Context) (io.ReadWriteCloser, error) {
		mu.Lock()
		u := up
		mu.Unlock()
		if !u {
			return nil, fmt.Errorf("dial: %w", ErrServiceNotRunning)
		}
		return okDial(ctx)
	}
	clk := newFakeClock()
	s := New(testOptions(clk, dial))
	runSession(t, s)
	waitFor(t, "not running", func() bool { return s.Status().State == StateNotRunning })
	for _, d := range []time.Duration{1, 2, 4, 8, 16, 30, 30} {
		clk.fire(t, d*time.Second)
	}
	waitFor(t, "next 30 s backoff", func() bool { return clk.pending(30 * time.Second) })
	if s.Status().State != StateNotRunning || s.Status().EverConnected {
		t.Fatalf("state %v", s.Status().State)
	}
	mu.Lock()
	up = true
	mu.Unlock()
	clk.fire(t, 30*time.Second)
	waitFor(t, "connected", connected(s))
	before := clk.count(time.Second)
	srv.Close() // connection lost
	waitFor(t, "unavailable", func() bool { return s.Status().State == StateUnavailable })
	waitFor(t, "backoff reset to 1 s", func() bool { return clk.count(time.Second) == before+1 })
	if errors.Is(s.Status().Err, ErrServiceNotRunning) {
		t.Error("lost connection is not 'not running'")
	}
}

// TestCHK_IPC_01_ServiceRestartDetected — a different
// hello.heartbeat.serviceStartedAt after reconnect is a restart; the new
// snapshot is accepted although its seq is lower.
func TestCHK_IPC_01_ServiceRestartDetected(t *testing.T) {
	h1 := &fakeHandler{snap: model.StatusSnapshot{Seq: 50}, hb: model.Heartbeat{Protocol: 2, ServiceStartedAt: time.Unix(100, 0)}}
	h2 := &fakeHandler{snap: model.StatusSnapshot{Seq: 1}, hb: model.Heartbeat{Protocol: 2, ServiceStartedAt: time.Unix(200, 0)}}
	srv1, d1 := startServer(t, h1)
	_, d2 := startServer(t, h2)
	var mu sync.Mutex
	cur := d1
	dial := func(ctx context.Context) (io.ReadWriteCloser, error) { mu.Lock(); d := cur; mu.Unlock(); return d(ctx) }
	clk := newFakeClock()
	s := New(testOptions(clk, dial))
	runSession(t, s)
	waitFor(t, "connected 1", connected(s))
	if s.Status().Restarts != 0 {
		t.Fatal("no restart yet")
	}
	mu.Lock()
	cur = d2
	mu.Unlock()
	srv1.Close()
	clk.fire(t, time.Second)
	waitFor(t, "restart", func() bool {
		st := s.Status()
		return st.State == StateConnected && st.Restarts == 1 && st.Snapshot != nil && st.Snapshot.Seq == 1
	})
	if !s.Status().ServiceStartedAt.Equal(time.Unix(200, 0)) {
		t.Error("new serviceStartedAt")
	}
}

// TestCHK_IPC_01_V1ServiceHidesV2 — hello version 1: version-2 commands
// are refused locally and never reach the service; v1 commands work.
func TestCHK_IPC_01_V1ServiceHidesV2(t *testing.T) {
	ms := &manualServer{}
	dial := ms.dialer(model.IPCMessage{Type: model.MsgHello, Version: 1}, func(r model.IPCRequest) *model.IPCMessage {
		switch r.Cmd {
		case model.CmdPing:
			return &model.IPCMessage{Type: model.MsgPong, Version: 1}
		case model.CmdGetStatus:
			return &model.IPCMessage{Type: model.MsgStatus, Status: &model.StatusSnapshot{Seq: 3}}
		case model.CmdPollNow, model.CmdReloadConfig:
			return &model.IPCMessage{Type: model.MsgOK, Result: model.ResultAccepted}
		}
		return &model.IPCMessage{Type: model.MsgError, Error: "unknown command: " + r.Cmd}
	})
	clk := newFakeClock()
	s := New(testOptions(clk, dial))
	runSession(t, s)
	waitFor(t, "connected", connected(s))
	st := s.Status()
	if st.Version != 1 || len(st.Capabilities) != 0 || st.Heartbeat != nil {
		t.Fatalf("v1 hello: %+v", st)
	}
	for _, cmd := range []string{model.CmdTestProfile, model.CmdAckAlert, "save_profile"} {
		if s.Supported(cmd) {
			t.Errorf("%s must be hidden for v1", cmd)
		}
		if _, err := s.Do(context.Background(), model.IPCRequest{Cmd: cmd, ProfileID: "p", Password: "secret"}); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v", cmd, err)
		}
	}
	if m, err := s.Call(context.Background(), model.CmdPollNow); err != nil || m.Result != model.ResultAccepted {
		t.Errorf("poll_now on v1: %+v %v", m, err)
	}
	clk.fire(t, tHeartbeat)
	waitFor(t, "v1 ping", func() bool {
		for _, c := range ms.got() {
			if c == model.CmdPing {
				return true
			}
		}
		return false
	})
	for _, c := range ms.got() {
		if c == model.CmdTestProfile || c == model.CmdAckAlert || c == "save_profile" {
			t.Errorf("v2 command %s was sent to a v1 service", c)
		}
	}
	if s.Status().State != StateConnected {
		t.Error("v1 pong keeps the session")
	}
}

// TestCHK_IPC_01_UnknownCommandDisablesFeature — a v2 service answering
// unknown_command: the command is disabled (not sent again) until the
// next hello; the v1 text form is recognized too.
func TestCHK_IPC_01_UnknownCommandDisablesFeature(t *testing.T) {
	ms := &manualServer{}
	dial := ms.dialer(model.IPCMessage{Type: model.MsgHello, Version: 2, Heartbeat: &model.Heartbeat{Protocol: 2}}, func(r model.IPCRequest) *model.IPCMessage {
		if r.Cmd == model.CmdGetStatus {
			return &model.IPCMessage{Type: model.MsgStatus, Status: &model.StatusSnapshot{}}
		}
		return &model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeUnknownCommand, Error: "unknown command: " + r.Cmd}
	})
	clk := newFakeClock()
	s := New(testOptions(clk, dial))
	runSession(t, s)
	waitFor(t, "connected", connected(s))
	if !s.Supported("future_command") {
		t.Fatal("unknown future command is tried on v2")
	}
	if _, err := s.Call(context.Background(), "future_command"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("first call: %v", err)
	}
	if _, err := s.Call(context.Background(), "future_command"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("second call: %v", err)
	}
	n := 0
	for _, c := range ms.got() {
		if c == "future_command" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("save_profile sent %d times, want 1", n)
	}
	// no capability announced -> ack_alert / test_profile hidden on v2 too
	if s.Supported(model.CmdAckAlert) || s.Supported(model.CmdTestProfile) {
		t.Error("commands without capability must be hidden")
	}
	if !IsUnknownCommand(model.IPCMessage{Type: model.MsgError, Error: "unknown command: x"}) ||
		IsUnknownCommand(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeNotFound, Error: "unknown command: x"}) ||
		IsUnknownCommand(model.IPCMessage{Type: model.MsgOK}) {
		t.Error("IsUnknownCommand")
	}
}

// TestCHK_IPC_01_PushNeverBlocks — a blocked consumer does not stop the
// pipe reader: the service keeps the client while many snapshots arrive.
func TestCHK_IPC_01_PushNeverBlocks(t *testing.T) {
	h := &fakeHandler{hb: model.Heartbeat{Protocol: 2}}
	srv, dial := startServer(t, h)
	release := make(chan struct{})
	var mu sync.Mutex
	var last uint64
	clk := newFakeClock()
	opt := testOptions(clk, dial)
	opt.OnEvent = func(e Event) {
		<-release
		if e.Kind == EventStatus && e.Status.Snapshot != nil {
			mu.Lock()
			last = e.Status.Snapshot.Seq
			mu.Unlock()
		}
	}
	s := New(opt)
	runSession(t, s)
	waitFor(t, "connected", connected(s))
	for i := uint64(1); i <= 200; i++ {
		srv.PublishStatus(model.StatusSnapshot{Seq: i})
		waitFor(t, "snapshot applied", func() bool { sn := s.Status().Snapshot; return sn != nil && sn.Seq == i })
	}
	if srv.Clients() != 1 || s.Status().State != StateConnected {
		t.Fatalf("client dropped: clients %d state %v", srv.Clients(), s.Status().State)
	}
	close(release)
	waitFor(t, "events delivered", func() bool { mu.Lock(); defer mu.Unlock(); return last == 200 })
}

// recRW records whether anything was written.
type recRW struct {
	io.ReadWriteCloser
	mu     sync.Mutex
	writes int
}

func (r *recRW) Write(p []byte) (int, error) {
	r.mu.Lock()
	r.writes++
	r.mu.Unlock()
	return r.ReadWriteCloser.Write(p)
}

// TestCHK_IPC_01_VerifyBeforeFirstCommand — G7: Verify runs before any
// byte is sent; a failed check closes the pipe, StateUntrusted, nothing
// (no password, no command) is sent.
func TestCHK_IPC_01_VerifyBeforeFirstCommand(t *testing.T) {
	h := &fakeHandler{hb: model.Heartbeat{Protocol: 2}}
	_, okDial := startServer(t, h)
	var rec *recRW
	var writesAtVerify = -1
	dial := func(ctx context.Context) (io.ReadWriteCloser, error) {
		rw, err := okDial(ctx)
		if err != nil {
			return nil, err
		}
		rec = &recRW{ReadWriteCloser: rw}
		return rec, nil
	}
	clk := newFakeClock()
	opt := testOptions(clk, dial)
	opt.Verify = func(rw io.ReadWriteCloser) error {
		r := rw.(*recRW)
		r.mu.Lock()
		writesAtVerify = r.writes
		r.mu.Unlock()
		return errors.New("pipe server PID 1234 is not the service PID 99")
	}
	s := New(opt)
	runSession(t, s)
	waitFor(t, "untrusted", func() bool { return s.Status().State == StateUntrusted })
	if writesAtVerify != 0 || rec.writes != 0 {
		t.Fatalf("bytes sent around a failed verification: at verify %d, total %d", writesAtVerify, rec.writes)
	}
	if _, err := s.Do(context.Background(), model.IPCRequest{Cmd: model.CmdTestProfile, Password: "x"}); !errors.Is(err, ErrNotConnected) {
		t.Errorf("untrusted Do: %v", err)
	}
	if !clk.pending(DefaultBackoffMin) {
		waitFor(t, "retry scheduled", func() bool { return clk.pending(DefaultBackoffMin) })
	}
}

// TestCHK_IPC_01_BadFrameReconnects — an undecodable message (unknown
// alert level) ends the connection without a panic; the session retries.
func TestCHK_IPC_01_BadFrameReconnects(t *testing.T) {
	dial := func(context.Context) (io.ReadWriteCloser, error) {
		srv, cli := net.Pipe()
		go func() {
			defer srv.Close()
			_ = ipc.WriteMessage(srv, model.IPCMessage{Type: model.MsgHello, Version: 2})
			var req model.IPCRequest
			if err := ipc.ReadFrame(srv, &req, model.IPCMaxRequest); err != nil {
				return
			}
			js := []byte(`{"id":1,"type":"status","status":{"seq":1,"level":"panic","profiles":[]}}`)
			_, _ = srv.Write(append([]byte{byte(len(js)), 0, 0, 0}, js...))
			_, _ = io.Copy(io.Discard, srv)
		}()
		return cli, nil
	}
	clk := newFakeClock()
	s := New(testOptions(clk, dial))
	runSession(t, s)
	waitFor(t, "connected then dropped", func() bool {
		st := s.Status()
		return st.EverConnected && st.State != StateConnected
	})
	clk.fire(t, DefaultBackoffMin) // and it retries
}

// TestCHK_TRAY_03_SessionRefresh — Refresh re-reads get_status and
// publishes it (after ack_alert), even when the sequence did not grow.
func TestCHK_TRAY_03_SessionRefresh(t *testing.T) {
	h := &fakeHandler{snap: model.StatusSnapshot{Seq: 4}, hb: model.Heartbeat{Protocol: 2}}
	l := ipc.NewMemListener()
	srv := ipc.NewServer(h, nil)
	go srv.Serve(l)
	defer srv.Close()
	clk := newFakeClock()
	s := New(testOptions(clk, func(context.Context) (io.ReadWriteCloser, error) { return l.Dial() }))
	if err := s.Refresh(context.Background()); err != ErrNotConnected {
		t.Fatalf("refresh before connect: %v", err)
	}
	runSession(t, s)
	waitFor(t, "connected", connected(s))
	h.mu.Lock()
	h.snap = model.StatusSnapshot{Seq: 4, Alerts: []model.AlertEvent{{ProfileID: "a", To: model.LevelError, Acknowledged: true}}}
	h.mu.Unlock()
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Snapshot == nil || len(st.Snapshot.Alerts) != 1 || !st.Snapshot.Alerts[0].Acknowledged {
		t.Fatalf("refreshed snapshot %+v", st.Snapshot)
	}
}

// TestCHK_TRAY_06_ConfigCapabilitiesAndElevation — phase 6 commands need
// their capabilities (config_read / config_write / cancel);
// clientElevated of get_status / ping replies is kept per connection.
func TestCHK_TRAY_06_ConfigCapabilitiesAndElevation(t *testing.T) {
	yes := true
	ms := &manualServer{}
	dial := ms.dialer(model.IPCMessage{Type: model.MsgHello, Version: 2, Capabilities: []string{model.CapTestProfile, model.CapConfigRead},
		Heartbeat: &model.Heartbeat{Protocol: 2}}, func(r model.IPCRequest) *model.IPCMessage {
		if r.Cmd == model.CmdGetStatus {
			return &model.IPCMessage{Type: model.MsgStatus, Status: &model.StatusSnapshot{}, ClientElevated: &yes}
		}
		return &model.IPCMessage{Type: model.MsgOK}
	})
	clk := newFakeClock()
	s := New(testOptions(clk, dial))
	runSession(t, s)
	waitFor(t, "connected", connected(s))
	if !s.Supported(model.CmdGetConfig) || s.Supported(model.CmdSaveProfile) || s.Supported(model.CmdCancel) {
		t.Errorf("capabilities: get_config %v save_profile %v cancel %v",
			s.Supported(model.CmdGetConfig), s.Supported(model.CmdSaveProfile), s.Supported(model.CmdCancel))
	}
	if _, err := s.Do(context.Background(), model.IPCRequest{Cmd: model.CmdSaveProfile}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("save_profile without config_write: %v", err)
	}
	if !s.Status().Elevated() {
		t.Error("clientElevated from get_status")
	}
}
