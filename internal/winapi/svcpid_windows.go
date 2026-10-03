//go:build windows

package winapi

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Pipe server / service process IDs for the tray's pipe check (PLAN §12,
// G7): GetNamedPipeServerProcessId (Vista+) and QueryServiceStatusEx
// (SC_STATUS_PROCESS_INFO). NT 6.1 compatible; calls go through callProc.

var (
	procGetNamedPipeServerProcessId = modkernel32.NewProc("GetNamedPipeServerProcessId")
	procOpenSCManagerW              = modadvapi32.NewProc("OpenSCManagerW")
	procOpenServiceW                = modadvapi32.NewProc("OpenServiceW")
	procQueryServiceStatusEx        = modadvapi32.NewProc("QueryServiceStatusEx")
	procCloseServiceHandle          = modadvapi32.NewProc("CloseServiceHandle")
)

const (
	scManagerConnect    = 0x0001
	serviceQueryStatus  = 0x0004
	scStatusProcessInfo = 0
	// ServiceRunning is SERVICE_RUNNING.
	ServiceRunning = 4
)

// serviceStatusProcess is SERVICE_STATUS_PROCESS.
type serviceStatusProcess struct {
	ServiceType             uint32
	CurrentState            uint32
	ControlsAccepted        uint32
	Win32ExitCode           uint32
	ServiceSpecificExitCode uint32
	CheckPoint              uint32
	WaitHint                uint32
	ProcessID               uint32
	ServiceFlags            uint32
}

// NamedPipeServerProcessID returns the PID of the server end of the pipe
// handle h (client side).
func NamedPipeServerProcessID(h syscall.Handle) (uint32, error) {
	var pid uint32
	r, _, e := callProc(procGetNamedPipeServerProcessId, uintptr(h), uintptr(unsafe.Pointer(&pid)))
	if r == 0 {
		return 0, lastErr(e)
	}
	return pid, nil
}

// ServiceProcessID returns the state and PID of a Windows service via the
// SCM (SC_MANAGER_CONNECT + SERVICE_QUERY_STATUS only). PID is 0 when the
// service does not run.
func ServiceProcessID(name string) (state, pid uint32, err error) {
	scm, _, e := callProc(procOpenSCManagerW, 0, 0, scManagerConnect)
	if scm == 0 {
		return 0, 0, fmt.Errorf("OpenSCManager: %w", lastErr(e))
	}
	defer callProc(procCloseServiceHandle, scm)
	svc, _, e := callProc(procOpenServiceW, scm, uintptr(unsafe.Pointer(UTF16Ptr(name))), serviceQueryStatus)
	if svc == 0 {
		return 0, 0, fmt.Errorf("OpenService %s: %w", name, lastErr(e))
	}
	defer callProc(procCloseServiceHandle, svc)
	var st serviceStatusProcess
	var need uint32
	r, _, e := callProc(procQueryServiceStatusEx, svc, scStatusProcessInfo,
		uintptr(unsafe.Pointer(&st)), unsafe.Sizeof(st), uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return 0, 0, fmt.Errorf("QueryServiceStatusEx %s: %w", name, lastErr(e))
	}
	return st.CurrentState, st.ProcessID, nil
}
