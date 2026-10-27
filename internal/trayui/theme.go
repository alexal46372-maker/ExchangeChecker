package trayui

import "sync"

// Unified visual theme of the tray (phase 11 light — CHK-TRAY-51;
// phase 12 dark — CHK-TRAY-55). Light constants stay the CHK-TRAY-51
// values; the active theme selects which COLORREF / HTML helpers return.
// Windows take colours from the helpers instead of scattered RGB.

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

// Theme is the chrome / pale palette of the tray windows.
type Theme string

const (
	ThemeLight Theme = "light"
	ThemeDark  Theme = "dark"
)

var (
	themeMu   sync.RWMutex
	curTheme  = ThemeLight
)

// NormalizeTheme returns light or dark; empty / unknown = light.
func NormalizeTheme(s string) Theme {
	switch Theme(trimLower(s)) {
	case ThemeDark:
		return ThemeDark
	}
	return ThemeLight
}

func trimLower(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n' || s[j-1] == '\r') {
		j--
	}
	b := make([]byte, j-i)
	for k := i; k < j; k++ {
		c := s[k]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[k-i] = c
	}
	return string(b)
}

// SetTheme selects the active palette (light by default).
func SetTheme(t Theme) {
	themeMu.Lock()
	curTheme = NormalizeTheme(string(t))
	themeMu.Unlock()
}

// Current is the active theme.
func Current() Theme {
	themeMu.RLock()
	defer themeMu.RUnlock()
	return curTheme
}

// IsDark reports whether the dark palette is active.
func IsDark() bool { return Current() == ThemeDark }

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

// darkPaleRGB: readable pale rows on a dark panel (CHK-TRAY-55).
var darkPaleRGB = map[Color][3]uint8{
	ColorGreen:  {0x1E, 0x3D, 0x28},
	ColorYellow: {0x4A, 0x3C, 0x14},
	ColorRed:    {0x4A, 0x22, 0x24},
	ColorGrey:   {0x3A, 0x3A, 0x3A},
}

func activePale() map[Color][3]uint8 {
	if IsDark() {
		return darkPaleRGB
	}
	return paleRGB
}

// PaleColorRef is the pale background of a state (active theme).
func PaleColorRef(c Color) uint32 {
	v, ok := activePale()[c]
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

// PaleHTML is the CSS #rrggbb of a pale state background (active theme).
func PaleHTML(c Color) string {
	v, ok := activePale()[c]
	if !ok {
		if IsDark() {
			return "#282828"
		}
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

// Chrome colours (COLORREF) — light theme constants (CHK-TRAY-51).
// Active painting / HTML must use Chrome*() helpers so dark theme applies.
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

// Dark chrome (CHK-TRAY-55): readable text on a dark panel, muted borders.
const (
	darkText          = 0x00E0E0E0
	darkPage          = 0x00202020 // page / chart background
	darkAxis          = 0x00A0A0A0
	darkGrid          = 0x00404040
	darkPanel         = 0x00282828
	darkCardFace      = 0x00303030
	darkCardFrame     = 0x00606060
	darkCardStripeOff = 0x00505050
	darkRequiredField = 0x00404080
	darkHeaderRow     = 0x00383838
	darkBorder        = 0x00666666
)

// ChromeText is body text of the active theme.
func ChromeText() uint32 {
	if IsDark() {
		return darkText
	}
	return Text
}

// ChromePage is the page / chart background of the active theme.
func ChromePage() uint32 {
	if IsDark() {
		return darkPage
	}
	return White
}

// ChromePanel is the card panel / window client background.
func ChromePanel() uint32 {
	if IsDark() {
		return darkPanel
	}
	return Panel
}

// ChromeCardFace is an empty card face.
func ChromeCardFace() uint32 {
	if IsDark() {
		return darkCardFace
	}
	return CardFace
}

// ChromeCardFrame is the card outline.
func ChromeCardFrame() uint32 {
	if IsDark() {
		return darkCardFrame
	}
	return CardFrame
}

// ChromeCardStripeOff is a disabled card stripe.
func ChromeCardStripeOff() uint32 {
	if IsDark() {
		return darkCardStripeOff
	}
	return CardStripeOff
}

// ChromeRequired is the empty required-edit highlight.
func ChromeRequired() uint32 {
	if IsDark() {
		return darkRequiredField
	}
	return RequiredField
}

// ChromeHeaderRow is the HTML / table header background.
func ChromeHeaderRow() uint32 {
	if IsDark() {
		return darkHeaderRow
	}
	return HeaderRow
}

// ChromeBorder is the HTML / table border colour.
func ChromeBorder() uint32 {
	if IsDark() {
		return darkBorder
	}
	return Border
}

// ChromeAxis is a chart axis colour.
func ChromeAxis() uint32 {
	if IsDark() {
		return darkAxis
	}
	return Axis
}

// ChromeGrid is a chart grid colour.
func ChromeGrid() uint32 {
	if IsDark() {
		return darkGrid
	}
	return Grid
}

// Chart line colours (history window) — same in both themes (on pale/dark page).
const (
	ChartAvg  = 0x00B05A1E // series average
	ChartBand = 0x00E0B48C // min–max band
)
