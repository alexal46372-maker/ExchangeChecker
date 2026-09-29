package model

import "time"

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
	// IPCVersion is the protocol version (Hello).
	IPCVersion = 1
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
)

// IPC message types (IPCMessage.Type), service -> tray. Replies carry the
// request ID; pushes (status after every poll, alerts) have ID 0.
const (
	MsgHello  = "hello"
	MsgPong   = "pong"
	MsgStatus = "status"
	MsgOK     = "ok"
	MsgError  = "error"
	MsgAlert  = "alert"
)

// IPCRequest is a tray -> service command.
type IPCRequest struct {
	ID  uint64 `json:"id"`
	Cmd string `json:"cmd"`
	// ProfileID optionally restricts poll_now to one profile.
	ProfileID string `json:"profileId,omitempty"`
}

// IPCMessage is a service -> tray reply or push.
type IPCMessage struct {
	// ID echoes IPCRequest.ID for replies; 0 for pushes.
	ID      uint64 `json:"id,omitempty"`
	Type    string `json:"type"`
	Version int    `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
	// Result is ResultAccepted / ResultCoalesced for poll_now and
	// reload_config replies.
	Result string          `json:"result,omitempty"`
	Status *StatusSnapshot `json:"status,omitempty"`
	Alert  *AlertEvent     `json:"alert,omitempty"`
}
