// Package config reads and writes the Exchange Checker files under
// %ProgramData%\ExchangeChecker\ (PLAN.md §4.1; -config <dir> of the
// service overrides the directory). Only the service writes them: an
// elevated administrator changes the configuration through IPC (save_profile,
// save_settings, set_enabled, import_config, ...), the tray never writes
// these files (CHK-IPC-06).
//
// Configuration (Store; atomic writes). The files get no DACL of their
// own: they inherit that of the data folder, which the installer sets to
// SYSTEM + Administrators only, protected (PLAN.md §12 п. 90); the tray
// reads the configuration through IPC (get_status / get_config), not from
// disk:
//
//	settings.json   global settings (model.Settings), including the
//	                non-secret parts of settings.viber / email / telegram
//	profiles.json   SQL profiles without passwords (model.Profile)
//
// Secrets (Store; DPAPI LocalMachine blobs with CryptProtectData, DACL
// SYSTEM + Administrators, protected, SecretsSDDL; never part of
// export_config, never shown to the tray, never logged):
//
//	secrets.dpapi   SQL passwords of the profiles (profile ID -> password),
//	                stored encrypted (decision of 20.11.2026); the service
//	                decrypts them for polling and tests
//	viber.dpapi     Viber bot token (CHK-SVC-06)
//	email.dpapi     SMTP password (CHK-CFG-09)
//	telegram.dpapi  Telegram bot token (CHK-CFG-10)
//
// State of the service in the same directory (written with
// WriteFileAtomic by internal/monitor and internal/notify; a broken
// counters / ack / mute file is set aside as <name>.corrupt and its state
// starts empty):
//
//	counters-state.json  counter baselines of 1C 8.1 counters mappings (CHK-MON-02)
//	ack-state.json       acknowledged alerts, kept across restarts (CHK-IPC-02)
//	mute-state.json      profiles muted until recovery (CHK-MON-20)
//	viber_state.json     date of the last Viber summary (CHK-SVC-06)
//
// Other files of the service (not handled by this package):
//
//	history\YYYY-MM-DD.jsonl  poll history, SYSTEM + Administrators (CHK-SVC-03)
//	logs\exchsvc.log[.1-.5]   service log, rotated at 1 MiB
//
// Every atomic write goes through a temp file <name>.tmp-<hex> in the same
// directory (fsync + MoveFileEx replace; one writer per path at a time,
// LockPath). The tray autostart value in HKLM Run is a registry value, not
// a file (CHK-CFG-04).
// Check IDs: CHK-CFG-01 … CHK-CFG-10, CHK-MON-02.
package config
