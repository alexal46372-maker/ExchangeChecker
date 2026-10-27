package trayui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_51_Theme — one light theme: strong and pale colours of
// every state, chrome, font; PaleHTML matches PaleColorRef; no dark
// palette (PLAN §12 p.146). With theme=light the light constants hold.
func TestCHK_TRAY_51_Theme(t *testing.T) {
	SetTheme(ThemeLight)
	t.Cleanup(func() { SetTheme(ThemeLight) })
	if FontFace != "Segoe UI" || FontPt != 9 {
		t.Errorf("font %s %d", FontFace, FontPt)
	}
	if SpaceXS != 4 || SpaceS != 8 || SpaceM != 12 || SpaceL != 16 {
		t.Errorf("spacing %d %d %d %d", SpaceXS, SpaceS, SpaceM, SpaceL)
	}
	for _, c := range []Color{ColorGreen, ColorYellow, ColorRed, ColorGrey} {
		if ColorRefOf(c) == 0 || PaleColorRef(c) == ColorRefOf(c) {
			t.Errorf("%d: strong and pale must differ", c)
		}
		html := PaleHTML(c)
		if len(html) != 7 || html[0] != '#' {
			t.Errorf("html %s", html)
		}
		// COLORREF low byte = R = first hex pair of #rrggbb
		ref := PaleColorRef(c)
		wantR := ref & 0xFF
		var gotR uint32
		for i := 1; i <= 2; i++ {
			gotR <<= 4
			ch := html[i]
			switch {
			case ch >= '0' && ch <= '9':
				gotR += uint32(ch - '0')
			default:
				gotR += uint32(ch - 'a' + 10)
			}
		}
		if gotR != wantR {
			t.Errorf("%d: html %s R=%02x, COLORREF R=%02x", c, html, gotR, wantR)
		}
	}
	if Text == 0 || RequiredField == 0 || ChartAvg == ChartBand {
		t.Error("chrome")
	}
	if ChromeBorder() != Border || ChromeHeaderRow() != HeaderRow || ChromePanel() != Panel {
		t.Error("light chrome helpers must match light constants")
	}
	r, g, b := RGB(ColorGreen)
	if ColorRefOf(ColorGreen) != StateOK || ColorRef(r, g, b) != StateOK {
		t.Error("ColorRefOf / RGB mismatch for green")
	}
}

// TestCHK_TRAY_55_ThemeSwitch — dark palette differs from light; helpers
// follow SetTheme; NormalizeTheme; round-trip of theme name (PLAN §12 p.194).
func TestCHK_TRAY_55_ThemeSwitch(t *testing.T) {
	t.Cleanup(func() { SetTheme(ThemeLight) })
	if NormalizeTheme("") != ThemeLight || NormalizeTheme("DARK") != ThemeDark || NormalizeTheme("x") != ThemeLight {
		t.Fatalf("NormalizeTheme: %q %q %q", NormalizeTheme(""), NormalizeTheme("DARK"), NormalizeTheme("x"))
	}
	SetTheme(ThemeLight)
	lightPale := map[Color]uint32{}
	lightHTML := map[Color]string{}
	for _, c := range []Color{ColorGreen, ColorYellow, ColorRed, ColorGrey} {
		lightPale[c] = PaleColorRef(c)
		lightHTML[c] = PaleHTML(c)
	}
	lightBorder, lightHeader := ChromeBorder(), ChromeHeaderRow()
	lightPage, lightText := ChromePage(), ChromeText()

	SetTheme(ThemeDark)
	if !IsDark() || Current() != ThemeDark {
		t.Fatal("IsDark / Current")
	}
	if ChromeBorder() == lightBorder || ChromeHeaderRow() == lightHeader {
		t.Error("dark Border/HeaderRow must differ from light")
	}
	if ChromePage() == lightPage || ChromeText() == lightText {
		t.Error("dark page/text must differ from light")
	}
	for _, c := range []Color{ColorGreen, ColorYellow, ColorRed, ColorGrey} {
		if PaleColorRef(c) == lightPale[c] {
			t.Errorf("%d: dark pale == light", c)
		}
		if PaleHTML(c) == lightHTML[c] {
			t.Errorf("%d: dark PaleHTML == light", c)
		}
		if PaleColorRef(c) == ColorRefOf(c) {
			t.Errorf("%d: dark pale == strong", c)
		}
	}
	// light constants themselves are unchanged (CHK-TRAY-51 contract)
	if Border != 0x00999999 || HeaderRow != 0x00EEEEEE {
		t.Error("light Border/HeaderRow constants must stay")
	}
	SetTheme(ThemeLight)
	if IsDark() || ChromeBorder() != Border {
		t.Error("back to light")
	}
}

// TestCHK_TRAY_56_HTMLFromChrome — export/history HTML take Border /
// HeaderRow from the active chrome theme; no #999 / #eee literals in
// the builders (PLAN §12 p.195).
func TestCHK_TRAY_56_HTMLFromChrome(t *testing.T) {
	t.Cleanup(func() { SetTheme(ThemeLight) })
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, model.DBZone)
	rows := []StatusRow{{Database: "db", Color: ColorRed}}
	at := now

	SetTheme(ThemeLight)
	hLight := string(ExportHTML(rows, "en", at))
	if !strings.Contains(hLight, ChromeHTML(Border)) || !strings.Contains(hLight, ChromeHTML(HeaderRow)) {
		t.Fatalf("light html missing chrome: %s", hLight)
	}
	if !strings.Contains(hLight, "#999999") || !strings.Contains(hLight, "#eeeeee") {
		t.Fatalf("light html expected #999999/#eeeeee via helpers: %s", hLight)
	}

	SetTheme(ThemeDark)
	hDark := string(ExportHTML(rows, "en", at))
	wantBorder, wantHeader := ChromeHTML(ChromeBorder()), ChromeHTML(ChromeHeaderRow())
	if !strings.Contains(hDark, wantBorder) || !strings.Contains(hDark, wantHeader) {
		t.Fatalf("dark html missing chrome %s %s: %s", wantBorder, wantHeader, hDark)
	}
	if strings.Contains(strings.ToLower(hDark), "#999999") || strings.Contains(strings.ToLower(hDark), "#eeeeee") {
		t.Fatalf("dark html must not keep light #999/#eee: %s", hDark)
	}
	if wantBorder == "#999999" || wantHeader == "#eeeeee" {
		t.Fatal("dark chrome helpers still light")
	}

	SetTheme(ThemeDark)
	hist := string(ExportHistoryHTML(nil, "t", at, at, "en", false))
	if !strings.Contains(hist, wantBorder) || !strings.Contains(hist, wantHeader) {
		t.Fatalf("history dark: %s", hist)
	}

	// source scan: export.go / history.go must not embed #999 / #eee
	root := "."
	for _, name := range []string{"export.go", "history.go", "theme.go"} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		low := strings.ToLower(string(b))
		if name != "theme.go" {
			if strings.Contains(low, "#999") || strings.Contains(low, "#eee") {
				t.Errorf("%s: hardcoded #999/#eee (use ChromeHTML helpers)", name)
			}
		}
	}
}
