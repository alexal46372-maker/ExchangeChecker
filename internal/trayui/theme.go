package trayui

// Unified visual theme of the tray (phase 11 — CHK-TRAY-51): one light
// palette. The dark theme (phase 12, CHK-TRAY-55) was removed in phase 13
// (CHK-TRAY-88). Windows take colours from the Chrome*() helpers instead
// of scattered RGB.

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

// Theme names the palette of the tray windows; only light exists since
// phase 13 (CHK-TRAY-88).
type Theme string

// ThemeLight is the only palette.
const ThemeLight Theme = "light"

// SetTheme is a no-op since phase 13 (CHK-TRAY-88): the palette is always
// light. Kept for the protected erase/redraw tests (CHK-TRAY-65..73) that
// pin the light palette before painting.
func SetTheme(Theme) {}

// Current is the active theme (always light).
func Current() Theme { return ThemeLight }

// COLORREF packs sRGB as Win32 0x00BBGGRR.
func ColorRef(r, g, b uint8) uint32 {
	return uint32(r) | uint32(g)<<8 | uint32(b)<<16
}

// paleRGB: soft backgrounds of list rows, cards and HTML export
// (normal / warning / error / disabled) — light theme (CHK-TRAY-51).
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
		return ChromePanel()
	}
	return ColorRef(v[0], v[1], v[2])
}

// ChromeHTML is the CSS #rrggbb of a chrome COLORREF (Border, HeaderRow…).
func ChromeHTML(c uint32) string {
	r, g, b := byte(c), byte(c>>8), byte(c>>16)
	return sprintfHTML(r, g, b)
}

func sprintfHTML(r, g, b byte) string {
	const hex = "0123456789abcdef"
	return string([]byte{'#', hex[r>>4], hex[r&15], hex[g>>4], hex[g&15], hex[b>>4], hex[b&15]})
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

// Chrome colours (COLORREF) — light palette constants (CHK-TRAY-51).
// Painting / HTML use the Chrome*() helpers.
const (
	Text          = 0x001E293B // body text (#1E293B, installer)
	White         = 0x00FFFFFF
	Black         = 0x00000000
	Axis          = 0x00808080
	Grid          = 0x00E0E0E0
	Panel         = 0x00F9F5F1 // #F1F5F9 footer/panel (installer bar; was BTNFACE)
	Page          = 0x00FCFBFA // #FAFBFC content (installer; CHK-TRAY-65)
	CardFace      = 0x00FFFFFF // card without a state
	CardFrame     = 0x00A0A0A0
	CardStripeOff = 0x00C0C0C0
	RequiredField = 0x00D0D0FF // empty required edit (light red)
	HeaderRow     = 0x00EEEEEE // HTML th (CHK-TRAY-56)
	Border        = 0x00999999 // HTML table border (CHK-TRAY-56)
	BrandDark     = 0x0044270F // #0F2744 installer brand
	Accent        = 0x00D97706 // #0677D9 installer accent
)

// ChromeText is body text.
func ChromeText() uint32 {
	return Text
}

// ChromePage is the page / chart background.
func ChromePage() uint32 {
	return Page
}

// ChromePanel is the card panel / window client background.
func ChromePanel() uint32 {
	return Panel
}

// ChromeCardFace is an empty card face.
func ChromeCardFace() uint32 {
	return CardFace
}

// ChromeCardFrame is the card outline.
func ChromeCardFrame() uint32 {
	return CardFrame
}

// ChromeCardStripeOff is a disabled card stripe.
func ChromeCardStripeOff() uint32 {
	return CardStripeOff
}

// ChromeRequired is the empty required-edit highlight.
func ChromeRequired() uint32 {
	return RequiredField
}

// ChromeHeaderRow is the HTML / table header background.
func ChromeHeaderRow() uint32 {
	return HeaderRow
}

// ChromeBorder is the HTML / table border colour.
func ChromeBorder() uint32 {
	return Border
}

// ChromeAxis is a chart axis colour.
func ChromeAxis() uint32 {
	return Axis
}

// ChromeGrid is a chart grid colour.
func ChromeGrid() uint32 {
	return Grid
}

// ChromeBrandDark is the installer brand navy (settings header accent).
func ChromeBrandDark() uint32 {
	return BrandDark
}

// ChromeAccent is the brand accent (#0677D9 light).
func ChromeAccent() uint32 {
	return Accent
}

// Chart line colours (history window).
const (
	ChartAvg  = 0x00B05A1E // series average
	ChartBand = 0x00E0B48C // min–max band
)
