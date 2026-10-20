//go:build windows

package winapi

import "unsafe"

// Phase 11 (CHK-TRAY-45 / 46): font metrics of the layout framework and
// the window capture of the CI screenshots. GetTextMetricsW, BitBlt,
// CreateCompatibleDC, CreateCompatibleBitmap, GetDIBits, DeleteDC exist
// since Windows 2000 (NT 5.0); PrintWindow since Windows XP (NT 5.1).

var (
	procGetTextMetricsW        = modgdi32.NewProc("GetTextMetricsW")
	procCreateCompatibleDC     = modgdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = modgdi32.NewProc("CreateCompatibleBitmap")
	procGetDIBits              = modgdi32.NewProc("GetDIBits")
	procDeleteDC               = modgdi32.NewProc("DeleteDC")
	procBitBlt                 = modgdi32.NewProc("BitBlt")
	procPrintWindow            = moduser32.NewProc("PrintWindow")
	procGetWindowDC            = moduser32.NewProc("GetWindowDC")
)

// TEXTMETRICW.
type TEXTMETRICW struct {
	Height, Ascent, Descent, InternalLeading, ExternalLeading int32
	AveCharWidth, MaxCharWidth, Weight, Overhang              int32
	DigitizedAspectX, DigitizedAspectY                        int32
	FirstChar, LastChar, DefaultChar, BreakChar               uint16
	Italic, Underlined, StruckOut, PitchAndFamily, CharSet    byte
}

// FontMetrics returns the ascent and the line height (tmHeight +
// tmExternalLeading) of font in pixels; 0, 0 on failure.
func FontMetrics(font uintptr) (ascent, lineH int32) {
	dc, _, _ := callProc(procGetDC, 0)
	if dc == 0 {
		return 0, 0
	}
	defer callProc(procReleaseDC, 0, dc)
	old := SelectObject(dc, font)
	defer SelectObject(dc, old)
	var tm TEXTMETRICW
	if r, _, _ := callProc(procGetTextMetricsW, dc, uintptr(unsafe.Pointer(&tm))); r == 0 {
		return 0, 0
	}
	return tm.Ascent, tm.Height + tm.ExternalLeading
}

const (
	SRCCOPY              = 0x00CC0020
	PW_RENDERFULLCONTENT = 0x00000002 // Windows 8.1+, ignored before
	DIB_RGB_COLORS       = 0
	BI_RGB               = 0
)

type bitmapInfoHeader struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
}

// CaptureWindow renders the whole window (also off-screen) with
// PrintWindow into a 32-bit top-down BGRA buffer; the screen copy
// (BitBlt of the window DC) is used when PrintWindow fails.
func CaptureWindow(hwnd uintptr) (w, h int32, bgra []byte, ok bool) {
	r := GetWindowRect(hwnd)
	w, h = r.Right-r.Left, r.Bottom-r.Top
	if w <= 0 || h <= 0 {
		return 0, 0, nil, false
	}
	sdc, _, _ := callProc(procGetDC, 0)
	if sdc == 0 {
		return 0, 0, nil, false
	}
	defer callProc(procReleaseDC, 0, sdc)
	mdc, _, _ := callProc(procCreateCompatibleDC, sdc)
	if mdc == 0 {
		return 0, 0, nil, false
	}
	defer callProc(procDeleteDC, mdc)
	bmp, _, _ := callProc(procCreateCompatibleBitmap, sdc, uintptr(w), uintptr(h))
	if bmp == 0 {
		return 0, 0, nil, false
	}
	defer DeleteObject(bmp)
	old := SelectObject(mdc, bmp)
	pr, _, _ := callProc(procPrintWindow, hwnd, mdc, PW_RENDERFULLCONTENT)
	if pr == 0 {
		wdc, _, _ := callProc(procGetWindowDC, hwnd)
		if wdc != 0 {
			callProc(procBitBlt, mdc, 0, 0, uintptr(w), uintptr(h), wdc, 0, 0, SRCCOPY)
			callProc(procReleaseDC, hwnd, wdc)
		}
	}
	SelectObject(mdc, old)
	bi := bitmapInfoHeader{Width: w, Height: -h, Planes: 1, BitCount: 32, Compression: BI_RGB}
	bi.Size = uint32(unsafe.Sizeof(bi))
	buf := make([]byte, int(w)*int(h)*4)
	n, _, _ := callProc(procGetDIBits, mdc, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), DIB_RGB_COLORS)
	return w, h, buf, n != 0
}

var procSetWindowLongW = moduser32.NewProc("SetWindowLongW")

const (
	GWL_EXSTYLE      = -20
	SWP_NOZORDER     = 0x0004
	SWP_FRAMECHANGED = 0x0020
	SW_SHOWNA        = 8
)

// WindowExStyle is GWL_EXSTYLE of hwnd.
func WindowExStyle(hwnd uintptr) uint32 {
	r, _, _ := callProc(procGetWindowLong, hwnd, uintptr(^uintptr(-GWL_EXSTYLE-1)))
	return uint32(r)
}

// SetWindowLong sets a 32-bit window value (GWL_STYLE / GWL_EXSTYLE).
func SetWindowLong(hwnd uintptr, index int32, v uint32) {
	callProc(procSetWindowLongW, hwnd, uintptr(int(index)), uintptr(v))
}
