package monitor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_IPC_04_PollerHung — heartbeat: service start, end of the last
// full cycle (a single-profile poll does not count), threshold
// max(2 × interval, interval + ceil(n / MaxConcurrent) × max profile
// timeout + jitter); PollerHung when no cycle completed for longer (also
// before the first cycle), reported once (log + Event Log warning), and
// cleared by the next completed cycle. Fake clock only.
func TestCHK_IPC_04_PollerHung(t *testing.T) {
	var ps []model.Profile
	f := newFactory()
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("p%d", i)
		ps = append(ps, prof(id))
		f.set(id, behavior{})
	}
	clk := &fakeClock{t: t0}
	src := NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: ps})
	opt := baseOpt(f, src, clk)
	opt.PollInterval = time.Minute
	opt.MaxConcurrent = 2
	opt.ProfileTimeout = func(model.Profile) time.Duration { return 10 * time.Second }
	w := &warnRec{}
	lg := &memLog{}
	opt.Warn, opt.Log = w.warn, lg
	var pushed []model.StatusSnapshot
	opt.OnSnapshot = func(s model.StatusSnapshot) { pushed = append(pushed, s) }
	m := New(opt)
	m.reloadConfig(true)
	// 5 profiles / 2 = 3 waves × 10 s = 30 s budget: max(120 s, 90 s)
	hb := m.Heartbeat()
	if hb.HungAfterSec != 120 || hb.Protocol != model.IPCVersion || hb.PollIntervalSec != 60 || !hb.ServiceStartedAt.Equal(t0) || hb.LastCycleAt != nil {
		t.Fatalf("heartbeat %+v", hb)
	}
	// 20 profiles: 10 waves × 10 s = 100 s: max(120 s, 160 s)
	var many []model.Profile
	for i := 0; i < 20; i++ {
		many = append(many, prof(fmt.Sprintf("q%d", i)))
	}
	m2 := New(Options{Source: NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: many}), NewReader: f.New,
		Now: clk.Now, PollInterval: time.Minute, MaxConcurrent: 2, Jitter: 5 * time.Second,
		ProfileTimeout: opt.ProfileTimeout, ConfigCheck: time.Hour})
	m2.reloadConfig(true)
	if got := m2.Heartbeat().HungAfterSec; got != 165 { // + 5 s jitter
		t.Errorf("threshold with 20 profiles: %d", got)
	}
	// never cycled: measured from the service start
	clk.Add(120 * time.Second)
	if m.Heartbeat().PollerHung {
		t.Error("hung at exactly the threshold")
	}
	clk.Add(time.Second)
	if !m.Heartbeat().PollerHung || !m.Snapshot().PollerHung {
		t.Error("never completed a cycle: not hung")
	}
	m.pollAll(context.Background())
	s := m.Snapshot()
	if s.PollerHung || s.LastCycleAt == nil || !s.LastCycleAt.Equal(clk.Now()) || s.ServiceStartedAt == nil || s.HungAfterSec != 120 {
		t.Fatalf("after a cycle %+v", s)
	}
	cycle := clk.Now()
	// a single-profile poll is not a cycle
	clk.Add(100 * time.Second)
	m.pollSome(context.Background(), map[string]bool{"p0": true})
	if hb := m.Heartbeat(); hb.LastCycleAt == nil || !hb.LastCycleAt.Equal(cycle) {
		t.Errorf("single-profile poll moved LastCycleAt: %+v", hb.LastCycleAt)
	}
	clk.Add(21 * time.Second)
	if !m.Snapshot().PollerHung {
		t.Fatal("121 s without a cycle: not hung")
	}
	n := len(pushed)
	m.checkHung()
	m.checkHung()
	if w.n() != 1 || !lg.has("poller hung") {
		t.Errorf("hung reported %d times: %v", w.n(), w.msgs)
	}
	if len(pushed) != n+1 || !pushed[len(pushed)-1].PollerHung {
		t.Error("hung snapshot not pushed")
	}
	m.pollAll(context.Background())
	if m.Snapshot().PollerHung || !lg.has("complete again") {
		t.Error("cycle completed: still hung")
	}
	if pushed[len(pushed)-1].PollerHung {
		t.Error("pushed snapshot after the cycle still hung")
	}
	// the flag appears again only after a new hang
	clk.Add(121 * time.Second)
	m.checkHung()
	if w.n() != 2 {
		t.Errorf("second hang reported %d", w.n())
	}
	if model.MonitorMessage(model.MsgMonitorPollerHung, "ru") == "" || model.MonitorMessage(model.MsgMonitorPollerHung, model.LangEN) == "" {
		t.Error("poller_hung message")
	}
}
