package monitor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// CountersFile is the counters-mode state file next to the config
// (%ProgramData%\ExchangeChecker\counters-state.json).
const CountersFile = "counters-state.json"

// countersFileVersion is the file schema version.
const countersFileVersion = 1

type countersDoc struct {
	Version  int                           `json:"version"`
	Profiles map[string]model.CounterState `json:"profiles"`
}

// Logger is the line logger of the monitor (alert.RotatingFile in the
// service, test doubles in tests).
type Logger interface {
	Printf(format string, args ...interface{})
}

type nopLogger struct{}

func (nopLogger) Printf(string, ...interface{}) {}

// FileCounterStore is the persistent model.CounterStateStore (CHK-MON-02):
// the states of all profiles in one JSON file, rewritten atomically on
// every Save (temp file + MoveFileEx REPLACE_EXISTING|WRITE_THROUGH on
// Windows, rename elsewhere; config.WriteFileAtomic). The state survives
// a service restart, so the counters baseline is not reset. A corrupt or
// unreadable file never stops the service: the state is reset, the file
// is kept as counters-state.json.corrupt and a log entry is written.
type FileCounterStore struct {
	dir, name string
	log       Logger

	mu      sync.Mutex
	data    map[string]model.CounterState
	lastErr error
	locks   map[string]*sync.Mutex // per-profile Update / Save locks
	write   func(dir, name string, data []byte, perm os.FileMode) error
	// retainHook is called by Retain with the IDs it is about to lock
	// (tests).
	retainHook func(ids []string)
}

// OpenFileCounterStore loads dir/CountersFile (missing file = empty).
func OpenFileCounterStore(dir string, log Logger) *FileCounterStore {
	if log == nil {
		log = nopLogger{}
	}
	s := &FileCounterStore{dir: dir, name: CountersFile, log: log,
		data: map[string]model.CounterState{}, write: config.WriteFileAtomic}
	s.load()
	return s
}

// Path returns the state file path.
func (s *FileCounterStore) Path() string { return filepath.Join(s.dir, s.name) }

func (s *FileCounterStore) load() {
	b, err := os.ReadFile(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		s.log.Printf("counters state: read %s: %v (starting with an empty state, new baseline)", s.Path(), err)
		return
	}
	var doc countersDoc
	err = json.Unmarshal(b, &doc)
	if err == nil && doc.Version != countersFileVersion {
		err = fmt.Errorf("unsupported version %d", doc.Version)
	}
	if err != nil {
		bad := s.Path() + ".corrupt"
		_ = os.Remove(bad)
		if rerr := os.Rename(s.Path(), bad); rerr != nil {
			bad = "(not kept: " + rerr.Error() + ")"
		}
		s.log.Printf("counters state: %s is corrupt: %v; state reset (new baseline), old file kept as %s", s.Path(), err, bad)
		return
	}
	for id, st := range doc.Profiles {
		if st.Points == nil {
			st.Points = map[string]model.CounterPoint{}
		}
		st.Version = model.CounterStateVersion
		s.data[id] = st
	}
}

func copyState(st model.CounterState) model.CounterState {
	c := model.CounterState{Version: model.CounterStateVersion, Points: make(map[string]model.CounterPoint, len(st.Points))}
	for k, v := range st.Points {
		c.Points[k] = v
	}
	return c
}

// Load implements model.CounterStateStore.
func (s *FileCounterStore) Load(profileID string) (model.CounterState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.data[profileID]
	if !ok {
		return model.CounterState{Version: model.CounterStateVersion, Points: map[string]model.CounterPoint{}}, nil
	}
	return copyState(st), nil
}

// plock returns the per-profile lock serializing Update / Save of one
// profile (CHK-MON-13).
func (s *FileCounterStore) plock(profileID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks == nil {
		s.locks = map[string]*sync.Mutex{}
	}
	l := s.locks[profileID]
	if l == nil {
		l = &sync.Mutex{}
		s.locks[profileID] = l
	}
	return l
}

// Update implements model.CounterStateUpdater: Load -> fn -> Save under
// the profile's lock, so concurrent readers of one profile (e.g. a new
// reader and an abandoned hung one) cannot lose updates. Other profiles
// are not blocked while fn runs.
func (s *FileCounterStore) Update(profileID string, fn func(prev model.CounterState) (model.CounterState, error)) error {
	l := s.plock(profileID)
	l.Lock()
	defer l.Unlock()
	prev, _ := s.Load(profileID)
	next, err := fn(prev)
	if err != nil {
		return err
	}
	return s.save(profileID, next)
}

// Save implements model.CounterStateStore: the in-memory state is updated
// and the whole file rewritten atomically. A write error is not fatal for
// monitoring (the in-memory state is used, the previous file stays intact):
// it is logged, kept in LastError and nil is returned, so a full disk does
// not turn every counters profile red.
func (s *FileCounterStore) Save(profileID string, st model.CounterState) error {
	l := s.plock(profileID)
	l.Lock()
	defer l.Unlock()
	return s.save(profileID, st)
}

func (s *FileCounterStore) save(profileID string, st model.CounterState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[profileID] = copyState(st)
	s.lastErr = s.flushLocked()
	return nil
}

// LastError returns the error of the last write (nil after a good one).
func (s *FileCounterStore) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// Retain drops the states of profiles not in ids (deleted profiles).
//
// The lock of every known profile not in ids is taken first — profiles
// with a stored state AND profiles that only have a lock (an Update in
// progress whose first write has not landed yet, phase 4) — so an Update
// that already passed its checks finishes before the deletion (and a
// later one is rejected by the monitor, whose entry was retired before
// Retain): a removed profile cannot reappear in the file, not even by
// its very first write (CHK-MON-13). The locks of dropped profiles are
// forgotten afterwards.
func (s *FileCounterStore) Retain(ids map[string]bool) error {
	s.mu.Lock()
	seen := map[string]bool{}
	var drop []string
	for id := range s.data {
		if !ids[id] && !seen[id] {
			seen[id] = true
			drop = append(drop, id)
		}
	}
	for id := range s.locks {
		if !ids[id] && !seen[id] {
			seen[id] = true
			drop = append(drop, id)
		}
	}
	s.mu.Unlock()
	if len(drop) == 0 {
		return nil
	}
	sort.Strings(drop)
	if s.retainHook != nil {
		s.retainHook(drop)
	}
	for _, id := range drop {
		l := s.plock(id)
		l.Lock()
		defer l.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, id := range drop {
		if _, ok := s.data[id]; ok {
			delete(s.data, id)
			changed = true
		}
		delete(s.locks, id)
	}
	if !changed {
		return nil
	}
	return s.flushLocked()
}

// ProfileIDs lists the stored profiles (sorted).
func (s *FileCounterStore) ProfileIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.data))
	for id := range s.data {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (s *FileCounterStore) flushLocked() error {
	b, err := json.MarshalIndent(countersDoc{Version: countersFileVersion, Profiles: s.data}, "", " ")
	if err != nil {
		return err
	}
	if err := s.write(s.dir, s.name, b, 0o644); err != nil {
		s.log.Printf("counters state: save %s: %v", s.Path(), err)
		return err
	}
	return nil
}
