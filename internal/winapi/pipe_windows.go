//go:build windows

package winapi

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Named pipe / overlapped I/O wrappers (kernel32, CHK-MON-06).
var (
	procCreateNamedPipeW       = modkernel32.NewProc("CreateNamedPipeW")
	procConnectNamedPipe       = modkernel32.NewProc("ConnectNamedPipe")
	procDisconnectNamedPipe    = modkernel32.NewProc("DisconnectNamedPipe")
	procWaitNamedPipeW         = modkernel32.NewProc("WaitNamedPipeW")
	procCreateEventW           = modkernel32.NewProc("CreateEventW")
	procSetEvent               = modkernel32.NewProc("SetEvent")
	procResetEvent             = modkernel32.NewProc("ResetEvent")
	procGetOverlappedResult    = modkernel32.NewProc("GetOverlappedResult")
	procWaitForMultipleObjects = modkernel32.NewProc("WaitForMultipleObjects")
)

// Pipe constants (winbase.h).
const (
	PipeAccessDuplex        = 0x00000003
	FileFlagFirstPipeInst   = 0x00080000
	FileFlagOverlapped      = 0x40000000
	PipeTypeByte            = 0x00000000
	PipeReadmodeByte        = 0x00000000
	PipeWait                = 0x00000000
	PipeRejectRemoteClients = 0x00000008
	PipeUnlimitedInstances  = 255
	SecuritySQOSPresent     = 0x00100000
	SecurityIdentification  = 0x00010000
	ErrorPipeBusy           = syscall.Errno(231)
	ErrorPipeConnected      = syscall.Errno(535)
	ErrorPipeNotConnected   = syscall.Errno(233)
	ErrorNoData             = syscall.Errno(232)
	ErrorIOPending          = syscall.Errno(997)
	ErrorOperationAborted   = syscall.Errno(995)
	ErrorSemTimeout         = syscall.Errno(121)
	ErrorFileNotFound       = syscall.Errno(2)
	ErrorAccessDenied       = syscall.Errno(5)
	waitObject0             = 0
	waitFailed              = 0xFFFFFFFF
	infinite                = 0xFFFFFFFF
	sddlRevision1           = 1
	invalidHandle           = ^uintptr(0)
)

// CreateNamedPipe wraps CreateNamedPipeW.
func CreateNamedPipe(name string, openMode, pipeMode, maxInst, outBuf, inBuf, timeoutMs uint32, sa *syscall.SecurityAttributes) (syscall.Handle, error) {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return syscall.InvalidHandle, err
	}
	// failure is INVALID_HANDLE_VALUE, not 0: resolve first
	if err := procCreateNamedPipeW.Find(); err != nil {
		return syscall.InvalidHandle, err
	}
	h, _, e := callProc(procCreateNamedPipeW, uintptr(unsafe.Pointer(n)), uintptr(openMode), uintptr(pipeMode),
		uintptr(maxInst), uintptr(outBuf), uintptr(inBuf), uintptr(timeoutMs), uintptr(unsafe.Pointer(sa)))
	if h == invalidHandle {
		return syscall.InvalidHandle, e
	}
	return syscall.Handle(h), nil
}

// ConnectNamedPipe wraps ConnectNamedPipe (overlapped).
func ConnectNamedPipe(h syscall.Handle, ov *syscall.Overlapped) error {
	r, _, e := callProc(procConnectNamedPipe, uintptr(h), uintptr(unsafe.Pointer(ov)))
	if r == 0 {
		return e
	}
	return nil
}

// DisconnectNamedPipe wraps DisconnectNamedPipe.
func DisconnectNamedPipe(h syscall.Handle) error {
	r, _, e := callProc(procDisconnectNamedPipe, uintptr(h))
	if r == 0 {
		return e
	}
	return nil
}

// WaitNamedPipe wraps WaitNamedPipeW.
func WaitNamedPipe(name string, timeoutMs uint32) error {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	r, _, e := callProc(procWaitNamedPipeW, uintptr(unsafe.Pointer(n)), uintptr(timeoutMs))
	if r == 0 {
		return e
	}
	return nil
}

// CreateEvent creates an unnamed event.
func CreateEvent(manualReset, initialState bool) (syscall.Handle, error) {
	var m, s uintptr
	if manualReset {
		m = 1
	}
	if initialState {
		s = 1
	}
	h, _, e := callProc(procCreateEventW, 0, m, s, 0)
	if h == 0 {
		return 0, e
	}
	return syscall.Handle(h), nil
}

// SetEvent wraps SetEvent.
func SetEvent(h syscall.Handle) error {
	r, _, e := callProc(procSetEvent, uintptr(h))
	if r == 0 {
		return e
	}
	return nil
}

// ResetEvent wraps ResetEvent.
func ResetEvent(h syscall.Handle) error {
	r, _, e := callProc(procResetEvent, uintptr(h))
	if r == 0 {
		return e
	}
	return nil
}

// GetOverlappedResult wraps GetOverlappedResult.
func GetOverlappedResult(h syscall.Handle, ov *syscall.Overlapped, wait bool) (uint32, error) {
	var n uint32
	var w uintptr
	if wait {
		w = 1
	}
	r, _, e := callProc(procGetOverlappedResult, uintptr(h), uintptr(unsafe.Pointer(ov)), uintptr(unsafe.Pointer(&n)), w)
	if r == 0 {
		return n, e
	}
	return n, nil
}

// WaitAny waits (INFINITE) until one of the handles is signaled and
// returns its index.
func WaitAny(hs ...syscall.Handle) (int, error) {
	// failure is WAIT_FAILED, not 0: resolve first
	if err := procWaitForMultipleObjects.Find(); err != nil {
		return -1, err
	}
	r, _, e := callProc(procWaitForMultipleObjects, uintptr(len(hs)), uintptr(unsafe.Pointer(&hs[0])), 0, infinite)
	if r == waitFailed {
		return -1, e
	}
	i := int(r - waitObject0)
	if i < 0 || i >= len(hs) {
		return -1, fmt.Errorf("winapi: WaitForMultipleObjects returned %#x", r)
	}
	return i, nil
}

// SecurityAttributesFromSDDL converts an SDDL string into security
// attributes (ConvertStringSecurityDescriptorToSecurityDescriptorW). The
// returned free function releases the descriptor (LocalFree).
func SecurityAttributesFromSDDL(sddl string) (*syscall.SecurityAttributes, func(), error) {
	s, err := syscall.UTF16PtrFromString(sddl)
	if err != nil {
		return nil, nil, err
	}
	var sd uintptr
	r, _, e := callProc(procConvertStringSDToSDW, uintptr(unsafe.Pointer(s)), sddlRevision1, uintptr(unsafe.Pointer(&sd)), 0)
	if r == 0 {
		return nil, nil, fmt.Errorf("winapi: ConvertStringSecurityDescriptorToSecurityDescriptorW: %w", e)
	}
	sa := &syscall.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	return sa, func() { callProc(procLocalFree, sd) }, nil
}
