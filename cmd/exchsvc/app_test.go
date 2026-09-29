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

type fakeEvents struct {
	mu  sync.Mutex
	got []string
}

func (f *fakeEvents) Report(typ uint16, id uint32, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, msg)
	return nil
}

func (f *fakeEvents) has(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.got {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

type plainProtector struct{}

func (plainProtector) Protect(b []byte) ([]byte, error)   { return b, nil }
func (plainProtector) Unprotect(b []byte) ([]byte, error) { return b, nil }

// TestCHK_MON_07_ServiceRunStop — the service function starts the monitor
// and the IPC server, answers the tray (status, poll now, reload), pushes
// alerts to the tray and the event log, and stops cleanly on cancel (SCM
// Stop / Shutdown) closing the readers and the clients.
func TestCHK_MON_07_ServiceRunStop(t *testing.T) {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	p := model.NewProfile("db1")
	p.Name, p.Server, p.Database, p.AuthMode = "Склад", "srv", "db", model.AuthWindows
	p.Mapping = model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	if err := st.SaveProfiles([]model.Profile{p}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, model.DBZone)
	var mu sync.Mutex
	lag := 10
	closed := 0
	newReader := func(pr model.Profile, pw string, opt sqlcheck.Options) (sqlcheck.ReadCloser, error) {
		return &stubRC{list: func() []model.ExchangePoint {
			mu.Lock()
			defer mu.Unlock()
			t := now.Add(-time.Duration(lag) * time.Minute)
			return []model.ExchangePoint{{Name: "Магазин 1", LastSend: &t}}
		}, onClose: func() { mu.Lock(); closed++; mu.Unlock() }}, nil
	}
	l := ipc.NewMemListener()
	ev := &fakeEvents{}
	ready := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runService(ctx, appConfig{Dir: dir, Listener: l, NewReader: newReader, Events: ev,
			Now: func() time.Time { return now }, PollInterval: time.Hour, ConfigCheck: 20 * time.Millisecond,
			Ready: func(*monitor.Monitor, *ipc.Server) { close(ready) }})
	}()
	<-ready
	rw, err := l.Dial()
	if err != nil {
		t.Fatal(err)
	}
	pushes := make(chan model.IPCMessage, 64)
	c := ipc.NewClient(rw, func(m model.IPCMessage) { pushes <- m })
	var snap *model.StatusSnapshot
	deadline := time.Now().Add(5 * time.Second)
	for {
		m, err := c.Call(model.CmdGetStatus)
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Status.Profiles) == 1 && m.Status.Profiles[0].Polled {
			snap = m.Status
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no polled status: %+v", m.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	ps := snap.Profiles[0]
	if ps.Name != "Склад" || ps.Level != model.LevelOK || len(ps.Points) != 1 || ps.Points[0].LagMinutes != 10 {
		t.Fatalf("status %+v", ps)
	}
	if len(ps.Warnings) != 0 {
		t.Errorf("windows auth must have no warnings: %v", ps.Warnings)
	}
	// the exchange stalls: poll now -> warning alert pushed + event log
	mu.Lock()
	lag = 40
	mu.Unlock()
	if m, err := c.Call(model.CmdPollNow); err != nil || m.Type != model.MsgOK {
		t.Fatalf("poll now %+v %v", m, err)
	}
	var alertMsg *model.AlertEvent
	timeout := time.After(5 * time.Second)
	for alertMsg == nil {
		select {
		case m := <-pushes:
			if m.Type == model.MsgAlert {
				alertMsg = m.Alert
			}
		case <-timeout:
			t.Fatal("no alert pushed")
		}
	}
	if alertMsg.To != model.LevelWarning || alertMsg.PointName != "Магазин 1" || alertMsg.LagMinutes != 40 {
		t.Errorf("alert %+v", alertMsg)
	}
	if !ev.has("Магазин 1") || !ev.has("started") {
		t.Errorf("event log %v", ev.got)
	}
	// the tray saves a new profile and asks for a reload
	p2 := p
	p2.ID, p2.Name = "db2", "Магазин-2"
	if err := st.SaveProfiles([]model.Profile{p, p2}); err != nil {
		t.Fatal(err)
	}
	if m, err := c.Call(model.CmdReloadConfig); err != nil || m.Type != model.MsgOK {
		t.Fatalf("reload %+v %v", m, err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		m, _ := c.Call(model.CmdGetStatus)
		if m.Status != nil && len(m.Status.Profiles) == 2 && m.Status.Profiles[1].Polled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reload not applied: %+v", m.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// SCM Stop
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runService: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("service did not stop")
	}
	if time.Since(start) > 3*time.Second {
		t.Error("stop too slow")
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("tray client not disconnected on stop")
	}
	mu.Lock()
	if closed != 2 {
		t.Errorf("readers closed: %d", closed)
	}
	mu.Unlock()
	if !ev.has("stopped") {
		t.Errorf("stop not in event log: %v", ev.got)
	}
	b, err := os.ReadFile(filepath.Join(dir, LogFile))
	if err != nil || !strings.Contains(string(b), "starting") || !strings.Contains(string(b), "alert #1") {
		t.Errorf("log file: %v %q", err, b)
	}
}

type stubRC struct {
	sqlcheck.StubReader
	list    func() []model.ExchangePoint
	onClose func()
}

func (s *stubRC) ListPoints(context.Context) ([]model.ExchangePoint, error) { return s.list(), nil }
func (s *stubRC) Close() error                                              { s.onClose(); return nil }
