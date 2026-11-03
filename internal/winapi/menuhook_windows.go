//go:build windows

package winapi

import "unsafe"

// Menu message filter hook (phase 13, CHK-TRAY-91): the tray left menu
// toggles polling with a click on a database item that also opens the
// points submenu. TrackPopupMenuEx sends no WM_COMMAND for such an item,
// so a thread WH_MSGFILTER hook sees the click in the menu modal loop
// (MSGF_MENU), MenuItemFromPoint finds the item and EndMenu closes the
// menu. All exports exist on Windows 7.

var (
	procSetWindowsHookExW   = moduser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = moduser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = moduser32.NewProc("CallNextHookEx")
	procMenuItemFromPoint   = moduser32.NewProc("MenuItemFromPoint")
	procEndMenu             = moduser32.NewProc("EndMenu")
	procGetCurrentThreadId  = modkernel32.NewProc("GetCurrentThreadId")
)

const (
	WH_MSGFILTER = -1
	MSGF_MENU    = 2

	WM_LBUTTONDOWN = 0x0201
)

// SetMsgFilterHook installs a WH_MSGFILTER hook for the calling thread;
// cb is a syscall.NewCallback of func(code int32, wParam, lParam uintptr) uintptr.
func SetMsgFilterHook(cb uintptr) (uintptr, error) {
	tid, _, _ := callProc(procGetCurrentThreadId)
	h, _, err := callProc(procSetWindowsHookExW, int32ToUintptr(WH_MSGFILTER), cb, 0, tid)
	if h == 0 {
		return 0, err
	}
	return h, nil
}

func int32ToUintptr(v int32) uintptr { return uintptr(v) }

// UnhookWindowsHookEx removes a hook.
func UnhookWindowsHookEx(h uintptr) { callProc(procUnhookWindowsHookEx, h) }

// CallNextHookEx passes the hook message on.
func CallNextHookEx(code int32, wParam, lParam uintptr) uintptr {
	r, _, _ := callProc(procCallNextHookEx, 0, uintptr(code), wParam, lParam)
	return r
}

// HookMSG reads the MSG of a WH_MSGFILTER lParam.
func HookMSG(lParam uintptr) MSG {
	var m MSG
	if lParam != 0 {
		callProc(procRtlMoveMemory, uintptr(unsafe.Pointer(&m)), lParam, unsafe.Sizeof(m))
	}
	return m
}

// MenuItemFromPoint returns the index of the item of menu at the screen
// point pt (-1 = none).
func MenuItemFromPoint(hwnd, menu uintptr, pt POINT) int {
	var r uintptr
	if unsafe.Sizeof(uintptr(0)) == 8 {
		// POINT by value: one 8-byte register on amd64
		r, _, _ = callProc(procMenuItemFromPoint, hwnd, menu, uintptr(uint64(uint32(pt.X))|uint64(uint32(pt.Y))<<32))
	} else {
		r, _, _ = callProc(procMenuItemFromPoint, hwnd, menu, uintptr(pt.X), uintptr(pt.Y))
	}
	return int(int32(r))
}

// EndMenu closes the active menu of the thread.
func EndMenu() { callProc(procEndMenu) }

// Full-screen flash overlay (phase 13 item 7, CHK-TRAY-95): a layered,
// click-through, never-activated window over the virtual screen (all
// monitors). Windows 2000+.
var procSetLayeredWindowAttributes = moduser32.NewProc("SetLayeredWindowAttributes")

const (
	WS_EX_LAYERED     = 0x00080000
	WS_EX_TRANSPARENT = 0x00000020
	LWA_ALPHA         = 0x00000002
	WM_MOUSEACTIVATE  = 0x0021
	MA_NOACTIVATE     = 3

	SM_XVIRTUALSCREEN  = 76
	SM_YVIRTUALSCREEN  = 77
	SM_CXVIRTUALSCREEN = 78
	SM_CYVIRTUALSCREEN = 79
)

// SetLayeredWindowAttributes sets the constant alpha of a layered window.
func SetLayeredWindowAttributes(hwnd uintptr, alpha uint8) error {
	r, _, err := callProc(procSetLayeredWindowAttributes, hwnd, 0, uintptr(alpha), LWA_ALPHA)
	if r == 0 {
		return err
	}
	return nil
}

// VirtualScreen is the rectangle of all monitors.
func VirtualScreen() RECT {
	x, y := GetSystemMetrics(SM_XVIRTUALSCREEN), GetSystemMetrics(SM_YVIRTUALSCREEN)
	return RECT{Left: x, Top: y, Right: x + GetSystemMetrics(SM_CXVIRTUALSCREEN), Bottom: y + GetSystemMetrics(SM_CYVIRTUALSCREEN)}
}
