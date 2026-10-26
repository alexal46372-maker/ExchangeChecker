package model

import (
	"fmt"
	"time"
)

// Phase 11 (CHK-SVC-07): summary — the dashboard freshness series of EC
// Tray (contract with EC Tray). Capability summary; any client (like
// history). Request {"cmd":"summary","hours":1..24 (0 = 24),
// "buckets":1..288 (0 = 48)}, hours*60 % buckets == 0 (else
// invalid_request + monitor.bad_request). Reply MsgSummary with Summary.
const (
	CmdSummary = "summary"
	CapSummary = "summary"
	MsgSummary = "summary"

	SummaryDefaultHours   = 24
	SummaryDefaultBuckets = 48
	SummaryMaxHours       = 24
	SummaryMaxBuckets     = 288
)

// SummaryBucket: Level = worst level of the profile's polls in
// [T, T+bucketMinutes), LagMaxMin = the largest point lag; both absent
// when nothing was polled in the bucket.
type SummaryBucket struct {
	T         time.Time   `json:"t"`
	Level     *AlertLevel `json:"level,omitempty"`
	LagMaxMin *int        `json:"lagMaxMin,omitempty"`
}

// SummaryProfile is one configured profile (deleted profiles are not
// listed). Level / FreshnessMin: the current state (FreshnessMin = the
// largest current point lag, absent without points or on an error);
// LastOKAt: last poll with level ok (history or now), absent if none in
// the window.
type SummaryProfile struct {
	ProfileID    string          `json:"profileId"`
	Name         string          `json:"name"`
	Level        AlertLevel      `json:"level"`
	FreshnessMin *int            `json:"freshnessMin,omitempty"`
	LastOKAt     *time.Time      `json:"lastOkAt,omitempty"`
	Buckets      []SummaryBucket `json:"buckets"`
}

// Summary is the reply. Truncated: the reply would exceed
// IPCMaxInteractiveReply; whole profiles were dropped from the end (the
// kept ones always have all buckets).
type Summary struct {
	From          time.Time        `json:"from"`
	To            time.Time        `json:"to"`
	BucketMinutes int              `json:"bucketMinutes"`
	Profiles      []SummaryProfile `json:"profiles"`
	Truncated     bool             `json:"truncated,omitempty"`
}

// SummaryParams validates hours / buckets (0 = defaults) and returns the
// bucket length.
func SummaryParams(hours, buckets int) (int, int, time.Duration, error) {
	if hours == 0 {
		hours = SummaryDefaultHours
	}
	if buckets == 0 {
		buckets = SummaryDefaultBuckets
	}
	bad := func(field, msg string, args ...string) error {
		return &IPCError{Code: ErrCodeInvalidRequest, Key: MsgMonitorBadRequest, Message: "summary: " + msg, Args: append([]string{field}, args...)}
	}
	if hours < 1 || hours > SummaryMaxHours {
		return 0, 0, 0, bad("hours", "hours must be 1..24", ValRange, "1", "24")
	}
	if buckets < 1 || buckets > SummaryMaxBuckets {
		return 0, 0, 0, bad("buckets", "buckets must be 1..288", ValRange, "1", "288")
	}
	if (hours*60)%buckets != 0 {
		return 0, 0, 0, bad("buckets", fmt.Sprintf("hours*60 (%d) must be a multiple of buckets", hours*60), ValRange)
	}
	return hours, buckets, time.Duration(hours*60/buckets) * time.Minute, nil
}
