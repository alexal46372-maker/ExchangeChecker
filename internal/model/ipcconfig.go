package model

// Phase 6 service side (G5 config writes, G6 cancel; CHK-IPC-06,
// CHK-CFG-03, CHK-IPC-07). Protocol version stays 2: every addition is a
// new command, capability or omitempty field (CHK-IPC-05).

// Commands (IPCRequest.Cmd).
const (
	// CmdGetConfig returns the full configuration (admin only): reply
	// MsgConfig with Config and Revision.
	CmdGetConfig = "get_config"
	// CmdSaveProfile creates / replaces IPCRequest.Profile (admin only;
	// Password, ClearPassword, ExpectedRevision). Reply MsgOK with Result
	// ResultSaved, ProfileID, Profile (HasPassword, never the password),
	// Reloaded, PollScheduled, Revision.
	CmdSaveProfile = "save_profile"
	// CmdDeleteProfile deletes IPCRequest.ProfileID (admin only). Reply
	// MsgOK with Result ResultDeleted, ProfileID, Reloaded, Revision.
	CmdDeleteProfile = "delete_profile"
	// CmdSaveSettings replaces the global settings (admin only,
	// IPCRequest.Settings incl. trayAutostart). Reply MsgOK with Result
	// ResultSaved, Reloaded, Revision.
	CmdSaveSettings = "save_settings"
	// CmdCancel cancels the caller's own running asynchronous request
	// IPCRequest.Ref on the same connection (test_profile, discover,
	// discover_by_value, list_node_points, check_mapping). Reply MsgOK
	// with Result ResultCanceled, or MsgError not_found +
	// MsgMonitorUnknownRequest. The canceled request answers
	// MsgError {id: Ref, code: canceled, errorKey: monitor.canceled}.
	CmdCancel = "cancel"
)

// Capabilities (Hello.Capabilities).
const (
	CapConfigRead  = "config_read"
	CapConfigWrite = "config_write"
	CapCancel      = "cancel"
	// CapTrayAutostartStatus: get_status replies carry trayAutostartError
	// (CHK-CFG-04): the service manages the tray autostart in HKLM Run.
	CapTrayAutostartStatus = "tray_autostart_status"
)

// Codes of IPCMessage.TrayAutostartError (get_status reply, capability
// tray_autostart_status, CHK-CFG-04): the last application of
// settings.trayAutostart to HKLM Run (at service start or after
// save_settings) failed; "" = applied (or nothing to change).
const (
	// AutostartErrAccessDenied: the registry refused access
	// (ERROR_ACCESS_DENIED).
	AutostartErrAccessDenied = "access_denied"
	// AutostartErrRegistry: any other registry error.
	AutostartErrRegistry = "registry_error"
	// AutostartErrTrayPath: the tray path could not be built from the
	// service executable path.
	AutostartErrTrayPath = "tray_path"
	// AutostartErrSettings: at start settings.json could not be loaded;
	// the Run value was left unchanged.
	AutostartErrSettings = "settings_unreadable"
)

// Results (IPCMessage.Result).
const (
	ResultSaved    = "saved"
	ResultDeleted  = "deleted"
	ResultCanceled = "canceled"
)

// MsgConfig is the reply type of get_config.
const MsgConfig = "config"

// Error codes (IPCMessage.Code).
const (
	// ErrCodeAccessDenied: an admin command from a client whose token is
	// not an elevated member of BUILTIN\Administrators (re-checked on
	// every admin command).
	ErrCodeAccessDenied = "access_denied"
	// ErrCodeCanceled: the request was canceled by cancel {ref}.
	ErrCodeCanceled = "canceled"
	// ErrCodeSaveFailed: the configuration could not be read or written.
	ErrCodeSaveFailed = "save_failed"
	// ErrCodeConflict: expectedRevision does not match the configuration
	// on disk; nothing was written.
	ErrCodeConflict = "conflict"
)

// AdminCommand reports whether cmd requires an elevated administrator.
func AdminCommand(cmd string) bool {
	switch cmd {
	case CmdGetConfig, CmdSaveProfile, CmdDeleteProfile, CmdSaveSettings,
		CmdDiscover, CmdDiscoverByValue, CmdListNodePoints, CmdCheckMapping:
		return true
	}
	return false
}

// ConfigView is the configuration returned by get_config: the settings and
// the profiles with HasPassword (never a password).
type ConfigView struct {
	Settings Settings  `json:"settings"`
	Profiles []Profile `json:"profiles"`
}

// ConfigChange describes one configuration change made through IPC (event
// 105, CHK-MON-05 extension): who, what, which profile. Never a password
// or a connection string.
type ConfigChange struct {
	Account   string `json:"account"`
	Action    string `json:"action"` // CmdSaveProfile, CmdDeleteProfile, CmdSaveSettings
	ProfileID string `json:"profileId,omitempty"`
}

// ProfileConnection is the connection part of a profile in the status
// snapshot (read-only settings view, phase 6): no password, no mapping.
type ProfileConnection struct {
	Server            string      `json:"server"`
	Port              int         `json:"port,omitempty"`
	Database          string      `json:"database"`
	AuthMode          AuthMode    `json:"authMode"`
	Login             string      `json:"login,omitempty"`
	HasPassword       bool        `json:"hasPassword"`
	Encrypt           EncryptMode `json:"encrypt,omitempty"`
	TrustServerCert   bool        `json:"trustServerCert,omitempty"`
	TLSMinVersion     string      `json:"tlsMinVersion,omitempty"`
	ServerPreset      string      `json:"serverPreset,omitempty"`
	ConnectTimeoutSec int         `json:"connectTimeoutSec,omitempty"`
	QueryTimeoutSec   int         `json:"queryTimeoutSec,omitempty"`
}

// ThresholdOverride are the profile's own thresholds (0 = global).
type ThresholdOverride struct {
	WarnMinutes  int `json:"warnMinutes"`
	ErrorMinutes int `json:"errorMinutes"`
}

// MappingSummary describes the mapping without SQL or column details.
type MappingSummary struct {
	Mode       MappingMode   `json:"mode,omitempty"`
	Configured bool          `json:"configured"`
	Manual     bool          `json:"manual,omitempty"` // uses manual SQL
	Plans      []MappingPlan `json:"plans,omitempty"`
}

// MappingPlan is one mapping of the summary.
type MappingPlan struct {
	PlanTable string `json:"planTable,omitempty"`
	PlanLabel string `json:"planLabel,omitempty"`
}

// ConnectionOf returns the snapshot connection block of p.
func ConnectionOf(p Profile, hasPassword bool) *ProfileConnection {
	return &ProfileConnection{Server: p.Server, Port: p.Port, Database: p.Database, AuthMode: p.AuthMode, Login: p.Login,
		HasPassword: hasPassword, Encrypt: p.Encrypt, TrustServerCert: p.TrustServerCert, TLSMinVersion: p.TLSMinVersion,
		ServerPreset: p.ServerPreset, ConnectTimeoutSec: p.ConnectTimeoutSec, QueryTimeoutSec: p.QueryTimeoutSec}
}

// MappingSummaryOf returns the mapping summary of p.
func MappingSummaryOf(p Profile) *MappingSummary {
	s := &MappingSummary{Mode: p.Mapping.Mode, Configured: p.Mapping.Configured()}
	for _, m := range p.AllMappings() {
		if m.ManualSQL != "" {
			s.Manual = true
		}
		table := ""
		if m.ManualSQL == "" {
			table = m.Table
		}
		s.Plans = append(s.Plans, MappingPlan{PlanTable: table, PlanLabel: m.PlanLabel})
	}
	return s
}

// SavedProfile is the result of a successful save_profile.
type SavedProfile struct {
	Profile       Profile // HasPassword set, never a password
	Revision      string
	PollScheduled bool
}
