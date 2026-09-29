package monitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	if len(s.Profiles) != 0 || s.ConfigError == "" || s.ConfigStale || s.ConfigErrorKey != model.MsgMonitorConfigInvalid {
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

// TestCHK_MON_10_SettingsFile — settings.json like profiles.json:
// missing = defaults; broken or unreadable after a good load = the last
// good settings stay, configStale, one warning (anti-spam), retry on the
// next check, profiles are still applied and polled; fixed = applied.
func TestCHK_MON_10_SettingsFile(t *testing.T) {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	if err := st.SaveProfiles([]model.Profile{prof("a")}); err != nil {
		t.Fatal(err)
	}
	f := newFactory()
	f.set("a", behavior{})
	f.set("b", behavior{})
	clk := &fakeClock{t: t0}
	lg := &memLog{}
	w := &warnRec{}
	opt := baseOpt(f, FileConfig{Store: st}, clk)
	opt.PollInterval = 0 // from settings
	opt.Log, opt.Warn = lg, w.warn
	m := New(opt)
	interval := func() int { return m.Snapshot().PollIntervalSec }
	writeSettings := func(body string) {
		t.Helper()
		time.Sleep(15 * time.Millisecond) // new mtime
		os.RemoveAll(st.Path(config.SettingsFile))
		if err := os.WriteFile(st.Path(config.SettingsFile), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 1. missing file = defaults, no error
	if c := (FileConfig{Store: st}).Load(); c.SettingsFatal != nil || c.Err != nil {
		t.Fatalf("missing settings: %+v", c)
	}
	m.reloadConfig(true)
	if s := m.Snapshot(); interval() != 60 || s.ConfigError != "" || s.ConfigStale {
		t.Fatalf("defaults: %+v", s)
	}
	// 2. good file
	writeSettings(`{"version":1,"pollIntervalSec":30}`)
	if !m.reloadConfig(false) || interval() != 30 {
		t.Fatalf("good settings: %d", interval())
	}
	stale := func(stage string) {
		t.Helper()
		s := m.Snapshot()
		if interval() != 30 || !s.ConfigStale || s.ConfigErrorKey != model.MsgMonitorConfigStale || s.ConfigError == "" {
			t.Errorf("%s: snapshot %+v", stage, s)
		}
		before := f.get(f.calls, "a")
		m.pollAll(context.Background())
		if f.get(f.calls, "a") != before+1 || f.get(f.created, "a") != 1 {
			t.Errorf("%s: polling blocked or reader recreated", stage)
		}
	}
	// 3. broken JSON after a good load
	writeSettings(`{"version":1,"pollIntervalSec":`)
	if c := (FileConfig{Store: st}).Load(); c.SettingsFatal == nil || c.Fatal != nil {
		t.Fatalf("broken settings: %+v", c)
	}
	m.reloadConfig(false)
	stale("broken json")
	if w.n() != 1 || !strings.Contains(w.last(), "keeping the last good settings") || !lg.has("settings.json") {
		t.Fatalf("warning %v", w.msgs)
	}
	// retry on the next check: no new warning, nothing re-applied
	if m.reloadConfig(false) {
		t.Error("a retry with the same broken file must not re-apply")
	}
	stale("retry")
	if w.n() != 1 {
		t.Errorf("warning repeated: %v", w.msgs)
	}
	clk.Add(DefaultConfigWarnEvery + time.Second)
	m.reloadConfig(false)
	if w.n() != 2 {
		t.Errorf("warning not repeated after the anti-spam period: %d", w.n())
	}
	// 4. unreadable (a directory) + a profile added at the same time
	time.Sleep(15 * time.Millisecond)
	os.Remove(st.Path(config.SettingsFile))
	if err := os.Mkdir(st.Path(config.SettingsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProfiles([]model.Profile{prof("a"), prof("b")}); err != nil {
		t.Fatal(err)
	}
	m.reloadConfig(false)
	stale("unreadable")
	if len(m.Snapshot().Profiles) != 2 || !profileByID(m.Snapshot(), "b").Polled {
		t.Errorf("profiles must still be applied with broken settings: %+v", m.Snapshot().Profiles)
	}
	if w.n() != 3 {
		t.Errorf("new error must warn at once: %v", w.msgs)
	}
	// 5. fixed
	writeSettings(`{"version":1,"pollIntervalSec":45}`)
	if !m.reloadConfig(false) {
		t.Fatal("fixed settings not applied")
	}
	if s := m.Snapshot(); interval() != 45 || s.ConfigStale || s.ConfigError != "" || s.ConfigErrorKey != "" {
		t.Errorf("after fix: %+v", s)
	}
	if !lg.has("loaded successfully again") {
		t.Error("recovery not logged")
	}
	// 6. file removed again = defaults
	time.Sleep(15 * time.Millisecond)
	os.Remove(st.Path(config.SettingsFile))
	m.reloadConfig(false)
	if s := m.Snapshot(); interval() != 60 || s.ConfigError != "" {
		t.Errorf("removed: %+v", s)
	}
}

// TestCHK_MON_10_SettingsBrokenOnFirstStart — a broken settings.json at
// the first start: defaults + warning, polling proceeds (unlike a broken
// profiles.json), not "stale" (there is no last good settings).
func TestCHK_MON_10_SettingsBrokenOnFirstStart(t *testing.T) {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	if err := st.SaveProfiles([]model.Profile{prof("a")}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(st.Path(config.SettingsFile), []byte("garbage"), 0o644)
	f := newFactory()
	f.set("a", behavior{points: lagPoints(map[string]int{"A": 1})})
	w := &warnRec{}
	opt := baseOpt(f, FileConfig{Store: st}, &fakeClock{t: t0})
	opt.PollInterval = 0
	opt.Warn = w.warn
	m := New(opt)
	if !m.reloadConfig(true) {
		t.Fatal("profiles must be applied")
	}
	m.pollAll(context.Background())
	s := m.Snapshot()
	p := profileByID(s, "a")
	if !p.Polled || p.Level != model.LevelOK || s.PollIntervalSec != 60 || p.WarnMinutes != model.DefaultSettings().WarnMinutes {
		t.Errorf("polling with defaults: %+v / %+v", s, p)
	}
	if s.ConfigStale || s.ConfigError == "" || s.ConfigErrorKey != model.MsgMonitorConfigInvalid {
		t.Errorf("snapshot flags %+v", s)
	}
	// the invalid key has its own RU/EN texts, distinct from "stale"
	ru, en := model.MonitorMessage(model.MsgMonitorConfigInvalid, model.LangRU), model.MonitorMessage(model.MsgMonitorConfigInvalid, model.LangEN)
	if ru == "" || en == "" || ru == model.MonitorMessage(model.MsgMonitorConfigStale, model.LangRU) ||
		strings.Contains(en, "last valid") || !strings.Contains(en, "default") {
		t.Errorf("config_invalid texts: %q / %q", ru, en)
	}
	if w.n() != 1 || !strings.Contains(w.last(), "using default settings") {
		t.Errorf("warn %v", w.msgs)
	}
	m.reloadConfig(false) // retry, same error: quiet
	if w.n() != 1 || m.Snapshot().ConfigStale {
		t.Errorf("retry: %v %+v", w.msgs, m.Snapshot())
	}
	time.Sleep(15 * time.Millisecond)
	os.WriteFile(st.Path(config.SettingsFile), []byte(`{"version":1,"pollIntervalSec":20}`), 0o644)
	if !m.reloadConfig(false) || m.Snapshot().PollIntervalSec != 20 || m.Snapshot().ConfigError != "" {
		t.Errorf("after fix %+v", m.Snapshot())
	}
}

// TestCHK_MON_13_AbandonedReaderCannotWriteCounters — a hung counters
// reader is abandoned, a new reader takes over and advances the state;
// when the old reader finally returns with its stale observation, its
// write is rejected (generation check inside the profile lock): the new
// reader's state and times are unchanged.
func TestCHK_MON_13_AbandonedReaderCannotWriteCounters(t *testing.T) {
	dir := t.TempDir()
	store := OpenFileCounterStore(dir, nil)
	f := newFactory()
	hang := make(chan struct{})
	f.set("c", behavior{counter: true, hang: hang})
	f.counters["c"] = []model.CounterObservation{{Key: "k", Name: "N", SentNo: 1, ReceivedNo: 1}} // stale obs
	clk := &fakeClock{t: t0}
	opt := baseOpt(f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("c")}}), clk)
	opt.ProfileTimeout = func(model.Profile) time.Duration { return 50 * time.Millisecond }
	opt.Counters = store
	m := New(opt)
	m.reloadConfig(true)
	m.pollAll(context.Background()) // hangs -> timeout
	clk.Add(200 * time.Millisecond)
	f.set("c", behavior{counter: true})
	m.pollAll(context.Background()) // abandoned as hung
	if k := profileByID(m.Snapshot(), "c").ErrorKey; k != model.MsgMonitorHung {
		t.Fatalf("not abandoned: %q", k)
	}
	f.mu.Lock()
	f.counters["c"] = []model.CounterObservation{{Key: "k", Name: "N", SentNo: 5, ReceivedNo: 5}}
	f.mu.Unlock()
	clk.Add(time.Minute)
	m.pollAll(context.Background()) // new reader: baseline
	f.mu.Lock()
	f.counters["c"] = []model.CounterObservation{{Key: "k", Name: "N", SentNo: 6, ReceivedNo: 6}}
	f.mu.Unlock()
	clk.Add(time.Minute)
	tNew := clk.Now()
	m.pollAll(context.Background()) // SentNo 5 -> 6: fresh time tNew
	if f.get(f.created, "c") != 2 {
		t.Fatalf("readers created %d", f.get(f.created, "c"))
	}
	want, _ := store.Load("c")
	if want.Points["k"].SentNo != 6 || !want.Points["k"].SentChangedAt.Equal(tNew) {
		t.Fatalf("new reader state %+v", want.Points["k"])
	}
	// the abandoned reader returns and tries to write SentNo 1
	close(hang)
	waitFor(t, func() bool { return f.get(f.finished, "c") >= 3 })
	got, _ := store.Load("c")
	if got.Points["k"] != want.Points["k"] {
		t.Fatalf("abandoned reader overwrote the state: %+v, want %+v", got.Points["k"], want.Points["k"])
	}
	if disk, _ := OpenFileCounterStore(dir, nil).Load("c"); disk.Points["k"].SentNo != 6 {
		t.Errorf("file rolled back: %+v", disk.Points["k"])
	}
	// the next poll sees no change: the time stays tNew (a rollback to
	// SentNo 1 would make 6 look like a new change)
	clk.Add(time.Minute)
	m.pollAll(context.Background())
	for _, p := range profileByID(m.Snapshot(), "c").Points {
		if !p.FreshAt.Equal(tNew) {
			t.Errorf("fresh time moved: %v, want %v", p.FreshAt, tNew)
		}
	}
}

// TestCHK_MON_13_CounterStoreProfileLock — FileCounterStore.Update runs
// Load -> apply -> Save under a per-profile lock: concurrent updates of
// one profile are not lost; another profile is not blocked meanwhile.
func TestCHK_MON_13_CounterStoreProfileLock(t *testing.T) {
	s := OpenFileCounterStore(t.TempDir(), nil)
	var wg sync.WaitGroup
	const n = 50
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.Update("p", func(prev model.CounterState) (model.CounterState, error) {
				pt := prev.Points["k"]
				runtime.Gosched() // widen the race window (no timing assumption)
				pt.SentNo++
				prev.Points["k"] = pt
				return prev, nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	// a slow update of p (holding p's lock) does not block q
	block, entered := make(chan struct{}), make(chan struct{})
	go s.Update("p", func(prev model.CounterState) (model.CounterState, error) {
		close(entered)
		<-block
		return prev, errors.New("abort")
	})
	<-entered // p's lock is held now
	done := make(chan struct{})
	go func() {
		s.Update("q", func(prev model.CounterState) (model.CounterState, error) { return prev, nil })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second): // generous: q only needs its own lock
		t.Error("profile q blocked by profile p")
	}
	close(block)
	wg.Wait()
	st, _ := s.Load("p")
	if st.Points["k"].SentNo != n {
		t.Errorf("lost updates: %d of %d", st.Points["k"].SentNo, n)
	}
	// an aborted step saves nothing
	s.Update("p", func(prev model.CounterState) (model.CounterState, error) {
		prev.Points["k"] = model.CounterPoint{SentNo: -1}
		return prev, errors.New("abort")
	})
	if st, _ := s.Load("p"); st.Points["k"].SentNo != n {
		t.Errorf("aborted update saved: %d", st.Points["k"].SentNo)
	}
}

// hungCountersSetup: profile c in counters mode with one saved state
// (SentNo 1), then a poll that hangs holding the stale observation
// SentNo 1 (the reader applies it only when released).
func hungCountersSetup(t *testing.T, dir string) (*Monitor, *fakeFactory, *MemConfig, *fakeClock, *FileCounterStore, chan struct{}) {
	t.Helper()
	store := OpenFileCounterStore(dir, nil)
	f := newFactory()
	f.set("c", behavior{counter: true})
	f.counters["c"] = []model.CounterObservation{{Key: "k", Name: "N", SentNo: 1, ReceivedNo: 1}}
	clk := &fakeClock{t: t0}
	src := NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("c")}})
	opt := baseOpt(f, src, clk)
	opt.ProfileTimeout = func(model.Profile) time.Duration { return 50 * time.Millisecond }
	opt.Counters = store
	m := New(opt)
	m.reloadConfig(true)
	m.pollAll(context.Background()) // state saved: SentNo 1
	hang := make(chan struct{})
	f.set("c", behavior{counter: true, hang: hang})
	m.pollAll(context.Background()) // hangs -> timeout, entry stays busy
	if k := profileByID(m.Snapshot(), "c").ErrorKey; k != model.MsgMonitorTimeout {
		t.Fatalf("hung poll: %q", k)
	}
	return m, f, src, clk, store, hang
}

// TestCHK_MON_13_RetiredEntryCannotWriteCounters — the profile's
// signature changes while its old reader hangs: the old entry is retired
// (not abandoned, gen unchanged), the new entry's reader advances the
// state; the old reader returning late is rejected, so SentNo is not
// rolled back and no false "fresh" time appears.
func TestCHK_MON_13_RetiredEntryCannotWriteCounters(t *testing.T) {
	dir := t.TempDir()
	m, f, src, clk, store, hang := hungCountersSetup(t, dir)
	pc := prof("c")
	pc.Database = "moved" // signature change -> new entry, old one retired
	src.Set(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{pc}})
	f.set("c", behavior{counter: true})
	f.mu.Lock()
	f.counters["c"] = []model.CounterObservation{{Key: "k", Name: "N", SentNo: 5, ReceivedNo: 5}}
	f.mu.Unlock()
	m.reloadConfig(false)
	clk.Add(time.Minute)
	tNew := clk.Now()
	m.pollAll(context.Background()) // new reader: 1 -> 5 = change at tNew
	if f.get(f.created, "c") != 2 {
		t.Fatalf("readers %d", f.get(f.created, "c"))
	}
	want, _ := store.Load("c")
	if want.Points["k"].SentNo != 5 || !want.Points["k"].SentChangedAt.Equal(tNew) {
		t.Fatalf("new state %+v", want.Points["k"])
	}
	close(hang) // the old reader applies SentNo 1 now
	waitFor(t, func() bool { return f.get(f.finished, "c") >= 3 })
	if got, _ := store.Load("c"); got.Points["k"] != want.Points["k"] {
		t.Fatalf("retired reader rolled the state back: %+v", got.Points["k"])
	}
	if disk, _ := OpenFileCounterStore(dir, nil).Load("c"); disk.Points["k"].SentNo != 5 {
		t.Errorf("file rolled back: %+v", disk.Points["k"])
	}
	clk.Add(time.Minute)
	m.pollAll(context.Background()) // unchanged 5: the time stays tNew
	for _, p := range profileByID(m.Snapshot(), "c").Points {
		if !p.FreshAt.Equal(tNew) {
			t.Errorf("false fresh time %v, want %v", p.FreshAt, tNew)
		}
	}
}

// TestCHK_MON_13_RemovedProfileNotResurrected — the profile is removed
// while its reader hangs: Retain drops its state, and the reader
// returning late does not bring it back (memory or file).
func TestCHK_MON_13_RemovedProfileNotResurrected(t *testing.T) {
	dir := t.TempDir()
	m, f, src, _, store, hang := hungCountersSetup(t, dir)
	src.Set(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{}})
	m.reloadConfig(false)
	if hasID(store.ProfileIDs(), "c") {
		t.Fatal("Retain did not drop the removed profile")
	}
	close(hang)
	waitFor(t, func() bool { return f.get(f.finished, "c") >= 2 })
	if hasID(store.ProfileIDs(), "c") {
		t.Error("removed profile resurrected in memory")
	}
	if hasID(OpenFileCounterStore(dir, nil).ProfileIDs(), "c") {
		t.Error("removed profile resurrected in counters-state.json")
	}
	if f.get(f.closed, "c") != 1 {
		t.Errorf("retired reader closed %d times", f.get(f.closed, "c"))
	}
}

// TestCHK_MON_10_InvalidWhenSettingsNeverValid — first start with a
// broken settings.json (defaults), then profiles.json breaks too: there
// never was a valid configuration, so the key is config_invalid (not
// config_stale); the profiles keep being polled with the last applied
// list. Once both files were valid, a later failure is config_stale.
func TestCHK_MON_10_InvalidWhenSettingsNeverValid(t *testing.T) {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	if err := st.SaveProfiles([]model.Profile{prof("a")}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(st.Path(config.SettingsFile), []byte("garbage"), 0o644)
	f := newFactory()
	f.set("a", behavior{})
	opt := baseOpt(f, FileConfig{Store: st}, &fakeClock{t: t0})
	m := New(opt)
	m.reloadConfig(true)
	if s := m.Snapshot(); s.ConfigErrorKey != model.MsgMonitorConfigInvalid || s.ConfigStale {
		t.Fatalf("broken settings on first start: %+v", s)
	}
	time.Sleep(15 * time.Millisecond)
	os.WriteFile(st.Path(config.ProfilesFile), []byte("{broken"), 0o644)
	m.reloadConfig(false)
	s := m.Snapshot()
	if s.ConfigErrorKey != model.MsgMonitorConfigInvalid || s.ConfigStale || len(s.Profiles) != 1 {
		t.Errorf("profiles broken, settings never valid: key %q stale %v profiles %d", s.ConfigErrorKey, s.ConfigStale, len(s.Profiles))
	}
	// both valid once -> a later profiles failure is stale
	time.Sleep(15 * time.Millisecond)
	os.WriteFile(st.Path(config.SettingsFile), []byte(`{"version":1}`), 0o644)
	if err := st.SaveProfiles([]model.Profile{prof("a")}); err != nil {
		t.Fatal(err)
	}
	if !m.reloadConfig(false) || m.Snapshot().ConfigError != "" {
		t.Fatalf("fixed: %+v", m.Snapshot())
	}
	time.Sleep(15 * time.Millisecond)
	os.WriteFile(st.Path(config.ProfilesFile), []byte("{broken"), 0o644)
	m.reloadConfig(false)
	if s := m.Snapshot(); s.ConfigErrorKey != model.MsgMonitorConfigStale || !s.ConfigStale {
		t.Errorf("after a valid load: key %q stale %v", s.ConfigErrorKey, s.ConfigStale)
	}
}
