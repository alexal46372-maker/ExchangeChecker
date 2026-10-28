//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Settings chrome geometry (CHK-TRAY-65): mirrors installer CHK-SETUP-14
// palette / spacing. Tab strip and footer are frame children; only the
// middle host scrolls (CHK-TRAY-66).
const (
	settingsTabChromeH = 36 // 96-dpi tab strip + pad (fixed; not scrolled)
	settingsFooterLine = 1
)

// pageActionIDs are Databases-tab actions that live in the footer bar
// (not in pageCtl leftovers — that hid Test after tab switches, Alex bug 3).
var pageActionIDs = []int32{idSetTest, idSetCancelTest, idSetAlarmTest, idSetPoll}

// settingsChromeErase paints content + footer bar (installer look).
// Not COLOR_BTNFACE. Dark theme keeps trayui dark palette (CHK-TRAY-55).
// Persistent brushes via ensureChromeBrushes (no Create+Delete per erase).
func (w *settingsWindow) settingsChromeErase(hdc uintptr) uintptr {
	if w == nil || w.hwnd == 0 {
		return themeErase(hdc, 0)
	}
	rc := winapi.GetClientRect(w.hwnd)
	panel, page, line, brand := ensureChromeBrushes()
	winapi.FillRect(hdc, rc, page)
	ft := w.footerTop
	if ft <= 0 || ft >= rc.Bottom {
		ft = rc.Bottom - px(setBarH)
	}
	if ft > settingsFooterLine {
		lr := winapi.RECT{Left: 0, Top: ft - settingsFooterLine, Right: rc.Right, Bottom: ft}
		winapi.FillRect(hdc, lr, line)
	}
	br := winapi.RECT{Left: 0, Top: ft, Right: rc.Right, Bottom: rc.Bottom}
	winapi.FillRect(hdc, br, panel)
	// thin brand accent under the title area / above tabs
	if th := w.tabChromeH; th > 0 {
		ar := winapi.RECT{Left: 0, Top: 0, Right: rc.Right, Bottom: 3}
		winapi.FillRect(hdc, ar, brand)
	}
	return 1
}

// contentErase fills a host / page panel with the content background
// using the persistent page brush (CHK-TRAY-68).
func contentErase(hdc, hwnd uintptr) uintptr {
	_, page := ensureThemeBrushes()
	winapi.FillRect(hdc, winapi.GetClientRect(hwnd), page)
	return 1
}

// contentPaint fills hwnd with the page brush on WM_PAINT (installer
// wizard_chrome pattern). Required on Win7 when uncover delivers
// WM_PAINT with fErase=FALSE — WM_ERASEBKGND alone never runs, and
// TRANSPARENT STATICs leave vertical smear trails (CHK-TRAY-71).
func contentPaint(hwnd uintptr) {
	if hwnd == 0 || !winapi.IsWindow(hwnd) {
		return
	}
	var ps winapi.PAINTSTRUCT
	dc := winapi.BeginPaint(hwnd, &ps)
	if dc == 0 {
		return
	}
	_, page := ensureThemeBrushes()
	winapi.FillRect(dc, winapi.GetClientRect(hwnd), page)
	winapi.EndPaint(hwnd, &ps)
}

