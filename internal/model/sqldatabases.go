package model

// Phase 9 (PLAN §12 p. 97, CHK-SQL-07): the databases the wizard offers
// after a successful connection (IPC sql_databases). Only user databases
// (database_id > 4: not master, tempdb, model, msdb) that are ONLINE and
// that the login can open (HAS_DBACCESS = 1).

import "encoding/json"

// Limits of the wizard lists (EC Service: one IPC frame is <= 64 KiB).
const (
	// DefaultSQLDatabasesLimit is the default number of databases.
	DefaultSQLDatabasesLimit = 256
	// MaxSQLDatabases is the largest configurable limit.
	MaxSQLDatabases = 1000
	// MaxSQLWizardJSONBytes caps the JSON of one list (instances or
	// databases) whatever the count limit, so a response stays well under
	// 64 KiB; the rest is dropped and Truncated is set.
	MaxSQLWizardJSONBytes = 48 * 1024
)

// CapSQLList returns the longest prefix of items with at most limit
// elements (limit <= 0: no count limit) whose JSON is at most
// MaxSQLWizardJSONBytes, and whether anything was dropped.
func CapSQLList[T any](items []T, limit int) ([]T, bool) {
	size := 2
	for i, it := range items {
		if limit > 0 && i >= limit {
			return items[:i], true
		}
		b, err := json.Marshal(it)
		if err != nil {
			return items[:i], true
		}
		size += len(b) + 1
		if size > MaxSQLWizardJSONBytes {
			return items[:i], true
		}
	}
	return items, false
}

// SQLDatabaseInfo is one database of the list.
type SQLDatabaseInfo struct {
	Name string `json:"name"`
	// CompatLevel is sys.databases.compatibility_level (100 = SQL Server
	// 2008).
	CompatLevel int `json:"compatLevel"`
	// State is sys.databases.state_desc (always "ONLINE" in the list).
	State string `json:"state"`
	// Is1C: the database has the 1C:Enterprise service tables (two of
	// Config, Params, DBSchema); HasNodeTables: it has _Node* tables
	// (exchange plans). Both false when the probe was skipped or denied
	// (Probed = false).
	Is1C          bool `json:"is1c"`
	HasNodeTables bool `json:"hasNodeTables"`
	Probed        bool `json:"probed"`
	// SizeMB: data + log size from sys.master_files; 0 if not visible.
	SizeMB int64 `json:"sizeMb"`
}

// SQLDatabasesResult is the answer of sql_databases.
type SQLDatabasesResult struct {
	Databases []SQLDatabaseInfo `json:"databases"`
	Truncated bool              `json:"truncated"`
	// ConnectMs: time of connect + TLS + login (0 when the pooled session
	// was reused); QueryMs: time of the database list query.
	ConnectMs int64 `json:"connectMs"`
	QueryMs   int64 `json:"queryMs"`
}
