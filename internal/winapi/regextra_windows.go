//go:build windows

package winapi

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

const (
	REG_EXPAND_SZ = 2
	REG_DWORD     = 4
)

// RegSetDWORDView writes a REG_DWORD value (key created when missing).
func RegSetDWORDView(root uintptr, path, name string, value uint32, view uint32) error {
	var k uintptr
	r, _, _ := callProc(procRegCreateKeyExW, root, uintptr(unsafe.Pointer(UTF16Ptr(path))), 0, 0, 0,
		uintptr(KEY_SET_VALUE|view), 0, uintptr(unsafe.Pointer(&k)), 0)
	if err := regErr(r); err != nil {
		return err
	}
	defer callProc(procRegCloseKey, k)
	var v uint32 = value
	r, _, _ = callProc(procRegSetValueExW, k, uintptr(unsafe.Pointer(UTF16Ptr(name))), 0, REG_DWORD,
		uintptr(unsafe.Pointer(&v)), 4)
	return regErr(r)
}

// RegSetExpandStringView writes a REG_EXPAND_SZ value.
func RegSetExpandStringView(root uintptr, path, name, value string, view uint32) error {
	v, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	var k uintptr
	r, _, _ := callProc(procRegCreateKeyExW, root, uintptr(unsafe.Pointer(UTF16Ptr(path))), 0, 0, 0,
		uintptr(KEY_SET_VALUE|view), 0, uintptr(unsafe.Pointer(&k)), 0)
	if err := regErr(r); err != nil {
		return err
	}
	defer callProc(procRegCloseKey, k)
	r, _, _ = callProc(procRegSetValueExW, k, uintptr(unsafe.Pointer(UTF16Ptr(name))), 0, REG_EXPAND_SZ,
		uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
	return regErr(r)
}

// RegGetDWORDView reads a REG_DWORD; ok=false when missing.
func RegGetDWORDView(root uintptr, path, name string, view uint32) (value uint32, ok bool, err error) {
	var k uintptr
	r, _, _ := callProc(procRegOpenKeyExW, root, uintptr(unsafe.Pointer(UTF16Ptr(path))), 0,
		uintptr(KEY_QUERY_VALUE|view), uintptr(unsafe.Pointer(&k)))
	if r == ERROR_FILE_NOT_FOUND {
		return 0, false, nil
	}
	if err := regErr(r); err != nil {
		return 0, false, err
	}
	defer callProc(procRegCloseKey, k)
	var typ, size uint32
	var buf [8]byte
	size = 4
	r, _, _ = callProc(procRegQueryValueExW, k, uintptr(unsafe.Pointer(UTF16Ptr(name))), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == ERROR_FILE_NOT_FOUND {
		return 0, false, nil
	}
	if err := regErr(r); err != nil {
		return 0, false, err
	}
	if typ != REG_DWORD || size < 4 {
		return 0, true, nil
	}
	return binary.LittleEndian.Uint32(buf[:4]), true, nil
}

// RegGetExpandStringView reads REG_EXPAND_SZ or REG_SZ (returned unexpanded).
func RegGetExpandStringView(root uintptr, path, name string, view uint32) (value string, ok bool, err error) {
	var k uintptr
	r, _, _ := callProc(procRegOpenKeyExW, root, uintptr(unsafe.Pointer(UTF16Ptr(path))), 0,
		uintptr(KEY_QUERY_VALUE|view), uintptr(unsafe.Pointer(&k)))
	if r == ERROR_FILE_NOT_FOUND {
		return "", false, nil
	}
	if err := regErr(r); err != nil {
		return "", false, err
	}
	defer callProc(procRegCloseKey, k)
	np := UTF16Ptr(name)
	var typ, size uint32
	r, _, _ = callProc(procRegQueryValueExW, k, uintptr(unsafe.Pointer(np)), 0, uintptr(unsafe.Pointer(&typ)), 0,
		uintptr(unsafe.Pointer(&size)))
	if r == ERROR_FILE_NOT_FOUND {
		return "", false, nil
	}
	if err := regErr(r); err != nil {
		return "", false, err
	}
	if typ != REG_EXPAND_SZ && typ != REG_SZ {
		return "", true, nil
	}
	buf := make([]uint16, size/2+1)
	size = uint32(len(buf) * 2)
	r, _, _ = callProc(procRegQueryValueExW, k, uintptr(unsafe.Pointer(np)), 0, uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == ERROR_FILE_NOT_FOUND {
		return "", false, nil
	}
	if err := regErr(r); err != nil {
		return "", false, err
	}
	return syscall.UTF16ToString(buf), true, nil
}
