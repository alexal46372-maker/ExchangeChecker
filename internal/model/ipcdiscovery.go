package model

// Phase 6, G2/G3 on the service side (CHK-IPC-08 discovery, CHK-IPC-09
// check_mapping): admin-only interactive SQL commands. Protocol version
// stays 2: new commands, capabilities and omitempty fields (CHK-IPC-05).

// Commands (IPCRequest.Cmd). The profile is the saved one (ProfileID,
// its saved password) or the unsaved Profile (+ Password), with the
// test_profile rules (Windows authentication only for a saved profile
// with its saved connection, CanReuseSavedPassword).
const (
	// CmdDiscover proposes mappings: reply MsgDiscovery (Candidates,
	// Truncated).
	CmdDiscover = "discover"
	// CmdDiscoverByValue proposes mappings from a known point
	// (IPCRequest.Query): reply MsgDiscovery.
	CmdDiscoverByValue = "discover_by_value"
	// CmdListNodePoints lists the exchange-plan points (IPCRequest.Limit
	// optional): reply MsgNodePoints (NodePoints, Truncated).
	CmdListNodePoints = "list_node_points"
	// CmdCheckMapping checks IPCRequest.Mapping against the database and
	// previews IPCRequest.Limit rows (1..MaxCheckMappingLimit, default
	// DefaultCheckMappingLimit): reply MsgMappingCheck (MappingCheck); a
	// failed check is MappingCheck.OK == false, not an error reply.
	CmdCheckMapping = "check_mapping"
)

// Capabilities (Hello.Capabilities) of the commands above.
const (
	CapDiscover        = "discover"
	CapDiscoverByValue = "discover_by_value"
	CapNodePoints      = "node_points"
	CapCheckMapping    = "check_mapping"
)

// Reply types (IPCMessage.Type).
const (
	MsgDiscovery    = "discovery"
	MsgNodePoints   = "node_points"
	MsgMappingCheck = "mapping_check"
)

// ErrCodeFailed: the interactive operation ran and failed (connection,
// login, query, timeout); ErrorKey says why (monitor.test_* category,
// sql.*, monitor.discover_*), Error is the technical text.
const ErrCodeFailed = "failed"

// Values of IPCMessage.TruncatedReason / MappingCheck.TruncatedReason
// (Вика 06.10): present if and only if Truncated is true. Precedence
// when several apply: budget > size > limit.
const (
	// TruncatedBudget: the discovery time budget (90 s) ran out; the
	// result is partial (discover / discover_by_value).
	TruncatedBudget = "budget"
	// TruncatedSize: the tail was dropped to fit IPCMaxInteractiveReply
	// (discovery, node_points, mapping_check).
	TruncatedSize = "size"
	// TruncatedLimit: list_node_points returned only IPCRequest.Limit of
	// the points read.
	TruncatedLimit = "limit"
)

// Limits of the interactive commands.
const (
	// IPCMaxInteractiveReply caps the serialized JSON of a discovery /
	// node_points / mapping_check reply (Вика, 06.10): the tail of
	// Candidates / NodePoints / SampleRows is dropped and Truncated
	// (MappingCheck.Truncated) is set.
	IPCMaxInteractiveReply = 64 << 10
	// DefaultCheckMappingLimit / MaxCheckMappingLimit: preview rows of
	// check_mapping (IPCRequest.Limit 0 = default, 1..20 allowed).
	DefaultCheckMappingLimit = 5
	MaxCheckMappingLimit     = 20
	// MaxNodePointsLimit caps IPCRequest.Limit of list_node_points (0 =
	// everything the service read, at most 1000 rows per plan table).
	MaxNodePointsLimit = 10000
)

// MappingCheck is the result of check_mapping (MsgMappingCheck).
type MappingCheck struct {
	// OK: the mapping matches the database and the preview was read.
	OK bool `json:"ok"`
	// ErrorKey / ErrorArgs of a failed check: MsgDiscoverMappingInvalid
	// with ErrorArgs [mapping.<field>, validation.* reason, params...]
	// (model.FieldErrorArgs, texts model.FormatMessage(ErrorArgs[1],
	// ErrorArgs, lang)), or a connection / query key (monitor.test_*,
	// monitor.query_error, monitor.discover_timeout, sql.*) without args.
	ErrorKey  string   `json:"errorKey,omitempty"`
	Error     string   `json:"error,omitempty"`
	ErrorArgs []string `json:"errorArgs,omitempty"`
	// Violations: SQL Server 2012+ constructs found in the manual SQL
	// (Compat2008Violations), checked before anything runs.
	Violations []string `json:"violations,omitempty"`
	// SampleRows: the preview (at most Limit rows, point names cut to
	// MaxSampleCellRunes).
	SampleRows []SampleRow `json:"sampleRows,omitempty"`
	// Truncated: SampleRows were cut to fit IPCMaxInteractiveReply;
	// TruncatedReason is then TruncatedSize (present iff Truncated).
	Truncated       bool   `json:"truncated,omitempty"`
	TruncatedReason string `json:"truncatedReason,omitempty"`
}
