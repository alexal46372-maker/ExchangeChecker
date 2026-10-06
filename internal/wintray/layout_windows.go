//go:build windows

package wintray

import "github.com/alexal46372-maker/ExchangeChecker/internal/winapi"

// fitRect places a window of the outer size w×h in the work area: centered
// horizontally, at a third from the top, never larger than the work area
// and never beyond its edges (Windows 7 at 1024×768 with a taskbar).
func fitRect(w, h int32, work winapi.RECT) (x, y, cw, ch int32) {
	aw, ah := work.Right-work.Left, work.Bottom-work.Top
	if w > aw {
		w = aw
	}
	if h > ah {
		h = ah
	}
	x = work.Left + (aw-w)/2
	y = work.Top + (ah-h)/3
	return x, y, w, h
}

// windowRect is the outer rectangle of a window with a client area of
// cw×ch (AdjustWindowRectEx for the caption and borders), placed by
// fitRect in the work area (SPI_GETWORKAREA; the screen if unknown).
func windowRect(cw, ch int32, style, exStyle uint32) (x, y, w, h int32) {
	r := winapi.RECT{Right: cw, Bottom: ch}
	if !winapi.AdjustWindowRectEx(&r, style, exStyle) {
		r = winapi.RECT{Right: cw + 8, Bottom: ch + 32}
	}
	work, ok := winapi.WorkArea()
	if !ok || work.Right <= work.Left || work.Bottom <= work.Top {
		work = winapi.RECT{Right: winapi.GetSystemMetrics(winapi.SM_CXSCREEN), Bottom: winapi.GetSystemMetrics(winapi.SM_CYSCREEN)}
	}
	return fitRect(r.Right-r.Left, r.Bottom-r.Top, work)
}

// frontWindow restores and activates a top-level window (a second
// «Настройки» / settings process: the open dialog, else the window).
func frontWindow(hwnd uintptr) {
	winapi.ShowWindow(hwnd, winapi.SW_SHOWNORMAL)
	winapi.SetForegroundWindow(hwnd)
}
