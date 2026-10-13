//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// About window control IDs.
const (
	aboutClass  = "ExchangeCheckerTrayAbout"
	idAboutText = 100
)

// aboutTips are the tooltips of the last About window (tests).
var aboutTips *tooltips

// newAboutWindow creates the About window (hidden unless show). OK, Esc
// via the system menu or the close box destroy it; onClose runs after.
func newAboutWindow(owner uintptr, title, text, okText, okTip string, show bool, onClose func()) (uintptr, error) {
	const w, h = 380, 190
	x := (winapi.GetSystemMetrics(winapi.SM_CXSCREEN) - w) / 2
	y := (winapi.GetSystemMetrics(winapi.SM_CYSCREEN) - h) / 2
	var hwnd uintptr
	handler := func(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
		switch msg {
		case winapi.WM_COMMAND:
			if wp&0xFFFF == winapi.IDOK {
				winapi.DestroyWindow(hw)
				return 0, true
			}
		case winapi.WM_CLOSE:
			winapi.DestroyWindow(hw)
			return 0, true
		case winapi.WM_DESTROY:
			if onClose != nil {
				onClose()
			}
		}
		return 0, false
	}
	hwnd, err := createWindow(aboutClass, title, winapi.WS_EX_DLGMODALFRAME|winapi.WS_EX_TOPMOST,
		winapi.WS_OVERLAPPED|winapi.WS_CAPTION|winapi.WS_SYSMENU, x, y, w, h, owner, handler)
	if err != nil {
		return 0, err
	}
	child(hwnd, "STATIC", text, winapi.SS_LEFT, idAboutText, 16, 14, w-40, h-90)
	ok := child(hwnd, "BUTTON", okText, winapi.BS_DEFPUSHBUTTON|winapi.WS_TABSTOP, winapi.IDOK, w-110, h-75, 80, 26)
	aboutTips = newTooltips(hwnd) // CHK-TRAY-15
	aboutTips.set(ok, okTip)
	if show {
		winapi.ShowWindow(hwnd, winapi.SW_SHOW)
		winapi.SetForegroundWindow(hwnd)
	}
	return hwnd, nil
}
