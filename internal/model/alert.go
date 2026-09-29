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
}

// Raised reports whether the event is an escalation (to a worse level).
func (e AlertEvent) Raised() bool { return e.To > e.From }

// ProfileLevel reports whether the event concerns the whole profile.
func (e AlertEvent) ProfileLevel() bool { return e.PointName == "" && e.PlanTable == "" }
