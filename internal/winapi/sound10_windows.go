//go:build windows

package winapi

import (
	"errors"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// Phase 10 A–C WinAPI (CHK-TRAY-33..39): sound (winmm PlaySoundW /
// mciSendStringW), timers (blinking icon), the open-file dialog (sound
// file), the trackbar (master volume) and the icon rectangle (panel by
// the clock). All NT 5.0 except Shell_NotifyIconGetRect (6.1, Windows 7):
// callProc runs LazyProc.Find first, a missing export is an error.

var (
	modwinmm                   = newSystemDLL("winmm.dll")
	procPlaySoundW             = modwinmm.NewProc("PlaySoundW")
	procMciSendStringW         = modwinmm.NewProc("mciSendStringW")
	procMciGetErrorStringW     = modwinmm.NewProc("mciGetErrorStringW")
	procSetTimer               = moduser32.NewProc("SetTimer")
	procKillTimer              = moduser32.NewProc("KillTimer")
	procGetOpenFileNameW       = modcomdlg32.NewProc("GetOpenFileNameW")
	procShellNotifyIconGetRect = modshell32.NewProc("Shell_NotifyIconGetRect")
	procShowWindowIsIconic     = moduser32.NewProc("IsIconic")
)

// Phase 10 constants.
const (
	SND_ASYNC     = 0x0001
	SND_NODEFAULT = 0x0002
	SND_MEMORY    = 0x0004
	SND_LOOP      = 0x0008
	SND_PURGE     = 0x0040

	WM_ACTIVATE = 0x0006
	WM_TIMER    = 0x0113
	WM_HSCROLL  = 0x0114

	WS_EX_TOOLWINDOW  = 0x00000080
	WS_EX_NOACTIVATE  = 0x08000000
	SW_SHOWNOACTIVATE = 4
	SW_MINIMIZE       = 6
	SW_RESTORE        = 9

	TBS_AUTOTICKS   = 0x0001
	TBM_GETPOS      = WM_USER
	TBM_SETPOS      = WM_USER + 5
	TBM_SETRANGE    = WM_USER + 6
	TBM_SETTICFREQ  = WM_USER + 20
	TBM_SETPAGESIZE = WM_USER + 21

	OFN_FILEMUSTEXIST = 0x00001000

	BS_AUTORADIOBUTTON = 0x00000009
)

var soundMu sync.Mutex

// soundBuf keeps the WAV of an async SND_MEMORY sound alive.
var soundBuf []byte

// PlayWAV plays a RIFF/WAVE image from memory asynchronously (loop =
// SND_LOOP); the previous sound stops.
func PlayWAV(wav []byte, loop bool) error {
	if len(wav) == 0 {
		return errors.New("empty sound")
	}
	soundMu.Lock()
	defer soundMu.Unlock()
	callProc(procPlaySoundW, 0, 0, 0) // stop, before soundBuf changes
	soundBuf = wav
	f := uintptr(SND_MEMORY | SND_ASYNC | SND_NODEFAULT)
	if loop {
		f |= SND_LOOP
	}
	r, _, e := callProc(procPlaySoundW, uintptr(unsafe.Pointer(&soundBuf[0])), 0, f)
	if r == 0 {
		soundBuf = nil
		if e != nil && e != syscall.Errno(0) {
			return e
		}
		return errors.New("PlaySound failed")
	}
	return nil
}

// StopWAV stops a PlayWAV sound.
func StopWAV() {
	soundMu.Lock()
	defer soundMu.Unlock()
	callProc(procPlaySoundW, 0, 0, 0)
	soundBuf = nil
}

// MCI runs one mciSendStringW command; returns its result string or the
// MCI error text.
func MCI(cmd string) (string, error) {
	buf := make([]uint16, 256)
	if err := procMciSendStringW.Find(); err != nil {
		return "", err
	}
	r, _, _ := callProc(procMciSendStringW, uintptr(unsafe.Pointer(UTF16Ptr(cmd))), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	if r != 0 {
		msg := make([]uint16, 256)
		callProc(procMciGetErrorStringW, r, uintptr(unsafe.Pointer(&msg[0])), uintptr(len(msg)))
		t := strings.TrimSpace(syscall.UTF16ToString(msg))
		if t == "" {
			t = "MCI error"
		}
		return "", errors.New(t)
	}
	return syscall.UTF16ToString(buf), nil
}

// SetTimer starts a WM_TIMER timer of hwnd.
func SetTimer(hwnd, id uintptr, ms uint32) bool {
	r, _, _ := callProc(procSetTimer, hwnd, id, uintptr(ms), 0)
	return r != 0
}

// KillTimer stops it.
func KillTimer(hwnd, id uintptr) { callProc(procKillTimer, hwnd, id) }

// IsIconic reports a minimized window.
func IsIconic(hwnd uintptr) bool {
	r, _, _ := callProc(procShowWindowIsIconic, hwnd)
	return r != 0
}

// GetOpenFileName shows the modal open dialog (filter: "name\x00*.ext"
// pairs); ok = false on cancel or error.
func GetOpenFileName(owner uintptr, title, filter, initial string) (string, bool) {
	buf := make([]uint16, 1024)
	if u, err := syscall.UTF16FromString(initial); err == nil && len(u) < len(buf) {
		copy(buf, u)
	}
	f := utf16Multi(filter)
	o := OPENFILENAMEW{Owner: owner, Filter: &f[0], FilterIndex: 1, File: &buf[0], MaxFile: uint32(len(buf)),
		Title: UTF16Ptr(title), Flags: OFN_FILEMUSTEXIST | OFN_HIDEREADONLY | OFN_NOCHANGEDIR | OFN_PATHMUSTEXIST}
	o.StructSize = uint32(unsafe.Sizeof(o))
	r, _, _ := callProc(procGetOpenFileNameW, uintptr(unsafe.Pointer(&o)))
	if r == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

type notifyIconIdentifier struct {
	Size uint32
	Wnd  uintptr
	ID   uint32
	Guid [16]byte
}

// NotifyIconRect is the screen rectangle of the tray icon (Windows 7+);
// ok = false when unknown (hidden in the overflow, older Windows).
func NotifyIconRect(hwnd uintptr, id uint32) (RECT, bool) {
	n := notifyIconIdentifier{Wnd: hwnd, ID: id}
	n.Size = uint32(unsafe.Sizeof(n))
	var r RECT
	if procShellNotifyIconGetRect.Find() != nil {
		return RECT{}, false
	}
	hr, _, _ := callProc(procShellNotifyIconGetRect, uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&r)))
	if hr != 0 { // S_OK only
		return RECT{}, false
	}
	return r, r.Right > r.Left
}
