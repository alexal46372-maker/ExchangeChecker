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
	r, _, e := procMoveFileExW.Call(
		uintptr(unsafe.Pointer(f)),
		uintptr(unsafe.Pointer(t)),
		uintptr(moveFileReplaceExisting|moveFileWriteThrough),
	)
	if r == 0 {
		return &os.LinkError{Op: "MoveFileEx", Old: from, New: to, Err: e}
	}
	return nil
}
