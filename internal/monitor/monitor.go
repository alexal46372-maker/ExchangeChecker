package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"runtime/debug"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// ReaderFactory creates the reader of one profile (sqlcheck.NewReader in
// the service, a fake in tests).
type ReaderFactory func(p model.Profile, password string, opt sqlcheck.Options) (sqlcheck.ReadCloser, error)

// Defaults of Options.
const (
	DefaultConfigCheck   = 5 * time.Second
	DefaultMaxConcurrent = 4
	DefaultMaxJitter     = 2 * time.Second
	// DefaultCommandInterval: poll_now / reload_config from IPC are
	// executed at most once per 10 s (globally and per profile); extra
	// calls are answered "accepted, coalesced" (CHK-MON-12).
	DefaultCommandInterval = model.IPCCommandInterval
	// DefaultConfigWarnEvery: an unchanged config-load failure is logged
	// and reported to the event log at most this often (CHK-MON-10).
	DefaultConfigWarnEvery = 15 * time.Minute
	// HungFactor: a profile busy for more than HungFactor poll timeouts
	// gets its reader closed and recreated (CHK-MON-13).
	HungFactor = 3
)

// ErrUnknownProfile is returned by PollNow for a profile ID that is not
// in the current configuration.
var ErrUnknownProfile = errors.New("monitor: unknown profile")

// Options configure a Monitor. Zero values = defaults.
type Options struct {
	Source    ConfigSource
	NewReader ReaderFactory // default sqlcheck.NewReader
	// Counters keeps the counters-mode state (FileCounterStore in the
	// service); nil = in-memory.
	Counters model.CounterStateStore
	Log      Logger
	// Sink receives confirmed alert events (event log, log file, IPC).
	Sink   alert.Sink
	Policy alert.Policy // zero = alert.DefaultPolicy()
	// Now is the clock (tests); nil = time.Now. Snapshot times are UTC+3.
	Now func() time.Time
	// PollInterval overrides settings.pollIntervalSec (tests).
	PollInterval time.Duration
	// ConfigCheck is how often config files are stat'ed (default 5 s).
	ConfigCheck time.Duration
	// MaxConcurrent bounds the profiles polled at the same time (default 4).
	MaxConcurrent int
	// Jitter is the max random start delay of each profile in a poll
	// (default min(2 s, interval/10)); negative = none.
	Jitter time.Duration
	// ProfileTimeout is the time budget of one profile's poll; nil =
	// connect timeout + query timeout × (mappings + 1).
	ProfileTimeout func(p model.Profile) time.Duration
	// OnSnapshot is called (outside locks) with every new snapshot.
	OnSnapshot func(model.StatusSnapshot)
	// Rand returns a value in [0, n) (tests); nil = math/rand.
	Rand func(n int64) int64
	// Warn receives operator warnings that belong in the Windows Event Log
	// (config load failures, hung readers); already rate-limited. nil =
	// log file only.
	Warn func(msg string)
	// CommandInterval is the minimum period between two executed
	// poll_now / reload_config commands (default 10 s; negative = none).
	CommandInterval time.Duration
	// ConfigWarnEvery is the repeat period of an unchanged config-load
	// warning (default 15 min).
	ConfigWarnEvery time.Duration
}

// entry is the per-profile runtime state.
type entry struct {
	profile  model.Profile
	password string
	sig      string

	mu        sync.Mutex
	busy      bool
	busySince time.Time
	gen       uint64 // incremented when a hung reader is abandoned
	retired   bool
	reader    sqlcheck.ReadCloser
}

// acquire marks the entry busy and returns its generation; false if a
// poll is still running (busySince then tells for how long).
func (e *entry) acquire(now time.Time) (uint64, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.busy {
		return 0, false
	}
	e.busy, e.busySince = true, now
	return e.gen, true
}

// release ends the poll of generation gen; a retired entry closes its
// reader now. A poll abandoned by abandonHung (older gen) changes nothing.
func (e *entry) release(gen uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if gen != e.gen {
		return
	}
	e.busy = false
	if e.retired {
		e.closeLocked()
	}
}

// abandonHung frees an entry whose poll has been running longer than
// limit: the reader is detached and closed in the background (sql.DB.Close
// waits for running queries, so it must not block the monitor), the busy
// flag is cleared and the next poll creates a new reader. It returns
// false when the entry is not hung (CHK-MON-13).
func (e *entry) abandonHung(now time.Time, limit time.Duration) (time.Duration, bool) {
	e.mu.Lock()
	since := now.Sub(e.busySince)
	if !e.busy || since <= limit {
		e.mu.Unlock()
		return since, false
	}
	old := e.reader
	e.reader, e.busy = nil, false
	e.gen++
	e.mu.Unlock()
	if old != nil {
		go func() { _ = old.Close() }()
	}
	return since, true
}

// retire closes the reader now, or after the running poll.
func (e *entry) retire() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.retired = true
	if !e.busy {
		e.closeLocked()
	}
}

func (e *entry) closeLocked() {
	if e.reader != nil {
		_ = e.reader.Close()
		e.reader = nil
	}
}

// Monitor polls all profiles and keeps the status snapshot.
type Monitor struct {
	opt     Options
	tracker *alert.Tracker
	pollNow chan struct{}
	reload  chan struct{}

	mu       sync.Mutex
	cfg      Config
	stamp    string
	loaded   bool  // a snapshot exists (after the first load attempt)
	good     bool  // a configuration was applied successfully
	retry    bool  // the last load failed: reload on the next check
	cfgFatal error // last load failure while the last good config is used
	warnMsg  string
	warnAt   time.Time
	entries  map[string]*entry
	results  map[string]model.ProfileSnapshot
	seq      uint64
	lastPoll time.Time
	snap     model.StatusSnapshot

	cmdMu   sync.Mutex
	cmdLast map[string]time.Time // rate limiter of IPC commands
	pendAll bool                 // poll_now for every profile pending
	pendIDs map[string]bool      // poll_now for single profiles pending
}

// New returns a Monitor; call Run to start it.
func New(opt Options) *Monitor {
	if opt.NewReader == nil {
		opt.NewReader = sqlcheck.NewReader
	}
	if opt.Counters == nil {
		opt.Counters = model.NewMemCounterStore()
	}
	if opt.Log == nil {
		opt.Log = nopLogger{}
	}
	if opt.Policy == (alert.Policy{}) {
		opt.Policy = alert.DefaultPolicy()
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.ConfigCheck <= 0 {
		opt.ConfigCheck = DefaultConfigCheck
	}
	if opt.MaxConcurrent <= 0 {
		opt.MaxConcurrent = DefaultMaxConcurrent
	}
	if opt.Rand == nil {
		opt.Rand = rand.Int63n
	}
	if opt.Source == nil {
		opt.Source = NewMemConfig(Config{Settings: model.DefaultSettings()})
	}
	if opt.CommandInterval == 0 {
		opt.CommandInterval = DefaultCommandInterval
	}
	if opt.ConfigWarnEvery <= 0 {
		opt.ConfigWarnEvery = DefaultConfigWarnEvery
	}
	return &Monitor{
		opt: opt, tracker: alert.NewTracker(opt.Policy),
		pollNow: make(chan struct{}, 1), reload: make(chan struct{}, 1),
		entries: map[string]*entry{}, results: map[string]model.ProfileSnapshot{},
		cmdLast: map[string]time.Time{}, pendIDs: map[string]bool{},
	}
}

// Rate-limiter keys of the IPC commands.
const (
	cmdKeyPollAll = "poll_now"
	cmdKeyReload  = "reload_config"
)

// allowLocked reports whether a command may run now: neither key nor any
// of also was executed within CommandInterval. On success key is stamped
// (cmdMu held).
func (m *Monitor) allowLocked(key string, also ...string) bool {
	if m.opt.CommandInterval < 0 {
		return true
	}
	now := m.opt.Now()
	for _, k := range append([]string{key}, also...) {
		if t, ok := m.cmdLast[k]; ok {
			if d := now.Sub(t); d >= 0 && d < m.opt.CommandInterval {
				return false
			}
		}
	}
	m.cmdLast[key] = now
	return true
}

// PollNow asks the loop to poll immediately (IPC "poll_now"): every
// profile for profileID "", otherwise that profile only. At most one
// executed command per CommandInterval globally and per profile (a global
// poll also covers every profile); coalesced == true means the request
// was accepted but merged into a recent / pending poll (CHK-MON-12).
func (m *Monitor) PollNow(profileID string) (coalesced bool, err error) {
	if profileID != "" {
		m.mu.Lock()
		_, ok := m.entries[profileID]
		m.mu.Unlock()
		if !ok {
			return false, ErrUnknownProfile
		}
	}
	m.cmdMu.Lock()
	if profileID == "" {
		if !m.allowLocked(cmdKeyPollAll) {
			m.cmdMu.Unlock()
			return true, nil
		}
		m.pendAll = true
	} else {
		if !m.allowLocked("poll:"+profileID, cmdKeyPollAll) {
			m.cmdMu.Unlock()
			return true, nil
		}
		m.pendIDs[profileID] = true
	}
	m.cmdMu.Unlock()
	select {
	case m.pollNow <- struct{}{}:
	default:
	}
	return false, nil
}

// takePending returns the pending poll_now request: nil = every profile.
func (m *Monitor) takePending() map[string]bool {
	m.cmdMu.Lock()
	defer m.cmdMu.Unlock()
	all, ids := m.pendAll, m.pendIDs
	m.pendAll, m.pendIDs = false, map[string]bool{}
	if all || len(ids) == 0 {
		return nil
	}
	return ids
}

// ReloadConfig asks the loop to re-read the config and poll (IPC
// "reload_config"), at most once per CommandInterval; coalesced == true
// means a reload ran recently and this request was merged (CHK-MON-12).
func (m *Monitor) ReloadConfig() (coalesced bool) {
	m.cmdMu.Lock()
	ok := m.allowLocked(cmdKeyReload)
	m.cmdMu.Unlock()
	if !ok {
		return true
	}
	select {
	case m.reload <- struct{}{}:
	default:
	}
	return false
}

// Snapshot returns the latest status snapshot.
func (m *Monitor) Snapshot() model.StatusSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded {
		return model.StatusSnapshot{Now: m.opt.Now().In(model.DBZone), Profiles: []model.ProfileSnapshot{}}
	}
	return m.snap
}

// interval returns the poll period.
func (m *Monitor) interval() time.Duration {
	if m.opt.PollInterval > 0 {
		return m.opt.PollInterval
	}
	m.mu.Lock()
	d := m.cfg.Settings.PollInterval()
	m.mu.Unlock()
	if d <= 0 {
		d = model.DefaultPollIntervalSec * time.Second
	}
	return d
}

// Run polls until ctx is canceled, then closes every reader. It returns
// nil on a normal stop.
func (m *Monitor) Run(ctx context.Context) error {
	m.reloadConfig(true)
	tick := time.NewTicker(m.opt.ConfigCheck)
	defer tick.Stop()
	defer m.closeAll()
	var only map[string]bool
	for {
		m.pollSome(ctx, only)
		only = nil
		if ctx.Err() != nil {
			return nil
		}
		timer := time.NewTimer(m.interval())
	wait:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
				break wait
			case <-m.pollNow:
				timer.Stop()
				only = m.takePending()
				break wait
			case <-m.reload:
				timer.Stop()
				m.takePending()
				m.reloadConfig(true)
				break wait
			case <-tick.C:
				if m.reloadConfig(false) {
					timer.Stop()
					break wait
				}
			}
		}
	}
}

// profileSig identifies everything that requires a new reader.
func profileSig(p model.Profile, pw string, s model.Settings) string {
	b, _ := json.Marshal(struct {
		P  model.Profile
		PW string
		S  model.Settings
	}{p, pw, p.EffectiveSettings(s)})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// reloadConfig re-reads the config when forced, when the stamp changed or
// when the previous load failed. Profiles whose connection/mapping/
// thresholds are unchanged keep their reader (connection pool); others
// get a new one. A failed load (Config.Fatal) changes nothing but the
// snapshot's config error (CHK-MON-10). It returns true when a new
// configuration was applied.
func (m *Monitor) reloadConfig(force bool) bool {
	stamp := m.opt.Source.Stamp()
	m.mu.Lock()
	same := m.good && stamp == m.stamp && !m.retry
	m.mu.Unlock()
	if same && !force {
		return false
	}
	cfg := m.opt.Source.Load()
	if cfg.Fatal != nil {
		m.configFailed(cfg.Fatal)
		return false
	}
	if cfg.Settings.WarnMinutes == 0 && cfg.Settings.ErrorMinutes == 0 {
		cfg.Settings = model.DefaultSettings()
	}
	var retired []*entry
	m.mu.Lock()
	next := make(map[string]*entry, len(cfg.Profiles))
	ids := make(map[string]bool, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		ids[p.ID] = true
		pw := cfg.Passwords[p.ID]
		sig := profileSig(p, pw, cfg.Settings)
		if old, ok := m.entries[p.ID]; ok && old.sig == sig {
			next[p.ID] = old
			continue
		}
		next[p.ID] = &entry{profile: p, password: pw, sig: sig}
	}
	for id, old := range m.entries {
		if next[id] != old {
			retired = append(retired, old)
			delete(m.results, id)
		}
	}
	recovered := m.retry
	m.entries, m.cfg, m.stamp, m.loaded, m.good = next, cfg, stamp, true, true
	m.retry, m.cfgFatal, m.warnMsg = false, nil, ""
	m.mu.Unlock()
	for _, e := range retired {
		e.retire()
	}
	// counters state and alerts are pruned only after a successful load
	if r, ok := m.opt.Counters.(interface{ Retain(map[string]bool) error }); ok {
		_ = r.Retain(ids)
	}
	m.tracker.Retain(func(key string) bool { return ids[alert.KeyProfile(key)] })
	if recovered {
		m.opt.Log.Printf("config: loaded successfully again")
	}
	if cfg.Err != nil {
		m.opt.Log.Printf("config: loaded with errors: %v", cfg.Err)
	}
	m.opt.Log.Printf("config: %d profile(s), poll interval %s", len(cfg.Profiles), m.interval())
	m.publish(nil)
	return true
}

// configFailed keeps the last good configuration after a failed load:
// no reader, counters state or alert is touched; the snapshot reports the
// error; the warning goes to the log (and Warn -> event log) once per
// distinct error and then at most every ConfigWarnEvery; the next config
// check retries.
func (m *Monitor) configFailed(err error) {
	now := m.opt.Now()
	msg := err.Error()
	m.mu.Lock()
	m.retry, m.cfgFatal = true, err
	first := !m.loaded
	m.loaded = true
	good := m.good
	warn := msg != m.warnMsg || now.Sub(m.warnAt) >= m.opt.ConfigWarnEvery || now.Before(m.warnAt)
	if warn {
		m.warnMsg, m.warnAt = msg, now
	}
	m.mu.Unlock()
	if warn {
		text := "config: cannot load the configuration, keeping the last good one: " + msg
		if !good {
			text = "config: cannot load the configuration (no previous good one, nothing is polled): " + msg
		}
		m.opt.Log.Printf("%s", text)
		if m.opt.Warn != nil {
			m.opt.Warn(text)
		}
	}
	if first || warn {
		m.publish(nil)
	}
}

func (m *Monitor) closeAll() {
	m.mu.Lock()
	es := make([]*entry, 0, len(m.entries))
	for _, e := range m.entries {
		es = append(es, e)
	}
	m.mu.Unlock()
	for _, e := range es {
		e.retire()
	}
}

// jitterFor returns the start delay of one profile in a poll.
func (m *Monitor) jitterFor(interval time.Duration) time.Duration {
	max := m.opt.Jitter
	if max < 0 {
		return 0
	}
	if max == 0 {
		max = interval / 10
		if max > DefaultMaxJitter {
			max = DefaultMaxJitter
		}
	}
	if max <= 0 {
		return 0
	}
	return time.Duration(m.opt.Rand(int64(max)))
}

// profileTimeout is the time budget of one profile's poll.
func (m *Monitor) profileTimeout(p model.Profile) time.Duration {
	if m.opt.ProfileTimeout != nil {
		return m.opt.ProfileTimeout(p)
	}
	n := len(p.AllMappings()) + 1
	return p.ConnectTimeout() + time.Duration(n)*p.QueryTimeout()
}

// pollAll polls every enabled profile once (bounded concurrency, jitter)
// and publishes the snapshot. Nothing is published when ctx was canceled.
func (m *Monitor) pollAll(ctx context.Context) { m.pollSome(ctx, nil) }

// pollSome polls the enabled profiles in only (nil = all).
func (m *Monitor) pollSome(ctx context.Context, only map[string]bool) {
	m.mu.Lock()
	var es []*entry
	for _, p := range m.cfg.Profiles {
		if only != nil && !only[p.ID] {
			continue
		}
		if e := m.entries[p.ID]; e != nil && p.Enabled {
			es = append(es, e)
		}
	}
	settings := m.cfg.Settings
	m.mu.Unlock()
	interval := m.interval()
	sem := make(chan struct{}, m.opt.MaxConcurrent)
	results := make([]*model.ProfileStatus, len(es))
	var wg sync.WaitGroup
	for i, e := range es {
		wg.Add(1)
		go func(i int, e *entry) {
			defer wg.Done()
			if d := m.jitterFor(interval); d > 0 {
				t := time.NewTimer(d)
				select {
				case <-t.C:
				case <-ctx.Done():
					t.Stop()
					return
				}
			}
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			ps := m.pollOne(ctx, e, settings)
			results[i] = &ps
		}(i, e)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return
	}
	m.publish(func(res map[string]model.ProfileSnapshot, cfg Config, now time.Time) map[string]bool {
		polled := make(map[string]bool, len(es))
		for i, e := range es {
			if results[i] == nil {
				continue
			}
			res[e.profile.ID] = model.NewProfileSnapshot(e.profile, *results[i], cfg.Settings, now)
			polled[e.profile.ID] = true
		}
		return polled
	})
}

// errRecorder remembers the last ListPoints error (for ErrorKey).
type errRecorder struct {
	sqlcheck.ExchangeReader
	err error
}

func (r *errRecorder) ListPoints(ctx context.Context) ([]model.ExchangePoint, error) {
	pts, err := r.ExchangeReader.ListPoints(ctx)
	r.err = err
	return pts, err
}

// ErrorKeyOf maps a reader error to a monitor.* message key.
func ErrorKeyOf(err error) string {
	switch sqlcheck.KindOf(err) {
	case sqlcheck.KindConn:
		return model.MsgMonitorConnError
	case sqlcheck.KindTimeout, sqlcheck.KindCanceled:
		return model.MsgMonitorTimeout
	case sqlcheck.KindQuery:
		return model.MsgMonitorQueryError
	case sqlcheck.KindMapping:
		return model.MsgMonitorMapping
	case sqlcheck.KindConfig:
		return model.MsgMonitorConfig
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return model.MsgMonitorTimeout
	}
	return model.MsgMonitorPollError
}

// pollOne polls one profile in its own goroutine with its own deadline;
// a panic is recovered and a reader that ignores the deadline is
// abandoned (the entry stays busy until it returns).
func (m *Monitor) pollOne(ctx context.Context, e *entry, settings model.Settings) model.ProfileStatus {
	p := e.profile
	now := m.opt.Now()
	fail := func(msg, key string) model.ProfileStatus {
		return model.ProfileStatus{ProfileID: p.ID, Name: p.DisplayName(), CheckedAt: now.In(model.DBZone),
			Error: msg, ErrorKey: key}
	}
	timeout := m.profileTimeout(p)
	gen, ok := e.acquire(now)
	if !ok {
		if since, hung := e.abandonHung(now, HungFactor*timeout); hung {
			msg := fmt.Sprintf("profile %s: poll hung for %s (> %d x %s), reader closed and will be recreated",
				p.ID, since.Round(time.Second), HungFactor, timeout)
			m.opt.Log.Printf("%s", msg)
			if m.opt.Warn != nil {
				m.opt.Warn(msg)
			}
			return fail(fmt.Sprintf("poll hung for %s; connection closed, reopening", since.Round(time.Second)), model.MsgMonitorHung)
		}
		m.opt.Log.Printf("profile %s: previous poll still running, skipped", p.ID)
		return fail("previous poll is still running", model.MsgMonitorBusy)
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resc := make(chan model.ProfileStatus, 1)
	go func() {
		defer e.release(gen)
		defer func() {
			if r := recover(); r != nil {
				m.opt.Log.Printf("profile %s: panic while polling: %v\n%s", p.ID, r, debug.Stack())
				resc <- fail(fmt.Sprintf("internal error: %v", r), model.MsgMonitorPanic)
			}
		}()
		e.mu.Lock()
		r := e.reader
		e.mu.Unlock()
		if r == nil {
			var err error
			r, err = m.opt.NewReader(p, e.password, sqlcheck.Options{
				Settings: p.EffectiveSettings(settings), Now: m.opt.Now, Counters: m.opt.Counters})
			if err != nil {
				resc <- fail(err.Error(), ErrorKeyOf(err))
				return
			}
			e.mu.Lock()
			if e.gen != gen { // abandoned as hung while connecting
				e.mu.Unlock()
				_ = r.Close()
				resc <- fail("poll abandoned", model.MsgMonitorHung)
				return
			}
			e.reader = r
			e.mu.Unlock()
		}
		rec := &errRecorder{ExchangeReader: r}
		ps := sqlcheck.Status(cctx, rec, p, now)
		if ps.Error != "" && ps.ErrorKey == "" {
			ps.ErrorKey = ErrorKeyOf(rec.err)
		}
		resc <- ps
	}()
	select {
	case ps := <-resc:
		return ps
	case <-cctx.Done():
		if ctx.Err() != nil {
			return fail("canceled", model.MsgMonitorTimeout)
		}
		m.opt.Log.Printf("profile %s: poll timed out after %s", p.ID, timeout)
		return fail(fmt.Sprintf("poll timed out after %s", timeout), model.MsgMonitorTimeout)
	}
}

// publish rebuilds the snapshot (update fills new poll results), feeds
// the alert tracker and emits events + the snapshot outside the lock.
// update returns the IDs it polled: only those feed the alert tracker (a
// single-profile poll_now must not advance the debounce of the others).
func (m *Monitor) publish(update func(res map[string]model.ProfileSnapshot, cfg Config, now time.Time) map[string]bool) {
	now := m.opt.Now().In(model.DBZone)
	var events []model.AlertEvent
	var polled map[string]bool
	m.mu.Lock()
	cfg := m.cfg
	if update != nil {
		polled = update(m.results, cfg, now)
		m.lastPoll = now
	}
	profiles := make([]model.ProfileSnapshot, 0, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		ps, ok := m.results[p.ID]
		if !ok || !p.Enabled {
			ps = model.PendingProfileSnapshot(p, cfg.Settings)
		}
		model.SortPoints(ps.Points)
		ps.Warnings = sqlcheck.Warnings(p)
		profiles = append(profiles, ps)
		if ok && p.Enabled && polled[p.ID] {
			events = append(events, m.observe(ps, now)...)
		}
	}
	if update != nil {
		m.tracker.Retain(m.keepKeys(profiles))
	}
	m.seq++
	snap := model.StatusSnapshot{
		Seq: m.seq, Now: now, Level: model.WorstProfileLevel(profiles), Profiles: profiles,
		Alerts: m.tracker.Active(), PollIntervalSec: int(m.intervalLocked() / time.Second),
		LastPollAt: m.lastPoll,
	}
	if cfg.Err != nil {
		snap.ConfigError = cfg.Err.Error()
	}
	if m.cfgFatal != nil {
		snap.ConfigError = m.cfgFatal.Error()
		snap.ConfigErrorKey = model.MsgMonitorConfigStale
		snap.ConfigStale = m.good
	}
	m.snap = snap
	m.mu.Unlock()
	for _, ev := range events {
		m.opt.Log.Printf("alert #%d: %s", ev.ID, alert.Format(ev, model.LangEN))
		if m.opt.Sink != nil {
			m.opt.Sink.Alert(ev)
		}
	}
	if m.opt.OnSnapshot != nil {
		m.opt.OnSnapshot(snap)
	}
}

func (m *Monitor) intervalLocked() time.Duration {
	if m.opt.PollInterval > 0 {
		return m.opt.PollInterval
	}
	if d := m.cfg.Settings.PollInterval(); d > 0 {
		return d
	}
	return model.DefaultPollIntervalSec * time.Second
}

// observe feeds one profile snapshot into the tracker. A profile error is
// a profile-level alert; its points keep their last confirmed level (the
// points are unknown while the database is unreachable).
func (m *Monitor) observe(ps model.ProfileSnapshot, now time.Time) []model.AlertEvent {
	var out []model.AlertEvent
	lvl := model.LevelOK
	if ps.Error != "" {
		lvl = model.LevelError
	}
	if ev, ok := m.tracker.Observe(alert.Observation{Key: alert.ProfileKey(ps.ProfileID), Profile: true, Level: lvl,
		Event: model.AlertEvent{ProfileID: ps.ProfileID, ProfileName: ps.Name, Error: ps.Error, ErrorKey: ps.ErrorKey}}, now); ok {
		out = append(out, ev)
	}
	if ps.Error != "" {
		return out
	}
	for _, pt := range ps.Points {
		tmpl := model.AlertEvent{ProfileID: ps.ProfileID, ProfileName: ps.Name, PointName: pt.Name,
			PlanTable: pt.PlanTable, FreshAt: pt.FreshAt, Baseline: pt.Baseline}
		if pt.LagMinutes > 0 {
			tmpl.LagMinutes = pt.LagMinutes
		}
		if pt.FreshAt == nil {
			tmpl.Error = pt.Message
		}
		if ev, ok := m.tracker.Observe(alert.Observation{Key: alert.PointKey(ps.ProfileID, model.PointKey(pt.ExchangePoint)),
			Level: pt.Level, Event: tmpl}, now); ok {
			out = append(out, ev)
		}
	}
	return out
}

// keepKeys: tracker keys of existing enabled profiles/points; points of a
// profile in error (or not polled yet) are kept as they are.
func (m *Monitor) keepKeys(profiles []model.ProfileSnapshot) func(string) bool {
	keep := map[string]bool{}
	whole := map[string]bool{}
	for _, ps := range profiles {
		if !ps.Enabled { // disabled profile: its alerts are dropped
			continue
		}
		keep[alert.ProfileKey(ps.ProfileID)] = true
		if ps.Error != "" || !ps.Polled {
			whole[ps.ProfileID] = true
			continue
		}
		for _, pt := range ps.Points {
			keep[alert.PointKey(ps.ProfileID, model.PointKey(pt.ExchangePoint))] = true
		}
	}
	return func(k string) bool { return keep[k] || whole[alert.KeyProfile(k)] }
}
