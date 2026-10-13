//go:build windows

package winapi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	ownerSecurityInformation = 0x1
	SIDLocalSystem           = "S-1-5-18"
)

// FileOwnerSID returns the owner SID string of path.
func FileOwnerSID(path string) (string, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	var owner, sd uintptr
	r, _, _ := callProc(procGetNamedSecurityInfoW, uintptr(unsafe.Pointer(p)), seFileObject, ownerSecurityInformation,
		uintptr(unsafe.Pointer(&owner)), 0, 0, 0, uintptr(unsafe.Pointer(&sd)))
	if r != 0 {
		return "", &os.PathError{Op: "GetNamedSecurityInfo", Path: path, Err: syscall.Errno(r)}
	}
	defer callProc(procLocalFree, sd)
	var str *uint16
	r, _, e := callProc(procConvertSDToStringSDW, sd, sddlRevision1, ownerSecurityInformation, uintptr(unsafe.Pointer(&str)), 0)
	if r == 0 {
		return "", fmt.Errorf("winapi: ConvertSDToStringSD(owner): %w", e)
	}
	defer callProc(procLocalFree, uintptr(unsafe.Pointer(str)))
	s := utf16PtrToString(str)
	if len(s) >= 2 && (s[0] == 'O' || s[0] == 'o') && s[1] == ':' {
		s = s[2:]
	}
	switch s {
	case "BA":
		return SIDAdministrators, nil
	case "SY":
		return SIDLocalSystem, nil
	}
	return s, nil
}

// SetFileOwner sets the owner of path to sidString (e.g. SIDAdministrators).
func SetFileOwner(path, sidString string) error {
	sid, err := syscall.StringToSid(sidString)
	if err != nil {
		return err
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, e := callProc(procSetNamedSecurityInfoW, uintptr(unsafe.Pointer(p)), seFileObject,
		ownerSecurityInformation, uintptr(unsafe.Pointer(sid)), 0, 0, 0)
	if e != nil && r == 0 {
		if _, ok := e.(syscall.Errno); !ok {
			return e
		}
	}
	if r != 0 {
		return &os.PathError{Op: "SetNamedSecurityInfo(owner)", Path: path, Err: syscall.Errno(r)}
	}
	return nil
}

// TakeOwnershipTree sets the owner of path and every descendant to
// SIDAdministrators when the current owner is neither SYSTEM nor
// Administrators. Enables SeTakeOwnershipPrivilege and SeRestorePrivilege
// first. FileOwnerSID failures are returned as a joined warning error
// (caller logs them); the walk continues. changed is true when at least
// one owner was foreign and was changed.
func TakeOwnershipTree(path string) (changed bool, err error) {
	if err := EnablePrivileges("SeTakeOwnershipPrivilege", "SeRestorePrivilege"); err != nil {
		return false, fmt.Errorf("enable ownership privileges: %w", err)
	}
	var warns []string
	walk := func(p string, _ os.FileInfo, werr error) error {
		if werr != nil {
			warns = append(warns, fmt.Sprintf("%s: walk: %v", p, werr))
			return nil
		}
		owner, oerr := FileOwnerSID(p)
		if oerr != nil {
			warns = append(warns, fmt.Sprintf("%s: FileOwnerSID: %v", p, oerr))
			return nil
		}
		if owner == SIDAdministrators || owner == SIDLocalSystem {
			return nil
		}
		if err := SetFileOwner(p, SIDAdministrators); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		changed = true
		return nil
	}
	if err := filepath.Walk(path, walk); err != nil {
		return changed, err
	}
	if len(warns) > 0 {
		return changed, fmt.Errorf("%s", strings.Join(warns, "; "))
	}
	return changed, nil
}
