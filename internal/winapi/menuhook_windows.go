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
