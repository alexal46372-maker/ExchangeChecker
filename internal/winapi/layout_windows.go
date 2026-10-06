//go:build windows

package winapi

import "unsafe"

// WinAPI for the window layout of the tray dialogs (phase 6d fixes,
// CHK-TRAY-07): the work area of the primary monitor (no taskbar) and the
// outer size of a window for a client area. Both exist since Windows 2000
// (NT 5.0); called through callProc (LazyProc.Find first), CHK-TRAY-10.

var (
	procSystemParametersInfoW = moduser32.NewProc("SystemParametersInfoW")
	procAdjustWindowRectEx    = moduser32.NewProc("AdjustWindowRectEx")
)

// SPI_GETWORKAREA for SystemParametersInfoW.
const SPI_GETWORKAREA = 0x0030

// RECT is the Win32 RECT.
type RECT struct {
	Left, Top, Right, Bottom int32
}

// WorkArea is the work area of the primary monitor (the screen without
// the taskbar); ok = false when the call failed.
func WorkArea() (r RECT, ok bool) {
	ret, _, _ := callProc(procSystemParametersInfoW, SPI_GETWORKAREA, 0, uintptr(unsafe.Pointer(&r)), 0)
	return r, ret != 0
}

// AdjustWindowRectEx grows r (a client rectangle) to the window rectangle
// of a window with style / exStyle and no menu.
func AdjustWindowRectEx(r *RECT, style, exStyle uint32) bool {
	ret, _, _ := callProc(procAdjustWindowRectEx, uintptr(unsafe.Pointer(r)), uintptr(style), 0, uintptr(exStyle))
	return ret != 0
}
