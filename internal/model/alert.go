package model

import "time"

// Alert events (phase 3, CHK-MON-05): produced by the service when the
// debounced level of a point or a profile changes (ok -> warning -> error
// and back). Sent to the tray over IPC, written to the Windows Event Log
// and to the service log file.

// AlertEvent is one level transition.
type AlertEvent struct {
	// ID increases monotonically within one service run.
	ID          uint64 `json:"id"`
	ProfileID   string `json:"profileId"`
	ProfileName string `json:"profileName"`
	// PointName / PlanTable identify the point; both empty for a
	// profile-level event (connection / query error of the database).
	PointName string     `json:"pointName,omitempty"`
	PlanTable string     `json:"planTable,omitempty"`
	From      AlertLevel `json:"from"`
	To        AlertLevel `json:"to"`
	// At is when the transition was confirmed (UTC+3).
	At time.Time `json:"at"`
	// FreshAt / LagMinutes of the point at that moment (point events).
	FreshAt    *time.Time `json:"freshAt,omitempty"`
	LagMinutes int        `json:"lagMinutes,omitempty"`
	Baseline   bool       `json:"baseline,omitempty"`
	// Error / ErrorKey of a profile-level event.
	Error    string `json:"error,omitempty"`
	ErrorKey string `json:"errorKey,omitempty"`
	// Key is the point key (PointStatus.Key) of a point event, "" for a
	// profile-level event (ack_alert, protocol version 2).
	Key string `json:"key,omitempty"`
	// Acknowledged / AckedAt: the alert was acknowledged (ack_alert); it
	// stays active and visible, the tray stops repeating notifications
	// (CHK-IPC-02).
	Acknowledged bool       `json:"acknowledged,omitempty"`
	AckedAt      *time.Time `json:"ackedAt,omitempty"`
	// RunID of the service run that produced the event (phase 9,
	// CHK-SVC-05): (RunID, ID) is unique.
	RunID string `json:"runId,omitempty"`
	// EventID = ID, sent under its own name in every push (phase 9).
	EventID uint64 `json:"eventId,omitempty"`
	// Kind: AlertKindRecovered for a transition back to ok, else
	// AlertKindAlert; DurationMin: recovered events, minutes since the
	// point / profile left ok in this run (phase 9, CHK-SVC-05).
	Kind string `json:"kind,omitempty"`
	// Stalled / StalledMinutes: a stall alert of the point (phase 11,
	// CHK-MON-18; level warning, recovered on leave).
	Stalled        bool `json:"stalled,omitempty"`
	StalledMinutes int  `json:"stalledMinutes,omitempty"`
	// Test marks the test alarm of alarm_test (phase 11, CHK-IPC-11).
	Test        bool   `json:"test,omitempty"`
	DurationMin *int64 `json:"durationMin,omitempty"`
	// Phase 10 (CHK-MON-17): the alarm persists until the error is
	// resolved. Since: when the point / profile left ok (active alerts and
	// repeats). Repeat: failing checks since then that were no transition.
	// StillFailing: an alert_repeat event (push only), also for
	// acknowledged alerts (Acknowledged set); LastCheckedAt: that check.
	Since         *time.Time `json:"since,omitempty"`
	Repeat        uint32     `json:"repeat,omitempty"`
	StillFailing  bool       `json:"stillFailing,omitempty"`
	LastCheckedAt *time.Time `json:"lastCheckedAt,omitempty"`
	// alert_repeat events (Kind AlertKindRepeat, CHK-MON-17): Level of
	// the alarm, PointKey (= Key), RepeatOf = eventId of the original
	// alert, CheckSeq = check counter when the failing check started
	// (every check_now with checkSeq <= CheckSeq covering this profile is
	// included in it).
	Level    AlertLevel `json:"level,omitempty"`
	PointKey string     `json:"pointKey,omitempty"`
	RepeatOf uint64     `json:"repeatOf,omitempty"`
	CheckSeq uint64     `json:"checkSeq,omitempty"`
}

// Raised reports whether the event is an escalation (to a worse level).
func (e AlertEvent) Raised() bool { return e.To > e.From }

// ProfileLevel reports whether the event concerns the whole profile.
func (e AlertEvent) ProfileLevel() bool { return e.PointName == "" && e.PlanTable == "" }
