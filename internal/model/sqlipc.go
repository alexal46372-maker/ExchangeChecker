package model

import "time"

// Phase 9 (EC Service, CHK-IPC-10): the connection wizard commands of EC
// Tray over IPC, built on the EC SQL APIs (CHK-SQL-06..09). All four are
// admin-only (like discover); sql_browse and sql_databases are
// asynchronous (cancel {ref}) under the interactive operation limits,
// sql_diagnose and sql_grant_script are answered synchronously (no I/O).
const (
	// CmdSQLBrowse lists SQL Server instances through the SQL Browser:
	// IPCRequest.Host (empty = local broadcast), Instance, TimeoutMs,
	// Limit. Reply MsgSQLBrowse (IPCMessage.SQLBrowse).
	CmdSQLBrowse = "sql_browse"
	// CmdSQLDatabases lists the user databases of IPCRequest.Profile
	// (Password, else the saved secret of the same profile and target).
	// Reply MsgSQLDatabases (IPCMessage.SQLDatabases).
	CmdSQLDatabases = "sql_databases"
	// CmdSQLDiagnose returns the troubleshooting steps for
	// IPCRequest.Category + ErrorKey, or Code + TLSKey (SQLErrorInfo).
	// Reply MsgSQLDiagnosis (IPCMessage.SQLDiagnosis).
	CmdSQLDiagnose = "sql_diagnose"
	// CmdSQLGrantScript returns the T-SQL that grants the monitoring
	// login read access (Principal, Windows / Auth, Database, Tables).
	// Reply MsgSQLGrantScript (IPCMessage.SQLGrantScript).
	CmdSQLGrantScript = "sql_grant_script"
)

// Capabilities (Hello.Capabilities) of the wizard commands.
const (
	CapSQLBrowse      = CmdSQLBrowse
	CapSQLDatabases   = CmdSQLDatabases
	CapSQLDiagnose    = CmdSQLDiagnose
	CapSQLGrantScript = CmdSQLGrantScript
)

// Reply types of the wizard commands.
const (
	MsgSQLBrowse      = "sql_browse"
	MsgSQLDatabases   = "sql_databases"
	MsgSQLDiagnosis   = "sql_diagnosis"
	MsgSQLGrantScript = "sql_grant_script"
)

// SQLCommand reports whether cmd is one of the wizard commands.
func SQLCommand(cmd string) bool {
	switch cmd {
	case CmdSQLBrowse, CmdSQLDatabases, CmdSQLDiagnose, CmdSQLGrantScript, CmdSQLTestSteps:
		return true
	}
	return false
}

// sql_browse over IPC: TimeoutMs is clamped to
// [IPCSQLBrowseMinTimeoutMs, IPCSQLBrowseMaxTimeoutMs] (0 =
// DefaultSQLBrowseTimeoutMs); the whole request ends at the timeout plus
// IPCSQLBrowseGrace.
const (
	IPCSQLBrowseMinTimeoutMs = 500
	IPCSQLBrowseMaxTimeoutMs = 5000
	IPCSQLBrowseGrace        = 2 * time.Second
)

// ClampSQLBrowseTimeoutMs applies the IPC bounds of sql_browse.
func ClampSQLBrowseTimeoutMs(ms int) int {
	switch {
	case ms <= 0:
		return DefaultSQLBrowseTimeoutMs
	case ms < IPCSQLBrowseMinTimeoutMs:
		return IPCSQLBrowseMinTimeoutMs
	case ms > IPCSQLBrowseMaxTimeoutMs:
		return IPCSQLBrowseMaxTimeoutMs
	}
	return ms
}

// sql_browse rate limits (CHK-IPC-10): a broadcast (no host) at most once
// per SQLBrowseBroadcastInterval per service, a targeted browse at most
// once per SQLBrowseHostInterval per host (ASCII case-insensitive) and
// any targeted browse at most once per SQLBrowseAnyHostInterval;
// refusal: rate_limited + retryAfterMs, key MsgBrowseRateLimited.
const (
	SQLBrowseBroadcastInterval = 15 * time.Second
	SQLBrowseHostInterval      = 3 * time.Second
	// SQLBrowseAnyHostInterval: any targeted browse at most once per
	// this interval service-wide (FQDN / IP aliases of one host).
	SQLBrowseAnyHostInterval = 1 * time.Second
	MsgBrowseRateLimited     = "monitor.browse_rate_limited"
)
