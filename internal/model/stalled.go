package model

import (
	"fmt"
	"time"
)

// Phase 11 (EC Service, CHK-MON-18): the stalled-exchange detector. A
// counters-mode point (1C _NodeNNN, message numbers _SentNo / _ReceivedNo
// read by sqlcheck.ReadNodeStates) is stalled when neither number changed
// for more than the stalled threshold while the exchange is otherwise
// alive: the poll succeeds, the point itself is not already warning /
// error by lag (in counters mode FreshAt is the last number change, so a
// stall is then the same fact as the lag alarm — no duplicate alert), and,
// when the profile has a time-based point of the same node (NodeKey, the
// 1C node key / code; never the display name), that point's exchange
// time is newer than the last number change. Consequence: a stall alert
// appears only when the lag thresholds exceed the stalled threshold. Since = the last number change (its
// FreshAt); a baseline (first observation) counts from the observation,
// so nothing is flagged before the threshold passed since the service
// first saw the numbers. The point / profile level is not changed; the
// stall is its own warning alert (problems kind "stalled", transitions
// in events_since / history / Event Log / Viber "warning" + "recovered").
const (
	CapStalled = "stalled"
	// CapAckEvent: ack_alert {eventId} acknowledges only that alert; the
	// legacy {profileId, pointKey} acks every alert of the point (lag +
	// stalled).
	CapAckEvent           = "ack_event"
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
	peers := map[string]time.Time{} // time-based points by node key
	for _, pt := range ps.Points {
		if pt.Source != PointSourceCounters && pt.FreshAt != nil && pt.NodeKey != "" {
			if t, ok := peers[pt.NodeKey]; !ok || pt.FreshAt.After(t) {
				peers[pt.NodeKey] = *pt.FreshAt
			}
		}
	}
	for i := range ps.Points {
		pt := &ps.Points[i]
		if pt.Source != PointSourceCounters || pt.FreshAt == nil || now.Sub(*pt.FreshAt) <= after {
			continue
		}
		if pt.Level != LevelOK {
			continue // already alarmed by lag: the stall would duplicate it
		}
		if t, ok := peers[pt.NodeKey]; pt.NodeKey != "" && ok && !t.After(*pt.FreshAt) {
			continue // the exchange time stopped too: a lag alarm, not a stall
		}
		since := pt.FreshAt.In(DBZone)
		pt.Stalled, pt.StalledSince = true, &since
		ps.Stalled = true
	}
}
