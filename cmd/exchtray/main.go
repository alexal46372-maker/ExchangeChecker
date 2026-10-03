// Command exchtray is the Exchange Checker system-tray UI (PLAN.md §7,
// phase 6): a NotifyIcon whose left-click menu lists the monitored
// databases and whose right-click menu has Settings / About / Exit. It
// talks to the service only over the named pipe (internal/trayipc) and
// never touches SQL Server itself (CHK-TRAY-10).
//
// Build: go build -ldflags="-H windowsgui" ./cmd/exchtray
// The icon and manifest come from rsrc_windows_amd64.syso (RESOURCES.md).
package main
