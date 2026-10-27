package model

import (
	"encoding/json"
	"time"
)

// Phase 10 (EC Service, CHK-CFG-06) + phase 12 (CHK-CFG-07 / CHK-IPC-14):
// export_config / import_config (admin only). The file format "ec-config/1"
// carries the settings and the profiles with HasPassword; never a
// password or a DPAPI secret. Viber: by default (includeViberRecipients
// false / absent) settings.viber is not exported and import keeps the
// stored one; with includeViberRecipients true the non-secret viber
// fields are transferred (recipients [{id,name}], enabled, schedule,
// events, reminderMinutes) — never token / hasToken / status /
// viber.dpapi. Root flag containsViberRecipients marks whether the file
// carries recipients. The "tray" block belongs to EC Tray: the service
// does not store it (accepted and ignored by import_config, never
// exported).

// Commands and capabilities.
const (
	// CmdExportConfig: reply MsgConfigExport with Export.
	CmdExportConfig = "export_config"
	// CmdImportConfig: IPCRequest.Config, Mode, ExpectedRevision, DryRun;
	// reply MsgOK with Result ResultImported / ResultImportChecked,
	// Revision and Import.
	CmdImportConfig = "import_config"
	CapExportConfig = "export_config"
	CapImportConfig = "import_config"
	// CapExportViberRecipients: export_config / import_config accept
	// includeViberRecipients (phase 12, CHK-IPC-14).
	CapExportViberRecipients = "export_viber_recipients"
	MsgConfigExport          = "config_export"

	ResultImported      = "imported"
	ResultImportChecked = "dry_run"

	ImportMerge   = "merge" // default: profiles by id updated / added, others kept
	ImportReplace = "replace"

	// ConfigFormatV1 is the only accepted format.
	ConfigFormatV1 = "ec-config/1"

	// IPCMaxImportRequest: the request limit of import_config from an
	// elevated administrator; every other request keeps IPCMaxRequest
	// (64 KiB). The export reply is one message within IPCMaxMessage (like
	// get_config; the 64 KiB cap is for interactive replies only).
	IPCMaxImportRequest = 1 << 20

	// ImportTrayMax caps the raw "tray" block of import_config (bytes;
	// EC Tray's block, ignored by the service): above -> invalid_request
	// MsgImportTrayTooLarge, errorArgs ["tray", "validation.too_long",
	// "262144"].
	ImportTrayMax = 256 << 10
)

// Error keys of import_config (texts in monitorMessages).
const (
	// MsgImportFormat: format missing or not ConfigFormatV1 (errorArgs
	// ["format", validation.*]).
	MsgImportFormat = "monitor.import_format"
	// MsgImportMode: mode not merge / replace.
	MsgImportMode = "monitor.import_mode"
	// MsgImportDuplicateID: two profiles with the same id (profileId set).
	MsgImportDuplicateID = "monitor.import_duplicate_id"
	// MsgImportNoConfig: import_config without config.
	MsgImportNoConfig = "monitor.import_no_config"
	// MsgImportTrayTooLarge: the tray block is longer than ImportTrayMax.
	MsgImportTrayTooLarge = "monitor.import_tray_too_large"
	// MsgImportRollbackFailed (code save_failed): a write step failed and
	// the previous files could not all be restored: the configuration on
	// disk is partly imported (state not restored); the service reloaded
	// what is on disk.
	MsgImportRollbackFailed = "monitor.import_rollback_failed"
)

// ConfigExport is the exported configuration (export_config reply,
// import_config request).
type ConfigExport struct {
	Format         string     `json:"format"`
	ExportedAt     *time.Time `json:"exportedAt,omitempty"`
	ServiceVersion string     `json:"serviceVersion,omitempty"`
	// Settings: nil on import = keep the current settings.
	Settings *Settings `json:"settings,omitempty"`
	// Profiles with HasPassword (export); HasPassword is ignored on import.
	Profiles []Profile `json:"profiles"`
	// ContainsLogins (export, always sent): at least one exported profile
	// has a non-empty SQL login (the file is then personal data; logins
	// are exported, passwords never). Ignored on import.
	ContainsLogins bool `json:"containsLogins"`
	// ContainsViberRecipients (export, always sent; CHK-CFG-07): the
	// export includes at least one settings.viber.recipients entry
	// (only possible when includeViberRecipients was set). Ignored on
	// import — the request flag includeViberRecipients decides whether
	// settings.viber is applied.
	ContainsViberRecipients bool `json:"containsViberRecipients"`
	// Tray is EC Tray's own block: ignored by the service.
	Tray json.RawMessage `json:"tray,omitempty"`
}

// ImportResult is the outcome of import_config (also of a dry run).
// Profile id lists, always sent (empty = []): Added (new ids, also the
// generated id of a profile without one), Updated (existing, changed),
// Removed (replace mode only), NeedPassword (SQL authentication without a
// reusable saved password: the password must be entered again; the
// profile is imported, its old secret dropped).
type ImportResult struct {
	DryRun       bool     `json:"dryRun"`
	Mode         string   `json:"mode"`
	Revision     string   `json:"revision"`
	Added        []string `json:"added"`
	Updated      []string `json:"updated"`
	Removed      []string `json:"removed"`
	NeedPassword []string `json:"needPassword"`
}
