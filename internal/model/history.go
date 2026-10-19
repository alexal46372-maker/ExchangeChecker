package model

import "time"

// Phase 9 (EC Service): poll history (CHK-SVC-03), check_now (CHK-SVC-04)
// and the transition-event feed events_since (CHK-SVC-05). Every addition
// is a new command, a new capability or an omitempty field: protocol
// version 2 clients keep working unchanged.

// Commands (IPCRequest.Cmd); all of them are allowed for a normal user.
const (
	// CmdHistory returns one page of the poll history (asynchronous,
	// cancel {ref}). Reply MsgHistory (IPCMessage.History) or MsgError.
	CmdHistory = "history"
	// CmdCheckNow asks for an immediate poll of every profile ("") or of
	// IPCRequest.ProfileID. Reply MsgOK with Result ResultAccepted /
	// ResultCheckCoalesced and CheckSeq, or MsgError rate_limited with
	// RetryAfterMs. Completion rule (normative for EC Tray, accepted and
	// coalesced alike): a status with lastCheckSeq >= checkSeq (exact; a
	// coalesced check may complete one poll later). The status seq is not
	// a completion signal. poll_now is unchanged (architect decision 5); EC Tray
	// should switch its "check now" action from poll_now to check_now.
	CmdCheckNow = "check_now"
	// CmdEventsSince returns the transition events after
	// IPCRequest.SinceID of run IPCRequest.RunID. Reply MsgEvents.
	CmdEventsSince = "events_since"
)

// Capabilities (Hello.Capabilities).
const (
	CapHistory     = "history"
	CapCheckNow    = "check_now"
	CapEventsSince = "events_since"
)

// Message types (IPCMessage.Type).
const (
	MsgHistory = "history"
	MsgEvents  = "events"
)

// ResultCheckCoalesced: check_now merged into a running or queued poll of
// the same profile(s); nothing new was started. Complete when a status has
// lastCheckSeq >= checkSeq (possibly one poll later).
const ResultCheckCoalesced = "coalesced"

// TruncatedScan: a history page stopped after its scan budget (lines /
// time); nextCursor continues the scan.
const TruncatedScan = "scan"

// History record kinds (HistoryRecord.Kind, IPCRequest.Kinds).
const (
	HistoryKindPoint   = "point"
	HistoryKindProfile = "profile"
	HistoryKindEvent   = "event"
	// HistoryKindRecovered: a transition back to ok (event record with
	// from, to and durationMin); kinds ["event"] does not include it.
	HistoryKindRecovered = "recovered"
)

// History aggregation buckets (IPCRequest.Bucket): instead of records the
// page carries HistoryPage.Buckets with min / max / avg of latency and lag
// per profile / point and bucket, so a week fits into one 64 KiB page.
const (
	HistoryBucket1m = "1m"
	HistoryBucket5m = "5m"
	HistoryBucket1h = "1h"
)

// Alert event kinds (AlertEvent.Kind, phase 9).
const (
	AlertKindAlert     = "alert"
	AlertKindRecovered = "recovered"
)

// History status of a profile record (HistoryRecord.Status).
const (
	HistoryStatusOK      = "ok"
	HistoryStatusWarning = "warning"
	HistoryStatusError   = "error"
	HistoryStatusTimeout = "timeout"
	HistoryStatusBusy    = "busy"
	HistoryStatusHung    = "hung"
)

// History limits (architect decision 2, PROPOSAL-svc §1).
const (
	HistoryDir          = "history"
	HistoryDays         = 30
	HistoryDayCap       = 64 << 20
	HistoryTotalCap     = 1 << 30
	HistoryMaxRange     = 31 * 24 * time.Hour
	HistoryDefaultLimit = 1000
	HistoryMaxLimit     = 5000
	// HistoryMaxPerConn / HistoryMaxGlobal: concurrent history requests
	// per connection / per service (more -> busy, history.busy).
	HistoryMaxPerConn = 1
	HistoryMaxGlobal  = 2
	// HistoryScanLines / HistoryScanTime: scan budget of one page.
	HistoryScanLines = 200000
	HistoryScanTime  = 2 * time.Second
	// HistorySDDL: the history folder is SYSTEM + Administrators only,
	// protected, auto-inherited flag like DataDirSDDL (architect decision
	// 1); the tray reads it through IPC.
	HistorySDDL = "D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
)

// check_now limits (CHK-SVC-04).
const (
	CheckAllInterval     = 30 * time.Second
	CheckProfileInterval = 10 * time.Second
	CheckConnInterval    = 2 * time.Second
)

// events_since limits (CHK-SVC-05).
const (
	EventRingSize      = 1000
	EventsDefaultLimit = 500
	EventsMaxLimit     = 1000
)

// Message keys of phase 9 (texts in monitorMessages).
const (
	MsgCheckRateLimited     = "monitor.check_rate_limited"
	MsgMonitorProfileOff    = "monitor.profile_disabled"
	MsgHistoryDisabled      = "history.disabled"
	MsgHistoryBadRange      = "history.bad_range"
	MsgHistoryRangeTooLong  = "history.range_too_long"
	MsgHistoryBadCursor     = "history.bad_cursor"
	MsgHistoryBadFilter     = "history.bad_filter"
	MsgHistoryBusy          = "history.busy"
	MsgHistoryFailed        = "history.failed"
	MsgPasswordTargetChange = "config.password_required_on_target_change"
)

// HistoryRecord is one history entry in an IPC reply (full field names;
// the disk format uses short names). No server, login, password, SQL or
// free error text is ever recorded.
type HistoryRecord struct {
	// Kind: HistoryKindPoint / HistoryKindProfile / HistoryKindEvent.
	Kind string `json:"kind"`
	// CheckedAt: poll time (point / profile) or transition time (event),
	// UTC+3.
	CheckedAt time.Time `json:"checkedAt"`
	ProfileID string    `json:"profileId"`
	// ProfileName: current name of the profile (deleted profiles are not
	// returned).
	ProfileName string `json:"profileName,omitempty"`
	// ID is a stable record id ("YYYY-MM-DD:offset"); records are sorted
	// by CheckedAt, then by ID position.
	ID string `json:"id"`
	// PointKey / PointName / PlanTable: point records and point events.
	PointKey  string `json:"pointKey,omitempty"`
	PointName string `json:"pointName,omitempty"`
	PlanTable string `json:"planTable,omitempty"`
	// Level: point / profile records.
	Level *AlertLevel `json:"level,omitempty"`
	// FreshAt / LagMin / Baseline / Source: point records.
	FreshAt  *time.Time `json:"freshAt,omitempty"`
	LagMin   *int       `json:"lagMin,omitempty"`
	Baseline bool       `json:"baseline,omitempty"`
	Source   string     `json:"source,omitempty"`
	// LatencyMs / QueryMs / Status: profile records (HistoryStatus*).
	// LatencyMs is the total wall time of the poll; QueryMs the time of
	// the queries (currently measured around the whole poll, = LatencyMs).
	LatencyMs *int64 `json:"latencyMs,omitempty"`
	QueryMs   *int64 `json:"queryMs,omitempty"`
	Status    string `json:"status,omitempty"`
	// ErrorKey: known localizable key only (profile records; point
	// records: monitor-level reason such as no_timestamps).
	ErrorKey string `json:"errorKey,omitempty"`
	// From / To / EventID / RunID: event and recovered records;
	// DurationMin: recovered records, minutes since the alert was raised.
	From        *AlertLevel `json:"from,omitempty"`
	To          *AlertLevel `json:"to,omitempty"`
	DurationMin *int64      `json:"durationMin,omitempty"`
	EventID     uint64      `json:"eventId,omitempty"`
	RunID       string      `json:"runId,omitempty"`
}

// HistoryAgg is min / max / avg of one value over a bucket.
type HistoryAgg struct {
	Min   int64   `json:"min"`
	Max   int64   `json:"max"`
	Avg   float64 `json:"avg"`
	Count int     `json:"count"`
}

// HistoryBucket aggregates the point (PointKey set) or profile records of
// one bucket [Start, Start+bucket).
type HistoryBucket struct {
	Start       time.Time `json:"start"`
	Kind        string    `json:"kind"`
	ProfileID   string    `json:"profileId"`
	ProfileName string    `json:"profileName,omitempty"`
	PointKey    string    `json:"pointKey,omitempty"`
	PointName   string    `json:"pointName,omitempty"`
	// Records: number of records; Errors: profile records with status
	// error / timeout / hung; WorstLevel: worst level seen.
	Records    int         `json:"records"`
	Errors     int         `json:"errors,omitempty"`
	WorstLevel *AlertLevel `json:"worstLevel,omitempty"`
	LatencyMs  *HistoryAgg `json:"latencyMs,omitempty"`
	QueryMs    *HistoryAgg `json:"queryMs,omitempty"`
	LagMin     *HistoryAgg `json:"lagMin,omitempty"`
}

// HistoryPage is IPCMessage.History.
type HistoryPage struct {
	// Records in ascending CheckedAt order (file order); [] when none.
	Records []HistoryRecord `json:"records"`
	// Bucket / Buckets: aggregated reply (IPCRequest.Bucket); Records is
	// then empty. Buckets are sorted by Start, profile, point.
	Bucket  string          `json:"bucket,omitempty"`
	Buckets []HistoryBucket `json:"buckets,omitempty"`
	// HasMore: another page follows (NextCursor); always present.
	HasMore bool `json:"hasMore"`
	// NextCursor continues after the last record (present iff Truncated).
	NextCursor string `json:"nextCursor,omitempty"`
	// Truncated / TruncatedReason: TruncatedSize (64 KiB),
	// TruncatedLimit (IPCRequest.Limit), TruncatedScan (scan budget).
	Truncated       bool   `json:"truncated,omitempty"`
	TruncatedReason string `json:"truncatedReason,omitempty"`
}

// CheckResult is the monitor's answer to check_now.
type CheckResult struct {
	Coalesced bool
	// CheckSeq numbers the request (its own counter, independent of the
	// status seq): complete when StatusSnapshot.LastCheckSeq >= CheckSeq.
	CheckSeq uint64
}

// EventsPage is the monitor's answer to events_since.
type EventsPage struct {
	Events        []AlertEvent
	RunID         string
	LastID        uint64
	ResetRequired bool
	Truncated     bool
}
