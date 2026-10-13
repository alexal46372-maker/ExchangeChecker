//go:build windows

package winapi

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	procLookupPrivilegeValueW = modadvapi32.NewProc("LookupPrivilegeValueW")
	procAdjustTokenPrivileges = modadvapi32.NewProc("AdjustTokenPrivileges")
	procOpenProcessToken      = modadvapi32.NewProc("OpenProcessToken")
	procSetLastError          = modkernel32.NewProc("SetLastError")
	procGetLastError          = modkernel32.NewProc("GetLastError")
)

const (
	tokenAdjustPrivileges = 0x0020
	tokenQuery            = 0x0008
	sePrivilegeEnabled    = 0x00000002
	errNotAllAssigned     = syscall.Errno(1300) // ERROR_NOT_ALL_ASSIGNED
)

type luid struct {
	LowPart  uint32
	HighPart int32
}

type luidAndAttributes struct {
	Luid       luid
	Attributes uint32
}

// tokenPrivileges with one privilege (ANYSIZE_ARRAY = 1).
type tokenPrivileges1 struct {
	PrivilegeCount uint32
	Privileges     luidAndAttributes
}

func setLastError(code uint32) {
	callProc(procSetLastError, uintptr(code))
}

// lastError is the thread's GetLastError after a WinAPI call (via callProc /
// LazyProc.Call). Prefer the error returned by callProc; this is a fallback
// when that value was cleared.
func lastError() syscall.Errno {
	r, _, _ := callProc(procGetLastError)
	return syscall.Errno(r)
}

// EnablePrivileges enables the named privileges on the current process token
// (AdjustTokenPrivileges). Used before SetNamedSecurityInfo OWNER changes
// (SeTakeOwnershipPrivilege, SeRestorePrivilege).
//
// After a successful AdjustTokenPrivileges, ERROR_NOT_ALL_ASSIGNED (1300)
// means one or more privileges were not held — treated as failure. The
// caller (TakeOwnershipTree) may still try SetFileOwner without them.
func EnablePrivileges(names ...string) error {
	var tok syscall.Token
	proc, _ := syscall.GetCurrentProcess()
	r, _, e := callProc(procOpenProcessToken, uintptr(proc), tokenAdjustPrivileges|tokenQuery, uintptr(unsafe.Pointer(&tok)))
	if r == 0 {
		return fmt.Errorf("OpenProcessToken: %w", lastErr(e))
	}
	defer tok.Close()
	for _, name := range names {
		var id luid
		r, _, e = callProc(procLookupPrivilegeValueW, 0, uintptr(unsafe.Pointer(UTF16Ptr(name))), uintptr(unsafe.Pointer(&id)))
		if r == 0 {
			return fmt.Errorf("LookupPrivilegeValue %s: %w", name, lastErr(e))
		}
		tp := tokenPrivileges1{PrivilegeCount: 1, Privileges: luidAndAttributes{Luid: id, Attributes: sePrivilegeEnabled}}
		// MSDN: SetLastError(0) before AdjustTokenPrivileges so a true return
		// with ERROR_NOT_ALL_ASSIGNED is distinguishable from a clean success.
		setLastError(0)
		r, _, e = callProc(procAdjustTokenPrivileges, uintptr(tok), 0, uintptr(unsafe.Pointer(&tp)), 0, 0, 0)
		if r == 0 {
			return fmt.Errorf("AdjustTokenPrivileges %s: %w", name, lastErr(e))
		}
		// callProc's third return is GetLastError from LazyProc.Call; also
		// re-read GetLastError in case the Errno was zeroed.
		en, _ := e.(syscall.Errno)
		if en == 0 {
			en = lastError()
		}
		if en == errNotAllAssigned {
			return fmt.Errorf("AdjustTokenPrivileges %s: ERROR_NOT_ALL_ASSIGNED (privilege not held)", name)
		}
	}
	return nil
}
