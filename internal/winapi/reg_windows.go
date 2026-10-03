package winapi

import (
	"syscall"
	"unsafe"
)

// Registry access of the tray (per-user language, phase 6e, CHK-TRAY-08):
// RegCreateKeyExW, RegOpenKeyExW, RegQueryValueExW, RegSetValueExW,
// RegDeleteValueW, RegDeleteKeyW, RegCloseKey (NT 3.1+), through callProc.
var (
	procRegCreateKeyExW  = modadvapi32.NewProc("RegCreateKeyExW")
	procRegOpenKeyExW    = modadvapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = modadvapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW   = modadvapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW  = modadvapi32.NewProc("RegDeleteValueW")
	procRegDeleteKeyW    = modadvapi32.NewProc("RegDeleteKeyW")
	procRegCloseKey      = modadvapi32.NewProc("RegCloseKey")
)

// Registry constants.
const (
	HKEY_CURRENT_USER    = 0x80000001
	KEY_QUERY_VALUE      = 0x0001
	KEY_SET_VALUE        = 0x0002
	REG_SZ               = 1
	ERROR_FILE_NOT_FOUND = 2
)

func regErr(r uintptr) error {
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

// RegGetString reads the REG_SZ value name of root\path; a missing key or
// value gives "" and nil.
func RegGetString(root uintptr, path, name string) (string, error) {
	var k uintptr
	r, _, _ := callProc(procRegOpenKeyExW, root, uintptr(unsafe.Pointer(UTF16Ptr(path))), 0, KEY_QUERY_VALUE, uintptr(unsafe.Pointer(&k)))
	if r == ERROR_FILE_NOT_FOUND {
		return "", nil
	}
	if err := regErr(r); err != nil {
		return "", err
	}
	defer callProc(procRegCloseKey, k)
	var typ, size uint32
	np := UTF16Ptr(name)
	r, _, _ = callProc(procRegQueryValueExW, k, uintptr(unsafe.Pointer(np)), 0, uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size)))
	if r == ERROR_FILE_NOT_FOUND {
		return "", nil
	}
	if err := regErr(r); err != nil {
		return "", err
	}
	if typ != REG_SZ || size == 0 || size > 4096 {
		return "", nil
	}
	buf := make([]uint16, size/2+1)
	size = uint32(len(buf) * 2)
	r, _, _ = callProc(procRegQueryValueExW, k, uintptr(unsafe.Pointer(np)), 0, uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if err := regErr(r); err != nil {
		return "", err
	}
	return syscall.UTF16ToString(buf), nil
}

// RegSetString writes the REG_SZ value name of root\path (the key is
// created when missing).
func RegSetString(root uintptr, path, name, value string) error {
	var k uintptr
	r, _, _ := callProc(procRegCreateKeyExW, root, uintptr(unsafe.Pointer(UTF16Ptr(path))), 0, 0, 0, KEY_SET_VALUE, 0,
		uintptr(unsafe.Pointer(&k)), 0)
	if err := regErr(r); err != nil {
		return err
	}
	defer callProc(procRegCloseKey, k)
	v, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	r, _, _ = callProc(procRegSetValueExW, k, uintptr(unsafe.Pointer(UTF16Ptr(name))), 0, REG_SZ,
		uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
	return regErr(r)
}

// RegDeleteValue removes the value name of root\path (missing = nil).
func RegDeleteValue(root uintptr, path, name string) error {
	var k uintptr
	r, _, _ := callProc(procRegOpenKeyExW, root, uintptr(unsafe.Pointer(UTF16Ptr(path))), 0, KEY_SET_VALUE, uintptr(unsafe.Pointer(&k)))
	if r == ERROR_FILE_NOT_FOUND {
		return nil
	}
	if err := regErr(r); err != nil {
		return err
	}
	defer callProc(procRegCloseKey, k)
	r, _, _ = callProc(procRegDeleteValueW, k, uintptr(unsafe.Pointer(UTF16Ptr(name))))
	if r == ERROR_FILE_NOT_FOUND {
		return nil
	}
	return regErr(r)
}

// RegDeleteKey removes the key root\path without subkeys (missing = nil).
func RegDeleteKey(root uintptr, path string) error {
	r, _, _ := callProc(procRegDeleteKeyW, root, uintptr(unsafe.Pointer(UTF16Ptr(path))))
	if r == ERROR_FILE_NOT_FOUND {
		return nil
	}
	return regErr(r)
}
