package monitor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 9 (EC Service): check_now (CHK-SVC-04) and the transition-event
// feed events_since with the run id (CHK-SVC-05).

// newRunID returns 16 random hex characters (a time-based fallback if the
// system RNG fails).
func newRunID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// RunID returns the run id of this monitor (heartbeat, AlertEvent.RunID).
func (m *Monitor) RunID() string { return m.runID }

// eventRing keeps the last size transition events of this run (m.mu).
type eventRing struct {
	size    int
	evs     []model.AlertEvent
	lastID  uint64
	evicted bool
}

func (r *eventRing) add(e model.AlertEvent) {
	r.evs = append(r.evs, e)
	if r.size > 0 && len(r.evs) > r.size {
		n := copy(r.evs, r.evs[len(r.evs)-r.size:])
		r.evs = r.evs[:n]
		r.evicted = true
	}
	if e.ID > r.lastID {
		r.lastID = e.ID
	}
}

// EventsSince returns the events of run runID with ID > since, at most
// limit (0 = model.EventsDefaultLimit, capped at model.EventsMaxLimit),
// oldest first. Reset is set (and no events returned) when runID is not
// this run, since lies in the future, or events after since were already
// dropped from the buffer (model.EventRingSize): the client then takes the
// active alerts from get_status and continues from LastID.
func (m *Monitor) EventsSince(runID string, since uint64, limit int) model.EventsPage {
	if limit <= 0 {
		limit = model.EventsDefaultLimit
	}
	if limit > model.EventsMaxLimit {
		limit = model.EventsMaxLimit
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := &m.ring
	page := model.EventsPage{RunID: m.runID, LastID: r.lastID}
	gap := r.evicted && len(r.evs) > 0 && since+1 < r.evs[0].ID
	if runID != m.runID || since > r.lastID || gap {
		page.ResetRequired = true
		return page
	}
	for _, e := range r.evs {
		if e.ID <= since {
			continue
		}
		if len(page.Events) == limit {
			page.Truncated = true
			break
		}
		page.Events = append(page.Events, e)
	}
	if n := len(page.Events); n > 0 {
		page.LastID = page.Events[n-1].ID
	}
	return page
}

// classifyEventLocked sets RunID, EventID, Kind and, for a recovery,
// DurationMin (minutes since the point / profile left ok in this run), and
// adds the event to the ring (m.mu).
func (m *Monitor) classifyEventLocked(e *model.AlertEvent) {
	e.RunID = m.runID
	e.EventID = e.ID
	e.Kind = model.AlertKindAlert
	k := e.ProfileID + "\x00" + e.Key
	switch {
	case e.To == model.LevelOK:
		e.Kind = model.AlertKindRecovered
		if t, ok := m.raisedAt[k]; ok {
			d := int64(e.At.Sub(t) / time.Minute)
			if d < 0 {
				d = 0
			}
			e.DurationMin = &d
			delete(m.raisedAt, k)
		}
	case e.From == model.LevelOK:
		m.raisedAt[k] = e.At
	}
	m.ring.add(*e)
}

// Rate-limiter keys of check_now (separate from poll_now's).
const (
	checkKeyAll     = "all"
	checkKeyProfile = "p:"
)

// CheckNow handles check_now (CHK-SVC-04): an immediate poll of every
// enabled profile ("") or of profileID. A running or queued poll of the
// same profile(s) is joined (Coalesced). Executed checks are limited to
// one per model.CheckAllInterval for all profiles and one per
// model.CheckProfileInterval per profile (also not within
// CheckProfileInterval after a check of all profiles); a refused check is
// rate_limited with RetryAfterMs. CheckSeq numbers the request (its own
// counter, not the status seq); the check is complete when a status has
// LastCheckSeq >= CheckSeq (the only completion rule). poll_now is not
// affected.
func (m *Monitor) CheckNow(profileID string) (model.CheckResult, error) {
	m.mu.Lock()
	found, enabled := false, false
	for _, p := range m.cfg.Profiles {
		if p.ID == profileID {
			found, enabled = true, p.Enabled
			break
		}
	}
	e := m.entries[profileID]
	m.mu.Unlock()
	if profileID != "" {
		if !found {
			return model.CheckResult{}, ErrUnknownProfile
		}
		if !enabled {
			return model.CheckResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorProfileOff,
				Message: "monitor: the profile is disabled", Args: []string{"profileId"}}
		}
	}
	now := m.opt.Now()
	m.cmdMu.Lock()
	defer m.cmdMu.Unlock()
	if m.pendAll || (profileID != "" && (m.pendIDs[profileID] || (e != nil && e.busyNow()))) {
		return model.CheckResult{Coalesced: true, CheckSeq: m.registerCheck(profileID)}, nil
	}
	var wait time.Duration
	check := func(key string, d time.Duration) {
		if t, ok := m.checkLast[key]; ok {
			if el := now.Sub(t); el >= 0 && el < d && d-el > wait {
				wait = d - el
			}
		}
	}
	if profileID == "" {
		check(checkKeyAll, model.CheckAllInterval)
	} else {
		check(checkKeyProfile+profileID, model.CheckProfileInterval)
		check(checkKeyAll, model.CheckProfileInterval)
	}
	if wait > 0 {
		ms := (wait + time.Millisecond - 1) / time.Millisecond
		return model.CheckResult{}, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgCheckRateLimited,
			Message:      "monitor: check_now rate limited, retry after " + strconv.FormatInt(int64(ms), 10) + " ms",
			RetryAfterMs: int64(ms)}
	}
	if profileID == "" {
		m.checkLast[checkKeyAll] = now
		m.pendAll = true
	} else {
		m.checkLast[checkKeyProfile+profileID] = now
		m.pendIDs[profileID] = true
	}
	res := model.CheckResult{CheckSeq: m.registerCheck(profileID)}
	select {
	case m.pollNow <- struct{}{}:
	default:
	}
	return res, nil
}

// registerCheck numbers an accepted / coalesced check_now of scope
// profileID ("" = all) and marks it pending (cmdMu held).
func (m *Monitor) registerCheck(profileID string) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkCounter++
	if m.checkPend == nil {
		m.checkPend = map[string]uint64{}
	}
	m.checkPend[profileID] = m.checkCounter
	if profileID == "" {
		// phase 10 (CHK-MON-16): complete when every enabled profile has
		// started a poll after this request
		m.checkAllWait = map[string]bool{}
		for _, p := range m.cfg.Profiles {
			if p.Enabled && m.entries[p.ID] != nil {
				m.checkAllWait[p.ID] = true
			}
		}
	}
	return m.checkCounter
}

// coverCheckLocked completes the pending checks covered by a published
// worker poll of profile id that started when the check counter was
// startSeq: the profile's own check and, once every awaited profile
// published such a poll, the check of all profiles (m.mu held,
// CHK-MON-16). A check registered after the poll started stays pending.
func (m *Monitor) coverCheckLocked(id string, startSeq uint64) {
	if s, ok := m.checkPend[id]; ok && s <= startSeq {
		delete(m.checkPend, id)
	}
	if s, ok := m.checkPend[""]; ok && s <= startSeq && m.checkAllWait != nil {
		delete(m.checkAllWait, id)
		if len(m.checkAllWait) == 0 {
			delete(m.checkPend, "")
			m.checkAllWait = nil
		}
	}
}

// takeChecksLocked removes the pending checks a poll starting now covers:
// all of them for a full cycle (only == nil), else those of the polled
// profiles (m.mu held). A check registered later stays pending: the poll
// that started before it does not complete it.
func (m *Monitor) takeChecksLocked(only map[string]bool) {
	for scope := range m.checkPend {
		if only == nil || (scope != "" && only[scope]) {
			delete(m.checkPend, scope)
		}
	}
	if only == nil {
		m.checkAllWait = nil
	}
}

// settleChecksLocked advances lastCheckSeq when a poll is published: every
// check below the oldest still pending one is complete (m.mu held).
func (m *Monitor) settleChecksLocked() {
	done := m.checkCounter
	for _, s := range m.checkPend {
		if s-1 < done {
			done = s - 1
		}
	}
	if done > m.lastCheckSeq {
		m.lastCheckSeq = done
	}
}

// dropChecksLocked removes the pending checks of profiles that were
// deleted or disabled (no poll will cover them) and settles lastCheckSeq,
// so later checks do not wait for a full cycle (m.mu held).
func (m *Monitor) dropChecksLocked(profiles []model.Profile) {
	enabled := make(map[string]bool, len(profiles))
	for _, p := range profiles {
		enabled[p.ID] = p.Enabled
	}
	changed := false
	for scope := range m.checkPend {
		if scope != "" && !enabled[scope] {
			delete(m.checkPend, scope)
			changed = true
		}
	}
	for id := range m.checkAllWait { // phase 10 (CHK-MON-16)
		if !enabled[id] {
			delete(m.checkAllWait, id)
			if len(m.checkAllWait) == 0 {
				delete(m.checkPend, "")
				m.checkAllWait = nil
			}
			changed = true
		}
	}
	if changed {
		m.settleChecksLocked()
	}
}
