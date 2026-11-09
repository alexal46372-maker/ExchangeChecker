//go:build windows

package winapi

import "unsafe"

// CAPTUREBLT: BitBlt of the screen includes layered windows.
const CAPTUREBLT = 0x40000000

// CaptureScreenRect copies screen rectangle r with SRCCOPY|CAPTUREBLT, so
// layered windows (the screen flash overlay, CHK-TRAY-95) are in the
// copy; BGRA, top-down rows. A separate helper: CaptureScreen (the
// CHK-TRAY-71..73 proofs) stays as it is.
func CaptureScreenRect(r RECT) (w, h int32, bgra []byte, ok bool) {
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
	callProc(procBitBlt, mdc, 0, 0, uintptr(w), uintptr(h), sdc, uintptr(r.Left), uintptr(r.Top), SRCCOPY|CAPTUREBLT)
	SelectObject(mdc, old)
	bi := bitmapInfoHeader{Width: w, Height: -h, Planes: 1, BitCount: 32, Compression: BI_RGB}
	bi.Size = uint32(unsafe.Sizeof(bi))
	buf := make([]byte, int(w)*int(h)*4)
	n, _, _ := callProc(procGetDIBits, mdc, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), DIB_RGB_COLORS)
	return w, h, buf, n != 0
}
