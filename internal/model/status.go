package model

import (
	"sort"
	"time"
)

// Status snapshot for the tray (phase 3, CHK-MON-01 / CHK-MON-06). These
// types are the IPC payload of the service's monitor; they live in model
// so the tray does not import the SQL driver. All times are instants
// expressed in DBZone (UTC+3, PLAN.md §4.5 / §12 p.3).

// PointStatus is one exchange point in a snapshot: the polled point plus
// the derived FreshAt = max(LastSend, LastRecv) and its lag (§12 p.2).
type PointStatus struct {
	ExchangePoint
	// FreshAt is max(LastSend, LastRecv) in UTC+3; nil when both are NULL.
	FreshAt *time.Time `json:"freshAt,omitempty"`
	// LagMinutes is now - FreshAt in whole minutes (-1 when unknown).
	LagMinutes int `json:"lagMinutes"`
	// Baseline: counters mode, the FreshAt time is only the first
	// observation after the service started ("counting since
	// observation", the real exchange happened at or before it).
	Baseline bool `json:"baseline,omitempty"`
	// Key identifies the point inside the profile (PointKey), e.g. for
	// ack_alert (protocol version 2).
	Key string `json:"key,omitempty"`
	// Acknowledged: the active alert of the point was acknowledged
	// (ack_alert); the level stays as it is (CHK-IPC-02).
	Acknowledged bool `json:"acknowledged,omitempty"`
	// Phase 11 (CHK-MON-18): Stalled since StalledSince (MarkStalled);
	// SentNo / ReceivedNo: the 1C message numbers (counters mode).
	Stalled      bool       `json:"stalled"`
	StalledSince *time.Time `json:"stalledSince,omitempty"`
}

// ProfileSnapshot is one database (profile) in a snapshot.
type ProfileSnapshot struct {
	ProfileID string `json:"profileId"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	// Level is the worst level of the points, LevelError on Error.
	Level  AlertLevel    `json:"level"`
	Points []PointStatus `json:"points"`
	// Error / ErrorKey: connection, query, timeout or mapping error of the
	// last poll (Level = Error). ErrorKey is a localizable message key
	// (sql.* from sqlcheck, monitor.* from MonitorMessage).
	Error    string `json:"error,omitempty"`
	ErrorKey string `json:"errorKey,omitempty"`
	// FreshAt is the freshest FreshAt of the points (nil if none).
	FreshAt *time.Time `json:"freshAt,omitempty"`
	// Baseline is set when any point is still "counting since observation".
	Baseline bool `json:"baseline,omitempty"`
	// Warnings are connection warning keys of the profile (e.g.
	// "sql.warn_plaintext_login", filled by the service from
	// sqlcheck.Warnings); they never change the level.
	Warnings     []string  `json:"warnings,omitempty"`
	WarnMinutes  int       `json:"warnMinutes"`
	ErrorMinutes int       `json:"errorMinutes"`
	CheckedAt    time.Time `json:"checkedAt"`
	// LastCheckSeq (phase 10, CHK-MON-17): every check_now with checkSeq
	// <= LastCheckSeq that covers this profile (its own or all profiles)
	// is included in this profile's data; 0 = none / not polled. Always
	// sent.
	LastCheckSeq uint64 `json:"lastCheckSeq"`
	// Stalled: any point is stalled (phase 11, CHK-MON-18). Always sent.
	Stalled bool `json:"stalled"`
	// Polled is false until the first poll of the profile finished.
	Polled bool `json:"polled"`
	// Acknowledged: the profile-level alert (connection / query error)
	// was acknowledged (CHK-IPC-02).
	Acknowledged bool `json:"acknowledged,omitempty"`
	// Muted / MutedSince: mute_until_recovered (phase 12, CHK-MON-20).
	// Muted is always sent; MutedSince is set while muted.
	Muted      bool       `json:"muted"`
	MutedSince *time.Time `json:"mutedSince,omitempty"`
	// Read-only settings view (phase 6): connection without password,
	// the profile's own thresholds, mapping summary (no SQL, no columns).
	Connection        *ProfileConnection `json:"connection,omitempty"`
	ThresholdOverride *ThresholdOverride `json:"thresholdOverride,omitempty"`
	MappingSummary    *MappingSummary    `json:"mappingSummary,omitempty"`
	// LatencyMs: wall time of the last poll of the profile (connect +
	// queries), phase 9 (CHK-SVC-03); omitted before the first poll.
	LatencyMs *int64 `json:"latencyMs,omitempty"`
	// LastQueryMs: duration of the last poll's queries (phase 9; measured
	// around the whole poll, = LatencyMs).
	LastQueryMs *int64 `json:"lastQueryMs,omitempty"`
}

// StatusSnapshot is the full monitor state sent to the tray.
type StatusSnapshot struct {
	// Seq increases with every published snapshot.
	Seq uint64 `json:"seq"`
	// Now is the service clock when the snapshot was built (UTC+3).
	Now time.Time `json:"now"`
	// ServerNow: service clock when the snapshot was sent (RFC 3339 with
	// the +03:00 offset, phase 9).
	ServerNow time.Time `json:"serverNow"`
	// LastCheckSeq (phase 9, CHK-SVC-04): every check_now with CheckSeq <=
	// LastCheckSeq has its requested poll included in this snapshot; 0 =
	// none. The only completion rule of check_now: LastCheckSeq >=
	// checkSeq. A status pushed for another reason (ack, a poll of another
	// profile) does not advance it.
	LastCheckSeq uint64 `json:"lastCheckSeq,omitempty"`
	// Level is the worst level of all enabled profiles.
	Level    AlertLevel        `json:"level"`
	Profiles []ProfileSnapshot `json:"profiles"`
	// Alerts are the currently active (non-OK, debounced) alerts.
	Alerts          []AlertEvent `json:"alerts,omitempty"`
	PollIntervalSec int          `json:"pollIntervalSec"`
	LastPollAt      time.Time    `json:"lastPollAt"`
	// ConfigError is set when settings/profiles/secrets could not be read
	// (the monitor keeps running with the last good or default config).
	ConfigError string `json:"configError,omitempty"`
	// ConfigErrorKey is set when settings.json / profiles.json / secrets
	// could not be read or parsed (CHK-MON-10): MsgMonitorConfigStale when
	// the last good configuration is in use (ConfigStale), otherwise
	// MsgMonitorConfigInvalid (first start: defaults / nothing polled).
	ConfigErrorKey string `json:"configErrorKey,omitempty"`
	// ConfigStale: the last load failed and the previous good
	// configuration is still in use (profiles, counters, alerts kept).
	ConfigStale bool `json:"configStale,omitempty"`
	// Heartbeat fields (protocol version 2, CHK-IPC-04): service start,
	// end of the last completed full poll cycle (nil before the first
	// one), PollerHung when no cycle completed for longer than
	// HungAfterSec (evaluated when the snapshot is read). pollerHung is
	// always sent, false explicitly (phase 4b).
	ServiceStartedAt *time.Time `json:"serviceStartedAt,omitempty"`
	LastCycleAt      *time.Time `json:"lastCycleAt,omitempty"`
	PollerHung       bool       `json:"pollerHung"`
	HungAfterSec     int        `json:"hungAfterSec,omitempty"`
	// Read-only settings view (phase 6): the applied global settings
	// (incl. beep, language, trayAutostart) and the configuration
	// revision (as returned by get_config / save_*).
	Settings       *Settings `json:"settings,omitempty"`
	ConfigRevision string    `json:"configRevision,omitempty"`
	// Phase 10 (CHK-MON-17): HasProblems (always sent) = at least one
	// active alert; Problems lists them (model.ProblemsOf), at most
	// ProblemsMax (ProblemsTruncated).
	HasProblems       bool      `json:"hasProblems"`
	Problems          []Problem `json:"problems,omitempty"`
	ProblemsTruncated bool      `json:"problemsTruncated,omitempty"`
}

// minutesFloor returns whole minutes of d (d >= 0).
func minutesFloor(d time.Duration) int { return int(d / time.Minute) }

// NewPointStatus evaluates p at now with settings s: Level is recomputed
// from the FreshAt lag (both NULL -> Error "no_timestamps"), times are
// converted to UTC+3.
func NewPointStatus(p ExchangePoint, s Settings, now time.Time) PointStatus {
	now = now.In(DBZone)
	ps := PointStatus{ExchangePoint: p, LagMinutes: -1, Key: PointKey(p)}
	if p.LastSend != nil {
		t := p.LastSend.In(DBZone)
		ps.LastSend = &t
	}
	if p.LastRecv != nil {
		t := p.LastRecv.In(DBZone)
		ps.LastRecv = &t
	}
	ps.Level = Evaluate(p, s, now)
	if f, ok := p.FreshAt(); ok {
		ps.FreshAt = &f
		lag, _ := p.Lag(now)
		ps.LagMinutes = minutesFloor(lag)
		if ps.Message == ReasonNoTimestamps {
			ps.Message = ""
		}
	} else {
		ps.Message = ReasonNoTimestamps
	}
	ps.Baseline = freshIsBaseline(p)
	if ps.CheckedAt.IsZero() {
		ps.CheckedAt = now
	} else {
		ps.CheckedAt = ps.CheckedAt.In(DBZone)
	}
	return ps
}

// freshIsBaseline reports whether the FreshAt time of a counters point
// comes only from baseline observations: the fresher side (or both, when
// equal) is a baseline.
func freshIsBaseline(p ExchangePoint) bool {
	if p.Source != PointSourceCounters {
		return false
	}
	switch {
	case p.LastSend == nil && p.LastRecv == nil:
		return p.SendBaseline || p.RecvBaseline
	case p.LastSend == nil:
		return p.RecvBaseline
	case p.LastRecv == nil:
		return p.SendBaseline
	case p.LastSend.After(*p.LastRecv):
		return p.SendBaseline
	case p.LastRecv.After(*p.LastSend):
		return p.RecvBaseline
	default:
		return p.SendBaseline && p.RecvBaseline
	}
}

// NewProfileSnapshot builds the snapshot of profile p from its polled
// status at now: every point is re-evaluated with the profile's effective
// thresholds (EffectiveSettings of global); the profile level is the
// worst point level, and any error makes it LevelError.
func NewProfileSnapshot(p Profile, st ProfileStatus, global Settings, now time.Time) ProfileSnapshot {
	s := p.EffectiveSettings(global)
	name := st.Name
	if name == "" {
		name = p.DisplayName()
	}
	out := ProfileSnapshot{
		ProfileID: p.ID, Name: name, Enabled: p.Enabled,
		Points: make([]PointStatus, 0, len(st.Points)),
		Error:  st.Error, ErrorKey: st.ErrorKey,
		WarnMinutes: s.WarnMinutes, ErrorMinutes: s.ErrorMinutes,
		CheckedAt: st.CheckedAt.In(DBZone), Polled: true,
	}
	levels := make([]AlertLevel, 0, len(st.Points)+1)
	for _, pt := range st.Points {
		ps := NewPointStatus(pt, s, now)
		if ps.FreshAt != nil && (out.FreshAt == nil || ps.FreshAt.After(*out.FreshAt)) {
			f := *ps.FreshAt
			out.FreshAt = &f
		}
		if ps.Baseline {
			out.Baseline = true
		}
		levels = append(levels, ps.Level)
		out.Points = append(out.Points, ps)
	}
	if out.Error != "" {
		levels = append(levels, LevelError)
		if out.ErrorKey == "" {
			out.ErrorKey = MsgMonitorPollError
		}
	}
	out.Level = Worst(levels...)
	MarkStalled(&out, p.StalledAfter(global), now)
	return out
}

// PendingProfileSnapshot is the snapshot of a profile that has not been
// polled yet (or is disabled): no points, LevelOK, Polled false.
func PendingProfileSnapshot(p Profile, global Settings) ProfileSnapshot {
	s := p.EffectiveSettings(global)
	return ProfileSnapshot{ProfileID: p.ID, Name: p.DisplayName(), Enabled: p.Enabled,
		Points: []PointStatus{}, WarnMinutes: s.WarnMinutes, ErrorMinutes: s.ErrorMinutes}
}

// WorstProfileLevel is the worst level of the enabled profiles.
func WorstProfileLevel(ps []ProfileSnapshot) AlertLevel {
	w := LevelOK
	for _, p := range ps {
		if p.Enabled {
			w = Worst(w, p.Level)
		}
	}
	return w
}

// PointKey identifies a point inside a profile across polls: plan table
// (several plans may have points with the same name) + name.
func PointKey(p ExchangePoint) string {
	if p.PlanTable != "" {
		return p.PlanTable + "/" + p.Name
	}
	return p.Name
}

// SortPoints orders points by plan table, then name (stable output).
func SortPoints(ps []PointStatus) {
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].PlanTable != ps[j].PlanTable {
			return ps[i].PlanTable < ps[j].PlanTable
		}
		return ps[i].Name < ps[j].Name
	})
}
