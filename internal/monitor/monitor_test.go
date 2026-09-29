package monitor

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// fakeClock is a settable clock in UTC+3.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Set(t time.Time)     { c.mu.Lock(); c.t = t; c.mu.Unlock() }
func (c *fakeClock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// behavior of a fake profile reader.
type behavior struct {
	points  func(now time.Time) []model.ExchangePoint
	err     error
	panic   bool
	hang    chan struct{} // blocks (ignoring ctx) until closed
	delay   time.Duration // respects ctx
	counter bool          // emulate counters mode through opt.Counters
}

// fakeReader implements sqlcheck.ReadCloser.
type fakeReader struct {
	sqlcheck.StubReader
	f   *fakeFactory
	id  string
	opt sqlcheck.Options
}

func (r *fakeReader) ListPoints(ctx context.Context) ([]model.ExchangePoint, error) {
	f := r.f
	cur := atomic.AddInt32(&f.active, 1)
	defer atomic.AddInt32(&f.active, -1)
	for {
		m := atomic.LoadInt32(&f.maxActive)
		if cur <= m || atomic.CompareAndSwapInt32(&f.maxActive, m, cur) {
			break
		}
	}
	f.mu.Lock()
	b := f.b[r.id]
	f.calls[r.id]++
	f.mu.Unlock()
	if b.panic {
		panic("boom in reader " + r.id)
	}
	if b.hang != nil {
		<-b.hang
	}
	if b.delay > 0 {
		select {
		case <-time.After(b.delay):
		case <-ctx.Done():
			return nil, &sqlcheck.Error{Kind: sqlcheck.KindTimeout, Op: "list points", Err: ctx.Err()}
		}
	}
	if b.err != nil {
		return nil, b.err
	}
	now := r.opt.Now()
	if b.counter {
		prev, _ := r.opt.Counters.Load(r.id)
		f.mu.Lock()
		obs := f.counters[r.id]
		f.mu.Unlock()
		next, times := model.ApplyCounters(prev, obs, now)
		_ = r.opt.Counters.Save(r.id, next)
		return model.CounterPoints(r.id, times, r.opt.Settings, now), nil
	}
	if b.points == nil {
		return []model.ExchangePoint{}, nil
	}
	return b.points(now), nil
}

func (r *fakeReader) Close() error {
	r.f.mu.Lock()
	r.f.closed[r.id]++
	r.f.mu.Unlock()
	return nil
}

type fakeFactory struct {
	mu        sync.Mutex
	b         map[string]behavior
	calls     map[string]int
	created   map[string]int
	closed    map[string]int
	counters  map[string][]model.CounterObservation
	lastOpt   map[string]sqlcheck.Options
	active    int32
	maxActive int32
}

func newFactory() *fakeFactory {
	return &fakeFactory{b: map[string]behavior{}, calls: map[string]int{}, created: map[string]int{},
		closed: map[string]int{}, counters: map[string][]model.CounterObservation{}, lastOpt: map[string]sqlcheck.Options{}}
}

func (f *fakeFactory) set(id string, b behavior) { f.mu.Lock(); f.b[id] = b; f.mu.Unlock() }

func (f *fakeFactory) get(m map[string]int, id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return m[id]
}

func (f *fakeFactory) New(p model.Profile, pw string, opt sqlcheck.Options) (sqlcheck.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created[p.ID]++
	f.lastOpt[p.ID] = opt
	if p.Server == "bad-config" {
		return nil, &sqlcheck.Error{Kind: sqlcheck.KindConfig, Op: "open", Err: errors.New("bad profile")}
	}
	return &fakeReader{f: f, id: p.ID, opt: opt}, nil
}

func prof(id string) model.Profile {
	return model.Profile{ID: id, Name: "DB " + id, Server: "srv", Database: id, AuthMode: model.AuthWindows,
		Enabled: true, Mapping: model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}}
}

func lagPoints(names map[string]int) func(now time.Time) []model.ExchangePoint {
	return func(now time.Time) []model.ExchangePoint {
		var out []model.ExchangePoint
		for n, min := range names {
			t := now.Add(-time.Duration(min) * time.Minute)
			out = append(out, model.ExchangePoint{Name: n, LastSend: &t})
		}
		return out
	}
}

func profileByID(s model.StatusSnapshot, id string) model.ProfileSnapshot {
	for _, p := range s.Profiles {
		if p.ProfileID == id {
			return p
		}
	}
	return model.ProfileSnapshot{}
}

func baseOpt(f *fakeFactory, src ConfigSource, clk *fakeClock) Options {
	return Options{Source: src, NewReader: f.New, Now: clk.Now, Jitter: -1,
		PollInterval: time.Hour, ConfigCheck: time.Hour}
}

// TestCHK_MON_03_IsolationPanicTimeoutError — one profile panics, one
// hangs ignoring its context, one fails to connect: the healthy profile
// is still polled, each broken one gets its own error key, and the poll
// finishes within the per-profile budget.
func TestCHK_MON_03_IsolationPanicTimeoutError(t *testing.T) {
	f := newFactory()
	hang := make(chan struct{})
	defer close(hang)
	f.set("ok", behavior{points: lagPoints(map[string]int{"A": 5})})
	f.set("panic", behavior{panic: true})
	f.set("hang", behavior{hang: hang})
	f.set("slow", behavior{delay: time.Hour})
	f.set("conn", behavior{err: &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "ping", Err: errors.New("login failed")}})
	f.set("tls", behavior{err: &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "ping", Err: errors.New("TLS"), Key: "sql.tls_enable_tls10"}})
	cfgProfiles := []model.Profile{prof("ok"), prof("panic"), prof("hang"), prof("slow"), prof("conn"), prof("tls"), prof("badcfg")}
	cfgProfiles[6].Server = "bad-config"
	clk := &fakeClock{t: t0}
	lg := &memLog{}
	opt := baseOpt(f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: cfgProfiles}), clk)
	opt.Log = lg
	opt.ProfileTimeout = func(model.Profile) time.Duration { return 150 * time.Millisecond }
	m := New(opt)
	m.reloadConfig(true)
	start := time.Now()
	m.pollAll(context.Background())
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("poll took %s: a broken profile blocked the others", d)
	}
	s := m.Snapshot()
	want := map[string]string{"panic": model.MsgMonitorPanic, "hang": model.MsgMonitorTimeout, "slow": model.MsgMonitorTimeout,
		"conn": model.MsgMonitorConnError, "tls": "sql.tls_enable_tls10", "badcfg": model.MsgMonitorConfig}
	for id, key := range want {
		p := profileByID(s, id)
		if p.Level != model.LevelError || p.ErrorKey != key || p.Error == "" || !p.Polled {
			t.Errorf("%s: level %v key %q err %q", id, p.Level, p.ErrorKey, p.Error)
		}
	}
	okp := profileByID(s, "ok")
	if okp.Level != model.LevelOK || len(okp.Points) != 1 || okp.Error != "" {
		t.Errorf("healthy profile affected: %+v", okp)
	}
	if len(okp.Warnings) != 0 { // Windows authentication: no plaintext-login warning
		t.Errorf("warnings %v", okp.Warnings)
	}
	if !lg.has("panic while polling") || !lg.has("timed out") {
		t.Errorf("log: %v", lg.lines)
	}
	// the hung reader is still running: the next poll skips it (busy)
	m.pollAll(context.Background())
	if p := profileByID(m.Snapshot(), "hang"); p.ErrorKey != model.MsgMonitorBusy {
		t.Errorf("hung profile: %q", p.ErrorKey)
	}
	if p := profileByID(m.Snapshot(), "panic"); p.ErrorKey != model.MsgMonitorPanic {
		t.Errorf("panic again: %q (a panic must not leave the profile busy)", p.ErrorKey)
	}
	if s.Level != model.LevelError || s.Now.Location() != model.DBZone {
		t.Errorf("snapshot level/zone: %v %v", s.Level, s.Now)
	}
}

// TestCHK_MON_03_BoundedConcurrency — never more than MaxConcurrent
// readers at the same time.
func TestCHK_MON_03_BoundedConcurrency(t *testing.T) {
	f := newFactory()
	var ps []model.Profile
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		f.set(id, behavior{delay: 40 * time.Millisecond})
		ps = append(ps, prof(id))
	}
	clk := &fakeClock{t: t0}
	opt := baseOpt(f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: ps}), clk)
	opt.MaxConcurrent = 3
	m := New(opt)
	m.reloadConfig(true)
	m.pollAll(context.Background())
	if mx := atomic.LoadInt32(&f.maxActive); mx > 3 || mx < 2 {
		t.Errorf("max concurrent readers = %d, want 2..3", mx)
	}
	for _, p := range ps {
		if f.get(f.calls, p.ID) != 1 {
			t.Errorf("%s polled %d times", p.ID, f.get(f.calls, p.ID))
		}
	}
}

// TestCHK_MON_03_JitterAndTimeoutDefaults — jitter bounds and the default
// per-profile budget.
func TestCHK_MON_03_JitterAndTimeoutDefaults(t *testing.T) {
	var gotMax int64
	m := New(Options{Rand: func(n int64) int64 { gotMax = n; return n - 1 }})
	if d := m.jitterFor(60 * time.Second); gotMax != int64(2*time.Second) || d != 2*time.Second-1 {
		t.Errorf("60s interval: max %d, d %s", gotMax, d)
	}
	m.jitterFor(5 * time.Second)
	if gotMax != int64(500*time.Millisecond) {
		t.Errorf("5s interval: max %d", gotMax)
	}
	m.opt.Jitter = -1
	if m.jitterFor(time.Minute) != 0 {
		t.Error("negative jitter = none")
	}
	m.opt.Jitter = 100 * time.Millisecond
	m.jitterFor(time.Minute)
	if gotMax != int64(100*time.Millisecond) {
		t.Errorf("explicit jitter: %d", gotMax)
	}
	p := prof("x")
	p.Mappings = []model.Mapping{p.Mapping}
	if d := m.profileTimeout(p); d != p.ConnectTimeout()+3*p.QueryTimeout() {
		t.Errorf("timeout %s", d)
	}
	// jitter is actually applied before polling (and cut short by stop)
	f := newFactory()
	f.set("x", behavior{})
	clk := &fakeClock{t: t0}
	opt := baseOpt(f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("x")}}), clk)
	opt.Jitter = time.Hour
	opt.Rand = func(n int64) int64 { return n - 1 }
	m = New(opt)
	m.reloadConfig(true)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	m.pollAll(ctx)
	if time.Since(start) > 2*time.Second || f.get(f.calls, "x") != 0 {
		t.Error("stop must interrupt the jitter delay")
	}
}

// TestCHK_MON_03_LoopStopAndPollNow — the loop polls every interval, on
// PollNow, and Run returns promptly on stop (even with a hung reader),
// closing the readers.
func TestCHK_MON_03_LoopStopAndPollNow(t *testing.T) {
	f := newFactory()
	hang := make(chan struct{})
	defer close(hang)
	f.set("a", behavior{points: lagPoints(map[string]int{"A": 1})})
	f.set("h", behavior{hang: hang})
	clk := &fakeClock{t: t0}
	var snaps int32
	opt := baseOpt(f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a"), prof("h")}}), clk)
	opt.PollInterval = 30 * time.Millisecond
	opt.ProfileTimeout = func(model.Profile) time.Duration { return 50 * time.Millisecond }
	opt.OnSnapshot = func(model.StatusSnapshot) { atomic.AddInt32(&snaps, 1) }
	m := New(opt)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for f.get(f.calls, "a") < 4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f.get(f.calls, "a") < 4 {
		t.Fatalf("loop polled %d times", f.get(f.calls, "a"))
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
	if f.get(f.closed, "a") != 1 {
		t.Errorf("reader a closed %d times", f.get(f.closed, "a"))
	}
	if atomic.LoadInt32(&snaps) < 4 {
		t.Errorf("snapshots published: %d", snaps)
	}

	// PollNow with a long interval
	f2 := newFactory()
	f2.set("a", behavior{})
	opt2 := baseOpt(f2, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a")}}), clk)
	m2 := New(opt2)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go m2.Run(ctx2)
	waitFor(t, func() bool { return f2.get(f2.calls, "a") == 1 })
	if c, err := m2.PollNow(""); c || err != nil {
		t.Errorf("first poll_now: coalesced %v err %v", c, err)
	}
	if c, _ := m2.PollNow(""); !c { // rate-limited, never blocks
		t.Error("second poll_now within 10 s must be coalesced")
	}
	waitFor(t, func() bool { return f2.get(f2.calls, "a") >= 2 })
	if n := m2.Snapshot().PollIntervalSec; n != 3600 {
		t.Errorf("interval in snapshot: %d", n)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestCHK_MON_03_IntervalFromSettings — period from settings (default 60 s).
func TestCHK_MON_03_IntervalFromSettings(t *testing.T) {
	m := New(Options{Source: NewMemConfig(Config{})})
	m.reloadConfig(true)
	if m.interval() != 60*time.Second {
		t.Errorf("default interval %s", m.interval())
	}
	st := model.DefaultSettings()
	st.PollIntervalSec = 15
	m = New(Options{Source: NewMemConfig(Config{Settings: st})})
	m.reloadConfig(true)
	if m.interval() != 15*time.Second || m.Snapshot().PollIntervalSec != 15 {
		t.Errorf("interval %s", m.interval())
	}
	if s := New(Options{}).Snapshot(); s.Profiles == nil || s.Now.Location() != model.DBZone {
		t.Errorf("snapshot before start: %+v", s)
	}
}

// TestCHK_MON_04_ReloadOnChangeAndCommand — MemConfig change detected by
// the stamp poll; unchanged profiles keep their reader; changed and
// removed profiles get their reader closed; ReloadConfig forces a reload.
func TestCHK_MON_04_ReloadOnChangeAndCommand(t *testing.T) {
	f := newFactory()
	for _, id := range []string{"a", "b", "c"} {
		f.set(id, behavior{points: lagPoints(map[string]int{"P": 1})})
	}
	clk := &fakeClock{t: t0}
	src := NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a"), prof("b")}})
	opt := baseOpt(f, src, clk)
	opt.ConfigCheck = 10 * time.Millisecond
	m := New(opt)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	waitFor(t, func() bool { return profileByID(m.Snapshot(), "b").Polled })

	pb := prof("b")
	pb.Database = "other" // connection change -> new reader
	src.Set(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a"), pb, prof("c")}})
	waitFor(t, func() bool { return profileByID(m.Snapshot(), "c").Polled })
	if f.get(f.created, "a") != 1 || f.get(f.closed, "a") != 0 {
		t.Errorf("unchanged profile a: created %d closed %d", f.get(f.created, "a"), f.get(f.closed, "a"))
	}
	waitFor(t, func() bool { return f.get(f.created, "b") == 2 && f.get(f.closed, "b") == 1 })

	src.Set(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a")}})
	waitFor(t, func() bool { return len(m.Snapshot().Profiles) == 1 })
	waitFor(t, func() bool { return f.get(f.closed, "c") == 1 })

	// thresholds change -> reader gets the new effective settings
	pa := prof("a")
	pa.WarnMinutes, pa.ErrorMinutes = 1, 2
	f.set("a", behavior{points: lagPoints(map[string]int{"P": 90})})
	src.Set(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{pa}})
	waitFor(t, func() bool {
		p := profileByID(m.Snapshot(), "a")
		return p.WarnMinutes == 1 && p.Level == model.LevelError
	})
	f.mu.Lock()
	if f.lastOpt["a"].Settings.ErrorMinutes != 2 {
		t.Errorf("reader settings %+v", f.lastOpt["a"].Settings)
	}
	f.mu.Unlock()

	// forced reload (IPC command) re-reads even without a stamp change
	before := f.get(f.calls, "a")
	m.ReloadConfig()
	waitFor(t, func() bool { return f.get(f.calls, "a") > before })
}

// TestCHK_MON_04_FileConfigReload — FileConfig stamp follows the files
// (size/mtime), invalid profiles are skipped and reported, invalid
// settings fall back to defaults.
func TestCHK_MON_04_FileConfigReload(t *testing.T) {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	fc := FileConfig{Store: st}
	s0 := fc.Stamp()
	c := fc.Load()
	if c.Err != nil || len(c.Profiles) != 0 || c.Settings != model.DefaultSettings() {
		t.Fatalf("empty dir: %+v", c)
	}
	if err := st.SaveProfiles([]model.Profile{prof("a")}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPassword("a", "pw"); err != nil {
		t.Fatal(err)
	}
	s1 := fc.Stamp()
	if s1 == s0 {
		t.Fatal("stamp must change after profiles.json is written")
	}
	c = fc.Load()
	if len(c.Profiles) != 1 || c.Passwords["a"] != "pw" || c.Err != nil {
		t.Errorf("load: %+v", c)
	}
	if fc.Stamp() != s1 {
		t.Error("stamp must be stable without changes")
	}
	// one invalid profile + invalid settings written by hand
	bad := `{"version":1,"profiles":[` + mustJSON(prof("a")) + `,{"id":"b","server":"","database":"x","authMode":"sql"}]}`
	os.WriteFile(st.Path(config.ProfilesFile), []byte(bad), 0o644)
	os.WriteFile(st.Path(config.SettingsFile), []byte(`{"version":1,"pollIntervalSec":1}`), 0o644)
	c = fc.Load()
	if len(c.Profiles) != 1 || c.Profiles[0].ID != "a" || c.Err == nil || c.Settings.PollIntervalSec != 60 {
		t.Errorf("invalid files: %+v", c)
	}
	// the monitor reports the config error in the snapshot
	f := newFactory()
	f.set("a", behavior{})
	m := New(Options{Source: fc, NewReader: f.New, Jitter: -1})
	m.reloadConfig(true)
	if s := m.Snapshot(); s.ConfigError == "" || len(s.Profiles) != 1 {
		t.Errorf("snapshot: %+v", s)
	}
	if m.reloadConfig(false) {
		t.Error("no change -> no reload")
	}
	time.Sleep(20 * time.Millisecond)
	os.WriteFile(st.Path(config.SettingsFile), []byte(`{"version":1,"pollIntervalSec":30}`), 0o644)
	if !m.reloadConfig(false) || m.interval() != 30*time.Second {
		t.Errorf("settings change not picked up: %s", m.interval())
	}
}

type plainProtector struct{}

func (plainProtector) Protect(b []byte) ([]byte, error) { return append([]byte("P:"), b...), nil }
func (plainProtector) Unprotect(b []byte) ([]byte, error) {
	return []byte(strings.TrimPrefix(string(b), "P:")), nil
}

func mustJSON(p model.Profile) string {
	b, err := jsonMarshal(p)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestCHK_MON_05_MonitorAlertsDebounced — with a fake clock the lag grows
// past the thresholds: one event per transition reaches the sink, active
// alerts are in the snapshot, a recovered point clears after 2 polls, a
// transient connection error is not reported.
func TestCHK_MON_05_MonitorAlertsDebounced(t *testing.T) {
	f := newFactory()
	clk := &fakeClock{t: t0}
	last := t0
	f.set("a", behavior{points: func(time.Time) []model.ExchangePoint {
		l := last
		return []model.ExchangePoint{{Name: "A", LastSend: &l, PlanTable: "_Node1"}}
	}})
	var evs []model.AlertEvent
	var mu sync.Mutex
	opt := baseOpt(f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a")}}), clk)
	opt.Sink = alert.SinkFunc(func(e model.AlertEvent) { mu.Lock(); evs = append(evs, e); mu.Unlock() })
	m := New(opt)
	m.reloadConfig(true)
	poll := func(adv time.Duration) model.StatusSnapshot {
		clk.Add(adv)
		m.pollAll(context.Background())
		return m.Snapshot()
	}
	poll(10 * time.Minute)      // lag 10: ok
	s := poll(25 * time.Minute) // lag 35: warning
	if len(s.Alerts) != 1 || s.Alerts[0].To != model.LevelWarning || s.Alerts[0].PlanTable != "_Node1" {
		t.Fatalf("active alerts %+v", s.Alerts)
	}
	poll(time.Minute)      // still warning: no new event
	poll(30 * time.Minute) // lag 66: error
	last = clk.Now()       // exchange happened
	poll(time.Minute)      // ok (1st)
	s = poll(time.Minute)  // ok (2nd) -> recovered
	if len(s.Alerts) != 0 {
		t.Errorf("alerts after recovery: %+v", s.Alerts)
	}
	mu.Lock()
	got := append([]model.AlertEvent(nil), evs...)
	mu.Unlock()
	if len(got) != 3 || got[0].To != model.LevelWarning || got[1].To != model.LevelError || got[2].To != model.LevelOK {
		t.Fatalf("events %+v", got)
	}
	if got[0].LagMinutes != 35 || got[0].FreshAt == nil || got[0].At.Location() != model.DBZone {
		t.Errorf("event details %+v", got[0])
	}
	// one failed poll: no profile event; two: error event; points keep state
	f.set("a", behavior{err: &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "ping", Err: errors.New("down")}})
	poll(time.Minute)
	f.set("a", behavior{points: func(time.Time) []model.ExchangePoint {
		l := last
		return []model.ExchangePoint{{Name: "A", LastSend: &l, PlanTable: "_Node1"}}
	}})
	poll(time.Minute)
	mu.Lock()
	n := len(evs)
	mu.Unlock()
	if n != 3 {
		t.Errorf("transient error reported: %d events", n)
	}
	f.set("a", behavior{err: &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "ping", Err: errors.New("down")}})
	poll(time.Minute)
	s = poll(time.Minute)
	mu.Lock()
	defer mu.Unlock()
	if len(evs) != 4 || !evs[3].ProfileLevel() || evs[3].To != model.LevelError || evs[3].ErrorKey != model.MsgMonitorConnError {
		t.Errorf("profile error event %+v", evs)
	}
	if len(s.Alerts) != 1 || profileByID(s, "a").Level != model.LevelError {
		t.Errorf("snapshot during outage %+v", s.Alerts)
	}
}

// TestCHK_MON_02_MonitorCountersRestart — the monitor passes the file
// store to the readers; after a "service restart" (new Monitor, new
// store over the same file) unchanged counters keep their baseline time.
func TestCHK_MON_02_MonitorCountersRestart(t *testing.T) {
	dir := t.TempDir()
	clk := &fakeClock{t: t0}
	run := func(obs []model.CounterObservation) model.ProfileSnapshot {
		f := newFactory()
		f.set("c", behavior{counter: true})
		f.counters["c"] = obs
		opt := baseOpt(f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("c")}}), clk)
		opt.Counters = OpenFileCounterStore(dir, nil)
		m := New(opt)
		m.reloadConfig(true)
		m.pollAll(context.Background())
		return profileByID(m.Snapshot(), "c")
	}
	obs := []model.CounterObservation{{Key: "k1", Name: "N1", SentNo: 1, ReceivedNo: 1}, {Key: "k2", Name: "N2", SentNo: 5, ReceivedNo: 5}}
	p := run(obs)
	if !p.Baseline || !p.Points[0].Baseline {
		t.Fatalf("first run: baseline expected %+v", p)
	}
	clk.Add(20 * time.Minute)
	obs[0].SentNo = 2
	p = run(obs) // restart #1: N1 changed
	pts := map[string]model.PointStatus{}
	for _, x := range p.Points {
		pts[x.Name] = x
	}
	if pts["N1"].Baseline || !pts["N1"].FreshAt.Equal(clk.Now()) {
		t.Errorf("N1 changed: %+v", pts["N1"])
	}
	if !pts["N2"].Baseline || !pts["N2"].FreshAt.Equal(t0) || pts["N2"].LagMinutes != 20 {
		t.Errorf("N2 baseline lost across restart: %+v", pts["N2"])
	}
	clk.Add(45 * time.Minute)
	p = run(obs) // restart #2: nothing changed, N2 baseline is 65 min old -> error
	for _, x := range p.Points {
		if x.Name == "N2" && (x.Level != model.LevelError || !x.Baseline) {
			t.Errorf("N2 after second restart: %+v", x)
		}
		if x.Name == "N1" && (x.Level != model.LevelWarning || x.Baseline) {
			t.Errorf("N1 after second restart: %+v", x)
		}
	}
}

// TestCHK_MON_01_SnapshotWarnings — the phase-2d plaintext-login warning
// (SQL authentication, encrypt disable = default) reaches the snapshot;
// it does not change the level.
func TestCHK_MON_01_SnapshotWarnings(t *testing.T) {
	f := newFactory()
	f.set("s", behavior{points: lagPoints(map[string]int{"A": 1})})
	p := prof("s")
	p.AuthMode, p.Login = model.AuthSQL, "mon"
	clk := &fakeClock{t: t0}
	m := New(baseOpt(f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{p}}), clk))
	m.reloadConfig(true)
	if w := profileByID(m.Snapshot(), "s").Warnings; len(w) != 1 || w[0] != sqlcheck.MsgWarnPlaintextLogin {
		t.Errorf("pending snapshot warnings %v", w)
	}
	m.pollAll(context.Background())
	ps := profileByID(m.Snapshot(), "s")
	if len(ps.Warnings) != 1 || ps.Level != model.LevelOK {
		t.Errorf("polled snapshot %+v", ps)
	}
	p.Encrypt = model.EncryptOn
	m = New(baseOpt(f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{p}}), clk))
	m.reloadConfig(true)
	if w := profileByID(m.Snapshot(), "s").Warnings; len(w) != 0 {
		t.Errorf("encrypted profile warnings %v", w)
	}
}
