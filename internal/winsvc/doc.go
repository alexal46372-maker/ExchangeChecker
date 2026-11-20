// Package winsvc — Windows Service wrapper through advapi32 via syscall
// (StartServiceCtrlDispatcherW, RegisterServiceCtrlHandlerExW,
// SetServiceStatus; PLAN.md §8). No sc.exe / net.exe / PowerShell helper
// processes (POLICY); no golang.org/x/sys dependency.
//
// Run starts the SCM dispatcher; ServiceMain reports START_PENDING, runs
// the service function with a context and reports RUNNING; SCM Stop,
// Shutdown and Preshutdown report STOP_PENDING and cancel the context;
// when the function returns, STOPPED is reported. Outside the SCM (console
// start) Run returns ErrNotService and the caller runs interactively.
//
// Stop, Shutdown, Preshutdown and a power Suspend (SERVICE_CONTROL_POWEREVENT
// + PBT_APMSUSPEND) also call the hook set with SetOnSilence, which the
// service uses to silence the PC speaker and unload its driver (CHK-SVC-11).
//
// Check IDs: CHK-MON-07, CHK-SVC-11.
package winsvc
