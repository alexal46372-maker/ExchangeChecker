//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
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
	const w, h = 380, 190 // replaced by the size of the content
	x, y := int32(0), int32(0)
	var hwnd uintptr
	handler := func(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
		switch msg {
		case winapi.WM_COMMAND:
			if wp&0xFFFF == winapi.IDOK {
				destroyWindow(hw)
				return 0, true
			}
		case winapi.WM_CLOSE:
			destroyWindow(hw)
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
	// phase 11 (CHK-TRAY-45): laid out by the ui framework
	label := child(hwnd, "STATIC", text, winapi.SS_LEFT, idAboutText, 0, 0, 0, 0)
	ok := child(hwnd, "BUTTON", okText, winapi.BS_DEFPUSHBUTTON|winapi.WS_TABSTOP, winapi.IDOK, 0, 0, 0, 0)
	fitForm(hwnd, &ui.Pad{Left: ui.P16, Top: ui.P16, Right: ui.P16, Child: uiCtl(label, ui.Label, text)},
		&ui.Pad{All: ui.P16, Child: &ui.Row{Right: true, Items: []ui.Node{uiCtl(ok, ui.Button, okText)}}}, 340, false)
	aboutTips = newTooltips(hwnd) // CHK-TRAY-15
	aboutTips.set(ok, okTip)
	if show {
		winapi.ShowWindow(hwnd, winapi.SW_SHOW)
		winapi.SetForegroundWindow(hwnd)
	}
	return hwnd, nil
}
