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
)

const (
	tokenAdjustPrivileges = 0x0020
	tokenQuery            = 0x0008
	sePrivilegeEnabled    = 0x00000002
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

// EnablePrivileges enables the named privileges on the current process token
// (AdjustTokenPrivileges). Used before SetNamedSecurityInfo OWNER changes
// (SeTakeOwnershipPrivilege, SeRestorePrivilege).
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
		r, _, e = callProc(procAdjustTokenPrivileges, uintptr(tok), 0, uintptr(unsafe.Pointer(&tp)), 0, 0, 0)
		// AdjustTokenPrivileges returns non-zero on success even when some
		// privileges were not assigned; GetLastError may be ERROR_NOT_ALL_ASSIGNED (1300).
		if r == 0 {
			return fmt.Errorf("AdjustTokenPrivileges %s: %w", name, lastErr(e))
		}
	}
	return nil
}
