package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/monitor"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// svc runs the service over a MemListener until stop.
type svc struct {
	c      *ipc.Client
	pushes chan model.IPCMessage
	hello  model.IPCMessage
	stop   func()
}

func startSvc(t *testing.T, dir string, lag int, test monitor.TestFunc, ev *fakeEvents) *svc {
	t.Helper()
	return startSvcWith(t, dir, lag, test, ev, nil)
}

// startSvcWith is startSvc with mut applied to the appConfig.
func startSvcWith(t *testing.T, dir string, lag int, test monitor.TestFunc, ev *fakeEvents, mut func(*appConfig)) *svc {
	t.Helper()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, model.DBZone)
	newReader := func(pr model.Profile, pw string, opt sqlcheck.Options) (sqlcheck.ReadCloser, error) {
		return &stubRC{list: func() []model.ExchangePoint {
			ts := now.Add(-time.Duration(lag) * time.Minute)
			return []model.ExchangePoint{{Name: "Магазин 1", LastSend: &ts}}
		}, onClose: func() {}}, nil
	}
	l := ipc.NewMemListener()
	ready := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	ac := appConfig{Dir: dir, Listener: l, NewReader: newReader, Events: ev, Test: test,
		Now: func() time.Time { return now }, PollInterval: time.Hour, ConfigCheck: time.Hour,
		Ready: func(*monitor.Monitor, *ipc.Server) { close(ready) }}
	if mut != nil {
		mut(&ac)
	}
	go func() { done <- runService(ctx, ac) }()
	<-ready
	rw, err := l.Dial()
	if err != nil {
		t.Fatal(err)
	}
	s := &svc{pushes: make(chan model.IPCMessage, 64)}
	s.c = ipc.NewClient(rw, func(m model.IPCMessage) { s.pushes <- m })
	s.stop = func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("runService: %v", err)
		}
		s.c.Close()
	}
	return s
}

// waitPush returns the first push matching ok.
func (s *svc) waitPush(t *testing.T, ok func(model.IPCMessage) bool) model.IPCMessage {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case m := <-s.pushes:
			if ok(m) {
				return m
			}
		case <-timeout:
			t.Fatal("push not received")
		}
	}
}

// polled returns the first polled status with one alert: the current one
// (get_status) or the next pushed one (the first poll may run before the
// client connected).
func (s *svc) polled(t *testing.T) *model.StatusSnapshot {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		m, err := s.c.CallTimeout(model.CmdGetStatus, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if polledWarning(m) {
			return m.Status
		}
		select {
		case <-s.pushes:
		case <-timeout:
			t.Fatalf("no polled status: %+v", m.Status)
		}
	}
}

func polledWarning(m model.IPCMessage) bool {
	return m.Type == model.MsgStatus && len(m.Status.Profiles) == 1 && m.Status.Profiles[0].Polled && len(m.Status.Alerts) == 1
}

// TestCHK_IPC_02_ServiceAckAndTest — the service wires protocol version 2:
// Hello announces test_profile / ack_alert / heartbeat, test_profile runs
// the injected test for the saved profile, ack_alert writes
// ack-state.json and one informational Event Log entry, and after a
// service restart the same alarm is still acknowledged.
func TestCHK_IPC_02_ServiceAckAndTest(t *testing.T) {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	p := model.NewProfile("db1")
	// Windows authentication: no secrets file (DPAPI is Windows-only) and
	// test_profile of the saved profile is allowed
	p.Name, p.Server, p.Database, p.AuthMode = "Склад", "srv", "db", model.AuthWindows
	p.Mapping = model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	if err := st.SaveProfiles([]model.Profile{p}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var gotDB string
	test := func(ctx context.Context, pr model.Profile, pw string) (monitor.TestInfo, error) {
		mu.Lock()
		gotDB = pr.Database
		mu.Unlock()
		return monitor.TestInfo{Version: "10.50.1600.1", CompatLevel: 100}, nil
	}
	ev := &fakeEvents{}
	s := startSvc(t, dir, 40, test, ev)
	hello := s.waitPush(t, func(m model.IPCMessage) bool { return m.Type == model.MsgHello })
	// phase 6 adds cancel, config_read, config_write and (G2/G3,
	// CHK-IPC-08/09) discover, discover_by_value, node_points,
	// check_mapping; phase 9 (CHK-SVC-03/04/05) history, check_now,
	// events_since; CHK-IPC-10 sql_browse, sql_databases, sql_diagnose,
	// sql_grant_script
	if hello.Version != 2 || len(hello.Capabilities) != 21 || hello.Heartbeat == nil {
		t.Fatalf("hello %+v", hello)
	}
	snap := s.polled(t)
	if snap.Profiles[0].Points[0].Acknowledged || snap.PollerHung {
		t.Fatalf("status %+v", snap)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := s.c.TestProfile(ctx, "db1", nil, "")
	mu.Lock()
	db := gotDB
	mu.Unlock()
	if err != nil || m.Type != model.MsgTestResult || !m.Test.OK || m.Test.CompatLevel != 100 || db != "db" {
		t.Fatalf("test_profile %+v %v", m, err)
	}
	pong, err := s.c.Call(ctx, model.CmdPing)
	if err != nil || pong.Heartbeat == nil || pong.Heartbeat.LastCycleAt == nil || pong.Heartbeat.PollerHung {
		t.Fatalf("pong %+v %v", pong, err)
	}
	m, err = s.c.AckAlert(ctx, "db1", snap.Profiles[0].Points[0].Key)
	if err != nil || m.Type != model.MsgOK || m.Acked != 1 {
		t.Fatalf("ack %+v %v", m, err)
	}
	pushed := s.waitPush(t, func(m model.IPCMessage) bool {
		return m.Type == model.MsgStatus && len(m.Status.Alerts) == 1 && m.Status.Alerts[0].Acknowledged
	})
	if !pushed.Status.Profiles[0].Points[0].Acknowledged {
		t.Errorf("pushed snapshot %+v", pushed.Status.Profiles[0].Points[0])
	}
	if !ev.has("Тревога подтверждена") && !ev.has("Alarm acknowledged") {
		t.Errorf("event log %v", ev.got)
	}
	if m, _ := s.c.AckAlert(ctx, "nope", ""); m.Code != model.ErrCodeNotFound {
		t.Errorf("unknown profile ack %+v", m)
	}
	s.stop()
	b, err := os.ReadFile(filepath.Join(dir, monitor.AcksFile))
	if err != nil || !strings.Contains(string(b), `"db1"`) {
		t.Fatalf("ack-state.json %v %s", err, b)
	}
	// restart: the same alarm is still acknowledged
	s2 := startSvc(t, dir, 40, test, &fakeEvents{})
	defer s2.stop()
	snap = s2.polled(t)
	if !snap.Alerts[0].Acknowledged || !snap.Profiles[0].Points[0].Acknowledged {
		t.Errorf("after restart %+v", snap.Alerts)
	}
}
