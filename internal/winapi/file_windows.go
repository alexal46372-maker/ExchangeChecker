//go:build windows

package winapi

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
)

// MoveFileReplace atomically replaces to with from using MoveFileExW
// (MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH). Both paths must be
// on the same volume.
func MoveFileReplace(from, to string) error {
	f, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return &os.LinkError{Op: "MoveFileEx", Old: from, New: to, Err: err}
	}
	t, err := syscall.UTF16PtrFromString(to)
	if err != nil {
		return &os.LinkError{Op: "MoveFileEx", Old: from, New: to, Err: err}
	}
	r, _, e := callProc(procMoveFileExW,
		uintptr(unsafe.Pointer(f)),
		uintptr(unsafe.Pointer(t)),
		uintptr(moveFileReplaceExisting|moveFileWriteThrough),
	)
	if r == 0 {
		return &os.LinkError{Op: "MoveFileEx", Old: from, New: to, Err: e}
	}
	return nil
}

const (
	MoveFileDelayUntilReboot = 0x4
	MoveFileCopyAllowed      = 0x2
)

// MoveFileEx is a thin MoveFileExW wrapper (flags as documented).
// to == "" schedules deletion when flags include MoveFileDelayUntilReboot.
func MoveFileEx(from, to string, flags uint32) error {
	f, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return &os.LinkError{Op: "MoveFileEx", Old: from, New: to, Err: err}
	}
	var t *uint16
	if to != "" {
		t, err = syscall.UTF16PtrFromString(to)
		if err != nil {
			return &os.LinkError{Op: "MoveFileEx", Old: from, New: to, Err: err}
		}
	}
	r, _, e := callProc(procMoveFileExW, uintptr(unsafe.Pointer(f)), uintptr(unsafe.Pointer(t)), uintptr(flags))
	if r == 0 {
		return &os.LinkError{Op: "MoveFileEx", Old: from, New: to, Err: e}
	}
	return nil
}
