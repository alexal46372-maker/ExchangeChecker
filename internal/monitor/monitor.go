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
	"sync/atomic"
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
	DefaultConfigCheck = 5 * time.Second
	// DefaultMaxConcurrent = MaxSQLConnections: polls running at the same
	// time, shared by all profile workers (phase 10, CHK-MON-16).
	DefaultMaxConcurrent = MaxSQLConnections
	// MaxSQLConnections is the global semaphore of poll connections (phase
	// 10, CHK-MON-16); interactive / test commands keep their own limits
	// and never queue behind polls.
	MaxSQLConnections = 8
	DefaultMaxJitter  = 2 * time.Second
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

// ErrUnknownProfile is returned by PollNow / AckAlert / TestProfile for a
// profile ID that is not in the current configuration
// (deleted or never saved); IPC code not_found.
var ErrUnknownProfile = &model.IPCError{Code: model.ErrCodeNotFound, Key: model.MsgMonitorUnknownProfile, Message: "monitor: unknown profile"}

// ErrRateLimited: test_profile refused by the command rate limit (IPC
// code rate_limited).
var ErrRateLimited = &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgMonitorRateLimited, Message: "monitor: too many requests"}

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
	// MaxConcurrent bounds the profiles polled at the same time (default
	// and maximum MaxSQLConnections = 8; tests use less).
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

	// ConfigStore is written by save_profile / delete_profile /
	// save_settings and read by get_config (phase 6); nil = the store of
	// a FileConfig Source.
	ConfigStore ConfigWriter
	// OnConfigChange is called after every configuration change made
	// through IPC (the service writes event 105, CHK-MON-05).
	OnConfigChange func(model.ConfigChange)
	// ApplySettings is called after save_settings has written
	// settings.json, under the configuration write lock (two saves are
	// applied in order): the service applies trayAutostart to HKLM Run
	// (CHK-CFG-04). An error does not undo the save and is not sent to
	// the client: it goes to the log and to Warn (PLAN §6.2).
	ApplySettings func(model.Settings) error

	// Acks keeps the alarm acknowledgements (FileAckStore in the
	// service); nil = in-memory (CHK-IPC-02).
	Acks AckStore
	// OnAck is called for every newly acknowledged alert (the service
	// writes one informational Event Log entry).
	OnAck func(model.AlertEvent)

	// Connection test of test_profile (CHK-IPC-03): Test (default
	// SQLTest = sqlcheck.TestConnectionInfo), TestCategory (default
	// sqlcheck.ConnCategory), TestTimeout (default 15 s), TestMax
	// concurrent tests (default 2), TestWithTimeout derives the test
	// context carrying the single test deadline (default
	// context.WithTimeout; tests inject a fake-clock context).
	Test            TestFunc
	TestCategory    func(error) string
	TestTimeout     time.Duration
	TestMax         int
	TestWithTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)

	// Interactive commands discover / discover_by_value /
	// list_node_points / check_mapping (phase 6, CHK-IPC-08/09):
	// Interactive (default SQLInteractive = sqlcheck.NewInteractiveReader),
	// DiscoverTimeout (default 90 s), InteractiveLimit (hard limit,
	// default 100 s), InteractiveWithTimeout (tests).
	Interactive            InteractiveFactory
	DiscoverTimeout        time.Duration
	InteractiveLimit       time.Duration
	InteractiveWithTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)

	// RunID identifies this run of the service in the heartbeat and in
	// every AlertEvent (phase 9, CHK-SVC-05); "" = random.
	RunID string

	// OnAlertRepeat receives the alert_repeat event after every failing
	// check of an active alert that was no transition (phase 10,
	// CHK-MON-17; kind alert_repeat, own id, repeatOf; also for
	// acknowledged alerts): the service pushes it as MsgAlertRepeat to
	// the IPC clients only (not the Event Log, log file, history or
	// events_since).
	OnAlertRepeat func(model.AlertEvent)
	// ViberStatus is settings.viber.status of the views (phase 10,
	// CHK-SVC-06; notify.Notifier.Status); nil = none.
	ViberStatus func() *model.ViberStatus
	// OnViberToken is called after save_settings changed the Viber token
	// (the notifier reloads it).
	OnViberToken func()
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

// busyNow reports whether a poll of the entry is running.
func (e *entry) busyNow() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.busy
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

// errAbandoned: the counters write of a reader abandoned as hung, or of a
// retired entry (profile changed or removed), is rejected (CHK-MON-13).
var errAbandoned = errors.New("monitor: reader abandoned or profile retired, counters state not written")

// genCounters is the counters store handed to one reader: every write is
// accepted only while the entry is still in the reader's generation AND
// not retired (profile signature/settings change, profile removed,
// service stop). The check runs inside the per-profile critical section
// of the underlying store (or the monitor's per-profile lock), right
// before the write, so a hung reader that returns late can never
// overwrite its successor's state with a stale observation nor resurrect
// a removed profile in counters-state.json (CHK-MON-13).
type genCounters struct {
	inner model.CounterStateStore
	e     *entry
	gen   uint64
	locks *keyedLocks // per-profile locks for stores without Update
}

// current: the reader may still write. retired is a one-way flag of the
// entry object (a changed profile gets a new entry), gen covers the
// abandoned-hung case within the same entry.
func (g genCounters) current() bool {
	g.e.mu.Lock()
	defer g.e.mu.Unlock()
	return !g.e.retired && g.e.gen == g.gen
}

// Load implements model.CounterStateStore.
func (g genCounters) Load(id string) (model.CounterState, error) { return g.inner.Load(id) }

// Save implements model.CounterStateStore (generation-checked).
func (g genCounters) Save(id string, st model.CounterState) error {
	return g.Update(id, func(model.CounterState) (model.CounterState, error) { return st, nil })
}

// Update implements model.CounterStateUpdater: the generation is checked
// inside the store's per-profile critical section, right before the write.
func (g genCounters) Update(id string, fn func(model.CounterState) (model.CounterState, error)) error {
	guarded := func(prev model.CounterState) (model.CounterState, error) {
		if !g.current() {
			return model.CounterState{}, errAbandoned
		}
		return fn(prev)
	}
	if u, ok := g.inner.(model.CounterStateUpdater); ok {
		return u.Update(id, guarded)
	}
	defer g.locks.lock(id)()
	return model.UpdateCounterState(g.inner, id, guarded)
}

// Monitor polls all profiles and keeps the status snapshot.
type Monitor struct {
	// initLoaded: the first configuration load is done (LoadConfig before
	// the IPC server starts, or Run).
	initLoaded int32
	opt        Options
	tracker    *alert.Tracker
	pollNow    chan struct{}
	reload     chan struct{}

	mu       sync.Mutex
	cfg      Config
	stamp    string
	loaded   bool  // a snapshot exists (after the first load attempt)
	good     bool  // profiles.json + secrets were applied successfully once
	retry    bool  // the last load failed: reload on the next check
	cfgFatal error // last load failure while the last good config is used
	stale    bool  // cfgFatal set and a last good config/settings is in use
	setGood  bool  // settings.json was loaded successfully at least once
	// failSettings: settings.json as parsed by a failed FIRST load
	// (profiles.json / secrets broken, no good configuration yet); nil when
	// settings.json itself failed too. Shown in get_status instead of zero
	// settings (Вика, 49d54ea..c163d0f).
	failSettings *model.Settings
	warnMsg      string
	warnAt       time.Time
	entries      map[string]*entry
	results      map[string]model.ProfileSnapshot
	seq          uint64
	lastPoll     time.Time
	snap         model.StatusSnapshot
	// retired entries whose poll was still running when they were
	// retired; reaped (abandonHung) like live entries (CHK-MON-13)
	retired []*entry

	// heartbeat (CHK-IPC-04)
	started      time.Time
	lastCycle    time.Time // end of the last completed full poll cycle
	hungReported bool

	// acknowledgements (CHK-IPC-02), guarded by mu; ackSave serializes
	// the writes
	acks    map[string]AckRecord
	ackSave sync.Mutex

	tester   *Tester
	explorer *Explorer // interactive commands (G2/G3)

	cLocks keyedLocks // per-profile counters locks (reference counted)

	// cfgMu serializes configuration writes (phase 6) and reloads, so a
	// write, its synchronous reload and the revision are consistent
	cfgMu    sync.Mutex
	revision string // configuration revision of the applied config (mu)

	cmdMu   sync.Mutex
	cmdLast map[string]time.Time // rate limiter of IPC commands
	pendAll bool                 // poll_now for every profile pending
	pendIDs map[string]bool      // poll_now for single profiles pending

	// phase 9: check_now limiter (cmdMu), run id and event ring (mu)
	checkLast map[string]time.Time
	runID     string
	ring      eventRing
	// phase 9: when a point / profile left ok (recovered durationMin, mu),
	// check_now (CHK-SVC-04, mu): checkCounter numbers the requests,
	// checkPend holds the newest pending CheckSeq per scope ("" = all
	// profiles, else a profile id), lastCheckSeq: every check with
	// CheckSeq <= lastCheckSeq is contained in the published status
	raisedAt     map[string]time.Time
	checkCounter uint64
	checkPend    map[string]uint64
	lastCheckSeq uint64

	// phase 10 (CHK-MON-16): one worker per enabled profile while Run is
	// active (runCtx != nil, mu); pollSem is the global poll semaphore;
	// checkAllWait: profiles that still have to start a poll for the
	// pending check_now of all profiles (mu).
	runCtx       context.Context
	workers      map[string]*worker
	wwg          sync.WaitGroup
	pollSem      chan struct{}
	checkAllWait map[string]bool
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
	if opt.MaxConcurrent <= 0 || opt.MaxConcurrent > MaxSQLConnections {
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
	if opt.Acks == nil {
		opt.Acks = &MemAckStore{}
	}
	m := &Monitor{
		opt: opt, tracker: alert.NewTracker(opt.Policy),
		pollNow: make(chan struct{}, 1), reload: make(chan struct{}, 1),
		entries: map[string]*entry{}, results: map[string]model.ProfileSnapshot{},
		cmdLast: map[string]time.Time{}, pendIDs: map[string]bool{},
		started: opt.Now().In(model.DBZone), acks: map[string]AckRecord{},
		tester: &Tester{Test: opt.Test, Category: opt.TestCategory, Timeout: opt.TestTimeout,
			Max: opt.TestMax, Now: opt.Now, WithTimeout: opt.TestWithTimeout},
		explorer: &Explorer{New: opt.Interactive, DiscoverTimeout: opt.DiscoverTimeout, HardTimeout: opt.InteractiveLimit,
			WithTimeout: opt.InteractiveWithTimeout},
	}
	m.explorer.defaults()
	m.workers = map[string]*worker{}
	m.pollSem = make(chan struct{}, opt.MaxConcurrent)
	m.checkLast = map[string]time.Time{}
	m.runID = opt.RunID
	if m.runID == "" {
		m.runID = newRunID()
	}
	m.ring.size = model.EventRingSize
	m.raisedAt = map[string]time.Time{}
	m.loadAcks()
	return m
}

// Rate-limiter keys of the IPC commands.
const (
	cmdKeyPollAll = "poll_now"
	cmdKeyReload  = "reload_config"
	cmdKeyTest    = "test_profile"
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
// The heartbeat fields (PollerHung, ...) are evaluated now (CHK-IPC-04).
func (m *Monitor) Snapshot() model.StatusSnapshot {
	now := m.opt.Now().In(model.DBZone)
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.snap
	if !m.loaded {
		s = model.StatusSnapshot{Now: now, Profiles: []model.ProfileSnapshot{}}
	}
	m.heartbeatLocked(now).apply(&s)
	s.ServerNow = now // phase 9
	return s
}

// hb is the heartbeat state at one instant.
type hb struct {
	started, lastCycle time.Time
	hung               bool
	hungAfter          time.Duration
	interval           time.Duration
}

func (h hb) apply(s *model.StatusSnapshot) {
	st := h.started
	s.ServiceStartedAt = &st
	s.LastCycleAt = nil
	if !h.lastCycle.IsZero() {
		lc := h.lastCycle
		s.LastCycleAt = &lc
	}
	s.PollerHung, s.HungAfterSec = h.hung, int(h.hungAfter/time.Second)
}

// HungAfter is the poller-hang threshold (CHK-IPC-04, PLAN.md §6.2):
//
//	hungAfter = max(2 × interval, interval + cycleBudget)
//	cycleBudget = ceil(enabled profiles / MaxConcurrent) × max profile
//	              timeout + max start jitter
//
// A cycle normally ends within cycleBudget (a hung profile is cut off at
// its timeout), so "no completed cycle for hungAfter" means the poll loop
// itself is stuck. Phase 10 (CHK-MON-16): with per-profile workers
// lastCycle is the end of the last published poll of ANY profile (or the
// idle heartbeat cycle when no profile is enabled).
func (m *Monitor) hungAfterLocked() time.Duration {
	interval := m.intervalLocked()
	n := 0
	var maxT time.Duration
	for _, p := range m.cfg.Profiles {
		if !p.Enabled {
			continue
		}
		n++
		if d := m.profileTimeout(p); d > maxT {
			maxT = d
		}
	}
	waves := (n + m.opt.MaxConcurrent - 1) / m.opt.MaxConcurrent
	budget := time.Duration(waves)*maxT + m.maxJitter(interval)
	h := 2 * interval
	if d := interval + budget; d > h {
		h = d
	}
	return h
}

// heartbeatLocked evaluates the heartbeat at now (m.mu held).
func (m *Monitor) heartbeatLocked(now time.Time) hb {
	h := hb{started: m.started, lastCycle: m.lastCycle, hungAfter: m.hungAfterLocked(), interval: m.intervalLocked()}
	ref := m.lastCycle
	if ref.IsZero() {
		ref = m.started
	}
	h.hung = now.Sub(ref) > h.hungAfter
	return h
}

// Heartbeat returns the heartbeat block of Hello / Pong (CHK-IPC-04).
func (m *Monitor) Heartbeat() model.Heartbeat {
	now := m.opt.Now().In(model.DBZone)
	m.mu.Lock()
	h := m.heartbeatLocked(now)
	m.mu.Unlock()
	out := model.Heartbeat{Protocol: model.IPCVersion, Now: now, ServiceStartedAt: h.started,
		PollerHung: h.hung, HungAfterSec: int(h.hungAfter / time.Second), PollIntervalSec: int(h.interval / time.Second),
		RunID: m.runID}
	if !h.lastCycle.IsZero() {
		lc := h.lastCycle
		out.LastCycleAt = &lc
	}
	return out
}

// checkHung is the poller watchdog (own goroutine in Run, so it works
// while the loop is stuck): the first time the poller is found hung it
// logs, warns (Event Log) and publishes the snapshot with PollerHung; the
// recovery is logged once (CHK-IPC-04).
func (m *Monitor) checkHung() {
	now := m.opt.Now().In(model.DBZone)
	m.mu.Lock()
	h := m.heartbeatLocked(now)
	report := h.hung && !m.hungReported
	recovered := !h.hung && m.hungReported
	if report || recovered {
		m.hungReported = h.hung
	}
	m.mu.Unlock()
	switch {
	case report:
		ref := h.lastCycle
		what := "the last completed poll cycle"
		if ref.IsZero() {
			ref, what = h.started, "the service start"
		}
		msg := fmt.Sprintf("poller hung: no poll cycle completed for %s since %s (threshold %s)",
			now.Sub(ref).Round(time.Second), what, h.hungAfter)
		m.opt.Log.Printf("%s", msg)
		if m.opt.Warn != nil {
			m.opt.Warn(msg)
		}
		m.publish(nil)
	case recovered:
		m.opt.Log.Printf("poller: poll cycles complete again")
	}
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

// LoadConfig performs the first configuration load synchronously (phase 6,
// windows CI 37123714167): the service calls it before the IPC server
// accepts clients, so the very first get_status already carries the
// settings, configRevision and profiles instead of an empty, not yet
// loaded snapshot. Run then skips its own initial load. Safe to call once;
// later calls are no-ops.
func (m *Monitor) LoadConfig() {
	if atomic.CompareAndSwapInt32(&m.initLoaded, 0, 1) {
		m.reloadConfig(true)
	}
}

// Run polls until ctx is canceled, then stops the profile workers and
// closes every reader. It returns nil on a normal stop. Phase 10
// (CHK-MON-16): every enabled profile is polled by its own worker
// (workers.go); this loop handles config checks, reloads and poll_now /
// check_now requests, and keeps the heartbeat alive when no profile is
// enabled.
func (m *Monitor) Run(ctx context.Context) error {
	if atomic.CompareAndSwapInt32(&m.initLoaded, 0, 1) {
		m.reloadConfig(true)
	}
	tick := time.NewTicker(m.opt.ConfigCheck)
	defer tick.Stop()
	defer m.closeAll()
	wdDone := make(chan struct{})
	defer func() { <-wdDone }()
	go func() { // poller watchdog (CHK-IPC-04)
		defer close(wdDone)
		t := time.NewTicker(m.opt.ConfigCheck)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.checkHung()
			}
		}
	}()
	m.mu.Lock()
	m.runCtx = ctx
	m.syncWorkersLocked()
	m.mu.Unlock()
	defer m.stopWorkers()
	if m.Workers() == 0 { // the workers poll right away
		m.pollSome(ctx, nil) // nothing enabled: the heartbeat cycle
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		timer := time.NewTimer(m.interval())
		var only map[string]bool
		kick, reload := false, false
	wait:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
				if m.kickWorkers(map[string]bool{}, false) == 0 {
					m.pollSome(ctx, nil) // idle heartbeat cycle
				}
				break wait
			case <-m.pollNow:
				timer.Stop()
				only, kick = m.takePending(), true
				break wait
			case <-m.reload:
				timer.Stop()
				m.takePending()
				m.reloadConfig(true)
				kick, reload = true, true
				break wait
			case <-tick.C:
				if m.reloadConfig(false) {
					timer.Stop()
					kick, reload = true, true
					break wait
				}
			}
		}
		if kick && m.kickWorkers(only, reload) == 0 && only == nil {
			m.pollSome(ctx, nil)
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
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	return m.reloadConfigLocked(force)
}

// reloadConfigLocked is reloadConfig with cfgMu held.
func (m *Monitor) reloadConfigLocked(force bool) bool {
	stamp := m.opt.Source.Stamp()
	m.mu.Lock()
	same := m.good && stamp == m.stamp && !m.retry
	retryOnly := m.good && stamp == m.stamp && m.retry && !force
	m.mu.Unlock()
	if same && !force {
		return false
	}
	cfg := m.opt.Source.Load()
	if cfg.Fatal != nil {
		m.mu.Lock()
		changed := false
		if !m.good {
			// no configuration applied yet: get_status shows settings.json
			// if that file itself is fine, otherwise no settings at all
			var fs *model.Settings
			if cfg.SettingsFatal == nil {
				st := cfg.Settings
				if st.WarnMinutes == 0 && st.ErrorMinutes == 0 {
					st = model.DefaultSettings()
				}
				st = st.Normalized()
				fs = &st
			}
			changed = m.loaded && (fs == nil) != (m.failSettings == nil) || fs != nil && m.failSettings != nil && !fs.Equal(*m.failSettings)
			m.failSettings = fs
		}
		m.mu.Unlock()
		m.configFailed(cfg.Fatal, false)
		if changed {
			m.publish(nil) // settings.json changed while profiles.json is still broken
		}
		return false
	}
	// settings.json unreadable / unparsable (CHK-MON-10): keep the last
	// good settings (defaults on first start) and apply the rest; polling
	// is not blocked.
	settingsErr := cfg.SettingsFatal
	if settingsErr != nil && retryOnly {
		// same files, still failing: nothing new to apply
		m.configFailed(settingsErr, true)
		return false
	}
	m.mu.Lock()
	stale := false
	if settingsErr != nil {
		if m.setGood {
			cfg.Settings, stale = m.cfg.Settings, true
		} else {
			cfg.Settings = model.DefaultSettings()
		}
	} else {
		m.setGood = true
	}
	m.mu.Unlock()
	if cfg.Settings.WarnMinutes == 0 && cfg.Settings.ErrorMinutes == 0 {
		cfg.Settings = model.DefaultSettings()
	}
	rev := cfg.Revision // same bytes as the applied data (FileConfig)
	if rev == "" {
		var rerr error
		if rev, rerr = configRevision(m.configWriter()); rerr != nil {
			m.opt.Log.Printf("config: revision: %v", rerr)
		}
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
			m.retired = append(m.retired, old)
			delete(m.results, id)
		}
	}
	recovered := m.retry && settingsErr == nil
	m.entries, m.cfg, m.stamp, m.loaded, m.good = next, cfg, stamp, true, true
	m.dropChecksLocked(cfg.Profiles) // phase 9 (CHK-SVC-04)
	m.syncWorkersLocked()            // phase 10 (CHK-MON-16)
	m.revision = rev
	if settingsErr == nil {
		m.retry, m.cfgFatal, m.stale, m.warnMsg = false, nil, false, ""
	} else {
		m.retry, m.cfgFatal, m.stale = true, settingsErr, stale
	}
	m.mu.Unlock()
	for _, e := range retired {
		e.retire()
	}
	// counters state and alerts are pruned only after a successful load
	if r, ok := m.opt.Counters.(interface{ Retain(map[string]bool) error }); ok {
		_ = r.Retain(ids)
	}
	m.tracker.Retain(func(key string) bool { return ids[alert.KeyProfile(key)] })
	m.mu.Lock()
	acksChanged := m.applyAcksLocked(func(key string) bool { return ids[alert.KeyProfile(key)] })
	m.mu.Unlock()
	if acksChanged {
		m.saveAcks()
	}
	if recovered {
		m.opt.Log.Printf("config: loaded successfully again")
	}
	if cfg.Err != nil {
		m.opt.Log.Printf("config: loaded with errors: %v", cfg.Err)
	}
	m.opt.Log.Printf("config: %d profile(s), poll interval %s", len(cfg.Profiles), m.interval())
	if settingsErr != nil {
		m.warnConfig(settingsErr, stale, true)
	}
	m.publish(nil)
	return true
}

// configFailed keeps the last good configuration after a failed load:
// no reader, counters state or alert is touched; the snapshot reports the
// error; the warning goes to the log (and Warn -> event log) once per
// distinct error and then at most every ConfigWarnEvery; the next config
// check retries.
// settings == true: a retry of an already applied config whose
// settings.json still fails (the stale flag set at apply time is kept).
func (m *Monitor) configFailed(err error, settings bool) {
	m.mu.Lock()
	m.retry, m.cfgFatal = true, err
	first := !m.loaded
	m.loaded = true
	if !settings {
		// "last good configuration" needs both parts to have been valid
		// once: after a first start with broken settings.json (defaults)
		// a broken profiles.json is config_invalid, not config_stale
		m.stale = m.good && m.setGood
	}
	stale := m.stale
	m.mu.Unlock()
	warn := m.warnConfig(err, stale, settings)
	if first || warn {
		m.publish(nil)
	}
}

// warnConfig logs (and sends to Warn -> event log) a config-load problem
// once per distinct error and then at most every ConfigWarnEvery. It
// reports whether it warned.
func (m *Monitor) warnConfig(err error, stale, settingsOnly bool) bool {
	now := m.opt.Now()
	msg := err.Error()
	m.mu.Lock()
	warn := msg != m.warnMsg || now.Sub(m.warnAt) >= m.opt.ConfigWarnEvery || now.Before(m.warnAt)
	if warn {
		m.warnMsg, m.warnAt = msg, now
	}
	good := m.good
	m.mu.Unlock()
	if !warn {
		return false
	}
	var text string
	switch {
	case settingsOnly && stale:
		text = "config: cannot load settings.json, keeping the last good settings: " + msg
	case settingsOnly:
		text = "config: cannot load settings.json, using default settings: " + msg
	case good:
		text = "config: cannot load the configuration, keeping the last good one: " + msg
	default:
		text = "config: cannot load the configuration (no previous good one, nothing is polled): " + msg
	}
	m.opt.Log.Printf("%s", text)
	if m.opt.Warn != nil {
		m.opt.Warn(text)
	}
	return true
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

// maxJitter is the max start delay of one profile in a poll.
func (m *Monitor) maxJitter(interval time.Duration) time.Duration {
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
	if max < 0 {
		return 0
	}
	return max
}

// jitterFor returns the start delay of one profile in a poll.
func (m *Monitor) jitterFor(interval time.Duration) time.Duration {
	max := m.maxJitter(interval)
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
	m.reapRetired()
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
	startSeq := m.checkCounter
	m.takeChecksLocked(only) // phase 9: check_now requests covered by this poll
	m.mu.Unlock()
	interval := m.interval()
	sem := m.pollSem // global poll semaphore (CHK-MON-16)
	results := make([]*model.ProfileStatus, len(es))
	latency := make([]int64, len(es))
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
			t0 := time.Now()
			ps := m.pollOne(ctx, e, settings)
			latency[i] = time.Since(t0).Milliseconds()
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
			snap := model.NewProfileSnapshot(e.profile, *results[i], cfg.Settings, now)
			ms := latency[i]
			snap.LatencyMs = &ms // phase 9 (CHK-SVC-03)
			qms := ms
			snap.LastQueryMs = &qms
			snap.LastCheckSeq = startSeq // phase 10 (CHK-MON-17)
			res[e.profile.ID] = snap
			polled[e.profile.ID] = true
		}
		if only == nil {
			m.lastCycle = now // a full cycle completed (CHK-IPC-04)
		}
		m.settleChecksLocked()
		return polled
	})
	if only == nil {
		m.checkHung()
	}
}

// reapRetired frees retired entries whose poll hangs: like a live entry
// (pollOne), the reader is detached and closed in the background after
// HungFactor poll timeouts, so the sql.DB of a changed or removed profile
// is closed even if its last poll never returns (CHK-MON-13). Entries
// that are no longer busy were closed by release and are forgotten.
func (m *Monitor) reapRetired() {
	now := m.opt.Now()
	m.mu.Lock()
	keep := m.retired[:0]
	var msgs []string
	for _, e := range m.retired {
		if !e.busyNow() {
			continue
		}
		limit := HungFactor * m.profileTimeout(e.profile)
		if since, hung := e.abandonHung(now, limit); hung {
			msgs = append(msgs, fmt.Sprintf("profile %s: poll of the retired (changed or removed) configuration hung for %s (> %s), reader closed",
				e.profile.ID, since.Round(time.Second), limit))
			continue
		}
		keep = append(keep, e)
	}
	for i := len(keep); i < len(m.retired); i++ {
		m.retired[i] = nil
	}
	m.retired = keep
	m.mu.Unlock()
	for _, msg := range msgs {
		m.opt.Log.Printf("%s", msg)
	}
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

// KnownErrorKey reports whether key has a text in model: monitor.* /
// test.* (model.MonitorMessage) or sql.* (model.SQLMessage, G1/G4). The
// service only passes such keys to the tray in errorKey.
func KnownErrorKey(key string) bool {
	return key != "" && (model.MonitorMessage(key, model.LangEN) != "" || model.SQLMessage(key, model.LangEN) != "")
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
// abandoned (the entry stays busy until it returns). A poll that finished
// in time has already released the entry when pollOne returns.
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
		// The entry is released BEFORE the result is handed over: once
		// pollOne returns a finished poll, the entry is free and a poll
		// right after it (next cycle, poll_now) must not see a stale
		// "previous poll still running" (CHK-MON-13). A retired entry's
		// reader is closed by release, i.e. also before the result.
		var ps model.ProfileStatus
		defer func() {
			if r := recover(); r != nil {
				m.opt.Log.Printf("profile %s: panic while polling: %v\n%s", p.ID, r, debug.Stack())
				ps = fail(fmt.Sprintf("internal error: %v", r), model.MsgMonitorPanic)
			}
			e.release(gen)
			resc <- ps
		}()
		e.mu.Lock()
		r := e.reader
		e.mu.Unlock()
		if r == nil {
			var err error
			r, err = m.opt.NewReader(p, e.password, sqlcheck.Options{
				Settings: p.EffectiveSettings(settings), Now: m.opt.Now,
				Counters: genCounters{inner: m.opt.Counters, e: e, gen: gen, locks: &m.cLocks}})
			if err != nil {
				ps = fail(err.Error(), ErrorKeyOf(err))
				return
			}
			e.mu.Lock()
			if e.gen != gen { // abandoned as hung while connecting
				e.mu.Unlock()
				_ = r.Close()
				ps = fail("poll abandoned", model.MsgMonitorHung)
				return
			}
			e.reader = r
			e.mu.Unlock()
		}
		rec := &errRecorder{ExchangeReader: r}
		ps = sqlcheck.Status(cctx, rec, p, now)
		if ps.Error != "" && !KnownErrorKey(ps.ErrorKey) {
			// no key, or a key without a text in model (G1): the
			// monitor.* key of the error kind
			ps.ErrorKey = ErrorKeyOf(rec.err)
		}
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
	var events, repeats, repEvents []model.AlertEvent
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
		// connection warnings (sql.* keys, texts model.SQLMessage; G1)
		ps.Warnings = model.SQLWarnings(p)
		// read-only settings view (phase 6): no password, no SQL
		ps.Connection = model.ConnectionOf(p, cfg.Passwords[p.ID] != "")
		ps.ThresholdOverride = &model.ThresholdOverride{WarnMinutes: p.WarnMinutes, ErrorMinutes: p.ErrorMinutes}
		ps.MappingSummary = model.MappingSummaryOf(p)
		profiles = append(profiles, ps)
		if ok && p.Enabled && polled[p.ID] {
			evs, reps := m.observe(ps, now)
			events = append(events, evs...)
			repeats = append(repeats, reps...)
		}
	}
	var keep func(string) bool
	if update != nil {
		keep = m.keepKeys(profiles)
		m.tracker.Retain(keep)
	}
	acksChanged := m.applyAcksLocked(keep)
	alerts := m.tracker.Active()
	m.markAckedLocked(profiles, alerts)
	for i := range events {
		m.markEventLocked(&events[i])
		m.classifyEventLocked(&events[i]) // phase 9 (CHK-SVC-05); adds it to the ring once
	}
	for i := range repeats { // phase 10 (CHK-MON-17)
		r := &repeats[i]
		m.markEventLocked(r)
		r.RunID, r.EventID, r.Kind = m.runID, r.ID, model.AlertKindAlert
		r.CheckSeq = m.results[r.ProfileID].LastCheckSeq
		// the alert_repeat event: own id, RepeatOf the original alert
		ev := *r
		ev.ID = m.tracker.NewID()
		ev.EventID, ev.Kind, ev.RepeatOf = ev.ID, model.AlertKindRepeat, r.ID
		ev.Level, ev.PointKey, ev.From = r.To, r.Key, r.To
		if r.LastCheckedAt != nil {
			ev.At = *r.LastCheckedAt
		}
		// not in the events_since ring (architect review): push only; the
		// current state is in status.problems
		repEvents = append(repEvents, ev)
	}
	m.seq++
	snap := model.StatusSnapshot{
		Seq: m.seq, Now: now, Level: model.WorstProfileLevel(profiles), Profiles: profiles,
		Alerts: alerts, PollIntervalSec: int(m.intervalLocked() / time.Second),
		LastPollAt: m.lastPoll, ConfigRevision: m.revision,
		ServerNow: now, LastCheckSeq: m.lastCheckSeq,
	}
	snap.Problems, snap.ProblemsTruncated = model.ProblemsOf(alerts)
	snap.HasProblems = len(snap.Problems) > 0
	switch {
	case m.good: // a configuration has been applied
		st := cfg.Settings.Normalized() // beep.enabled always explicit
		m.viberView(&st)
		snap.Settings = &st
	case m.failSettings != nil:
		// the first load failed (profiles.json / secrets): the real
		// settings.json, never zero thresholds / trayAutostart=false
		st := m.failSettings.Normalized()
		m.viberView(&st)
		snap.Settings = &st
	}
	m.heartbeatLocked(now).apply(&snap)
	if cfg.Err != nil {
		snap.ConfigError = cfg.Err.Error()
	}
	if m.cfgFatal != nil {
		snap.ConfigError = m.cfgFatal.Error()
		snap.ConfigStale = m.stale
		snap.ConfigErrorKey = model.MsgMonitorConfigInvalid
		if m.stale {
			snap.ConfigErrorKey = model.MsgMonitorConfigStale
		}
	}
	m.snap = snap
	m.mu.Unlock()
	if acksChanged {
		m.saveAcks()
	}
	for _, ev := range events {
		m.opt.Log.Printf("alert #%d: %s", ev.ID, alert.Format(ev, model.LangEN))
		if m.opt.Sink != nil {
			m.opt.Sink.Alert(ev)
		}
	}
	if m.opt.OnSnapshot != nil {
		m.opt.OnSnapshot(snap)
	}
	if m.opt.OnAlertRepeat != nil {
		for _, ev := range repEvents {
			m.opt.OnAlertRepeat(ev)
		}
	}
}

// ReportsProblems: hasProblems / problems and repeat pushes (CHK-MON-17).
func (m *Monitor) ReportsProblems() bool { return true }

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
//
// Phase 10 (CHK-MON-17): reps are the repeat pushes of active alerts whose
// check failed again without a transition (the alarm persists until the
// error is resolved; acknowledging does not stop checks or repeats).
func (m *Monitor) observe(ps model.ProfileSnapshot, now time.Time) (out, reps []model.AlertEvent) {
	obs := func(o alert.Observation) {
		if ev, ok := m.tracker.Observe(o, now); ok {
			out = append(out, ev)
		} else if r, ok := m.tracker.Repeat(o.Key, now); ok {
			reps = append(reps, r)
		}
	}
	lvl := model.LevelOK
	if ps.Error != "" {
		lvl = model.LevelError
	}
	obs(alert.Observation{Key: alert.ProfileKey(ps.ProfileID), Profile: true, Level: lvl,
		Event: model.AlertEvent{ProfileID: ps.ProfileID, ProfileName: ps.Name, Error: ps.Error, ErrorKey: ps.ErrorKey}})
	if ps.Error != "" {
		return out, reps
	}
	for _, pt := range ps.Points {
		tmpl := model.AlertEvent{ProfileID: ps.ProfileID, ProfileName: ps.Name, PointName: pt.Name,
			PlanTable: pt.PlanTable, FreshAt: pt.FreshAt, Baseline: pt.Baseline, Key: model.PointKey(pt.ExchangePoint)}
		if pt.LagMinutes > 0 {
			tmpl.LagMinutes = pt.LagMinutes
		}
		if pt.FreshAt == nil {
			tmpl.Error = pt.Message
		}
		obs(alert.Observation{Key: alert.PointKey(ps.ProfileID, model.PointKey(pt.ExchangePoint)),
			Level: pt.Level, Event: tmpl})
	}
	return out, reps
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
