//go:build windows

package winapi

import "unsafe"

// Service control wrappers (advapi32, CHK-MON-07). The table and status
// pointers are passed as unsafe.Pointer to keep this package free of the
// winsvc types.

// StartServiceCtrlDispatcher wraps StartServiceCtrlDispatcherW; table
// points to a NULL-terminated SERVICE_TABLE_ENTRYW array. It blocks until
// all services of the process stopped.
func StartServiceCtrlDispatcher(table unsafe.Pointer) error {
	r, _, e := callProc(procStartServiceCtrlDispW, uintptr(table))
	if r == 0 {
		return e
	}
	return nil
}

// RegisterServiceCtrlHandlerEx wraps RegisterServiceCtrlHandlerExW.
func RegisterServiceCtrlHandlerEx(name *uint16, handler uintptr) (uintptr, error) {
	h, _, e := callProc(procRegisterServiceCtrlHExW, uintptr(unsafe.Pointer(name)), handler, 0)
	if h == 0 {
		return 0, e
	}
	return h, nil
}

// SetServiceStatus wraps SetServiceStatus (status -> SERVICE_STATUS).
func SetServiceStatus(h uintptr, status unsafe.Pointer) error {
	r, _, e := callProc(procSetServiceStatus, h, uintptr(status))
	if r == 0 {
		return e
	}
	return nil
}
