//go:build windows

package winapi

import "unsafe"

// Volume slider in the tray menu (phase 13 item 11, CHK-TRAY-96): an
// owner-draw menu item (WM_MEASUREITEM / WM_DRAWITEM to the menu owner),
// dragged through the WH_MSGFILTER hook. All exports exist on Windows 7.

var (
	procGetMenuItemRect = moduser32.NewProc("GetMenuItemRect")
	procGetSysColor     = moduser32.NewProc("GetSysColor")
	procSetCursorPos    = moduser32.NewProc("SetCursorPos")
)

const (
	MFT_OWNERDRAW  = 0x00000100
	WM_MEASUREITEM = 0x002C
	WM_MOUSEMOVE   = 0x0200
	ODT_MENU       = 1
	ODS_SELECTED   = 0x0001
	DT_RIGHT       = 0x0002

	WM_KEYDOWN = 0x0100
	VK_ESCAPE  = 0x1B

	COLOR_MENU      = 4
	COLOR_MENUTEXT  = 7
	COLOR_HIGHLIGHT = 13
	COLOR_BTNSHADOW = 16
)

// DRAWITEMSTRUCT of WM_DRAWITEM.
type DRAWITEMSTRUCT struct {
	CtlType, CtlID, ItemID, ItemAction, ItemState uint32
	HwndItem, HDC                                 uintptr
	RcItem                                        RECT
	ItemData                                      uintptr
}

// MEASUREITEMSTRUCT of WM_MEASUREITEM.
type MEASUREITEMSTRUCT struct {
	CtlType, CtlID, ItemID, ItemWidth, ItemHeight uint32
	ItemData                                      uintptr
}

// ReadDrawItem copies the DRAWITEMSTRUCT at lParam.
func ReadDrawItem(lp uintptr) DRAWITEMSTRUCT {
	var d DRAWITEMSTRUCT
	if lp != 0 {
		callProc(procRtlMoveMemory, uintptr(unsafe.Pointer(&d)), lp, unsafe.Sizeof(d))
	}
	return d
}

// ReadMeasureItem copies the MEASUREITEMSTRUCT at lParam.
func ReadMeasureItem(lp uintptr) MEASUREITEMSTRUCT {
	var m MEASUREITEMSTRUCT
	if lp != 0 {
		callProc(procRtlMoveMemory, uintptr(unsafe.Pointer(&m)), lp, unsafe.Sizeof(m))
	}
	return m
}

// WriteMeasureItem stores m back at lParam (the answer of WM_MEASUREITEM).
func WriteMeasureItem(lp uintptr, m MEASUREITEMSTRUCT) {
	if lp != 0 {
		callProc(procRtlMoveMemory, lp, uintptr(unsafe.Pointer(&m)), unsafe.Sizeof(m))
	}
}

// GetMenuItemRect is the screen rectangle of item i of a shown popup menu
// (hwnd 0: the system finds the menu window).
func GetMenuItemRect(hwnd, menu uintptr, i int) (RECT, bool) {
	var r RECT
	ok, _, _ := callProc(procGetMenuItemRect, hwnd, menu, uintptr(i), uintptr(unsafe.Pointer(&r)))
	return r, ok != 0
}

// GetSysColor is a system color (COLORREF).
func GetSysColor(i int) uint32 {
	r, _, _ := callProc(procGetSysColor, uintptr(i))
	return uint32(r)
}

// RenderDC draws into a w×h memory bitmap and returns it as top-down BGRA
// (tests of owner-draw painting).
func RenderDC(w, h int32, draw func(dc uintptr)) ([]byte, bool) {
	sdc, _, _ := callProc(procGetDC, 0)
	if sdc == 0 {
		return nil, false
	}
	defer callProc(procReleaseDC, 0, sdc)
	mdc, _, _ := callProc(procCreateCompatibleDC, sdc)
	if mdc == 0 {
		return nil, false
	}
	defer callProc(procDeleteDC, mdc)
	bmp, _, _ := callProc(procCreateCompatibleBitmap, sdc, uintptr(w), uintptr(h))
	if bmp == 0 {
		return nil, false
	}
	defer DeleteObject(bmp)
	old := SelectObject(mdc, bmp)
	draw(mdc)
	SelectObject(mdc, old)
	bi := bitmapInfoHeader{Width: w, Height: -h, Planes: 1, BitCount: 32, Compression: BI_RGB}
	bi.Size = uint32(unsafe.Sizeof(bi))
	buf := make([]byte, int(w)*int(h)*4)
	n, _, _ := callProc(procGetDIBits, mdc, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), DIB_RGB_COLORS)
	return buf, n != 0
}

// SetCursorPos moves the mouse cursor (tests drive the shown menu).
func SetCursorPos(x, y int32) bool {
	r, _, _ := callProc(procSetCursorPos, uintptr(x), uintptr(y))
	return r != 0
}
