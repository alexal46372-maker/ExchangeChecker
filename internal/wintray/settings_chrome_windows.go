//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
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
func (w *settingsWindow) settingsChromeErase(hdc uintptr) uintptr {
	if w == nil || w.hwnd == 0 {
		return themeErase(hdc, 0)
	}
	rc := winapi.GetClientRect(w.hwnd)
	page := winapi.CreateSolidBrush(trayui.ChromePage())
	panel := winapi.CreateSolidBrush(trayui.ChromePanel())
	line := winapi.CreateSolidBrush(trayui.ChromeCardFrame())
	defer func() {
		winapi.DeleteObject(page)
		winapi.DeleteObject(panel)
		winapi.DeleteObject(line)
	}()
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
		acc := winapi.CreateSolidBrush(trayui.ChromeBrandDark())
		ar := winapi.RECT{Left: 0, Top: 0, Right: rc.Right, Bottom: 3}
		winapi.FillRect(hdc, ar, acc)
		winapi.DeleteObject(acc)
	}
	return 1
}

// contentErase fills a host / page panel with the content background.
func contentErase(hdc, hwnd uintptr) uintptr {
	br := winapi.CreateSolidBrush(trayui.ChromePage())
	winapi.FillRect(hdc, winapi.GetClientRect(hwnd), br)
	winapi.DeleteObject(br)
	return 1
}
