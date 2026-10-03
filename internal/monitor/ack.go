package monitor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Alarm acknowledgements (ack_alert, phase 4, CHK-IPC-02).
//
// An acknowledgement belongs to one tracker key (profile-level alert or
// one point) and remembers the level it acknowledged. The alert stays
// active and visible (Acknowledged flag in the snapshot and in the alert
// events); the tray stops repeating its notification. Rules, applied after
// every poll against the debounced (confirmed) level of the key:
//   - the level returned to OK       -> the acknowledgement is dropped;
//   - the level got worse            -> dropped (the new alarm must be
//     noticed: warning -> error);
//   - the level improved but not OK  -> kept, its level lowered
//     (error -> warning stays acknowledged);
//   - the key is not confirmed yet   -> kept (right after a restart the
//     first observations are debounced; the same alarm coming back after
//     the restart is still acknowledged).
//
// Acknowledgements of removed profiles / points are dropped after a
// successful config load, like the counters state. They are stored in
// AcksFile next to the config (atomic write, MoveFileEx on Windows) and
// survive a service restart. The Windows Event Log is not changed by an
// acknowledgement except one optional informational entry (Options.OnAck).

// AcksFile is the acknowledgement state file next to the config
// (%ProgramData%\ExchangeChecker\ack-state.json).
const AcksFile = "ack-state.json"

const acksFileVersion = 1

// AckRecord is one acknowledgement.
type AckRecord struct {
	ProfileID string `json:"profileId"`
	// PointKey is the point (PointStatus.Key); "" = profile-level alert.
	PointKey string           `json:"pointKey,omitempty"`
	Level    model.AlertLevel `json:"level"`
	At       time.Time        `json:"at"`
}

// trackerKey is the alert tracker key of the record.
func (r AckRecord) trackerKey() string {
	if r.PointKey == "" {
		return alert.ProfileKey(r.ProfileID)
	}
	return alert.PointKey(r.ProfileID, r.PointKey)
}

// eventKey is the tracker key of an alert event.
func eventKey(ev model.AlertEvent) string {
	if ev.ProfileLevel() {
		return alert.ProfileKey(ev.ProfileID)
	}
	return alert.PointKey(ev.ProfileID, ev.Key)
}

// AckStore persists the acknowledgements (FileAckStore in the service).
type AckStore interface {
	Load() ([]AckRecord, error)
	Save([]AckRecord) error
}

// MemAckStore is an in-memory AckStore (tests, default).
type MemAckStore struct {
	mu   sync.Mutex
	recs []AckRecord
}

// Load implements AckStore.
func (s *MemAckStore) Load() ([]AckRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AckRecord(nil), s.recs...), nil
}

// Save implements AckStore.
func (s *MemAckStore) Save(r []AckRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs = append([]AckRecord(nil), r...)
	return nil
}

type acksDoc struct {
	Version int         `json:"version"`
	Acks    []AckRecord `json:"acks"`
}

// FileAckStore keeps the acknowledgements in dir/AcksFile, rewritten
// atomically (config.WriteFileAtomic: temp + MoveFileExW
// REPLACE_EXISTING|WRITE_THROUGH on Windows). A corrupt file is kept as
// ack-state.json.corrupt and the state starts empty (alarms are simply
// not acknowledged); a write error is logged and returned.
type FileAckStore struct {
	dir, name string
	log       Logger
	mu        sync.Mutex
	write     func(dir, name string, data []byte, perm os.FileMode) error
}

// OpenFileAckStore returns the store of dir (the file is read by Load).
func OpenFileAckStore(dir string, log Logger) *FileAckStore {
	if log == nil {
		log = nopLogger{}
	}
	return &FileAckStore{dir: dir, name: AcksFile, log: log, write: config.WriteFileAtomic}
}

// Path returns the state file path.
func (s *FileAckStore) Path() string { return filepath.Join(s.dir, s.name) }

// Load implements AckStore (missing file = empty).
func (s *FileAckStore) Load() ([]AckRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := config.ReadFile(s.Path()) // FILE_SHARE_DELETE on Windows
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		s.log.Printf("ack state: read %s: %v (no alarm is acknowledged)", s.Path(), err)
		return nil, err
	}
	var doc acksDoc
	err = json.Unmarshal(b, &doc)
	if err == nil && doc.Version != acksFileVersion {
		err = fmt.Errorf("unsupported version %d", doc.Version)
	}
	if err != nil {
		bad := s.Path() + ".corrupt"
		_ = os.Remove(bad)
		if rerr := os.Rename(s.Path(), bad); rerr != nil {
			bad = "(not kept: " + rerr.Error() + ")"
		}
		s.log.Printf("ack state: %s is corrupt: %v; acknowledgements reset, old file kept as %s", s.Path(), err, bad)
		return nil, err
	}
	return doc.Acks, nil
}

// Save implements AckStore.
func (s *FileAckStore) Save(recs []AckRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if recs == nil {
		recs = []AckRecord{}
	}
	b, err := json.MarshalIndent(acksDoc{Version: acksFileVersion, Acks: recs}, "", " ")
	if err != nil {
		return err
	}
	if err := s.write(s.dir, s.name, b, 0o644); err != nil {
		s.log.Printf("ack state: save %s: %v", s.Path(), err)
		return err
	}
	return nil
}

// Errors of AckAlert (IPC codes not_found / no_active_alert).
var (
	ErrUnknownPoint  = &model.IPCError{Code: model.ErrCodeNotFound, Key: model.MsgMonitorUnknownPoint, Message: "monitor: unknown point"}
	ErrNoActiveAlert = &model.IPCError{Code: model.ErrCodeNoActiveAlert, Key: model.MsgMonitorNoActiveAlert, Message: "monitor: no active alert to acknowledge"}
)

// loadAcks reads the store into m.acks (New).
func (m *Monitor) loadAcks() {
	recs, err := m.opt.Acks.Load()
	if err != nil {
		m.opt.Log.Printf("ack state: %v", err)
	}
	for _, r := range recs {
		if r.ProfileID == "" || !r.Level.Valid() || r.Level == model.LevelOK {
			continue
		}
		m.acks[r.trackerKey()] = r
	}
}

// saveAcks writes the current acknowledgements (serialized, the latest
// state wins).
func (m *Monitor) saveAcks() {
	m.ackSave.Lock()
	defer m.ackSave.Unlock()
	m.mu.Lock()
	recs := make([]AckRecord, 0, len(m.acks))
	for _, r := range m.acks {
		recs = append(recs, r)
	}
	m.mu.Unlock()
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].ProfileID != recs[j].ProfileID {
			return recs[i].ProfileID < recs[j].ProfileID
		}
		return recs[i].PointKey < recs[j].PointKey
	})
	if err := m.opt.Acks.Save(recs); err != nil {
		m.opt.Log.Printf("ack state: %v", err)
	}
}

// applyAcksLocked applies the reset rules to every acknowledgement (m.mu
// held); keep (nil = everything) drops the ones of removed keys. It
// reports whether anything changed.
func (m *Monitor) applyAcksLocked(keep func(string) bool) bool {
	changed := false
	for k, r := range m.acks {
		if keep != nil && !keep(k) {
			delete(m.acks, k)
			changed = true
			continue
		}
		lvl, ok := m.tracker.Confirmed(k)
		if !ok {
			continue
		}
		switch {
		case lvl == model.LevelOK || lvl > r.Level:
			delete(m.acks, k)
			changed = true
		case lvl < r.Level:
			r.Level = lvl
			m.acks[k] = r
			changed = true
		}
	}
	return changed
}

// markAckedLocked sets the Acknowledged flags of a snapshot being built
// (m.mu held). Points are copied: they are shared with m.results.
func (m *Monitor) markAckedLocked(profiles []model.ProfileSnapshot, alerts []model.AlertEvent) {
	if len(m.acks) == 0 {
		return
	}
	for i := range profiles {
		ps := &profiles[i]
		_, ps.Acknowledged = m.acks[alert.ProfileKey(ps.ProfileID)]
		pts := make([]model.PointStatus, len(ps.Points))
		copy(pts, ps.Points)
		for j := range pts {
			_, pts[j].Acknowledged = m.acks[alert.PointKey(ps.ProfileID, pts[j].Key)]
		}
		ps.Points = pts
	}
	for i := range alerts {
		m.markEventLocked(&alerts[i])
	}
}

// markEventLocked sets Acknowledged / AckedAt of one event (m.mu held).
func (m *Monitor) markEventLocked(ev *model.AlertEvent) {
	if r, ok := m.acks[eventKey(*ev)]; ok && ev.To != model.LevelOK {
		at := r.At
		ev.Acknowledged, ev.AckedAt = true, &at
	}
}

// AckAlert acknowledges the active alerts of profileID: every one of them
// (pointKey "") or only the point pointKey (IPC "ack_alert",
// CHK-IPC-02). It returns the number of acknowledged alerts (already
// acknowledged ones included). Errors: ErrUnknownProfile (deleted /
// unknown profile), ErrUnknownPoint, ErrNoActiveAlert.
func (m *Monitor) AckAlert(profileID, pointKey string) (int, error) {
	now := m.opt.Now().In(model.DBZone)
	m.mu.Lock()
	if _, ok := m.entries[profileID]; !ok || profileID == "" {
		m.mu.Unlock()
		return 0, ErrUnknownProfile
	}
	// a point is known from the last snapshot or from the tracker (its
	// points are not listed while the profile is in error)
	pointKnown := pointKey == "" || m.tracker.Has(alert.PointKey(profileID, pointKey))
	for _, pt := range m.results[profileID].Points {
		if pt.Key == pointKey {
			pointKnown = true
		}
	}
	n := 0
	var fresh []model.AlertEvent
	for _, ev := range m.tracker.Active() {
		if ev.ProfileID != profileID || ev.To == model.LevelOK {
			continue
		}
		if pointKey != "" && (ev.ProfileLevel() || ev.Key != pointKey) {
			continue
		}
		pointKnown = true
		n++
		k := eventKey(ev)
		if r, ok := m.acks[k]; ok && r.Level == ev.To {
			continue
		}
		pk := ev.Key
		if ev.ProfileLevel() {
			pk = ""
		}
		m.acks[k] = AckRecord{ProfileID: profileID, PointKey: pk, Level: ev.To, At: now}
		ev.Acknowledged, ev.AckedAt = true, &now
		fresh = append(fresh, ev)
	}
	m.mu.Unlock()
	if !pointKnown {
		return 0, ErrUnknownPoint
	}
	if n == 0 {
		return 0, ErrNoActiveAlert
	}
	if len(fresh) > 0 {
		m.saveAcks()
		for _, ev := range fresh {
			m.opt.Log.Printf("alert acknowledged: %s", alert.FormatAck(ev, model.LangEN))
			if m.opt.OnAck != nil {
				m.opt.OnAck(ev)
			}
		}
		m.publish(nil)
	}
	return n, nil
}
