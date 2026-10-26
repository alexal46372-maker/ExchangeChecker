package model

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Counters fallback (Mapping.Mode = "counters", PLAN.md §5.3): when the
// configuration stores no exchange times at all, the service reads the
// 1C exchange-plan message counters _SentNo / _ReceivedNo of _NodeNNN and
// remembers when each counter last changed; that change time becomes
// LastSend / LastRecv of the point.

// Default counter columns of a 1C exchange-plan table.
const (
	DefaultSentNoColumn     = "_SentNo"
	DefaultReceivedNoColumn = "_ReceivedNo"
)

// PointSourceCounters is ExchangePoint.Source for points whose times are
// counter change times observed by the service ("" = database timestamps).
const PointSourceCounters = "counters"

// SentNoCol returns the sent-counter column (default _SentNo).
func (m Mapping) SentNoCol() string {
	if strings.TrimSpace(m.SentNoColumn) == "" {
		return DefaultSentNoColumn
	}
	return m.SentNoColumn
}

// ReceivedNoCol returns the received-counter column (default _ReceivedNo).
func (m Mapping) ReceivedNoCol() string {
	if strings.TrimSpace(m.ReceivedNoColumn) == "" {
		return DefaultReceivedNoColumn
	}
	return m.ReceivedNoColumn
}

// validateCounters checks the counters mode: a node table with a point
// name/code column; no times table, no manual SQL.
func (m Mapping) validateCounters() error {
	if m.Mode != MappingCounters {
		if strings.TrimSpace(m.SentNoColumn) != "" || strings.TrimSpace(m.ReceivedNoColumn) != "" {
			return fieldErr("mode", ValMapping, "mapping: sentNoColumn/receivedNoColumn require mode \"counters\"", "mode")
		}
		return nil
	}
	switch {
	case strings.TrimSpace(m.Table) == "":
		return fieldErr("table", ValRequired, "mapping: counters mode requires the exchange-plan table")
	case m.Linked():
		return fieldErr("timesTable", ValMapping, "mapping: counters mode does not use a times table", "timesTable")
	case strings.TrimSpace(m.ManualSQL) != "":
		return fieldErr("manualSql", ValMapping, "mapping: counters mode does not use manual SQL", "manualSql")
	}
	return nil
}

// CounterObservation is one point's counters read in one poll.
type CounterObservation struct {
	// Key identifies the point across polls (hex _IDRRef, else code/name).
	Key        string `json:"key"`
	Name       string `json:"name"`
	SentNo     int64  `json:"sentNo"`
	ReceivedNo int64  `json:"receivedNo"`
}

// CounterPoint is the remembered state of one point.
type CounterPoint struct {
	Name       string `json:"name"`
	SentNo     int64  `json:"sentNo"`
	ReceivedNo int64  `json:"receivedNo"`
	// SentChangedAt / ReceivedChangedAt: when the counter was last seen
	// changing, in DBZone. With *Baseline set it is only the first
	// observation (the real change happened at or before it).
	SentChangedAt     time.Time `json:"sentChangedAt"`
	ReceivedChangedAt time.Time `json:"receivedChangedAt"`
	SentBaseline      bool      `json:"sentBaseline,omitempty"`
	ReceivedBaseline  bool      `json:"receivedBaseline,omitempty"`
	FirstSeen         time.Time `json:"firstSeen"`
	LastSeen          time.Time `json:"lastSeen"`
}

// CounterStateVersion is the CounterState JSON schema version.
const CounterStateVersion = 1

// CounterState is the per-profile memory of the counters mode. It is
// JSON-serializable (persisted by the monitor in phase 3).
type CounterState struct {
	Version int                     `json:"version"`
	Points  map[string]CounterPoint `json:"points"`
}

// CounterTimes is the result of ApplyCounters for one observed point.
type CounterTimes struct {
	Key          string    `json:"key"`
	Name         string    `json:"name"`
	LastSend     time.Time `json:"lastSend"`
	LastRecv     time.Time `json:"lastRecv"`
	SendBaseline bool      `json:"sendBaseline,omitempty"`
	RecvBaseline bool      `json:"recvBaseline,omitempty"`
}

// ApplyCounters is the pure was/now step of the counters mode:
//
//   - a point seen for the first time (service start, new node, or state
//     lost) gets both change times = now with the baseline flags set;
//   - an unchanged counter keeps its change time and baseline flag;
//   - a changed counter (any difference: an increase, or a DECREASE, which
//     happens when 1C re-creates / re-initializes the node or resets the
//     message numbers) gets change time = now and loses the baseline flag;
//   - points not observed any more are dropped from the next state;
//   - duplicate keys in one poll: the first observation wins.
//
// now is converted to DBZone (UTC+3). Results follow the observation order.
func ApplyCounters(prev CounterState, observed []CounterObservation, now time.Time) (CounterState, []CounterTimes) {
	now = now.In(DBZone)
	next := CounterState{Version: CounterStateVersion, Points: make(map[string]CounterPoint, len(observed))}
	out := make([]CounterTimes, 0, len(observed))
	for _, o := range observed {
		if _, dup := next.Points[o.Key]; dup {
			continue
		}
		p, known := prev.Points[o.Key]
		if !known {
			p = CounterPoint{
				SentNo: o.SentNo, ReceivedNo: o.ReceivedNo,
				SentChangedAt: now, ReceivedChangedAt: now,
				SentBaseline: true, ReceivedBaseline: true,
				FirstSeen: now,
			}
		} else {
			if o.SentNo != p.SentNo {
				p.SentNo, p.SentChangedAt, p.SentBaseline = o.SentNo, now, false
			}
			if o.ReceivedNo != p.ReceivedNo {
				p.ReceivedNo, p.ReceivedChangedAt, p.ReceivedBaseline = o.ReceivedNo, now, false
			}
		}
		p.Name, p.LastSeen = o.Name, now
		next.Points[o.Key] = p
		out = append(out, CounterTimes{
			Key: o.Key, Name: o.Name,
			LastSend: p.SentChangedAt.In(DBZone), LastRecv: p.ReceivedChangedAt.In(DBZone),
			SendBaseline: p.SentBaseline, RecvBaseline: p.ReceivedBaseline,
		})
	}
	return next, out
}

// CounterPoints turns ApplyCounters results into ExchangePoints (Source
// "counters", baseline flags) and evaluates them with Refresh.
func CounterPoints(profileID string, times []CounterTimes, s Settings, now time.Time) []ExchangePoint {
	out := make([]ExchangePoint, 0, len(times))
	for _, t := range times {
		send, recv := t.LastSend, t.LastRecv
		p := ExchangePoint{
			ProfileID: profileID, Name: t.Name, NodeKey: t.Key, LastSend: &send, LastRecv: &recv,
			Source: PointSourceCounters, SendBaseline: t.SendBaseline, RecvBaseline: t.RecvBaseline,
		}
		p.Refresh(s, now)
		out = append(out, p)
	}
	return out
}

// CounterStateStore persists CounterState per profile. The file-backed
// implementation belongs to the monitor (phase 3); MemCounterStore is the
// in-process default and the test double.
type CounterStateStore interface {
	// Load returns the saved state; a missing state is an empty state and
	// a nil error.
	Load(profileID string) (CounterState, error)
	Save(profileID string, st CounterState) error
}

// CounterStateUpdater is implemented by stores that can run a whole
// Load -> apply -> Save step atomically for one profile (a per-profile
// lock), so two readers of the same profile cannot lose each other's
// update (CHK-MON-13). fn returning an error aborts the step: nothing is
// saved and the error is returned.
type CounterStateUpdater interface {
	Update(profileID string, fn func(prev CounterState) (CounterState, error)) error
}

// UpdateCounterState runs fn through s.Update when s is a
// CounterStateUpdater, otherwise as Load -> fn -> Save.
func UpdateCounterState(s CounterStateStore, profileID string, fn func(prev CounterState) (CounterState, error)) error {
	if u, ok := s.(CounterStateUpdater); ok {
		return u.Update(profileID, fn)
	}
	prev, err := s.Load(profileID)
	if err != nil {
		return err
	}
	next, err := fn(prev)
	if err != nil {
		return err
	}
	return s.Save(profileID, next)
}

// MemCounterStore is an in-memory CounterStateStore (lost on restart,
// which only means a new baseline).
type MemCounterStore struct {
	mu sync.Mutex
	m  map[string]CounterState
}

// NewMemCounterStore returns an empty in-memory store.
func NewMemCounterStore() *MemCounterStore { return &MemCounterStore{m: map[string]CounterState{}} }

func copyCounterState(st CounterState) CounterState {
	c := CounterState{Version: st.Version, Points: make(map[string]CounterPoint, len(st.Points))}
	for k, v := range st.Points {
		c.Points[k] = v
	}
	return c
}

// Load implements CounterStateStore.
func (s *MemCounterStore) Load(profileID string) (CounterState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[profileID]
	if !ok {
		return CounterState{Version: CounterStateVersion, Points: map[string]CounterPoint{}}, nil
	}
	return copyCounterState(st), nil
}

// Save implements CounterStateStore.
func (s *MemCounterStore) Save(profileID string, st CounterState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]CounterState{}
	}
	s.m[profileID] = copyCounterState(st)
	return nil
}

// Update implements CounterStateUpdater (the store lock is held for the
// whole step).
func (s *MemCounterStore) Update(profileID string, fn func(prev CounterState) (CounterState, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.m[profileID]
	if !ok {
		prev = CounterState{Version: CounterStateVersion, Points: map[string]CounterPoint{}}
	}
	next, err := fn(copyCounterState(prev))
	if err != nil {
		return err
	}
	if s.m == nil {
		s.m = map[string]CounterState{}
	}
	s.m[profileID] = copyCounterState(next)
	return nil
}

// ProfileIDs lists stored profiles (sorted; tests).
func (s *MemCounterStore) ProfileIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.m))
	for k := range s.m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
