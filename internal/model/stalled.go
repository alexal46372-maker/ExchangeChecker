package model

import (
	"fmt"
	"time"
)

// Phase 11 (EC Service, CHK-MON-18): the stalled-exchange detector. A
// counters-mode point (1C _NodeNNN, message numbers _SentNo / _ReceivedNo
// read by sqlcheck.ReadNodeStates) is stalled when neither number changed
// for more than the stalled threshold while the exchange is otherwise
// alive: the poll succeeds and, when the profile has a time-based point
// of the same name (another mapping), that point's exchange time is
// newer than the last number change. Since = the last number change (its
// FreshAt); a baseline (first observation) counts from the observation,
// so nothing is flagged before the threshold passed since the service
// first saw the numbers. The point / profile level is not changed; the
// stall is its own warning alert (problems kind "stalled", transitions
// in events_since / history / Event Log / Viber "warning" + "recovered").
const (
	CapStalled            = "stalled"
	ProblemStalled        = "stalled"
	DefaultStalledMinutes = 120
	MinStalledMinutes     = 10
	MaxStalledMinutes     = 1440
	MsgMonitorStalled     = "monitor.stalled"
	// StalledKeySuffix makes the tracker key of a point's stall alert.
	StalledKeySuffix = "#stalled"
)

func validStalled(v int) bool {
	return v == 0 || (v >= MinStalledMinutes && v <= MaxStalledMinutes)
}

func stalledErr(field string, v int) error {
	return fieldErr(field, ValRange, fmt.Sprintf("%s must be 0 or %d..%d, got %d", field, MinStalledMinutes, MaxStalledMinutes, v),
		itoa(MinStalledMinutes), itoa(MaxStalledMinutes), itoa(v))
}

// StalledAfter is the effective threshold of profile p (0 = off):
// profile.stalledMinutes (0 = inherit), else settings.stalledMinutes
// (absent = DefaultStalledMinutes, 0 = off).
func (p Profile) StalledAfter(global Settings) time.Duration {
	v := DefaultStalledMinutes
	if global.StalledMinutes != nil {
		v = *global.StalledMinutes
	}
	if p.StalledMinutes > 0 {
		v = p.StalledMinutes
	}
	return time.Duration(v) * time.Minute
}

// MarkStalled sets PointStatus.Stalled / StalledSince and
// ProfileSnapshot.Stalled of a polled snapshot (no error) at now.
func MarkStalled(ps *ProfileSnapshot, after time.Duration, now time.Time) {
	ps.Stalled = false
	for i := range ps.Points {
		ps.Points[i].Stalled, ps.Points[i].StalledSince = false, nil
	}
	if after <= 0 || ps.Error != "" {
		return
	}
	peers := map[string]time.Time{} // time-based points by name
	for _, pt := range ps.Points {
		if pt.Source != PointSourceCounters && pt.FreshAt != nil {
			if t, ok := peers[pt.Name]; !ok || pt.FreshAt.After(t) {
				peers[pt.Name] = *pt.FreshAt
			}
		}
	}
	for i := range ps.Points {
		pt := &ps.Points[i]
		if pt.Source != PointSourceCounters || pt.FreshAt == nil || now.Sub(*pt.FreshAt) <= after {
			continue
		}
		if t, ok := peers[pt.Name]; ok && !t.After(*pt.FreshAt) {
			continue // the exchange time stopped too: a lag alarm, not a stall
		}
		since := pt.FreshAt.In(DBZone)
		pt.Stalled, pt.StalledSince = true, &since
		ps.Stalled = true
	}
}
