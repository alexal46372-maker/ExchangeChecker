//go:build windows

package winapi

import (
	"syscall"
	"unsafe"
)

// WinAPI of the phase 10 D3 layout (CHK-TRAY-42): the system DPI of the
// screen (the tray is system-DPI-aware by its manifest), the Segoe UI 9
// font for that DPI, and the window walk + text measuring of the overlap
// test. All exist since Windows 2000 (NT 5.0); called through callProc.

var (
	procGetDC         = moduser32.NewProc("GetDC")
	procReleaseDC     = moduser32.NewProc("ReleaseDC")
	procGetDeviceCaps = modgdi32.NewProc("GetDeviceCaps")
	procCreateFontW   = modgdi32.NewProc("CreateFontW")
	procDrawTextW     = moduser32.NewProc("DrawTextW")
	procGetWindowRect = moduser32.NewProc("GetWindowRect")
	procGetWindow     = moduser32.NewProc("GetWindow")
	procGetWindowLong = moduser32.NewProc("GetWindowLongW")
)

const (
	LOGPIXELSY      = 90
	GW_HWNDNEXT     = 2
	GW_CHILD        = 5
	GWL_STYLE       = -16
	DT_WORDBREAK    = 0x0010
	DT_SINGLELINE   = 0x0020
	DT_CALCRECT     = 0x0400
	DT_NOPREFIX     = 0x0800
	BS_GROUPBOX     = 0x0007
	BS_TYPEMASK     = 0x000F
	BS_MULTILINE    = 0x2000
	SS_TYPEMASK     = 0x001F
	FW_NORMAL       = 400
	DEFAULT_CHARSET = 1
)

// ScreenDPI is LOGPIXELSY of the screen (96 when unknown).
func ScreenDPI() int32 {
	dc, _, _ := callProc(procGetDC, 0)
	if dc == 0 {
		return 96
	}
	defer callProc(procReleaseDC, 0, dc)
	r, _, _ := callProc(procGetDeviceCaps, dc, LOGPIXELSY)
	if int32(r) <= 0 {
		return 96
	}
	return int32(r)
}

// CreateFont creates a normal-weight font of the face, height in pixels
// (negative = character height); 0 on failure.
func CreateFont(face string, height int32) uintptr {
	p, err := syscall.UTF16PtrFromString(face)
	if err != nil {
		return 0
	}
	r, _, _ := callProc(procCreateFontW, uintptr(height), 0, 0, 0, FW_NORMAL, 0, 0, 0, DEFAULT_CHARSET, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(p)))
	return r
}

// TextSize measures text in font: wrapped at width when width > 0,
// otherwise on one line.
func TextSize(font uintptr, text string, width int32) (w, h int32) {
	dc, _, _ := callProc(procGetDC, 0)
	if dc == 0 {
		return 0, 0
	}
	defer callProc(procReleaseDC, 0, dc)
	old := SelectObject(dc, font)
	defer SelectObject(dc, old)
	u, err := syscall.UTF16FromString(text)
	if err != nil {
		return 0, 0
	}
	r := RECT{Right: width}
	f := uintptr(DT_CALCRECT | DT_NOPREFIX)
	if width > 0 {
		f |= DT_WORDBREAK
	} else {
		f |= DT_SINGLELINE
	}
	callProc(procDrawTextW, dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&r)), f)
	return r.Right - r.Left, r.Bottom - r.Top
}

// GetWindowRect is the screen rectangle of hwnd.
func GetWindowRect(hwnd uintptr) (r RECT) {
	callProc(procGetWindowRect, hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

// Children lists the direct child windows of hwnd.
func Children(hwnd uintptr) []uintptr {
	var out []uintptr
	c, _, _ := callProc(procGetWindow, hwnd, GW_CHILD)
	for c != 0 && len(out) < 4096 {
		out = append(out, c)
		c, _, _ = callProc(procGetWindow, c, GW_HWNDNEXT)
	}
	return out
}

// WindowStyle is GWL_STYLE of hwnd.
func WindowStyle(hwnd uintptr) uint32 {
	r, _, _ := callProc(procGetWindowLong, hwnd, uintptr(^uintptr(15))) // GWL_STYLE = -16
	return uint32(r)
}

// Phase 10 E (CHK-TRAY-42): scrolled settings pages and the
// controls-inside-the-window check. Windows 2000+ (NT 5.0).
var (
	procSetScrollInfo  = moduser32.NewProc("SetScrollInfo")
	procClientToScreen = moduser32.NewProc("ClientToScreen")
)

const (
	WS_CLIPCHILDREN     = 0x02000000
	WS_EX_CONTROLPARENT = 0x00010000
	SB_HORZ             = 0
	SB_VERT             = 1
	SIF_RANGE           = 0x1
	SIF_PAGE            = 0x2
	SIF_POS             = 0x4
	SB_LINEUP           = 0
	SB_LINEDOWN         = 1
	SB_PAGEUP           = 2
	SB_PAGEDOWN         = 3
	SB_THUMBTRACK       = 5
	SB_THUMBPOSITION    = 4
	SB_TOP              = 6
	SB_BOTTOM           = 7
	WM_VSCROLL          = 0x0115
	WM_MOUSEWHEEL       = 0x020A
	SM_CXVSCROLL        = 2
	SM_CYHSCROLL        = 3
)

type scrollInfo struct {
	Size, Mask    uint32
	Min, Max      int32
	Page          uint32
	Pos, TrackPos int32
}

// SetScroll sets the range 0..max-1, the page and the position of a
// scroll bar (a page >= max hides it).
func SetScroll(hwnd uintptr, bar int32, max, page, pos int32) {
	si := scrollInfo{Mask: SIF_RANGE | SIF_PAGE | SIF_POS, Max: max - 1, Page: uint32(page), Pos: pos}
	si.Size = uint32(unsafe.Sizeof(si))
	callProc(procSetScrollInfo, hwnd, uintptr(bar), uintptr(unsafe.Pointer(&si)), 1)
}

// ClientOrigin is the screen position of the client area of hwnd.
func ClientOrigin(hwnd uintptr) (x, y int32) {
	var p [2]int32
	callProc(procClientToScreen, hwnd, uintptr(unsafe.Pointer(&p[0])))
	return p[0], p[1]
}

// Focus notifications (phase 10 E: the settings page scrolls to the
// focused control).
const (
	BS_NOTIFY    = 0x00004000
	EN_SETFOCUS  = 0x0100
	CBN_SETFOCUS = 3
	LBN_SETFOCUS = 4
	BN_DBLCLK    = 5
	BN_SETFOCUS  = 6
)
