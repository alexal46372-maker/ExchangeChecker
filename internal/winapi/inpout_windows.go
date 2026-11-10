//go:build windows

package winapi

import (
	"fmt"
	"syscall"
	"unsafe"
)

// DeviceIoControl (kernel32, Windows 2000+ / NT 5.0) used to issue
// InpOutx64 port-I/O IOCTLs (CHK-SVC-12). No InpOut DLL is loaded.
var procDeviceIoControl = modkernel32.NewProc("DeviceIoControl")

// OpenDeviceFile opens an existing device path (e.g. \\.\inpoutx64)
// with GENERIC_READ|GENERIC_WRITE, share 0, OPEN_EXISTING,
// FILE_ATTRIBUTE_NORMAL — the same flags as InpOut Opendriver
// (inpout32drv.cpp lines 175–181). Uses syscall.CreateFile (already
// used elsewhere in this package); no extra NewProc.
func OpenDeviceFile(path string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return 0, fmt.Errorf("CreateFile %s: %w", path, err)
	}
	return h, nil
}

// DeviceIoControl wraps kernel32!DeviceIoControl (synchronous).
func DeviceIoControl(h syscall.Handle, code uint32, in, out []byte) (uint32, error) {
	var ret uint32
	var inPtr, outPtr uintptr
	var inLen, outLen uint32
	if len(in) > 0 {
		inPtr = uintptr(unsafe.Pointer(&in[0]))
		inLen = uint32(len(in))
	}
	if len(out) > 0 {
		outPtr = uintptr(unsafe.Pointer(&out[0]))
		outLen = uint32(len(out))
	}
	r, _, e := callProc(procDeviceIoControl, uintptr(h), uintptr(code), inPtr, uintptr(inLen), outPtr, uintptr(outLen), uintptr(unsafe.Pointer(&ret)), 0)
	if r == 0 {
		return 0, fmt.Errorf("DeviceIoControl: %w", lastErr(e))
	}
	return ret, nil
}
