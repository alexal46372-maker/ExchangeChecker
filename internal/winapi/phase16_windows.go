//go:build windows

package winapi

import "unsafe"

// Phase 16 (CHK-TRAY-92/104/105): the speech-bubble panel by the clock
// (a window region: rounded rectangle + tail, a region frame) and the alert
// window without a close command (the Close item removed from the system
// menu). All exports exist since Windows 2000 (NT 5.0).

var (
	procCreateRoundRectRgn = modgdi32.NewProc("CreateRoundRectRgn")
	procCreatePolygonRgn   = modgdi32.NewProc("CreatePolygonRgn")
	procCreateRectRgn      = modgdi32.NewProc("CreateRectRgn")
	procCombineRgn         = modgdi32.NewProc("CombineRgn")
	procFrameRgn           = modgdi32.NewProc("FrameRgn")
	procPtInRegion         = modgdi32.NewProc("PtInRegion")
	procSetWindowRgn       = moduser32.NewProc("SetWindowRgn")
	procGetWindowRgn       = moduser32.NewProc("GetWindowRgn")
	procGetSystemMenu      = moduser32.NewProc("GetSystemMenu")
	procDeleteMenu         = moduser32.NewProc("DeleteMenu")
	procGetMenuItemID      = moduser32.NewProc("GetMenuItemID")
)

// Region and system-menu constants.
const (
	RGN_OR        = 2
	WINDING       = 2
	NULLREGION    = 1
	SIMPLEREGION  = 2
	COMPLEXREGION = 3

	WM_SYSCOMMAND = 0x0112
	SC_CLOSE      = 0xF060
	SC_MINIMIZE   = 0xF020
	MF_BYCOMMAND  = 0x0000
	MF_BYPOSITION = 0x0400
)

// CreateRoundRectRgn: a rounded rectangle region (right / bottom
// exclusive; ellipse w × h at the corners). 0 on failure.
func CreateRoundRectRgn(l, t, r, b, w, h int32) uintptr {
	rg, _, _ := callProc(procCreateRoundRectRgn, uintptr(l), uintptr(t), uintptr(r), uintptr(b), uintptr(w), uintptr(h))
	return rg
}

// CreatePolygonRgn: a polygon region (WINDING fill). 0 on failure.
func CreatePolygonRgn(pts []POINT) uintptr {
	if len(pts) < 3 {
		return 0
	}
	rg, _, _ := callProc(procCreatePolygonRgn, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)), WINDING)
	return rg
}

// CreateRectRgn: a rectangle region (right / bottom exclusive).
func CreateRectRgn(l, t, r, b int32) uintptr {
	rg, _, _ := callProc(procCreateRectRgn, uintptr(l), uintptr(t), uintptr(r), uintptr(b))
	return rg
}

// CombineRgn sets dst = a <mode> b; returns the region type (0 = error).
func CombineRgn(dst, a, b uintptr, mode int) int {
	r, _, _ := callProc(procCombineRgn, dst, a, b, uintptr(mode))
	return int(r)
}

// FrameRgn draws a w × h pixel frame along the inside of rgn with brush.
func FrameRgn(dc, rgn, brush uintptr, w, h int32) bool {
	r, _, _ := callProc(procFrameRgn, dc, rgn, brush, uintptr(w), uintptr(h))
	return r != 0
}

// PtInRegion reports whether the point is in rgn.
func PtInRegion(rgn uintptr, x, y int32) bool {
	r, _, _ := callProc(procPtInRegion, rgn, uintptr(x), uintptr(y))
	return r != 0
}

// SetWindowRgn sets the window region (the system owns rgn afterwards).
func SetWindowRgn(hwnd, rgn uintptr, redraw bool) bool {
	r, _, _ := callProc(procSetWindowRgn, hwnd, rgn, boolArg(redraw))
	return r != 0
}

// GetWindowRgn copies the window region into rgn (an existing region);
// returns the region type, 0 (ERROR) when the window has none.
func GetWindowRgn(hwnd, rgn uintptr) int {
	r, _, _ := callProc(procGetWindowRgn, hwnd, rgn)
	return int(r)
}

// GetSystemMenu returns the window menu (revert = restore the default).
func GetSystemMenu(hwnd uintptr, revert bool) uintptr {
	r, _, _ := callProc(procGetSystemMenu, hwnd, boolArg(revert))
	return r
}

// DeleteMenu deletes an item (by command or by position, flags).
func DeleteMenu(menu uintptr, item uint32, flags uint32) bool {
	r, _, _ := callProc(procDeleteMenu, menu, uintptr(item), uintptr(flags))
	return r != 0
}

// GetMenuItemID is the command id of the item at pos (0 for a
// separator, 0xFFFFFFFF for a submenu or an error).
func GetMenuItemID(menu uintptr, pos int) uint32 {
	r, _, _ := callProc(procGetMenuItemID, menu, uintptr(pos))
	return uint32(r)
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// Phase 17 (CHK-TRAY-109): WM_SIZE wParam values and SC_RESTORE.
const (
	SIZE_RESTORED  = 0
	SIZE_MINIMIZED = 1
	SIZE_MAXIMIZED = 2
	SC_RESTORE     = 0xF120
)
