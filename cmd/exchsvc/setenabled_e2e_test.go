package main

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/monitor"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// gatedDB counts reads and can hold a read in flight.
type gatedDB struct {
	calls   int32
	mu      sync.Mutex
	hold    bool
	started chan struct{}
	release chan struct{}
}

func (g *gatedDB) reader(now time.Time) func(model.Profile, string, sqlcheck.Options) (sqlcheck.ReadCloser, error) {
	return func(model.Profile, string, sqlcheck.Options) (sqlcheck.ReadCloser, error) {
		return &stubRC{list: func() []model.ExchangePoint {
			atomic.AddInt32(&g.calls, 1)
			g.mu.Lock()
			hold := g.hold
			g.mu.Unlock()
			if hold {
				g.started <- struct{}{}
				<-g.release
			}
			ts := now.Add(-600 * time.Minute) // far above the error threshold
			return []model.ExchangePoint{{Name: "Магазин 1", LastSend: &ts}}
		}, onClose: func() {}}, nil
	}
}

func (g *gatedDB) n() int32 { return atomic.LoadInt32(&g.calls) }

// TestCHK_MON_21_SetEnabledE2E — set_enabled through the real service:
// disable while a poll is in flight → its result is discarded (no alert),
// no further reads even after check_now; the second client gets the
// profile_enabled push; enable → polling resumes at once with an alert;
// the flag persists across a restart; unknown profile → not_found;
// stale expectedRevision → conflict.
func TestCHK_MON_21_SetEnabledE2E(t *testing.T) {
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
	g := &gatedDB{hold: true, started: make(chan struct{}, 4), release: make(chan struct{})}
	var lis *ipc.MemListener
	mut := func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Admin: true, Account: `TEST\admin`}, nil }
		ac.NewReader = g.reader(now)
		lis, _ = ac.Listener.(*ipc.MemListener)
	}
	s := startSvcWith(t, dir, 0, nil, &fakeEvents{}, mut)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	hello := s.waitPush(t, func(m model.IPCMessage) bool { return m.Type == model.MsgHello })
	hasCap := false
	for _, c := range hello.Capabilities {
		hasCap = hasCap || c == model.CapProfileEnabled
	}
	if !hasCap {
		t.Fatalf("caps %v", hello.Capabilities)
	}
	// second client
	rw, err := lis.Dial()
	if err != nil {
		t.Fatal(err)
	}
	pushes2 := make(chan model.IPCMessage, 64)
	c2 := ipc.NewClient(rw, func(m model.IPCMessage) { pushes2 <- m })
	defer c2.Close()

	select { // first poll in flight
	case <-g.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first poll did not start")
	}
	no, yes := false, true
	cfg, err := s.c.Call(ctx, model.CmdGetConfig)
	if err != nil {
		t.Fatal(err)
	}
	// stale revision → conflict; unknown profile → not_found; missing enabled
	if m, _ := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSetEnabled, ProfileID: p.ID, Enabled: &no, ExpectedRevision: "stale"}); m.Code != model.ErrCodeConflict {
		t.Fatalf("conflict: %+v", m)
	}
	if m, _ := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSetEnabled, ProfileID: "nope", Enabled: &no}); m.Code != model.ErrCodeNotFound {
		t.Fatalf("unknown: %+v", m)
	}
	if m, _ := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSetEnabled, ProfileID: p.ID}); m.Code != model.ErrCodeInvalidRequest {
		t.Fatalf("no enabled: %+v", m)
	}
	m, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSetEnabled, ProfileID: p.ID, Enabled: &no, ExpectedRevision: cfg.Revision})
	if err != nil || m.Type != model.MsgOK || m.Revision == "" || m.Enabled == nil || *m.Enabled {
		t.Fatalf("set_enabled false: %+v %v", m, err)
	}
	// push to the second client
	waitP := func(ch chan model.IPCMessage, want bool) {
		t.Helper()
		to := time.After(5 * time.Second)
		for {
			select {
			case pm := <-ch:
				if pm.Type == model.MsgProfileEnabled {
					if pm.ProfileID != p.ID || pm.Enabled == nil || *pm.Enabled != want || pm.Revision == "" {
						t.Fatalf("push %+v", pm)
					}
					return
				}
			case <-to:
				t.Fatal("profile_enabled push not received")
			}
		}
	}
	waitP(pushes2, false)
	// finish the in-flight read: its result must be discarded
	g.mu.Lock()
	g.hold = false
	g.mu.Unlock()
	close(g.release)
	before := g.n()
	_, _ = s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdCheckNow})
	time.Sleep(700 * time.Millisecond)
	if g.n() != before {
		t.Fatalf("polled after disable: %d → %d", before, g.n())
	}
	for len(s.pushes) > 0 {
		if pm := <-s.pushes; pm.Type == model.MsgAlert && pm.Alert != nil && pm.Alert.ProfileID == p.ID && pm.Alert.To != model.LevelOK {
			t.Fatalf("alert after disable: %+v", pm.Alert)
		}
	}
	stat, err := s.c.Call(ctx, model.CmdGetStatus)
	if err != nil || stat.Status == nil || len(stat.Status.Profiles) != 1 || stat.Status.Profiles[0].Enabled {
		t.Fatalf("status %+v %v", stat.Status, err)
	}
	if ps, _ := st.LoadProfiles(); len(ps) != 1 || ps[0].Enabled {
		t.Fatal("not persisted")
	}
	s.stop()

	// restart: stays disabled, no reads
	s = startSvcWith(t, dir, 0, nil, &fakeEvents{}, mut)
	time.Sleep(500 * time.Millisecond)
	if g.n() != before {
		t.Fatalf("disabled profile polled after restart")
	}
	rw, err = lis.Dial()
	if err != nil {
		t.Fatal(err)
	}
	pushes3 := make(chan model.IPCMessage, 64)
	c3 := ipc.NewClient(rw, func(m model.IPCMessage) { pushes3 <- m })
	defer c3.Close()
	// enable without expectedRevision → polls at once, alert raised
	m, err = s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSetEnabled, ProfileID: p.ID, Enabled: &yes})
	if err != nil || m.Type != model.MsgOK || m.Enabled == nil || !*m.Enabled {
		t.Fatalf("set_enabled true: %+v %v", m, err)
	}
	waitP(pushes3, true)
	s.waitPush(t, func(pm model.IPCMessage) bool {
		return pm.Type == model.MsgAlert && pm.Alert != nil && pm.Alert.ProfileID == p.ID
	})
	if g.n() <= before {
		t.Fatal("not polled after enable")
	}
	s.stop()
	if ps, _ := st.LoadProfiles(); len(ps) != 1 || !ps[0].Enabled {
		t.Fatal("enable not persisted")
	}
	_ = monitor.ErrUnknownProfile
}
