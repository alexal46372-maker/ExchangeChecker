package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 12 (CHK-MON-20): mute until recovered. State file next to
// ack-state.json.

// MutesFile is the mute state file next to the config
// (%ProgramData%\ExchangeChecker\mute-state.json).
const MutesFile = "mute-state.json"

const mutesFileVersion = 1

// MuteRecord is one muted profile.
type MuteRecord struct {
	ProfileID string    `json:"profileId"`
	Since     time.Time `json:"since"`
}

// MuteStore persists the mutes (FileMuteStore in the service).
type MuteStore interface {
	Load() ([]MuteRecord, error)
	Save([]MuteRecord) error
}

// MemMuteStore is an in-memory MuteStore (tests, default).
type MemMuteStore struct {
	mu   sync.Mutex
	recs []MuteRecord
}

// Load implements MuteStore.
func (s *MemMuteStore) Load() ([]MuteRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]MuteRecord(nil), s.recs...), nil
}

// Save implements MuteStore.
func (s *MemMuteStore) Save(r []MuteRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs = append([]MuteRecord(nil), r...)
	return nil
}

type mutesDoc struct {
	Version int          `json:"version"`
	Mutes   []MuteRecord `json:"mutes"`
}

// FileMuteStore keeps the mutes in dir/MutesFile (atomic write like
// FileAckStore). A corrupt file is kept as mute-state.json.corrupt.
type FileMuteStore struct {
	dir, name string
	log       Logger
	mu        sync.Mutex
	write     func(dir, name string, data []byte, perm os.FileMode) error
}

// OpenFileMuteStore returns the store of dir.
func OpenFileMuteStore(dir string, log Logger) *FileMuteStore {
	if log == nil {
		log = nopLogger{}
	}
	return &FileMuteStore{dir: dir, name: MutesFile, log: log, write: config.WriteFileAtomic}
}

// Path of the mute-state file.
func (s *FileMuteStore) Path() string { return filepath.Join(s.dir, s.name) }

// Load implements MuteStore.
func (s *FileMuteStore) Load() ([]MuteRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc mutesDoc
	if err := json.Unmarshal(b, &doc); err != nil || doc.Version != mutesFileVersion {
		bad := s.Path() + ".corrupt"
		_ = os.WriteFile(bad, b, 0o644)
		s.log.Printf("mute state: corrupt %s kept as %s: %v", s.Path(), bad, err)
		return nil, nil
	}
	return doc.Mutes, nil
}

// Save implements MuteStore.
func (s *FileMuteStore) Save(recs []MuteRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if recs == nil {
		recs = []MuteRecord{}
	}
	b, err := json.MarshalIndent(mutesDoc{Version: mutesFileVersion, Mutes: recs}, "", " ")
	if err != nil {
		return err
	}
	if err := s.write(s.dir, s.name, b, 0o644); err != nil {
		s.log.Printf("mute state: save %s: %v", s.Path(), err)
		return err
	}
	return nil
}

// Errors of unmute_profile.
var ErrNoActiveMute = &model.IPCError{Code: model.ErrCodeNoActiveMute, Key: model.MsgMonitorNoActiveMute,
	Message: "monitor: profile is not muted"}

func (m *Monitor) loadMutes() {
	if m.opt.Mutes == nil {
		return
	}
	recs, err := m.opt.Mutes.Load()
	if err != nil {
		m.opt.Log.Printf("mute state: %v", err)
	}
	for _, r := range recs {
		if r.ProfileID == "" {
			continue
		}
		m.mutes[r.ProfileID] = r
	}
}

func (m *Monitor) saveMutes() {
	if m.opt.Mutes == nil {
		return
	}
	m.muteSave.Lock()
	defer m.muteSave.Unlock()
	m.mu.Lock()
	recs := make([]MuteRecord, 0, len(m.mutes))
	for _, r := range m.mutes {
		recs = append(recs, r)
	}
	m.mu.Unlock()
	sort.Slice(recs, func(i, j int) bool { return recs[i].ProfileID < recs[j].ProfileID })
	if err := m.opt.Mutes.Save(recs); err != nil {
		m.opt.Log.Printf("mute state: %v", err)
	}
}

// applyMutesLocked clears mutes of profiles that returned to OK or were
// removed; fills ProfileSnapshot.Muted / MutedSince. Reports whether the
// mute set changed.
func (m *Monitor) applyMutesLocked(profiles []model.ProfileSnapshot) bool {
	changed := false
	alive := map[string]bool{}
	for i := range profiles {
		ps := &profiles[i]
		alive[ps.ProfileID] = true
		if r, ok := m.mutes[ps.ProfileID]; ok {
			if ps.Level == model.LevelOK && ps.Polled && ps.Error == "" {
				delete(m.mutes, ps.ProfileID)
				changed = true
				continue
			}
			since := r.Since.In(model.DBZone)
			ps.Muted, ps.MutedSince = true, &since
		}
	}
	for id := range m.mutes {
		if !alive[id] {
			delete(m.mutes, id)
			changed = true
		}
	}
	return changed
}

func (m *Monitor) mutedLocked(profileID string) bool {
	_, ok := m.mutes[profileID]
	return ok
}

// MuteUntilRecovered mutes profileID until it returns to OK or
// UnmuteProfile (IPC mute_until_recovered, CHK-MON-20). Idempotent when
// already muted (refreshes nothing; returns muted). Errors:
// ErrUnknownProfile.
func (m *Monitor) MuteUntilRecovered(profileID string) error {
	now := m.opt.Now().In(model.DBZone)
	m.mu.Lock()
	if _, ok := m.entries[profileID]; !ok || profileID == "" {
		m.mu.Unlock()
		return ErrUnknownProfile
	}
	if _, ok := m.mutes[profileID]; !ok {
		m.mutes[profileID] = MuteRecord{ProfileID: profileID, Since: now}
	}
	m.mu.Unlock()
	m.saveMutes()
	m.publish(nil) // refresh status muted flags
	return nil
}

// UnmuteProfile clears the mute of profileID (IPC unmute_profile).
// Errors: ErrUnknownProfile, ErrNoActiveMute.
func (m *Monitor) UnmuteProfile(profileID string) error {
	m.mu.Lock()
	if _, ok := m.entries[profileID]; !ok || profileID == "" {
		m.mu.Unlock()
		return ErrUnknownProfile
	}
	if _, ok := m.mutes[profileID]; !ok {
		m.mu.Unlock()
		return ErrNoActiveMute
	}
	delete(m.mutes, profileID)
	m.mu.Unlock()
	m.saveMutes()
	m.publish(nil)
	return nil
}

// mutedOutbound reports whether outbound alert / alert_repeat / Viber /
// Event Log for profileID must be suppressed.
func (m *Monitor) mutedOutbound(profileID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mutedLocked(profileID)
}
