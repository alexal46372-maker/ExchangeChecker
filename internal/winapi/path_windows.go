//go:build windows

package winapi

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	procGetDriveTypeW        = modkernel32.NewProc("GetDriveTypeW")
	procGetDiskFreeSpaceExW  = modkernel32.NewProc("GetDiskFreeSpaceExW")
	procGetWindowsDirectoryW = modkernel32.NewProc("GetWindowsDirectoryW")
	procWaitForSingleObject  = modkernel32.NewProc("WaitForSingleObject")
	procOpenProcess          = modkernel32.NewProc("OpenProcess")
	procGetLongPathNameW     = modkernel32.NewProc("GetLongPathNameW")
)

const (
	DriveUnknown   = 0
	DriveNoRoot    = 1
	DriveRemovable = 2
	DriveFixed     = 3
	DriveRemote    = 4
	DriveCDROM     = 5
	DriveRAMDisk   = 6

	ProcessSynchronize = 0x00100000
	Infinite           = 0xFFFFFFFF
	WaitObject0        = 0
	WaitTimeout        = 0x00000102
)

// DriveType wraps GetDriveTypeW for a path (root or any path on the volume).
func DriveType(path string) (uint32, error) {
	// GetDriveTypeW wants a root like "C:\"; accept any absolute path.
	root := path
	if len(path) >= 2 && path[1] == ':' {
		root = string([]byte{path[0], ':', '\\'})
	}
	r, _, e := callProc(procGetDriveTypeW, uintptr(unsafe.Pointer(UTF16Ptr(root))))
	if r == DriveUnknown {
		return 0, lastErr(e)
	}
	return uint32(r), nil
}

// DiskFreeBytes wraps GetDiskFreeSpaceExW (free bytes available to the caller).
func DiskFreeBytes(path string) (uint64, error) {
	var freeToCaller, total, totalFree uint64
	r, _, e := callProc(procGetDiskFreeSpaceExW,
		uintptr(unsafe.Pointer(UTF16Ptr(path))),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)))
	if r == 0 {
		return 0, lastErr(e)
	}
	return freeToCaller, nil
}

// WindowsDirectory is %SystemRoot% (GetWindowsDirectoryW).
func WindowsDirectory() (string, error) {
	buf := make([]uint16, 260)
	n, _, e := callProc(procGetWindowsDirectoryW, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return "", lastErr(e)
	}
	return syscall.UTF16ToString(buf[:n]), nil
}

// OpenProcess wraps OpenProcess.
func OpenProcess(access, pid uint32) (syscall.Handle, error) {
	h, _, e := callProc(procOpenProcess, uintptr(access), 0, uintptr(pid))
	if h == 0 {
		return 0, lastErr(e)
	}
	return syscall.Handle(h), nil
}

// WaitForSingleObject wraps WaitForSingleObject; timeoutMs = Infinite waits forever.
func WaitForSingleObject(h syscall.Handle, timeoutMs uint32) (uint32, error) {
	r, _, e := callProc(procWaitForSingleObject, uintptr(h), uintptr(timeoutMs))
	if r == 0xFFFFFFFF {
		return 0, lastErr(e)
	}
	return uint32(r), nil
}

// WaitProcessExit waits until process pid exits or timeout elapses.
func WaitProcessExit(pid uint32, timeoutMs uint32) error {
	if pid == 0 {
		return nil
	}
	h, err := OpenProcess(ProcessSynchronize, pid)
	if err != nil {
		// process already gone
		if errno, ok := err.(syscall.Errno); ok && errno == 87 /*ERROR_INVALID_PARAMETER*/ {
			return nil
		}
		return err
	}
	defer syscall.CloseHandle(h)
	r, err := WaitForSingleObject(h, timeoutMs)
	if err != nil {
		return err
	}
	if r == WaitTimeout {
		return fmt.Errorf("wait process %d: timeout", pid)
	}
	return nil
}

// LongPath expands 8.3 components (e.g. C:\Users\RUNNER~1) with
// GetLongPathNameW; on failure the input is returned unchanged.
// A path that does not exist yet (e.g. a new /DIR) is expanded through its
// nearest existing parent and the missing tail is appended unchanged.
func LongPath(p string) string {
	if l, ok := longPathExisting(p); ok {
		return l
	}
	clean := filepath.Clean(p)
	tail := ""
	for dir := clean; ; {
		parent := filepath.Dir(dir)
		tail = filepath.Join(filepath.Base(dir), tail)
		if parent == dir {
			return p
		}
		if l, ok := longPathExisting(parent); ok {
			return filepath.Join(l, tail)
		}
		dir = parent
	}
}

func longPathExisting(p string) (string, bool) {
	in, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, 512)
	for {
		r, _, _ := callProc(procGetLongPathNameW, uintptr(unsafe.Pointer(in)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if r == 0 {
			return "", false
		}
		if int(r) < len(buf) {
			return syscall.UTF16ToString(buf[:r]), true
		}
		buf = make([]uint16, r+1)
	}
}
