package trayui

// Unified light visual theme of the tray (phase 11, PLAN §12 p.146,
// CHK-TRAY-51). One set of colours, spacing and the UI font; there is no
// dark theme. Windows take colours from here instead of scattered RGB.

// Font of every tray window (Segoe UI 9, scaled to the system DPI).
const (
	FontFace = "Segoe UI"
	FontPt   = 9
)

// Spacing in 96-dpi units (same grid as internal/wintray/ui).
const (
	SpaceXS = 4
	SpaceS  = 8
	SpaceM  = 12
	SpaceL  = 16
)

// COLORREF packs sRGB as Win32 0x00BBGGRR.
func ColorRef(r, g, b uint8) uint32 {
	return uint32(r) | uint32(g)<<8 | uint32(b)<<16
}

// paleRGB: soft backgrounds of list rows, cards and HTML export
// (normal / warning / error / disabled).
var paleRGB = map[Color][3]uint8{
	ColorGreen:  {0xDF, 0xF3, 0xDF},
	ColorYellow: {0xFF, 0xF3, 0xC4},
	ColorRed:    {0xF8, 0xD0, 0xD0},
	ColorGrey:   {0xE6, 0xE6, 0xE6},
}

// PaleColorRef is the pale background of a state.
func PaleColorRef(c Color) uint32 {
	v, ok := paleRGB[c]
	if !ok {
		return Panel
	}
	return ColorRef(v[0], v[1], v[2])
}

// PaleHTML is the CSS #rrggbb of a pale state background.
func PaleHTML(c Color) string {
	v, ok := paleRGB[c]
	if !ok {
		return "#f0f0f0"
	}
	const hex = "0123456789abcdef"
	var b [7]byte
	b[0] = '#'
	b[1], b[2] = hex[v[0]>>4], hex[v[0]&15]
	b[3], b[4] = hex[v[1]>>4], hex[v[1]&15]
	b[5], b[6] = hex[v[2]>>4], hex[v[2]&15]
	return string(b[:])
}

// Strong state colours as COLORREF (must match colorRGB / RGB()).
const (
	StateOK       = 0x0043A02E // #2EA043 green
	StateWarning  = 0x0000C2F2 // #F2C200 yellow
	StateError    = 0x002530D9 // #D93025 red
	StateDisabled = 0x009A9A9A // #9A9A9A grey
)

// ColorRefOf is the strong colour of a state (icons, card stripe, chart).
func ColorRefOf(c Color) uint32 {
	switch c {
	case ColorGreen:
		return StateOK
	case ColorYellow:
		return StateWarning
	case ColorRed:
		return StateError
	case ColorGrey:
		return StateDisabled
	}
	return StateDisabled
}

// Chrome colours (COLORREF).
const (
	Text          = 0x00404040 // body text
	White         = 0x00FFFFFF
	Black         = 0x00000000
	Axis          = 0x00808080
	Grid          = 0x00E0E0E0
	Panel         = 0x00F0F0F0 // card panel / page background
	CardFace      = 0x00F8F8F8 // card without a state
	CardFrame     = 0x00A0A0A0
	CardStripeOff = 0x00C0C0C0
	RequiredField = 0x00D0D0FF // empty required edit (light red)
	HeaderRow     = 0x00EEEEEE // HTML th
	Border        = 0x00999999 // HTML table border
)

// Chart line colours (history window).
const (
	ChartAvg  = 0x00B05A1E // series average
	ChartBand = 0x00E0B48C // min–max band
)
