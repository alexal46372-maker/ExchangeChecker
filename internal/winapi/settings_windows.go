//go:build windows

package winapi

import (
	"runtime"
	"syscall"
	"unsafe"
)

// WinAPI for the tray settings window (phase 6c, CHK-TRAY-04/05/06):
// elevation through ShellExecuteExW "runas" (no os/exec), focus, the
// confirmations. All exports exist since Windows 2000 (NT 5.0), except
// GetProcessId (NT 5.1); called through callProc (LazyProc.Find first),
// CHK-TRAY-10.

var (
	procShellExecuteExW    = modshell32.NewProc("ShellExecuteExW")
	procGetProcessId       = modkernel32.NewProc("GetProcessId")
	procAllowSetFgWindow   = moduser32.NewProc("AllowSetForegroundWindow")
	procGetModuleFileNameW = modkernel32.NewProc("GetModuleFileNameW")
	procSetFocus           = moduser32.NewProc("SetFocus")
	procGetFocus           = moduser32.NewProc("GetFocus")
	procMessageBoxW        = moduser32.NewProc("MessageBoxW")
	procIsDialogMessageW   = moduser32.NewProc("IsDialogMessageW")
)

// Settings window constants (controls of user32 / comctl32 v6).
const (
	WM_SETTEXT = 0x000C

	ES_MULTILINE   = 0x0004
	ES_PASSWORD    = 0x0020
	ES_NUMBER      = 0x2000
	ES_AUTOVSCROLL = 0x0040
	ES_AUTOHSCROLL = 0x0080
	ES_READONLY    = 0x0800
	EM_SETREADONLY = 0x00CF
	EN_CHANGE      = 0x0300

	BS_AUTOCHECKBOX = 0x00000003
	BM_GETCHECK     = 0x00F0
	BM_SETCHECK     = 0x00F1
	BST_UNCHECKED   = 0
	BST_CHECKED     = 1

	CBS_DROPDOWNLIST = 0x0003
	CB_ADDSTRING     = 0x0143
	CB_GETCURSEL     = 0x0147
	CB_RESETCONTENT  = 0x014B
	CB_SETCURSEL     = 0x014E
	CBN_SELCHANGE    = 1

	LBS_NOTIFY      = 0x0001
	LB_ADDSTRING    = 0x0180
	LB_RESETCONTENT = 0x0184
	LB_SETCURSEL    = 0x0186
	LB_GETCURSEL    = 0x0188
	LB_GETCOUNT     = 0x018B
	LBN_SELCHANGE   = 1

	WS_BORDER        = 0x00800000
	WS_VSCROLL       = 0x00200000
	WS_MINIMIZEBOX   = 0x00020000
	WS_EX_CLIENTEDGE = 0x00000200

	MB_OKCANCEL     = 0x00000001
	MB_YESNO        = 0x00000004
	MB_ICONWARNING  = 0x00000030
	MB_DEFBUTTON2   = 0x00000100
	MB_SETFOREGRND  = 0x00010000
	IDYES           = 6
	ERROR_CANCELLED = 1223
)

// SHELLEXECUTEINFOW is the Win32 SHELLEXECUTEINFOW (112 bytes on x64).
type SHELLEXECUTEINFOW struct {
	Size       uint32
	Mask       uint32
	Hwnd       uintptr
	Verb       *uint16
	File       *uint16
	Parameters *uint16
	Directory  *uint16
	Show       int32
	InstApp    uintptr
	IDList     uintptr
	Class      *uint16
	KeyClass   uintptr
	HotKey     uint32
	Icon       uintptr // union hIcon / hMonitor
	Process    uintptr
}

// SEE_MASK_NOCLOSEPROCESS: ShellExecuteExW returns the process handle.
const SEE_MASK_NOCLOSEPROCESS = 0x00000040

// ShellExecuteEx runs file with params through ShellExecuteExW (verb
// "runas" asks for elevation via UAC) and returns the process handle
// (may be 0; the caller closes it). A refused UAC prompt is FALSE with
// GetLastError() == ERROR_CANCELLED.
func ShellExecuteEx(hwnd uintptr, verb, file, params string) (process syscall.Handle, err error) {
	sei := SHELLEXECUTEINFOW{Mask: SEE_MASK_NOCLOSEPROCESS, Hwnd: hwnd, Verb: UTF16Ptr(verb), File: UTF16Ptr(file),
		Parameters: UTF16Ptr(params), Show: SW_SHOWNORMAL}
	sei.Size = uint32(unsafe.Sizeof(sei))
	r, _, e := callProc(procShellExecuteExW, uintptr(unsafe.Pointer(&sei)))
	runtime.KeepAlive(&sei)
	if r == 0 {
		return 0, lastErr(e)
	}
	return syscall.Handle(sei.Process), nil
}

// GetProcessId returns the id of a process handle (0 on failure).
func GetProcessId(h syscall.Handle) uint32 {
	r, _, _ := callProc(procGetProcessId, uintptr(h))
	return uint32(r)
}

// AllowSetForegroundWindow lets process pid take the foreground (the
// caller must be the foreground process); pid 0 does nothing.
func AllowSetForegroundWindow(pid uint32) {
	if pid != 0 {
		callProc(procAllowSetFgWindow, uintptr(pid))
	}
}

// ModuleFileName is the path of the running exe (GetModuleFileNameW).
func ModuleFileName() (string, error) {
	buf := make([]uint16, 260)
	for len(buf) <= 32768 {
		n, _, e := callProc(procGetModuleFileNameW, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 {
			return "", lastErr(e)
		}
		if int(n) < len(buf) {
			return syscall.UTF16ToString(buf[:n]), nil
		}
		buf = make([]uint16, 2*len(buf))
	}
	return "", syscall.ERROR_INSUFFICIENT_BUFFER
}

// SendMessageString sends msg with a string LPARAM (CB_ADDSTRING,
// LB_ADDSTRING, WM_SETTEXT); the UTF-16 buffer stays alive for the call.
func SendMessageString(hwnd uintptr, msg uint32, wp uintptr, s string) uintptr {
	p := UTF16Ptr(s)
	r, _, _ := callProc(procSendMessageW, hwnd, uintptr(msg), wp, uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
	return r
}

// SetFocus sets the keyboard focus (thread of the window).
func SetFocus(hwnd uintptr) { callProc(procSetFocus, hwnd) }

// GetFocus returns the window with the keyboard focus of this thread.
func GetFocus() uintptr {
	r, _, _ := callProc(procGetFocus)
	return r
}

// IsDialogMessage handles dialog keyboard navigation (Tab, Enter, Esc)
// for a top-level window with child controls.
func IsDialogMessage(hwnd uintptr, m *MSG) bool {
	r, _, _ := callProc(procIsDialogMessageW, hwnd, uintptr(unsafe.Pointer(m)))
	return r != 0
}

// MessageBox shows a modal message box and returns the button id.
func MessageBox(hwnd uintptr, text, caption string, flags uint32) int {
	r, _, _ := callProc(procMessageBoxW, hwnd, uintptr(unsafe.Pointer(UTF16Ptr(text))),
		uintptr(unsafe.Pointer(UTF16Ptr(caption))), uintptr(flags))
	return int(r)
}
