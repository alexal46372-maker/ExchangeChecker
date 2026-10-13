//go:build windows

package winapi

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procCreateToolhelp32Snapshot   = modkernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW            = modkernel32.NewProc("Process32FirstW")
	procProcess32NextW             = modkernel32.NewProc("Process32NextW")
	procTerminateProcess           = modkernel32.NewProc("TerminateProcess")
	procQueryFullProcessImageNameW = modkernel32.NewProc("QueryFullProcessImageNameW")
	procSendMessageTimeoutW        = moduser32.NewProc("SendMessageTimeoutW")
	procProcessIdToSessionId       = modkernel32.NewProc("ProcessIdToSessionId")
	procGetCurrentProcessId        = modkernel32.NewProc("GetCurrentProcessId")
)

const (
	Th32csSnapProcess       = 0x00000002
	ProcessTerminate        = 0x0001
	ProcessQueryLimitedInfo = 0x1000

	HwndBroadcast   = 0xffff
	SmtoAbortIfHung = 0x0002
	SmtoNormal      = 0x0000
)

// ProcessEntry32W is PROCESSENTRY32W (exe name only; path via QueryFullProcessImageNameW).
type ProcessEntry32W struct {
	Size            uint32
	CntUsage        uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	CntThreads      uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

// Process has PID and image path (when available).
type Process struct {
	PID  uint32
	Exe  string // base name
	Path string // full path when QueryFullProcessImageNameW succeeds
}

// ListProcesses snapshots running processes (CreateToolhelp32Snapshot).
func ListProcesses() ([]Process, error) {
	h, _, e := callProc(procCreateToolhelp32Snapshot, Th32csSnapProcess, 0)
	if h == uintptr(syscall.InvalidHandle) || h == 0 {
		return nil, fmt.Errorf("CreateToolhelp32Snapshot: %w", lastErr(e))
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	var pe ProcessEntry32W
	pe.Size = uint32(unsafe.Sizeof(pe))
	r, _, e := callProc(procProcess32FirstW, h, uintptr(unsafe.Pointer(&pe)))
	if r == 0 {
		return nil, fmt.Errorf("Process32FirstW: %w", lastErr(e))
	}
	var out []Process
	for {
		name := syscall.UTF16ToString(pe.ExeFile[:])
		p := Process{PID: pe.ProcessID, Exe: name}
		if path, err := ProcessImagePath(pe.ProcessID); err == nil {
			p.Path = path
		}
		out = append(out, p)
		pe.Size = uint32(unsafe.Sizeof(pe))
		r, _, _ = callProc(procProcess32NextW, h, uintptr(unsafe.Pointer(&pe)))
		if r == 0 {
			break
		}
	}
	return out, nil
}

// ProcessImagePath returns the full image path of pid (QueryFullProcessImageNameW, NT 6.0+).
func ProcessImagePath(pid uint32) (string, error) {
	h, err := OpenProcess(ProcessQueryLimitedInfo|ProcessSynchronize, pid)
	if err != nil {
		return "", err
	}
	defer syscall.CloseHandle(h)
	return imagePathOf(h)
}

func imagePathOf(h syscall.Handle) (string, error) {
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	r, _, e := callProc(procQueryFullProcessImageNameW, uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return "", lastErr(e)
	}
	return syscall.UTF16ToString(buf[:n]), nil
}

// ErrImageMismatch: the pid now belongs to another image (PID reuse).
var ErrImageMismatch = errors.New("winapi: process image does not match (pid reused?)")

// TerminatePID opens pid ONCE with
// PROCESS_QUERY_LIMITED_INFORMATION|PROCESS_TERMINATE|SYNCHRONIZE (NT 6.0+),
// checks the image via QueryFullProcessImageNameW on that handle (match
// must accept it, otherwise ErrImageMismatch and nothing is killed), then
// TerminateProcess and WaitForSingleObject(waitMs) on the same handle, so
// a reused pid can never be hit.
func TerminatePID(pid uint32, match func(image string) bool, waitMs uint32) error {
	h, err := OpenProcess(ProcessQueryLimitedInfo|ProcessTerminate|ProcessSynchronize, pid)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	img, err := imagePathOf(h)
	if err != nil {
		return err
	}
	if match == nil || !match(img) {
		return fmt.Errorf("%w: pid %d is %s", ErrImageMismatch, pid, img)
	}
	r, _, e := callProc(procTerminateProcess, uintptr(h), 1)
	if r == 0 {
		return lastErr(e)
	}
	if waitMs > 0 {
		ev, err := WaitForSingleObject(h, waitMs)
		if err != nil {
			return err
		}
		if ev == WaitTimeout {
			return fmt.Errorf("winapi: pid %d did not exit within %d ms", pid, waitMs)
		}
	}
	return nil
}

// SameImage reports whether image is exactly want (case-insensitive,
// 8.3 names expanded).
func SameImage(image, want string) bool {
	return strings.EqualFold(filepath.Clean(longIfShort(image)), filepath.Clean(longIfShort(want)))
}

// longIfShort calls GetLongPathNameW only for paths with an 8.3 component.
func longIfShort(p string) string {
	if strings.Contains(p, "~") {
		return LongPath(p)
	}
	return p
}

// SendMessageTimeout wraps SendMessageTimeoutW.
func SendMessageTimeout(hwnd uintptr, msg uint32, wp, lp uintptr, flags, timeoutMs uint32) (uintptr, error) {
	var result uintptr
	r, _, e := callProc(procSendMessageTimeoutW, hwnd, uintptr(msg), wp, lp, uintptr(flags), uintptr(timeoutMs), uintptr(unsafe.Pointer(&result)))
	if r == 0 {
		return 0, lastErr(e)
	}
	return result, nil
}

// BroadcastMessage sends msg to all top-level windows (HWND_BROADCAST) with a timeout.
func BroadcastMessage(msg uint32, timeoutMs uint32) {
	_, _ = SendMessageTimeout(HwndBroadcast, msg, 0, 0, SmtoAbortIfHung|SmtoNormal, timeoutMs)
}

// ProcessesInDir returns processes whose image path is under installDir
// and whose base name is one of exeNames (case-insensitive).
func ProcessesInDir(installDir string, exeNames ...string) ([]Process, error) {
	all, err := ListProcesses()
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, n := range exeNames {
		want[strings.ToLower(n)] = true
	}
	// 8.3 vs long names: QueryFullProcessImageName returns the long form
	root := strings.ToLower(filepath.Clean(longIfShort(installDir)))
	var out []Process
	for _, p := range all {
		if !want[strings.ToLower(p.Exe)] {
			continue
		}
		if p.Path == "" {
			continue
		}
		path := strings.ToLower(filepath.Clean(longIfShort(p.Path)))
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) || strings.HasPrefix(path, root+`\`) {
			out = append(out, p)
		}
	}
	return out, nil
}

// ProcessSessionID returns the RD session of pid (ProcessIdToSessionId).
func ProcessSessionID(pid uint32) (uint32, error) {
	var sid uint32
	r, _, e := callProc(procProcessIdToSessionId, uintptr(pid), uintptr(unsafe.Pointer(&sid)))
	if r == 0 {
		return 0, lastErr(e)
	}
	return sid, nil
}

// CurrentProcessID wraps GetCurrentProcessId.
func CurrentProcessID() uint32 {
	r, _, _ := callProc(procGetCurrentProcessId)
	return uint32(r)
}

// CurrentSessionID is ProcessSessionID of the calling process.
func CurrentSessionID() (uint32, error) {
	return ProcessSessionID(CurrentProcessID())
}
