//go:build windows

package winapi

import (
	"unsafe"
)

// WinAPI for the tray alert window and sound (phase 6b, CHK-TRAY-03). All
// exports exist since Windows 2000 (NT 5.0), called through callProc
// (LazyProc.Find first), CHK-TRAY-10.

var (
	procBeep             = modkernel32.NewProc("Beep")
	procSetWindowTextW   = moduser32.NewProc("SetWindowTextW")
	procEnableWindow     = moduser32.NewProc("EnableWindow")
	procIsWindowEnabled  = moduser32.NewProc("IsWindowEnabled")
	procIsWindowVisible  = moduser32.NewProc("IsWindowVisible")
	procLoadIconW        = moduser32.NewProc("LoadIconW")
	procSetWindowPos     = moduser32.NewProc("SetWindowPos")
	procFlashWindowEx    = moduser32.NewProc("FlashWindowEx")
	procGetForegroundWnd = moduser32.NewProc("GetForegroundWindow")
)

// Alert window constants.
const (
	IDCANCEL      = 2
	BM_CLICK      = 0x00F5
	STM_SETICON   = 0x0170
	SS_ICON       = 0x00000003
	SW_SHOWNORMAL = 1
	IDI_ERROR     = 32513
	IDI_WARNING   = 32515

	SWP_NOSIZE     = 0x0001
	SWP_NOMOVE     = 0x0002
	SWP_NOACTIVATE = 0x0010
	SWP_SHOWWINDOW = 0x0040

	FLASHW_ALL       = 0x00000003
	FLASHW_TIMERNOFG = 0x0000000C
)

// HWND_TOPMOST for SetWindowPos.
var HWND_TOPMOST = ^uintptr(0) // (HWND)-1

// FLASHWINFO is the Win32 FLASHWINFO.
type FLASHWINFO struct {
	Size    uint32
	Hwnd    uintptr
	Flags   uint32
	Count   uint32
	Timeout uint32
}

// Beep plays a tone on the PC speaker (kernel32 Beep, synchronous).
func Beep(freqHz, durMs uint32) error {
	r, _, e := callProc(procBeep, uintptr(freqHz), uintptr(durMs))
	if r == 0 {
		return lastErr(e)
	}
	return nil
}

// SetWindowText calls SetWindowTextW.
func SetWindowText(hwnd uintptr, text string) {
	callProc(procSetWindowTextW, hwnd, uintptr(unsafe.Pointer(UTF16Ptr(text))))
}

// EnableWindow enables or disables a window / control.
func EnableWindow(hwnd uintptr, enable bool) {
	b := uintptr(0)
	if enable {
		b = 1
	}
	callProc(procEnableWindow, hwnd, b)
}

// IsWindowEnabled calls IsWindowEnabled.
func IsWindowEnabled(hwnd uintptr) bool {
	r, _, _ := callProc(procIsWindowEnabled, hwnd)
	return r != 0
}

// IsWindowVisible calls IsWindowVisible.
func IsWindowVisible(hwnd uintptr) bool {
	r, _, _ := callProc(procIsWindowVisible, hwnd)
	return r != 0
}

// LoadSystemIcon loads a predefined icon (IDI_*; shared, never destroyed).
func LoadSystemIcon(id uintptr) uintptr {
	r, _, _ := callProc(procLoadIconW, 0, id)
	return r
}

// SetWindowPos calls SetWindowPos.
func SetWindowPos(hwnd, after uintptr, x, y, cx, cy int32, flags uint32) {
	callProc(procSetWindowPos, hwnd, after, uintptr(x), uintptr(y), uintptr(cx), uintptr(cy), uintptr(flags))
}

// FlashWindow flashes the caption and taskbar button until the window
// comes to the foreground (used when SetForegroundWindow is refused).
func FlashWindow(hwnd uintptr) {
	fi := FLASHWINFO{Hwnd: hwnd, Flags: FLASHW_ALL | FLASHW_TIMERNOFG}
	fi.Size = uint32(unsafe.Sizeof(fi))
	callProc(procFlashWindowEx, uintptr(unsafe.Pointer(&fi)))
}

// GetForegroundWindow calls GetForegroundWindow.
func GetForegroundWindow() uintptr {
	r, _, _ := callProc(procGetForegroundWnd)
	return r
}
