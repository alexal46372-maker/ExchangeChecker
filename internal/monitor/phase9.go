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
		page.Reset = true
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
// rate_limited with RetryAfterMs. CheckSeq: the result is in the first
// status with seq > CheckSeq. poll_now is not affected.
func (m *Monitor) CheckNow(profileID string) (model.CheckResult, error) {
	m.mu.Lock()
	seq := m.seq
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
	res := model.CheckResult{CheckSeq: seq}
	if m.pendAll || (profileID != "" && (m.pendIDs[profileID] || (e != nil && e.busyNow()))) {
		res.Coalesced = true
		return res, nil
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
	select {
	case m.pollNow <- struct{}{}:
	default:
	}
	return res, nil
}
