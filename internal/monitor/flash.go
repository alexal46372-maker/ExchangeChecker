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
// Muted profiles do not flash (CHK-MON-20); disabled profiles are not
// observed and their alerts are dropped → flash_stop.
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

// DismissFlash implements ipc.FlashDismisser (CHK-IPC-19): no more flash
// pushes for the active alert eventID until it recovers.
func (m *Monitor) DismissFlash(eventID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ev := range m.tracker.Active() {
		if ev.ID == eventID && eventID != 0 && ev.To != model.LevelOK {
			if m.flashDismissed == nil {
				m.flashing, m.flashDismissed = map[string]flashEntry{}, map[uint64]bool{}
			}
			m.flashDismissed[eventID] = true
			return nil
		}
	}
	return ErrUnknownEvent
}
