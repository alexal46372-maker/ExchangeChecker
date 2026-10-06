package winapi

import (
	"syscall"
	"unsafe"
)

// Registry access of the service with an explicit registry view (HKLM Run
// for the tray autostart, phase 6, CHK-CFG-04). It reuses the advapi32
// procs of reg_windows.go (RegCreateKeyExW, RegOpenKeyExW,
// RegQueryValueExW, RegSetValueExW, RegDeleteValueW, RegCloseKey; NT 3.1+,
// in the CHK-TRAY-10 allowlist); every call goes through callProc with the
// pointers converted in its argument list (callProc is
// //go:uintptrescapes).

// Registry roots and access flags of the service.
const (
	HKEY_LOCAL_MACHINE = 0x80000002
	// KEY_WOW64_64KEY / KEY_WOW64_32KEY select the 64-bit / 32-bit view
	// of a redirected key (XP x64 / NT 5.2+; ignored by 32-bit Windows).
	KEY_WOW64_64KEY = 0x0100
	KEY_WOW64_32KEY = 0x0200

	errorMoreData = 234
)

// RegGetStringView reads the REG_SZ value name of root\path opened with
// KEY_QUERY_VALUE|view. ok = false when the key or the value is missing;
// a value of another type is reported as ok with "" (the caller rewrites
// it).
func RegGetStringView(root uintptr, path, name string, view uint32) (value string, ok bool, err error) {
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
	for attempt := 0; attempt < 3; attempt++ {
		var typ, size uint32
		r, _, _ = callProc(procRegQueryValueExW, k, uintptr(unsafe.Pointer(np)), 0, uintptr(unsafe.Pointer(&typ)), 0,
			uintptr(unsafe.Pointer(&size)))
		if r == ERROR_FILE_NOT_FOUND {
			return "", false, nil
		}
		if err := regErr(r); err != nil {
			return "", false, err
		}
		if typ != REG_SZ {
			return "", true, nil
		}
		buf := make([]uint16, size/2+1) // +1: the stored string may lack its NUL
		size = uint32(len(buf) * 2)
		r, _, _ = callProc(procRegQueryValueExW, k, uintptr(unsafe.Pointer(np)), 0, uintptr(unsafe.Pointer(&typ)),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
		if r == errorMoreData {
			continue // changed between the two calls
		}
		if r == ERROR_FILE_NOT_FOUND {
			return "", false, nil
		}
		if err := regErr(r); err != nil {
			return "", false, err
		}
		if typ != REG_SZ {
			return "", true, nil
		}
		return syscall.UTF16ToString(buf), true, nil
	}
	return "", false, syscall.Errno(errorMoreData)
}

// RegSetStringView writes the REG_SZ value name of root\path (the key is
// created when missing) opened with KEY_SET_VALUE|view.
func RegSetStringView(root uintptr, path, name, value string, view uint32) error {
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
	r, _, _ = callProc(procRegSetValueExW, k, uintptr(unsafe.Pointer(UTF16Ptr(name))), 0, REG_SZ,
		uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
	return regErr(r)
}

// RegDeleteValueView removes the value name of root\path opened with
// KEY_SET_VALUE|view; a missing key or value is not an error.
func RegDeleteValueView(root uintptr, path, name string, view uint32) error {
	var k uintptr
	r, _, _ := callProc(procRegOpenKeyExW, root, uintptr(unsafe.Pointer(UTF16Ptr(path))), 0,
		uintptr(KEY_SET_VALUE|view), uintptr(unsafe.Pointer(&k)))
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

// RegDeleteKeyExW (advapi32; Vista / XP x64+, present on Windows 7 x64,
// CHK-TRAY-10 allowlist 6.0) deletes a key in an explicit registry view.
var procRegDeleteKeyExW = modadvapi32.NewProc("RegDeleteKeyExW")

// RegDeleteKeyView removes the key root\path (without subkeys) from the
// registry view (KEY_WOW64_64KEY / KEY_WOW64_32KEY); a missing key is not
// an error.
func RegDeleteKeyView(root uintptr, path string, view uint32) error {
	r, _, _ := callProc(procRegDeleteKeyExW, root, uintptr(unsafe.Pointer(UTF16Ptr(path))), uintptr(view), 0)
	if r == ERROR_FILE_NOT_FOUND {
		return nil
	}
	return regErr(r)
}

// RegKeyExistsView reports whether the key root\path exists in the
// registry view (RegOpenKeyExW with KEY_QUERY_VALUE|view).
func RegKeyExistsView(root uintptr, path string, view uint32) (bool, error) {
	var k uintptr
	r, _, _ := callProc(procRegOpenKeyExW, root, uintptr(unsafe.Pointer(UTF16Ptr(path))), 0,
		uintptr(KEY_QUERY_VALUE|view), uintptr(unsafe.Pointer(&k)))
	if r == ERROR_FILE_NOT_FOUND {
		return false, nil
	}
	if err := regErr(r); err != nil {
		return false, err
	}
	callProc(procRegCloseKey, k)
	return true, nil
}
