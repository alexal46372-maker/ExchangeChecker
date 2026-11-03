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

// TestCHK_TRAY_88_NoDarkTheme — phase 13: the dark theme is gone; the
// palette is always light whatever SetTheme gets, helpers return the
// light constants and theme.go has no dark palette left.
func TestCHK_TRAY_88_NoDarkTheme(t *testing.T) {
	SetTheme(Theme("dark"))
	if Current() != ThemeLight {
		t.Fatalf("Current %q", Current())
	}
	if ChromePage() != Page || ChromePanel() != Panel || ChromeText() != Text || ChromeBorder() != Border ||
		ChromeHeaderRow() != HeaderRow || ChromeCardFace() != CardFace || ChromeCardFrame() != CardFrame ||
		ChromeCardStripeOff() != CardStripeOff || ChromeRequired() != RequiredField || ChromeAxis() != Axis ||
		ChromeGrid() != Grid || ChromeBrandDark() != BrandDark || ChromeAccent() != Accent {
		t.Fatal("chrome helpers must return the light constants")
	}
	if PaleColorRef(ColorGreen) != ColorRef(0xDF, 0xF3, 0xDF) || PaleHTML(ColorGreen) != "#dff3df" {
		t.Fatal("pale must be light")
	}
	b, err := os.ReadFile("theme.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"Theme" + "Dark", "Is" + "Dark", "dark" + "Pale", "dark" + "Page", "dark" + "Panel", "sync.RWMutex"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("theme.go still has %s", bad)
	}
	}
}

// TestCHK_TRAY_56_HTMLFromChrome — export/history HTML take Border /
// HeaderRow from the chrome helpers; no #999 / #eee literals in
// the builders (PLAN §12 p.195).
func TestCHK_TRAY_56_HTMLFromChrome(t *testing.T) {
	t.Cleanup(func() { SetTheme(ThemeLight) })
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, model.DBZone)
	rows := []StatusRow{{Database: "db", Color: ColorRed}}
	at := now

	SetTheme(ThemeLight)
	hLight := string(ExportHTML(rows, at))
	if !strings.Contains(hLight, ChromeHTML(Border)) || !strings.Contains(hLight, ChromeHTML(HeaderRow)) {
		t.Fatalf("light html missing chrome: %s", hLight)
	}
	if !strings.Contains(hLight, "#999999") || !strings.Contains(hLight, "#eeeeee") {
		t.Fatalf("light html expected #999999/#eeeeee via helpers: %s", hLight)
	}

	wantBorder, wantHeader := ChromeHTML(ChromeBorder()), ChromeHTML(ChromeHeaderRow())
	hist := string(ExportHistoryHTML(nil, "t", at, at, false))
	if !strings.Contains(hist, wantBorder) || !strings.Contains(hist, wantHeader) {
		t.Fatalf("history: %s", hist)
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

// TestCHK_TRAY_65_PaletteExact — exact installer hex (COLORREF BGR)
// (CHK-TRAY-65; dark mappings removed with the dark theme, CHK-TRAY-88). Pure Go; Linux OK.
func TestCHK_TRAY_65_PaletteExact(t *testing.T) {
	t.Cleanup(func() { SetTheme(ThemeLight) })
	// #0F2744 / #FAFBFC / #F1F5F9 / #0677D9 as COLORREF (0x00BBGGRR)
	if BrandDark != 0x0044270F {
		t.Fatalf("BrandDark %#x want 0x0044270F (#0F2744)", BrandDark)
	}
	if Page != 0x00FCFBFA {
		t.Fatalf("Page %#x want 0x00FCFBFA (#FAFBFC)", Page)
	}
	if Panel != 0x00F9F5F1 {
		t.Fatalf("Panel %#x want 0x00F9F5F1 (#F1F5F9)", Panel)
	}
	if Accent != 0x00D97706 {
		t.Fatalf("Accent %#x want 0x00D97706 (#0677D9)", Accent)
	}
	if Page == 0x00F0F0F0 || Panel == 0x00F0F0F0 {
		t.Fatal("Page/Panel must not be COLOR_BTNFACE gray")
	}
	SetTheme(ThemeLight)
	if ChromeBrandDark() != BrandDark || ChromeAccent() != Accent {
		t.Fatal("light ChromeBrandDark/Accent must match constants")
	}
	if ChromePage() != Page || ChromePanel() != Panel {
		t.Fatal("light ChromePage/Panel must match constants")
	}
}

