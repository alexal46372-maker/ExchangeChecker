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
	// Ref is the request ID canceled by cancel (phase 6, G6).
	Ref uint64 `json:"ref,omitempty"`
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
	return fmt.Sprintf("{id:%d cmd:%s profileId:%s profile:%s password:%s pointKey:%s clearPassword:%t expectedRevision:%s ref:%d settings:%t}",
		r.ID, r.Cmd, r.ProfileID, prof, pw, r.PointKey, r.ClearPassword, r.ExpectedRevision, r.Ref, r.Settings != nil)
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
}
