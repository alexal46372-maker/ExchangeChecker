// Package trayipc is the tray's connection to the service (phase 6a,
// CHK-IPC-01): a reconnecting session on top of ipc.Client.
//
//   - after Dial the optional Verify hook runs BEFORE anything is sent
//     (PLAN §12: the pipe server PID must be the PID of the service from
//     SCM, G7); a failed check closes the pipe and nothing is sent;
//   - the first message of the service is the hello push (ID 0): protocol
//     version, capabilities and heartbeat (serviceStartedAt); then
//     get_status;
//   - ping every model.IPCHeartbeat (60 s; the service drops a client that
//     is silent for model.IPCIdleTimeout); no pong within PingTimeout ->
//     StateNotResponding and reconnect;
//   - reconnect with backoff BackoffMin..BackoffMax (1..30 s), reset after
//     a successful hello; a changed hello.heartbeat.serviceStartedAt means
//     the service restarted (snapshot sequence starts again);
//   - protocol version 1 or a missing capability hides the new features:
//     version-2 commands are refused locally (ErrUnsupported) and never
//     sent to a version-1 service; an unknown_command reply (version 2:
//     code, version 1: "unknown command: " text) disables the command until
//     the next hello.
//
// Events are delivered by a dispatcher goroutine through an unbounded
// queue, so a slow consumer never blocks the pipe reader (the service
// disconnects clients that do not read, internal/ipc/server.go).
package trayipc
