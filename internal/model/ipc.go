package model

import (
	"fmt"
	"time"
)

// IPC protocol types between the service (server) and the tray (client),
// phase 3 (CHK-MON-06). Transport: named pipe PipeName, each message is a
// 4-byte little-endian length followed by UTF-8 JSON (internal/ipc).
// The types are here so the tray does not import the SQL driver.

// Well-known names shared by the service, the tray and the installer.
const (
	// ServiceName is the Windows service name (SCM key).
	ServiceName = "ExchangeChecker"
	// EventSource is the Windows Event Log source of the service.
	EventSource = "ExchangeChecker"
	// PipeName is the service's named pipe.
	PipeName = `\\.\pipe\ExchangeChecker`
	// TrayExitMessage is the RegisterWindowMessageW name that makes the
	// tray (window class ExchangeCheckerTray) and the settings / Discovery
	// windows exit at once without questions (CHK-TRAY-11): the installer
	// posts it before replacing or removing the files (phase 7 §7.3)
	// without importing internal/wintray. wParam / lParam are ignored.
	TrayExitMessage = "ExchangeChecker.TrayExit"
	// Window classes that handle TrayExitMessage (CHK-TRAY-11). A separate
	// `exchtray --settings` process never sees the message posted to the
	// main tray window: the installer sends it with HWND_BROADCAST or to
	// each of these classes.
	TrayWindowClass = "ExchangeCheckerTray"
	// TraySettingsClass: the administrator settings window (elevated
	// `--settings` process); TraySettingsViewClass: the read-only one.
	TraySettingsClass     = "ExchangeCheckerTraySettings"
	TraySettingsViewClass = "ExchangeCheckerTraySettingsView"
	// TrayDiscoveryClass: the Discovery dialog of the settings window.
	TrayDiscoveryClass = "ExchangeCheckerTrayDiscovery"
	// IPCVersion is the protocol version (Hello, Pong). Version 2 (phase
	// 4, CHK-IPC-05) adds test_profile, ack_alert, the heartbeat, error
	// codes and Hello.Capabilities; every addition is an omitempty field or
	// a new command, so version-1 clients keep working unchanged (unknown
	// JSON fields are ignored) and a new client talking to a version-1
	// service sees Version 1 / no Capabilities and hides the new features.
	IPCVersion = 2
	// IPCMaxMessage caps one framed service -> tray message (bytes):
	// status snapshots of many bases may be large.
	IPCMaxMessage = 16 << 20
	// IPCMaxRequest caps one framed tray -> service request; the server
	// drops a client that announces a larger frame (CHK-MON-11).
	IPCMaxRequest = 64 << 10
	// IPCMaxClients caps the concurrent pipe instances (connected clients
	// plus the listening instance); further clients wait in
	// WaitNamedPipe (ERROR_PIPE_BUSY) until a slot frees (CHK-MON-11).
	IPCMaxClients = 8
	// IPCReadTimeout: once the first byte of a request arrived, the rest
	// of the frame must arrive within this time or the client is dropped.
	IPCReadTimeout = 10 * time.Second
	// IPCIdleTimeout: a client that sends no request for this long is
	// dropped. Clients (the tray) send CmdPing every IPCHeartbeat.
	IPCIdleTimeout = 5 * time.Minute
	// IPCHeartbeat is the recommended client ping period.
	IPCHeartbeat = 60 * time.Second
	// IPCCommandInterval: poll_now / reload_config are executed at most
	// once per this period (globally and per profile); extra requests get
	// MsgOK with Result = ResultCoalesced (CHK-MON-12).
	IPCCommandInterval = 10 * time.Second
)

// IPCMessage.Result of MsgOK replies to poll_now / reload_config.
const (
	// ResultAccepted: the command was executed (poll / reload started).
	ResultAccepted = "accepted"
	// ResultCoalesced: accepted but merged into a recent or pending poll /
	// reload (rate limit, see IPCCommandInterval); nothing new started.
	ResultCoalesced = "accepted, coalesced"
)

// IPC commands (IPCRequest.Cmd), tray -> service.
const (
	CmdPing         = "ping"
	CmdGetStatus    = "get_status"
	CmdReloadConfig = "reload_config"
	CmdPollNow      = "poll_now"
	// CmdTestProfile tests a connection (phase 4, CHK-IPC-03): the saved
	// profile IPCRequest.ProfileID or the unsaved IPCRequest.Profile (+
	// Password). Reply MsgTestResult or MsgError (Code).
	CmdTestProfile = "test_profile"
	// CmdAckAlert acknowledges the active alerts of IPCRequest.ProfileID
	// (all of them, or only IPCRequest.PointKey) (phase 4, CHK-IPC-02).
	// Reply MsgOK with Acked / Result ResultAcknowledged, or MsgError.
	CmdAckAlert = "ack_alert"
)

// Capabilities announced in Hello.Capabilities (protocol version 2).
const (
	CapTestProfile = "test_profile"
	CapAckAlert    = "ack_alert"
	CapHeartbeat   = "heartbeat"
)

// ResultAcknowledged is IPCMessage.Result of an ack_alert reply.
const ResultAcknowledged = "acknowledged"

// Machine-readable error codes (IPCMessage.Code of MsgError, protocol
// version 2). Version-1 replies had only Error; Error stays filled.
const (
	// ErrCodeBusy: the maximum number of concurrent connection tests runs.
	ErrCodeBusy = "busy"
	// ErrCodeNotFound: unknown profile (or point) ID.
	ErrCodeNotFound = "not_found"
	// ErrCodeInvalidRequest: malformed request (bad profile, missing
	// password, ...); ErrorKey tells what to fix.
	ErrCodeInvalidRequest = "invalid_request"
	// ErrCodeNoActiveAlert: ack_alert found nothing to acknowledge.
	ErrCodeNoActiveAlert = "no_active_alert"
	// ErrCodeUnknownCommand: the service does not know the command.
	ErrCodeUnknownCommand = "unknown_command"
	// ErrCodeRateLimited: the command was refused by the command rate
	// limit (IPCCommandInterval); test_profile cannot be merged into an
	// earlier call, so it is refused instead of coalesced.
	ErrCodeRateLimited = "rate_limited"
)

// IPCError is an error with a protocol error code and a localizable
// message key; the IPC server copies both into the MsgError reply.
type IPCError struct {
	Code    string
	Key     string
	Message string
	// Args are copied into IPCMessage.ErrorArgs (phase 6).
	Args []string
	// ProfileID is copied into IPCMessage.ProfileID: the OTHER profile
	// whose invalid entry in profiles.json refused the write (phase 6).
	ProfileID string
	// RetryAfterMs is copied into IPCMessage.RetryAfterMs (rate_limited
	// of check_now, phase 9).
	RetryAfterMs int64
	// Info is copied into IPCMessage.ErrorInfo (SQL errors of the
	// interactive and wizard commands, phase 9 CHK-IPC-10).
	Info *SQLErrorInfo
}

func (e *IPCError) Error() string { return e.Message }

// IPC message types (IPCMessage.Type), service -> tray. Replies carry the
// request ID; pushes (status after every poll, alerts) have ID 0.
const (
	MsgHello  = "hello"
	MsgPong   = "pong"
	MsgStatus = "status"
	MsgOK     = "ok"
	MsgError  = "error"
	MsgAlert  = "alert"
	// MsgTestResult is the reply of test_profile (IPCMessage.Test).
	MsgTestResult = "test_result"
)

// IPCRequest is a tray -> service command.
type IPCRequest struct {
	ID  uint64 `json:"id"`
	Cmd string `json:"cmd"`
	// ProfileID optionally restricts poll_now to one profile; the saved
	// profile of test_profile; the profile of ack_alert.
	ProfileID string `json:"profileId,omitempty"`
	// Profile is the unsaved profile of test_profile (version 2).
	Profile *Profile `json:"profile,omitempty"`
	// Password of Profile (test_profile). Never logged, never written to
	// disk; String / GoString mask it.
	Password string `json:"password,omitempty"`
	// PointKey restricts ack_alert to one point (PointStatus.Key).
	PointKey string `json:"pointKey,omitempty"`
	// Settings of save_settings (phase 6).
	Settings *Settings `json:"settings,omitempty"`
	// ClearPassword: save_profile deletes the saved password (phase 6).
	ClearPassword bool `json:"clearPassword,omitempty"`
	// ExpectedRevision of save_profile / delete_profile / save_settings:
	// mandatory since phase 6b (missing -> invalid_request, errorArgs
	// ["expectedRevision", "validation.required"]); mismatch -> conflict.
	ExpectedRevision string `json:"expectedRevision,omitempty"`
	// import_config (phase 10, CHK-CFG-06): Config, Mode (merge default |
	// replace), DryRun (validate and report, write nothing).
	Config *ConfigExport `json:"config,omitempty"`
	Mode   string        `json:"mode,omitempty"`
	DryRun bool          `json:"dryRun,omitempty"`
	// Viber (phase 10, CHK-SVC-06): ViberToken (save_settings: set,
	// empty / absent = no change; viber_test: use once, not saved;
	// write-only, never logged), ClearViberToken (save_settings: remove
	// the saved token), RecipientID (viber_test).
	ViberToken      string `json:"viberToken,omitempty"`
	ClearViberToken bool   `json:"clearViberToken,omitempty"`
	RecipientID     string `json:"recipientId,omitempty"`
	// Level / Viber: alarm_test (phase 11, CHK-IPC-11).
	Level string `json:"level,omitempty"`
	Viber bool   `json:"viber,omitempty"`
	// Hours / Buckets: summary (phase 11, CHK-SVC-07).
	Hours   int `json:"hours,omitempty"`
	// Phase 11 (CHK-IPC-12/13): sql_browse v2 Port / CIDR (Mode shared
	// with import_config), sql_test_steps Server (+ ProfileID).
	Port   int    `json:"port,omitempty"`
	CIDR   string `json:"cidr,omitempty"`
	Server string `json:"server,omitempty"`
	Buckets int `json:"buckets,omitempty"`
	// Ref is the request ID canceled by cancel (phase 6, G6).
	Ref uint64 `json:"ref,omitempty"`
	// Query of discover_by_value (phase 6, G2).
	Query *ValueQuery `json:"query,omitempty"`
	// Mapping checked by check_mapping (phase 6, G3).
	Mapping *Mapping `json:"mapping,omitempty"`
	// Limit: preview rows of check_mapping (1..20, 0 = 5), points of
	// list_node_points (0 = all read), records of history (1..5000,
	// 0 = 1000), events of events_since (1..1000, 0 = 500).
	Limit int `json:"limit,omitempty"`
	// Phase 9: history window [From, To) (RFC 3339), opaque Cursor of the
	// next page, record Kinds (HistoryKind*, empty = all), and the
	// events_since position (RunID, SinceID).
	From    *time.Time `json:"from,omitempty"`
	To      *time.Time `json:"to,omitempty"`
	Cursor  string     `json:"cursor,omitempty"`
	Kinds   []string   `json:"kinds,omitempty"`
	RunID   string     `json:"runId,omitempty"`
	SinceID uint64     `json:"sinceId,omitempty"`
	// Bucket: history aggregation (HistoryBucket1m / 5m / 1h), "" = records.
	Bucket string `json:"bucket,omitempty"`
	// Phase 9 wizard (CHK-IPC-10): sql_browse Host / Instance / TimeoutMs
	// (Limit shared); sql_diagnose Category + ErrorKey or Code + TLSKey;
	// sql_grant_script Principal, Windows / Auth, Database, Tables.
	Host      string   `json:"host,omitempty"`
	Instance  string   `json:"instance,omitempty"`
	TimeoutMs int      `json:"timeoutMs,omitempty"`
	Category  string   `json:"category,omitempty"`
	ErrorKey  string   `json:"errorKey,omitempty"`
	Code      string   `json:"code,omitempty"`
	TLSKey    string   `json:"tlsKey,omitempty"`
	Principal string   `json:"principal,omitempty"`
	Windows   bool     `json:"windows,omitempty"`
	Auth      string   `json:"auth,omitempty"`
	Database  string   `json:"database,omitempty"`
	Tables    []string `json:"tables,omitempty"`
}

// String masks the password (fmt %v / %+v of a request).
func (r IPCRequest) String() string {
	pw := ""
	if r.Password != "" {
		pw = "***"
	}
	prof := ""
	if r.Profile != nil {
		prof = r.Profile.ID + "@" + r.Profile.Server + "/" + r.Profile.Database
	}
	// query / mapping only as present (no point names, no manual SQL)
	return fmt.Sprintf("{id:%d cmd:%s profileId:%s profile:%s password:%s pointKey:%s clearPassword:%t expectedRevision:%s ref:%d settings:%t query:%t mapping:%t limit:%d viberToken:%t clearViberToken:%t recipientId:%s}",
		r.ID, r.Cmd, r.ProfileID, prof, pw, r.PointKey, r.ClearPassword, r.ExpectedRevision, r.Ref, r.Settings != nil,
		r.Query != nil, r.Mapping != nil, r.Limit, r.ViberToken != "", r.ClearViberToken, r.RecipientID)
}

// GoString masks the password (fmt %#v).
func (r IPCRequest) GoString() string { return "model.IPCRequest" + r.String() }

// Heartbeat is the service liveness block of Hello / Pong (version 2,
// CHK-IPC-04). Times are UTC+3.
type Heartbeat struct {
	Protocol         int       `json:"protocol"`
	Now              time.Time `json:"now"`
	ServiceStartedAt time.Time `json:"serviceStartedAt"`
	// LastCycleAt is the end of the last completed full poll cycle (nil
	// before the first one; a single-profile poll_now does not count).
	LastCycleAt *time.Time `json:"lastCycleAt,omitempty"`
	// PollerHung: no full cycle completed for longer than HungAfterSec
	// (measured from LastCycleAt, or from the service start); always
	// sent, false explicitly (phase 4b). ServiceStartedAt is always sent.
	PollerHung      bool `json:"pollerHung"`
	HungAfterSec    int  `json:"hungAfterSec"`
	PollIntervalSec int  `json:"pollIntervalSec"`
	// RunID identifies this run of the service (phase 9, CHK-SVC-05):
	// AlertEvent.ID restarts with every run.
	RunID string `json:"runId,omitempty"`
}

// IPCMessage is a service -> tray reply or push.
type IPCMessage struct {
	// ID echoes IPCRequest.ID for replies; 0 for pushes.
	ID      uint64 `json:"id,omitempty"`
	Type    string `json:"type"`
	Version int    `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
	// Code / ErrorKey of MsgError (version 2): ErrCode* and a localizable
	// key (monitor.*).
	Code     string `json:"code,omitempty"`
	ErrorKey string `json:"errorKey,omitempty"`
	// Result is ResultAccepted / ResultCoalesced for poll_now and
	// reload_config replies.
	Result string          `json:"result,omitempty"`
	Status *StatusSnapshot `json:"status,omitempty"`
	Alert  *AlertEvent     `json:"alert,omitempty"`
	// Capabilities of the service (Hello, version 2).
	Capabilities []string `json:"capabilities,omitempty"`
	// Heartbeat (Hello and Pong, version 2).
	Heartbeat *Heartbeat `json:"heartbeat,omitempty"`
	// Test is the test_profile result (MsgTestResult).
	Test *TestProfileResult `json:"test,omitempty"`
	// Acked is the number of alerts acknowledged by ack_alert.
	Acked int `json:"acked,omitempty"`

	// Phase 6 (protocol 2, capabilities config_read / config_write /
	// cancel):
	// ErrorArgs are the parameters of ErrorKey; for profile_invalid /
	// settings_invalid: [JSON field path, validation.* reason, params...].
	ErrorArgs []string `json:"errorArgs,omitempty"`
	// ClientElevated (replies to get_status and ping only, display only):
	// the client token is an elevated administrator. Admin commands
	// re-check it every time.
	ClientElevated *bool `json:"clientElevated,omitempty"`
	// TrayAutostartError (get_status replies only, capability
	// tray_autostart_status, CHK-CFG-04): AutostartErr* code of the last
	// failed application of trayAutostart to HKLM Run; omitted after a
	// successful one.
	TrayAutostartError string `json:"trayAutostartError,omitempty"`
	// ProfileID / Profile of save_profile / delete_profile replies
	// (Profile with HasPassword, never a password). In an error reply
	// (invalid_request + monitor.profile_invalid) ProfileID names ANOTHER
	// profile in profiles.json whose invalid entry refused the write
	// (errorArgs[0] is then that profile's field).
	ProfileID string   `json:"profileId,omitempty"`
	Profile   *Profile `json:"profile,omitempty"`
	// Reloaded: the configuration was re-read synchronously.
	Reloaded bool `json:"reloaded,omitempty"`
	// PollScheduled: a poll of the saved (enabled) profile is queued.
	PollScheduled bool `json:"pollScheduled,omitempty"`
	// Revision of the configuration after save_* / of get_config.
	Revision string `json:"revision,omitempty"`
	// Config is the get_config reply (MsgConfig).
	Config *ConfigView `json:"config,omitempty"`
	// Export: export_config reply; Import: import_config reply (phase 10,
	// CHK-CFG-06).
	Export *ConfigExport `json:"export,omitempty"`
	Import *ImportResult `json:"import,omitempty"`
	// ViberTest: viber_test reply (phase 10, CHK-SVC-06).
	ViberTest *ViberTestResult `json:"viberTest,omitempty"`
	// AlarmTest: alarm_test reply (phase 11, CHK-IPC-11).
	AlarmTest *AlarmTestResult `json:"alarmTest,omitempty"`
	// Summary: summary reply (phase 11, CHK-SVC-07).
	Summary *Summary `json:"summary,omitempty"`
	// Phase 11 (CHK-IPC-12/13): progress pushes (written flat, see
	// MarshalJSON) and the sql_test_steps reply.
	Ref            uint64                `json:"-"`
	BrowseProgress *SQLBrowseProgress    `json:"-"`
	TestStep       *SQLCheckStep         `json:"-"`
	SQLTestSteps   *SQLServerCheckResult `json:"sqlTestSteps,omitempty"`

	// Phase 6, G2/G3 (capabilities discover, discover_by_value,
	// node_points, check_mapping; CHK-IPC-08/09):
	// Candidates of a MsgDiscovery reply, best first (omitted when none).
	Candidates []DiscoveryCandidate `json:"candidates,omitempty"`
	// Truncated (MsgDiscovery, MsgNodePoints): the result is partial;
	// TruncatedReason (present iff Truncated) says why: TruncatedBudget —
	// the discovery time budget ran out (candidates then also carry
	// HintTruncated), TruncatedSize — the tail was dropped to fit
	// IPCMaxInteractiveReply, TruncatedLimit — list_node_points kept
	// IPCRequest.Limit points. Precedence: budget > size > limit.
	Truncated       bool   `json:"truncated,omitempty"`
	TruncatedReason string `json:"truncatedReason,omitempty"`
	// Limit (MsgNodePoints, only with TruncatedReason == TruncatedLimit):
	// the IPCRequest.Limit the list was cut to (EC Tray, CHK-IPC-08).
	Limit int `json:"limit,omitempty"`
	// NodePoints of a MsgNodePoints reply (omitted when none).
	NodePoints []NodePoint `json:"nodePoints,omitempty"`
	// MappingCheck of a MsgMappingCheck reply.
	MappingCheck *MappingCheck `json:"mappingCheck,omitempty"`

	// Phase 9 (capabilities history, check_now, events_since):
	// History of a MsgHistory reply.
	History *HistoryPage `json:"history,omitempty"`
	// RetryAfterMs: rate_limited reply of check_now.
	RetryAfterMs int64 `json:"retryAfterMs,omitempty"`
	// CheckSeq: check_now reply (always present there); complete when a
	// status has lastCheckSeq >= checkSeq.
	CheckSeq *uint64 `json:"checkSeq,omitempty"`
	// Events / RunID / LastID / ResetRequired of a MsgEvents reply (Truncated is
	// shared): LastID is the ID of the last event in Events, or the
	// newest event ID of the run when Events is empty (always present);
	// ResetRequired: RunID differs or SinceID is older than the buffer (the
	// client takes the active alerts from get_status).
	Events        []AlertEvent `json:"events,omitempty"`
	RunID         string       `json:"runId,omitempty"`
	LastID        *uint64      `json:"lastId,omitempty"`
	ResetRequired bool         `json:"resetRequired,omitempty"`
	// Phase 9 wizard replies (CHK-IPC-10): SQLBrowse / SQLDatabases /
	// SQLDiagnosis / SQLGrantScript; AutoPick of a discovery reply
	// (model.SQLAutoPickFor over the returned candidates); ErrorInfo of
	// a SQL error (stable code, diagnosis key, SQL error number).
	SQLBrowse      *SQLBrowseResult    `json:"sqlBrowse,omitempty"`
	SQLDatabases   *SQLDatabasesResult `json:"sqlDatabases,omitempty"`
	SQLDiagnosis   *SQLDiagnosis       `json:"sqlDiagnosis,omitempty"`
	SQLGrantScript *SQLGrantScript     `json:"sqlGrantScript,omitempty"`
	AutoPick       *SQLAutoPick        `json:"autoPick,omitempty"`
	ErrorInfo      *SQLErrorInfo       `json:"errorInfo,omitempty"`
}
