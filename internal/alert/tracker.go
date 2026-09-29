package alert

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Policy is the anti-flap debounce: a new level must be observed in that
// many consecutive polls before the transition is confirmed and an event
// is emitted. A level that stays the same never produces another event.
type Policy struct {
	// RaisePolls: escalation of a point (ok -> warning -> error). The lag
	// of a point only grows between exchanges, so 1 = immediate.
	RaisePolls int
	// ClearPolls: de-escalation (error -> warning -> ok) of points and
	// profiles; > 1 suppresses ok/warning flapping around a threshold.
	ClearPolls int
	// ProfileRaisePolls: escalation of a profile-level error (connection /
	// query / timeout); > 1 ignores a single transient network failure.
	ProfileRaisePolls int
}

// DefaultPolicy: points escalate immediately, recoveries and connection
// errors need two consecutive polls.
func DefaultPolicy() Policy { return Policy{RaisePolls: 1, ClearPolls: 2, ProfileRaisePolls: 2} }

func (p Policy) norm() Policy {
	if p.RaisePolls < 1 {
		p.RaisePolls = 1
	}
	if p.ClearPolls < 1 {
		p.ClearPolls = 1
	}
	if p.ProfileRaisePolls < 1 {
		p.ProfileRaisePolls = 1
	}
	return p
}

// keySep separates profile ID and point key in tracker keys.
const keySep = "\x00"

// ProfileKey is the tracker key of a profile-level alert.
func ProfileKey(profileID string) string { return profileID + keySep }

// PointKey is the tracker key of a point alert.
func PointKey(profileID, pointKey string) string { return profileID + keySep + pointKey }

// KeyProfile returns the profile ID of a tracker key.
func KeyProfile(key string) string {
	if i := strings.Index(key, keySep); i >= 0 {
		return key[:i]
	}
	return key
}

// Observation is the level of one key seen in one poll. Event is the
// template of the event (names, FreshAt, error); From/To/ID/At are set by
// the tracker.
type Observation struct {
	Key     string
	Profile bool // profile-level key (connection / query error)
	Level   model.AlertLevel
	Event   model.AlertEvent
}

type keyState struct {
	stable  model.AlertLevel
	pending model.AlertLevel
	count   int
	active  model.AlertEvent // last event, refreshed while not OK
}

// Tracker turns per-poll observations into debounced transition events.
// It is safe for concurrent use.
type Tracker struct {
	mu     sync.Mutex
	policy Policy
	nextID uint64
	states map[string]*keyState
}

// NewTracker returns a tracker with policy p (zero fields = 1 poll).
func NewTracker(p Policy) *Tracker {
	return &Tracker{policy: p.norm(), states: map[string]*keyState{}}
}

// Observe records o at now and returns the transition event, if the
// debounce confirmed one. A new key starts at LevelOK.
func (t *Tracker) Observe(o Observation, now time.Time) (model.AlertEvent, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.states[o.Key]
	if !ok {
		st = &keyState{stable: model.LevelOK, pending: model.LevelOK}
		t.states[o.Key] = st
	}
	if o.Level == st.stable {
		st.pending, st.count = st.stable, 0
		if st.stable != model.LevelOK {
			t.refresh(st, o)
		}
		return model.AlertEvent{}, false
	}
	up := o.Level > st.stable
	if st.count == 0 || (st.pending > st.stable) != up {
		st.count = 0
	}
	st.pending = o.Level
	st.count++
	need := t.policy.ClearPolls
	if up {
		need = t.policy.RaisePolls
		if o.Profile {
			need = t.policy.ProfileRaisePolls
		}
	}
	if st.count < need {
		return model.AlertEvent{}, false
	}
	t.nextID++
	ev := o.Event
	ev.ID, ev.From, ev.To, ev.At = t.nextID, st.stable, o.Level, now.In(model.DBZone)
	st.stable, st.pending, st.count = o.Level, o.Level, 0
	st.active = ev
	return ev, true
}

// refresh updates the details of an active alert (lag, freshAt, error)
// without emitting an event.
func (t *Tracker) refresh(st *keyState, o Observation) {
	a := st.active
	a.FreshAt, a.LagMinutes, a.Baseline = o.Event.FreshAt, o.Event.LagMinutes, o.Event.Baseline
	a.Error, a.ErrorKey, a.ProfileName = o.Event.Error, o.Event.ErrorKey, o.Event.ProfileName
	st.active = a
}

// Level returns the confirmed level of key (LevelOK if unknown).
func (t *Tracker) Level(key string) model.AlertLevel {
	t.mu.Lock()
	defer t.mu.Unlock()
	if st, ok := t.states[key]; ok {
		return st.stable
	}
	return model.LevelOK
}

// Active returns the currently active alerts (confirmed level != OK),
// ordered by event ID.
func (t *Tracker) Active() []model.AlertEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []model.AlertEvent
	for _, st := range t.states {
		if st.stable != model.LevelOK {
			out = append(out, st.active)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Retain forgets every key for which keep returns false (removed points
// or profiles); forgetting emits no event.
func (t *Tracker) Retain(keep func(key string) bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k := range t.states {
		if !keep(k) {
			delete(t.states, k)
		}
	}
}

// Len returns the number of tracked keys (tests).
func (t *Tracker) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.states)
}
