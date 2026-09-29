package model

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
	// IPCMaxMessage caps one framed message (bytes).
	IPCMaxMessage = 16 << 20
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
}

// IPCMessage is a service -> tray reply or push.
type IPCMessage struct {
	// ID echoes IPCRequest.ID for replies; 0 for pushes.
	ID      uint64          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Version int             `json:"version,omitempty"`
	Error   string          `json:"error,omitempty"`
	Status  *StatusSnapshot `json:"status,omitempty"`
	Alert   *AlertEvent     `json:"alert,omitempty"`
}
