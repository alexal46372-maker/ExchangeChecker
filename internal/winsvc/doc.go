// Package winsvc — Windows Service wrapper through advapi32 via syscall
// (StartServiceCtrlDispatcherW, RegisterServiceCtrlHandlerExW,
// SetServiceStatus; PLAN.md §8). No sc.exe / net.exe / PowerShell helper
// processes (POLICY); no golang.org/x/sys dependency.
//
// Run starts the SCM dispatcher; ServiceMain reports START_PENDING, runs
// the service function with a context and reports RUNNING; SCM Stop and
// Shutdown report STOP_PENDING and cancel the context; when the function
// returns, STOPPED is reported. Outside the SCM (console start) Run
// returns ErrNotService and the caller runs interactively.
//
// Check ID: CHK-MON-07.
package winsvc
