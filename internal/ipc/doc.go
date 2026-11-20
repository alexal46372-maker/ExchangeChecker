// Package ipc — service↔tray protocol over a named pipe (PLAN.md §6).
//
// Framing: 4-byte little-endian length + UTF-8 JSON (model.IPCRequest from
// the tray, model.IPCMessage from the service): requests at most
// model.IPCMaxRequest (64 KB; import_config from an elevated administrator:
// model.IPCMaxImportRequest, 1 MiB, phase 10), service messages at most
// model.IPCMaxMessage. Per connection the server enforces a read timeout
// for a started request and an idle timeout, and it serves at most
// model.IPCMaxClients clients (CHK-MON-11). The connection is persistent:
// the service replies to commands and pushes (see below).
//
// Commands (model.Cmd*; the server switch and the capabilities announced
// in hello, protocol version 2, CHK-IPC-05). Any client:
//
//	ping, get_status, reload_config, poll_now   (CHK-MON-06, CHK-MON-12)
//	client_hello                                (CHK-IPC-11)
//	test_profile, cancel {ref}                  (CHK-IPC-03, CHK-IPC-07)
//	ack_alert {profileId, pointKey? | eventId}  (CHK-IPC-02, CHK-MON-18)
//	mute_until_recovered, unmute_profile        (CHK-IPC-15, CHK-MON-20)
//	dismiss_flash, resume_flash {eventId}       (CHK-IPC-19, CHK-IPC-22)
//	speaker_tone, speaker_status                (CHK-IPC-20)
//	history, check_now, events_since            (CHK-SVC-03, CHK-SVC-04, CHK-SVC-05)
//	summary                                     (CHK-SVC-07)
//
// Only for an elevated administrator (checked on every request,
// CHK-IPC-06; otherwise access_denied):
//
//	get_config, save_profile, delete_profile, save_settings   (CHK-IPC-06)
//	clone_profile                                             (CHK-CFG-08)
//	set_enabled                                               (CHK-IPC-18)
//	export_config, import_config                              (CHK-CFG-06, CHK-CFG-07)
//	discover, discover_by_value, list_node_points             (CHK-IPC-08)
//	check_mapping                                             (CHK-IPC-09)
//	sql_browse, sql_databases, sql_diagnose, sql_grant_script (CHK-IPC-10, CHK-IPC-12)
//	sql_test_steps                                            (CHK-IPC-13)
//	alarm_test                                                (CHK-IPC-11)
//	viber_test, email_test, telegram_test                     (CHK-SVC-06, CHK-IPC-16, CHK-IPC-17)
//
// Pushes to every client: status after every poll and every alert event,
// alert, alert_repeat, flash / flash_stop (CHK-MON-22, CHK-IPC-19) and
// profile_enabled (CHK-IPC-18); progress pushes of sql_browse and
// sql_test_steps go to the requester. Passwords and tokens never travel
// back to the tray: a saved SQL password (secrets.dpapi, see package
// config) is used by the service only.
//
// The server is transport-agnostic (Listener); on Windows PipeListener
// implements it with CreateNamedPipeW / ConnectNamedPipe and overlapped
// ReadFile / WriteFile (kernel32 via syscall, no helper processes). The
// pipe DACL gives SYSTEM and Administrators full access and interactive
// users read/write without FILE_CREATE_PIPE_INSTANCE; remote clients are
// rejected (PIPE_REJECT_REMOTE_CLIENTS).
//
// Check IDs: CHK-MON-06, CHK-MON-11, CHK-MON-12, CHK-MON-17, CHK-MON-18,
// CHK-MON-20, CHK-IPC-02 … CHK-IPC-20, CHK-IPC-22, CHK-CFG-04,
// CHK-CFG-06 … CHK-CFG-10, CHK-SVC-03 … CHK-SVC-08, CHK-SQL-16.
package ipc
