package monitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

type warnRec struct {
	mu   sync.Mutex
	msgs []string
}

func (w *warnRec) warn(m string) { w.mu.Lock(); w.msgs = append(w.msgs, m); w.mu.Unlock() }
func (w *warnRec) n() int        { w.mu.Lock(); defer w.mu.Unlock(); return len(w.msgs) }
func (w *warnRec) last() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.msgs) == 0 {
		return ""
	}
	return w.msgs[len(w.msgs)-1]
}

func hasID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// TestCHK_MON_10_ConfigFailureKeepsLastGood — broken profiles.json, an
// unreadable profiles.json and a broken secrets file are NOT applied: the
// readers are not recreated, the counters state file and the active
// alerts are kept, the snapshot says "stale config", the warning is
// logged / sent to the event log once (anti-spam) and repeated after
// ConfigWarnEvery; the next check retries and a fixed file is applied.
func TestCHK_MON_10_ConfigFailureKeepsLastGood(t *testing.T) {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	if err := st.SaveProfiles([]model.Profile{prof("a")}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPassword("a", "pw"); err != nil {
		t.Fatal(err)
	}
	f := newFactory()
	f.set("a", behavior{counter: true})
	f.counters["a"] = []model.CounterObservation{{Key: "k1", Name: "N1", SentNo: 1, ReceivedNo: 1}}
	clk := &fakeClock{t: t0}
	lg := &memLog{}
	w := &warnRec{}
	opt := baseOpt(f, FileConfig{Store: st}, clk)
	opt.Counters = OpenFileCounterStore(dir, lg)
	opt.Log, opt.Warn = lg, w.warn
	m := New(opt)
	if !m.reloadConfig(true) {
		t.Fatal("initial load")
	}
	m.pollAll(context.Background())
	// a profile-level alert becomes active (2 failed polls)
	f.set("a", behavior{err: &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "ping", Err: errors.New("down")}})
	m.pollAll(context.Background())
	m.pollAll(context.Background())
	if n := len(m.Snapshot().Alerts); n != 1 {
		t.Fatalf("active alerts before: %d", n)
	}
	// the database stays down during the config failure: the alert must
	// stay active (a config reset would drop it)

	check := func(stage string) {
		t.Helper()
		if m.reloadConfig(false) {
			t.Errorf("%s: a failed load must not be applied", stage)
		}
		s := m.Snapshot()
		if len(s.Profiles) != 1 || s.Profiles[0].ProfileID != "a" || !s.ConfigStale ||
			s.ConfigErrorKey != model.MsgMonitorConfigStale || s.ConfigError == "" {
			t.Errorf("%s: snapshot %+v", stage, s)
		}
		if len(s.Alerts) != 1 {
			t.Errorf("%s: alerts reset: %+v", stage, s.Alerts)
		}
		if f.get(f.created, "a") != 1 || f.get(f.closed, "a") != 0 {
			t.Errorf("%s: reader recreated: created %d closed %d", stage, f.get(f.created, "a"), f.get(f.closed, "a"))
		}
		if ids := OpenFileCounterStore(dir, nil).ProfileIDs(); !hasID(ids, "a") {
			t.Errorf("%s: counters state wiped: %v", stage, ids)
		}
		// polling continues with the last good config and the same reader
		before := f.get(f.calls, "a")
		m.pollAll(context.Background())
		if f.get(f.calls, "a") != before+1 {
			t.Errorf("%s: last good profile not polled", stage)
		}
	}

	// 1. broken JSON
	time.Sleep(15 * time.Millisecond)
	if err := os.WriteFile(st.Path(config.ProfilesFile), []byte(`{"version":1,"profiles":[{"id":"a",`), 0o644); err != nil {
		t.Fatal(err)
	}
	check("broken json")
	if w.n() != 1 || !strings.Contains(w.last(), "keeping the last good") || !lg.has("keeping the last good") {
		t.Fatalf("warning: %v", w.msgs)
	}
	// anti-spam: the retry on the next check does not repeat the warning
	check("retry")
	check("retry 2")
	if w.n() != 1 {
		t.Errorf("warning repeated: %d", w.n())
	}
	clk.Add(DefaultConfigWarnEvery + time.Second)
	check("after warn period")
	if w.n() != 2 {
		t.Errorf("warning not repeated after %s: %d", DefaultConfigWarnEvery, w.n())
	}

	// 2. read failure (profiles.json is a directory)
	os.Remove(st.Path(config.ProfilesFile))
	if err := os.Mkdir(st.Path(config.ProfilesFile), 0o755); err != nil {
		t.Fatal(err)
	}
	check("read failure")
	if w.n() != 3 { // a different error is reported at once
		t.Errorf("read failure warning: %v", w.msgs)
	}
	os.Remove(st.Path(config.ProfilesFile))

	// 3. valid profiles.json, broken secrets
	if err := st.SaveProfiles([]model.Profile{prof("a")}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path(config.SecretsFile), []byte("P:{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	check("broken secrets")

	// 4. fixed: applied on the next check; the same reader is kept
	if err := st.SaveSecrets(config.Secrets{"a": "pw"}); err != nil {
		t.Fatal(err)
	}
	if !m.reloadConfig(false) {
		t.Fatal("fixed config not applied")
	}
	s := m.Snapshot()
	if s.ConfigStale || s.ConfigError != "" || len(s.Alerts) != 1 || f.get(f.created, "a") != 1 {
		t.Errorf("after recovery: %+v created %d", s, f.get(f.created, "a"))
	}
	if !lg.has("loaded successfully again") {
		t.Error("recovery not logged")
	}
	if m.reloadConfig(false) {
		t.Error("no change after recovery -> no reload")
	}
	// a successful load that removes the profile does prune state + alerts
	if err := st.SaveProfiles([]model.Profile{}); err != nil {
		t.Fatal(err)
	}
	m.reloadConfig(true)
	if ids := OpenFileCounterStore(dir, nil).ProfileIDs(); hasID(ids, "a") || len(m.Snapshot().Alerts) != 0 {
		t.Errorf("successful load must prune: %v %+v", ids, m.Snapshot().Alerts)
	}
}

// TestCHK_MON_10_FirstLoadFailure — the service starts with a broken
// profiles.json: nothing is polled, the error is in the snapshot and the
// saved counters state is NOT pruned; fixing the file starts polling.
func TestCHK_MON_10_FirstLoadFailure(t *testing.T) {
	dir := t.TempDir()
	cs := OpenFileCounterStore(dir, nil)
	if err := cs.Save("a", model.CounterState{Version: model.CounterStateVersion, Points: map[string]model.CounterPoint{}}); err != nil {
		t.Fatal(err)
	}
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	os.WriteFile(filepath.Join(dir, config.ProfilesFile), []byte("garbage"), 0o644)
	f := newFactory()
	f.set("a", behavior{})
	w := &warnRec{}
	opt := baseOpt(f, FileConfig{Store: st}, &fakeClock{t: t0})
	opt.Counters = OpenFileCounterStore(dir, nil)
	opt.Warn = w.warn
	m := New(opt)
	m.reloadConfig(true)
	m.pollAll(context.Background())
	s := m.Snapshot()
	if len(s.Profiles) != 0 || s.ConfigError == "" || s.ConfigStale || s.ConfigErrorKey != model.MsgMonitorConfigStale {
		t.Errorf("snapshot %+v", s)
	}
	if w.n() != 1 || !strings.Contains(w.last(), "no previous good") {
		t.Errorf("warn %v", w.msgs)
	}
	if !hasID(OpenFileCounterStore(dir, nil).ProfileIDs(), "a") {
		t.Error("counters state pruned by a failed first load")
	}
	time.Sleep(15 * time.Millisecond)
	if err := st.SaveProfiles([]model.Profile{prof("a")}); err != nil {
		t.Fatal(err)
	}
	if !m.reloadConfig(false) {
		t.Fatal("retry did not apply the fixed file")
	}
	m.pollAll(context.Background())
	if p := profileByID(m.Snapshot(), "a"); !p.Polled || m.Snapshot().ConfigError != "" {
		t.Errorf("after fix %+v", m.Snapshot())
	}
}

// TestCHK_MON_12_CommandRateLimit — poll_now / reload_config run at most
// once per 10 s (globally and per profile); extra calls are coalesced; a
// single-profile poll_now polls only that profile.
func TestCHK_MON_12_CommandRateLimit(t *testing.T) {
	if DefaultCommandInterval != 10*time.Second {
		t.Fatalf("interval %s", DefaultCommandInterval)
	}
	f := newFactory()
	f.set("a", behavior{})
	f.set("b", behavior{})
	clk := &fakeClock{t: t0}
	m := New(baseOpt(f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a"), prof("b")}}), clk))
	m.reloadConfig(true)
	expect := func(what string, got, want bool) {
		t.Helper()
		if got != want {
			t.Errorf("%s: coalesced = %v, want %v", what, got, want)
		}
	}
	c, err := m.PollNow("")
	expect("first global", c, false)
	if err != nil {
		t.Fatal(err)
	}
	c, _ = m.PollNow("")
	expect("second global", c, true)
	c, _ = m.PollNow("a")
	expect("profile right after global", c, true)
	if m.takePending() != nil {
		t.Error("global request pending = all profiles")
	}
	clk.Add(9 * time.Second)
	c, _ = m.PollNow("")
	expect("global after 9 s", c, true)
	clk.Add(2 * time.Second)
	c, _ = m.PollNow("a")
	expect("profile a after 11 s", c, false)
	c, _ = m.PollNow("a")
	expect("profile a again", c, true)
	c, _ = m.PollNow("b")
	expect("profile b", c, false)
	if p := m.takePending(); len(p) != 2 || !p["a"] || !p["b"] {
		t.Errorf("pending %v", p)
	}
	if _, err := m.PollNow("zz"); !errors.Is(err, ErrUnknownProfile) {
		t.Errorf("unknown profile: %v", err)
	}
	expect("reload 1", m.ReloadConfig(), false)
	expect("reload 2", m.ReloadConfig(), true)
	clk.Add(11 * time.Second)
	expect("reload after 11 s", m.ReloadConfig(), false)

	// in the loop: poll_now for one profile polls only that one
	f2 := newFactory()
	f2.set("a", behavior{})
	f2.set("b", behavior{})
	m2 := New(baseOpt(f2, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a"), prof("b")}}), clk))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m2.Run(ctx)
	waitFor(t, func() bool { return f2.get(f2.calls, "a") == 1 && f2.get(f2.calls, "b") == 1 })
	if c, err := m2.PollNow("a"); c || err != nil {
		t.Fatalf("poll a: %v %v", c, err)
	}
	waitFor(t, func() bool { return f2.get(f2.calls, "a") == 2 })
	time.Sleep(50 * time.Millisecond)
	if f2.get(f2.calls, "b") != 1 {
		t.Errorf("profile b polled by a single-profile poll_now: %d", f2.get(f2.calls, "b"))
	}
	if !profileByID(m2.Snapshot(), "b").Polled {
		t.Error("b's previous result must stay in the snapshot")
	}
}

// TestCHK_MON_13_HungReaderRecreated — a reader that ignores its deadline
// keeps the profile busy; after more than 3 poll timeouts the reader is
// closed (in the background), the status is an error with monitor.hung,
// the event log gets a warning and the next poll opens a new reader.
func TestCHK_MON_13_HungReaderRecreated(t *testing.T) {
	f := newFactory()
	hang := make(chan struct{})
	f.set("h", behavior{hang: hang})
	clk := &fakeClock{t: t0}
	w := &warnRec{}
	opt := baseOpt(f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("h")}}), clk)
	opt.ProfileTimeout = func(model.Profile) time.Duration { return 50 * time.Millisecond }
	opt.Warn = w.warn
	m := New(opt)
	m.reloadConfig(true)
	key := func() string { return profileByID(m.Snapshot(), "h").ErrorKey }
	m.pollAll(context.Background())
	if key() != model.MsgMonitorTimeout {
		t.Fatalf("first poll: %q", key())
	}
	clk.Add(100 * time.Millisecond) // busy 2 timeouts: still just busy
	m.pollAll(context.Background())
	if key() != model.MsgMonitorBusy || f.get(f.closed, "h") != 0 {
		t.Fatalf("second poll: %q closed %d", key(), f.get(f.closed, "h"))
	}
	clk.Add(60 * time.Millisecond) // busy 160 ms > 3 x 50 ms
	f.set("h", behavior{points: lagPoints(map[string]int{"A": 1})})
	m.pollAll(context.Background())
	p := profileByID(m.Snapshot(), "h")
	if p.ErrorKey != model.MsgMonitorHung || p.Level != model.LevelError || p.Error == "" {
		t.Fatalf("hung poll: %+v", p)
	}
	waitFor(t, func() bool { return f.get(f.closed, "h") == 1 }) // old reader (sql.DB) closed
	if w.n() != 1 || !strings.Contains(w.last(), "hung") {
		t.Errorf("warn %v", w.msgs)
	}
	m.pollAll(context.Background())
	p = profileByID(m.Snapshot(), "h")
	if f.get(f.created, "h") != 2 || p.Level != model.LevelOK || p.Error != "" {
		t.Fatalf("new reader: created %d %+v", f.get(f.created, "h"), p)
	}
	// the abandoned poll finally returns: it must not disturb the new one
	close(hang)
	time.Sleep(20 * time.Millisecond)
	m.pollAll(context.Background())
	if p := profileByID(m.Snapshot(), "h"); p.Level != model.LevelOK || f.get(f.created, "h") != 2 {
		t.Errorf("after the old poll returned: %+v created %d", p, f.get(f.created, "h"))
	}
	if model.MonitorMessage(model.MsgMonitorHung, model.LangRU) == "" {
		t.Error("monitor.hung has no text")
	}
}

// TestCHK_MON_05_SnapshotErrorImmediate — the base (profile) error alert
// is debounced (ProfileRaisePolls = 2) but the snapshot shows the error
// level at once, from the first failed poll.
func TestCHK_MON_05_SnapshotErrorImmediate(t *testing.T) {
	f := newFactory()
	f.set("a", behavior{err: &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "ping", Err: errors.New("down")}})
	m := New(baseOpt(f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a")}}), &fakeClock{t: t0}))
	m.reloadConfig(true)
	m.pollAll(context.Background())
	s := m.Snapshot()
	if p := profileByID(s, "a"); p.Level != model.LevelError || p.ErrorKey != model.MsgMonitorConnError || s.Level != model.LevelError {
		t.Errorf("snapshot after 1st failure %+v", p)
	}
	if len(s.Alerts) != 0 {
		t.Errorf("alert must wait for the 2nd failure: %+v", s.Alerts)
	}
	m.pollAll(context.Background())
	if len(m.Snapshot().Alerts) != 1 {
		t.Error("alert after the 2nd failure")
	}
}

// TestCHK_MON_08_SinglePresetSource — the default preset of new bases
// comes only from model (DefaultServerPreset = ServerPresetMSSQL2008,
// applied by NewProfile via ApplySQLDefaults + ApplyServerPreset); the
// monitor has no copy of the preset or of its defaults.
func TestCHK_MON_08_SinglePresetSource(t *testing.T) {
	if model.DefaultServerPreset != model.ServerPresetMSSQL2008 {
		t.Fatalf("DefaultServerPreset %q", model.DefaultServerPreset)
	}
	want := model.Profile{ID: "x"}
	want.ApplySQLDefaults()
	if err := want.ApplyServerPreset(model.DefaultServerPreset); err != nil {
		t.Fatal(err)
	}
	got := model.NewProfile("x")
	if got.ServerPreset != want.ServerPreset || got.Encrypt != want.Encrypt || got.TLSMinVersion != want.TLSMinVersion ||
		got.Encrypt != model.DefaultEncrypt {
		t.Errorf("NewProfile %+v, want %+v", got, want)
	}
	files, _ := filepath.Glob("*.go")
	for _, fn := range files {
		if strings.HasSuffix(fn, "_test.go") {
			continue
		}
		b, err := os.ReadFile(fn)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{`"mssql2008"`, "ServerPresetMSSQL2008", "ApplyServerPreset", "EncryptDisable", `TLSMinVersion = "1.0"`} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s duplicates the preset (%s): use model.NewProfile / ApplySQLDefaults", fn, bad)
			}
		}
	}
}
