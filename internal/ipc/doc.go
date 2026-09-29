// Package ipc — service↔tray protocol over a named pipe (PLAN.md §6).
//
// Framing: 4-byte little-endian length + UTF-8 JSON (model.IPCRequest from
// the tray, model.IPCMessage from the service): requests at most
// model.IPCMaxRequest (64 KB), service messages at most
// model.IPCMaxMessage. Per connection the server enforces a read timeout
// for a started request and an idle timeout, and it serves at most
// model.IPCMaxClients clients (CHK-MON-11). The connection is persistent: the service
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
// Check IDs: CHK-MON-06, CHK-MON-11, CHK-MON-12.
package ipc
