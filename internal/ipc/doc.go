// Package ipc — service↔tray protocol over a named pipe (PLAN.md §6).
//
// Framing: 4-byte little-endian length + UTF-8 JSON (model.IPCRequest from
// the tray, model.IPCMessage from the service), at most
// model.IPCMaxMessage bytes. The connection is persistent: the service
// replies to commands (ping, get_status, reload_config, poll_now) and
// pushes a status snapshot after every poll and every alert event.
//
// The server is transport-agnostic (Listener); on Windows PipeListener
// implements it with CreateNamedPipeW / ConnectNamedPipe and overlapped
// ReadFile / WriteFile (kernel32 via syscall, no helper processes). The
// pipe DACL gives SYSTEM and Administrators full access and interactive
// users read/write without FILE_CREATE_PIPE_INSTANCE; remote clients are
// rejected (PIPE_REJECT_REMOTE_CLIENTS).
//
// Check ID: CHK-MON-06.
package ipc
