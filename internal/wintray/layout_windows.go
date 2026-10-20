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
	cw, ch = px(cw), px(ch)
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

// Phase 10 D3 (CHK-TRAY-42): every window is laid out on a 96-dpi grid
// and scaled to the system DPI (the manifest declares dpiAware), the font
// is Segoe UI 9 pt for that DPI.

// testDPI overrides the screen DPI (the overlap test at 96/120/144).
var testDPI int32

var (
	screenDPI int32
	uiFonts   = map[int32]uintptr{}
)

// uiDPI is the DPI windows are scaled to.
func uiDPI() int32 {
	if testDPI > 0 {
		return testDPI
	}
	if screenDPI == 0 {
		screenDPI = winapi.ScreenDPI()
	}
	return screenDPI
}

// px scales a 96-dpi grid value to the current DPI.
func px(v int32) int32 {
	d := uiDPI()
	if d == 96 {
		return v
	}
	n := int64(v) * int64(d)
	if n >= 0 {
		return int32((n + 48) / 96)
	}
	return int32((n - 48) / 96)
}

// pxRect scales a grid rectangle by its corners, so touching controls
// stay touching at every DPI (no rounding overlap).
func pxRect(x, y, w, h int32) (int32, int32, int32, int32) {
	l, t := px(x), px(y)
	return l, t, px(x+w) - l, px(y+h) - t
}

// uiFontFace / uiFontPt: the font of every tray window.
const (
	uiFontFace = "Segoe UI"
	uiFontPt   = 9
)

// uiFont is Segoe UI 9 for the current DPI (cached per DPI, kept for the
// process lifetime); the stock GUI font if it cannot be created.
func uiFont() uintptr {
	d := uiDPI()
	if f, ok := uiFonts[d]; ok {
		return f
	}
	f := winapi.CreateFont(uiFontFace, -int32((int64(uiFontPt)*int64(d)+36)/72))
	if f == 0 {
		return winapi.GetStockObject(winapi.DEFAULT_GUI_FONT)
	}
	uiFonts[d] = f
	return f
}

// Layout grid of the dialogs (96-dpi units).
const (
	gridMargin   = 12 // window edge
	gridGap      = 6  // between controls / groups
	gridRow      = 25 // one label + field row
	gridEditH    = 23 // edit / combo height
	gridLabelH   = 18
	gridCheckH   = 20
	gridButtonH  = 28
	gridGroupTop = 20 // group box caption to the first row
	gridGroupPad = 8  // group box inner left / bottom
)

// groupBox is a BS_GROUPBOX frame on the grid (caption text).
func groupBox(parent uintptr, text string, x, y, w, h int32) uintptr {
	return child(parent, "BUTTON", text, winapi.BS_GROUPBOX, -1, x, y, w, h)
}

// unpx converts pixels back to 96-dpi units, rounding up (a box of
// unpx(p) units is at least p pixels).
func unpx(p int32) int32 {
	d := int64(uiDPI())
	return int32((int64(p)*96 + d - 1) / d)
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
