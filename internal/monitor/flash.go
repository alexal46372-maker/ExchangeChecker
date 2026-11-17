package monitor

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 13 item 7 (CHK-MON-22): flash pushes. See model/flash.go.

// ErrUnknownEvent: dismiss_flash for an eventId that is not active.
var ErrUnknownEvent = &model.IPCError{Code: model.ErrCodeNotFound, Key: model.MsgMonitorUnknownEvent, Message: "monitor: unknown or recovered event"}

type flashEntry struct {
	id                  uint64
	profileID, pointKey string
}

func flashKey(ev model.AlertEvent) string {
	k := ev.ProfileID + "\x00" + ev.Key
	if ev.Stalled {
		k += model.StalledKeySuffix
	}
	return k
}

// flashesLocked computes the flash / flash_stop pushes of one publish
// (m.mu held). cands: transitions into an alert and repeats of active
// alerts (every failing check); active: the tracker's active alerts.
// Muted profiles do not flash (CHK-MON-20); disabled or removed
// profiles have already left the tracker (reload, CHK-MON-23) →
// flash_stop. flashDismissed is set by dismiss_flash (a suspend:
// flash_stop is also pushed then) and is cleared by resume_flash, or
// when the eventID is no longer active (recovery, disable, delete,
// mute). A dismiss therefore does not survive disable: re-enable of the
// same problem raises a new eventID and can flash again. resume_flash
// of an active, unacked event clears the mark and immediately pushes
// flash with the same eventID (CHK-MON-24).
func (m *Monitor) flashesLocked(cands, active []model.AlertEvent) []model.FlashEvent {
	if m.flashing == nil {
		m.flashing, m.flashDismissed = map[string]flashEntry{}, map[uint64]bool{}
	}
	act := map[uint64]bool{}
	for _, a := range active {
		if a.To != model.LevelOK {
			act[a.ID] = true
		}
	}
	var out []model.FlashEvent
	for _, ev := range cands {
		if ev.To == model.LevelOK || !act[ev.ID] || m.mutedLocked(ev.ProfileID) {
			continue
		}
		m.flashing[flashKey(ev)] = flashEntry{id: ev.ID, profileID: ev.ProfileID, pointKey: ev.Key}
		if m.flashDismissed[ev.ID] {
			continue
		}
		out = append(out, model.FlashEvent{ProfileID: ev.ProfileID, PointKey: ev.Key, EventID: ev.ID, Level: ev.To})
	}
	for k, f := range m.flashing {
		if act[f.id] && !m.mutedLocked(f.profileID) {
			continue
		}
		delete(m.flashing, k)
		delete(m.flashDismissed, f.id)
		out = append(out, model.FlashEvent{ProfileID: f.profileID, PointKey: f.pointKey, EventID: f.id, Stop: true})
	}
	for id := range m.flashDismissed {
		if !act[id] {
			delete(m.flashDismissed, id)
		}
	}
	return out
}

// deliverFlashes sends the pushes of one publish in order. Publishes run
// concurrently (workers, mute, set_enabled, reload) and compute their
// pushes under m.mu but send after it: a flash computed by an older
// publish could otherwise reach the clients after the flash_stop of a
// newer one (eternal flashing). flashSendMu serializes delivery, and a
// flash is re-checked under m.mu right before it is sent: dropped when
// its alert is no longer flashing (stopped / muted / dismissed / new
// alert on the key) — a stop computed later is always sent after it.
func (m *Monitor) deliverFlashes(fs []model.FlashEvent) {
	if m.opt.OnFlash == nil || len(fs) == 0 {
		return
	}
	m.flashSendMu.Lock()
	defer m.flashSendMu.Unlock()
	for _, f := range fs {
		if !f.Stop && !m.flashCurrent(f) {
			continue
		}
		m.opt.OnFlash(f)
	}
}

func (m *Monitor) flashCurrent(f model.FlashEvent) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mutedLocked(f.ProfileID) || m.flashDismissed[f.EventID] {
		return false
	}
	for _, e := range m.flashing {
		if e.id == f.EventID && e.profileID == f.ProfileID {
			return true
		}
	}
	return false
}

// DismissFlash implements ipc.FlashDismisser (CHK-IPC-19, phase 17
// CHK-MON-24): suspend flashing of the active alert eventID. flash_stop
// is pushed immediately; flashDismissed suppresses further flashes until
// resume_flash, recovery, disable, delete or mute.
func (m *Monitor) DismissFlash(eventID uint64) error {
	m.mu.Lock()
	var stop model.FlashEvent
	ok := false
	for _, ev := range m.tracker.Active() {
		if ev.ID == eventID && eventID != 0 && ev.To != model.LevelOK {
			if m.flashDismissed == nil {
				m.flashing, m.flashDismissed = map[string]flashEntry{}, map[uint64]bool{}
			}
			m.flashDismissed[eventID] = true
			stop = model.FlashEvent{ProfileID: ev.ProfileID, PointKey: ev.Key, EventID: ev.ID, Stop: true}
			ok = true
			break
		}
	}
	m.mu.Unlock()
	if !ok {
		return ErrUnknownEvent
	}
	m.deliverFlashes([]model.FlashEvent{stop})
	return nil
}

// ResumeFlash implements ipc.FlashResumer (CHK-MON-24 / CHK-IPC-22):
// clear flashDismissed for eventID. If that event is still an active,
// unacked, unmuted alert, immediately push flash with the same eventID
// and return true. Otherwise leave state alone and return false (acked,
// recovered, unknown, muted, or another event). eventID 0 is a no-op
// (the IPC layer already returns invalid_request).
func (m *Monitor) ResumeFlash(eventID uint64) (bool, error) {
	if eventID == 0 {
		return false, nil
	}
	m.mu.Lock()
	var ev model.AlertEvent
	found := false
	for _, a := range m.tracker.Active() {
		if a.ID == eventID && a.To != model.LevelOK {
			ev, found = a, true
			break
		}
	}
	if !found || m.mutedLocked(ev.ProfileID) {
		m.mu.Unlock()
		return false, nil
	}
	if _, acked := m.acks[eventKey(ev)]; acked {
		m.mu.Unlock()
		return false, nil
	}
	delete(m.flashDismissed, eventID)
	if m.flashing == nil {
		m.flashing, m.flashDismissed = map[string]flashEntry{}, map[uint64]bool{}
	}
	m.flashing[flashKey(ev)] = flashEntry{id: ev.ID, profileID: ev.ProfileID, pointKey: ev.Key}
	flash := model.FlashEvent{ProfileID: ev.ProfileID, PointKey: ev.Key, EventID: ev.ID, Level: ev.To}
	m.mu.Unlock()
	m.deliverFlashes([]model.FlashEvent{flash})
	return true, nil
}
