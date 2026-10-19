//go:build windows

package winapi

import (
	"unsafe"
)

// Phase 9b history chart (CHK-TRAY-26): plain GDI drawing (NT 5.0).

var (
	procBeginPaint       = moduser32.NewProc("BeginPaint")
	procEndPaint         = moduser32.NewProc("EndPaint")
	procGetClientRect    = moduser32.NewProc("GetClientRect")
	procInvalidateRect   = moduser32.NewProc("InvalidateRect")
	procFillRect         = moduser32.NewProc("FillRect")
	procCreatePen        = modgdi32.NewProc("CreatePen")
	procCreateSolidBrush = modgdi32.NewProc("CreateSolidBrush")
	procSelectObject     = modgdi32.NewProc("SelectObject")
	procMoveToEx         = modgdi32.NewProc("MoveToEx")
	procLineTo           = modgdi32.NewProc("LineTo")
	procPolyline         = modgdi32.NewProc("Polyline")
	procPolygon          = modgdi32.NewProc("Polygon")
	procEllipse          = modgdi32.NewProc("Ellipse")
	procTextOutW         = modgdi32.NewProc("TextOutW")
	procSetBkMode        = modgdi32.NewProc("SetBkMode")
	procSetTextColor     = modgdi32.NewProc("SetTextColor")
)

const (
	WM_PAINT         = 0x000F
	WM_ERASEBKGND    = 0x0014
	PS_SOLID         = 0
	PS_DOT           = 2
	TRANSPARENT      = 1
	BS_AUTORADIO     = 0x0009
	LVM_GETNEXTITEM  = 0x100C
	LVNI_SELECTED    = 0x0002
	LVM_SETITEMSTATE = 0x102B
	LVIS_SELECTED    = 0x0002
	LVIS_FOCUSED     = 0x0001
	LVN_ITEMCHANGED  = 0xFFFFFF9B // (UINT)-101
	NM_DBLCLK        = 0xFFFFFFFD // (UINT)-3
)

// PAINTSTRUCT of BeginPaint.
type PAINTSTRUCT struct {
	DC        uintptr
	Erase     int32
	Paint     RECT
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

// BeginPaint / EndPaint wrap WM_PAINT.
func BeginPaint(hwnd uintptr, ps *PAINTSTRUCT) uintptr {
	r, _, _ := callProc(procBeginPaint, hwnd, uintptr(unsafe.Pointer(ps)))
	return r
}
func EndPaint(hwnd uintptr, ps *PAINTSTRUCT) {
	callProc(procEndPaint, hwnd, uintptr(unsafe.Pointer(ps)))
}

// GetClientRect returns the client rectangle.
func GetClientRect(hwnd uintptr) RECT {
	var r RECT
	callProc(procGetClientRect, hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

// InvalidateRect repaints the whole client area.
func InvalidateRect(hwnd uintptr, erase bool) {
	e := uintptr(0)
	if erase {
		e = 1
	}
	callProc(procInvalidateRect, hwnd, 0, e)
}

// FillRect fills r with brush.
func FillRect(dc uintptr, r RECT, brush uintptr) {
	callProc(procFillRect, dc, uintptr(unsafe.Pointer(&r)), brush)
}

// CreatePen / CreateSolidBrush (free with DeleteObject).
func CreatePen(style, width int32, color uint32) uintptr {
	r, _, _ := callProc(procCreatePen, uintptr(style), uintptr(width), uintptr(color))
	return r
}
func CreateSolidBrush(color uint32) uintptr {
	r, _, _ := callProc(procCreateSolidBrush, uintptr(color))
	return r
}

// SelectObject returns the previous object.
func SelectObject(dc, obj uintptr) uintptr {
	r, _, _ := callProc(procSelectObject, dc, obj)
	return r
}

// Line draws from (x1, y1) to (x2, y2) with the current pen.
func Line(dc uintptr, x1, y1, x2, y2 int32) {
	callProc(procMoveToEx, dc, uintptr(x1), uintptr(y1), 0)
	callProc(procLineTo, dc, uintptr(x2), uintptr(y2))
}

// POINTS is a GDI point list element.
type GDIPoint struct{ X, Y int32 }

// Polyline / Polygon draw a point list.
func Polyline(dc uintptr, pts []GDIPoint) {
	if len(pts) == 0 {
		return
	}
	callProc(procPolyline, dc, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
}
func Polygon(dc uintptr, pts []GDIPoint) {
	if len(pts) == 0 {
		return
	}
	callProc(procPolygon, dc, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
}

// Ellipse in the bounding box.
func Ellipse(dc uintptr, l, t, r, b int32) {
	callProc(procEllipse, dc, uintptr(l), uintptr(t), uintptr(r), uintptr(b))
}

// TextOut draws s at (x, y).
func TextOut(dc uintptr, x, y int32, s string) {
	u := utf16NoNul(s)
	if len(u) == 0 {
		return
	}
	callProc(procTextOutW, dc, uintptr(x), uintptr(y), uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)))
}

// SetBkMode / SetTextColor of a DC.
func SetBkMode(dc uintptr, mode int32)      { callProc(procSetBkMode, dc, uintptr(mode)) }
func SetTextColor(dc uintptr, color uint32) { callProc(procSetTextColor, dc, uintptr(color)) }

func utf16NoNul(s string) []uint16 {
	u := utf16Multi(s) // s has no NUL: "s\0\0"
	if len(u) >= 2 {
		return u[:len(u)-2]
	}
	return nil
}

// ListViewSelected is the selected row (-1 = none).
func ListViewSelected(lv uintptr) int {
	return int(int32(SendMessage(lv, LVM_GETNEXTITEM, ^uintptr(0), LVNI_SELECTED)))
}

// ListViewSelect selects row i.
func ListViewSelect(lv uintptr, i int) {
	it := LVITEMW{State: LVIS_SELECTED | LVIS_FOCUSED, StateMask: LVIS_SELECTED | LVIS_FOCUSED}
	callSend(lv, LVM_SETITEMSTATE, uintptr(i), uintptr(unsafe.Pointer(&it)))
}
