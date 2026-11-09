//go:build windows

package winapi

import "unsafe"

// Menu font of the owner-draw volume slider (phase 13, CHK-TRAY-96): the
// text of an owner-draw item is drawn with the DC's font, which for a DC
// of the menu window (GetDC) is the system font, not the menu font. The
// menu font is lfMenuFont of SPI_GETNONCLIENTMETRICS (Windows 2000+; the
// Vista+ iPaddedBorderWidth is left out so the size works on every NT).

const spiGetNonClientMetrics = 0x0029

// LOGFONTW (92 bytes).
type LOGFONTW struct {
	Height, Width, Escapement, Orientation, Weight       int32
	Italic, Underline, StrikeOut, CharSet                byte
	OutPrecision, ClipPrecision, Quality, PitchAndFamily byte
	FaceName                                             [32]uint16
}

type nonClientMetricsW struct {
	Size                                                                uint32
	BorderWidth, ScrollWidth, ScrollHeight, CaptionWidth, CaptionHeight int32
	CaptionFont                                                         LOGFONTW
	SmCaptionWidth, SmCaptionHeight                                     int32
	SmCaptionFont                                                       LOGFONTW
	MenuWidth, MenuHeight                                               int32
	MenuFont, StatusFont, MessageFont                                   LOGFONTW
}

var (
	procCreateFontIndirectW = modgdi32.NewProc("CreateFontIndirectW")
	menuFont                uintptr
	menuFontLF              LOGFONTW
)

// MenuLogFont is lfMenuFont of the system (ok false when unavailable).
func MenuLogFont() (LOGFONTW, bool) {
	var m nonClientMetricsW
	m.Size = uint32(unsafe.Sizeof(m))
	r, _, _ := callProc(procSystemParametersInfoW, spiGetNonClientMetrics, uintptr(m.Size), uintptr(unsafe.Pointer(&m)), 0)
	return m.MenuFont, r != 0
}

// MenuFont is the cached HFONT of lfMenuFont (0 when unavailable).
func MenuFont() uintptr {
	if menuFont != 0 {
		return menuFont
	}
	lf, ok := MenuLogFont()
	if !ok {
		return 0
	}
	r, _, _ := callProc(procCreateFontIndirectW, uintptr(unsafe.Pointer(&lf)))
	menuFont, menuFontLF = r, lf
	return menuFont
}
