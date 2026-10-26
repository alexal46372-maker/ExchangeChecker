package trayui

import "testing"

// TestCHK_TRAY_51_Theme — one light theme: strong and pale colours of
// every state, chrome, font; PaleHTML matches PaleColorRef; no dark
// palette (PLAN §12 p.146).
func TestCHK_TRAY_51_Theme(t *testing.T) {
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
	r, g, b := RGB(ColorGreen)
	if ColorRefOf(ColorGreen) != StateOK || ColorRef(r, g, b) != StateOK {
		t.Error("ColorRefOf / RGB mismatch for green")
	}
}
