//go:build windows

package winapi

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	ownerSecurityInformation = 0x1
	SIDLocalSystem           = "S-1-5-18"
)

// FileOwnerSID returns the owner SID string of path (GetNamedSecurityInfoW +
// ConvertSidToStringSid is avoided: we use ConvertSecurityDescriptorToString-
// SecurityDescriptorW with OWNER_SECURITY_INFORMATION).
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
	// SDDL like "O:BA" or "O:S-1-5-32-544"
	s := utf16PtrToString(str)
	if len(s) >= 2 && (s[0] == 'O' || s[0] == 'o') && s[1] == ':' {
		s = s[2:]
	}
	// Expand well-known short forms used in SDDL
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
// Administrators. changed is true when at least one owner was foreign.
func TakeOwnershipTree(path string) (changed bool, err error) {
	walk := func(p string, _ os.FileInfo, werr error) error {
		if werr != nil {
			return nil
		}
		owner, oerr := FileOwnerSID(p)
		if oerr != nil {
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
	return changed, nil
}
