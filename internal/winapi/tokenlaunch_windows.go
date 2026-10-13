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

// Tray launch without elevation after an interactive install / upgrade
// (PLAN §12 п. 90.8, docs/PHASE7.md §D.8): the elevated setup takes the
// token of explorer.exe in its own session and starts
// ExchangeCheckerTray.exe with CreateProcessWithTokenW. This starts the
// product, not a helper process (POLICY §4) — the ONLY CreateProcess* in
// cmd/ internal/ tools/, exempted by name in CHK-POL-01 (this file only).
// Never used in silent mode.

var (
	procDuplicateTokenEx        = modadvapi32.NewProc("DuplicateTokenEx")
	procCreateProcessWithTokenW = modadvapi32.NewProc("CreateProcessWithTokenW")
)

const (
	tokenAssignPrimary  = 0x0001
	tokenDuplicate      = 0x0002
	tokenQueryAccess    = 0x0008
	maximumAllowed      = 0x02000000
	securityImpersonate = 2
	tokenPrimary        = 1
)

// ErrNoShell: no explorer.exe in the caller's session (e.g. a service
// session or a CI runner) — the tray appears at the next logon instead.
var ErrNoShell = errors.New("winapi: no explorer.exe in this session")

// LaunchAsShellUser starts exe (working directory dir) with the token of
// the explorer.exe of the caller's session (non-elevated user token).
func LaunchAsShellUser(exe, dir string) error {
	sess, err := CurrentSessionID()
	if err != nil {
		return err
	}
	win, err := WindowsDirectory()
	if err != nil || win == "" {
		// never launch without verifying the shell image
		return fmt.Errorf("winapi: Windows directory: %v", err)
	}
	wantShell := filepath.Join(win, "explorer.exe")
	procs, err := ListProcesses()
	if err != nil {
		return err
	}
	for _, p := range procs {
		if !strings.EqualFold(p.Exe, "explorer.exe") {
			continue
		}
		if s, err := ProcessSessionID(p.PID); err != nil || s != sess {
			continue
		}
		err := launchWithTokenOf(p.PID, wantShell, exe, dir)
		if errors.Is(err, ErrImageMismatch) {
			continue
		}
		return err
	}
	return ErrNoShell
}

func launchWithTokenOf(pid uint32, wantShell, exe, dir string) error {
	h, err := OpenProcess(ProcessQueryLimitedInfo, pid)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	img, err := imagePathOf(h)
	if err != nil {
		return err
	}
	if !SameImage(img, wantShell) {
		return fmt.Errorf("%w: pid %d is %s", ErrImageMismatch, pid, img)
	}
	var tok syscall.Token
	r, _, e := callProc(procOpenProcessToken, uintptr(h), tokenDuplicate|tokenQueryAccess|tokenAssignPrimary, uintptr(unsafe.Pointer(&tok)))
	if r == 0 {
		return fmt.Errorf("OpenProcessToken(explorer): %w", lastErr(e))
	}
	defer tok.Close()
	var prim syscall.Token
	r, _, e = callProc(procDuplicateTokenEx, uintptr(tok), maximumAllowed, 0, securityImpersonate, tokenPrimary, uintptr(unsafe.Pointer(&prim)))
	if r == 0 {
		return fmt.Errorf("DuplicateTokenEx: %w", lastErr(e))
	}
	defer prim.Close()
	cmd := `"` + exe + `"`
	cmdW, err := syscall.UTF16FromString(cmd) // writable buffer
	if err != nil {
		return err
	}
	var si syscall.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi syscall.ProcessInformation
	r, _, e = callProc(procCreateProcessWithTokenW, uintptr(prim), 0,
		uintptr(unsafe.Pointer(UTF16Ptr(exe))), uintptr(unsafe.Pointer(&cmdW[0])), 0, 0,
		uintptr(unsafe.Pointer(UTF16Ptr(dir))), uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	if r == 0 {
		return fmt.Errorf("CreateProcessWithTokenW: %w", lastErr(e))
	}
	syscall.CloseHandle(pi.Thread)
	syscall.CloseHandle(pi.Process)
	return nil
}
