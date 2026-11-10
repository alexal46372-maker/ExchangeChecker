package trayui

import (
	"bytes"
	"testing"
)

// TestCHK_TRAY_49_Icons — every icon is an .ico with all IconSizes
// (16/20/24/32/40/48 as 32-bit BMP, 256 as PNG — phase 15), the same
// bytes on every call (deterministic), equals the committed file in
// internal/wintray/icons, decodes back to its image at every size (and
// scales to a size it does not have). The blank state icon is empty (no
// opaque pixel at any size); the tray blink «off» icon tray_blank stays
// the normal icon (CHK-TRAY-90, logic unchanged).
func TestCHK_TRAY_49_Icons(t *testing.T) {
	names := IconNames()
	if len(names) != 17 {
		t.Fatalf("names %v", names)
	}
	want := []int{16, 20, 24, 32, 40, 48, 256}
	if len(IconSizes) != len(want) {
		t.Fatalf("IconSizes %v, want %v", IconSizes, want)
	}
	for i := range want {
		if IconSizes[i] != want[i] {
			t.Fatalf("IconSizes %v, want %v", IconSizes, want)
		}
	}
	for _, n := range names {
		a, err := IconICO(n)
		if err != nil {
			t.Fatal(n, err)
		}
		b, _ := IconICO(n)
		if !bytes.Equal(a, b) {
			t.Errorf("%s: not deterministic", n)
		}
		if c := readCommitted(t, "internal", "wintray", "icons", n+".ico"); !bytes.Equal(c, a) {
			t.Errorf("%s.ico stale (go run ./tools/mkres)", n)
		}
		checkSizes(t, n, a, want)
		for _, s := range IconSizes {
			p, err := DecodeICO(a, s)
			if err != nil || len(p) != s*s {
				t.Fatalf("%s %d: %v", n, s, err)
			}
			img, _ := iconImage(n, s)
			opaque := 0
			for i := range p {
				if p[i]>>24 != img[i]>>24 {
					t.Fatalf("%s %d: pixel %d alpha %08x, want %08x", n, s, i, p[i], img[i])
				}
				if p[i]>>24 == 255 {
					opaque++
					if p[i] != img[i] {
						t.Fatalf("%s %d: pixel %d %08x, want %08x", n, s, i, p[i], img[i])
					}
				}
			}
			switch {
			case n == IcoStateBlank:
				if opaque != 0 {
					t.Errorf("%s %d: blank icon has %d opaque pixels", n, s, opaque)
				}
			case opaque < s*s/10:
				t.Errorf("%s %d: only %d opaque pixels", n, s, opaque)
			}
		}
		if p, err := DecodeICO(a, 28); err != nil || len(p) != 28*28 {
			t.Errorf("%s: scaled decode %v", n, err)
		}
	}
	if _, err := IconICO("nope"); err == nil {
		t.Error("unknown icon")
	}
	if StateIco(IconWarning) != IcoStateWarning || CardIco(ColorGrey) != IcoStateOffline {
		t.Error("state mapping")
	}
	ok, _ := IconICO(IcoTrayOK)
	if blank, _ := IconICO(IcoTrayBlank); !bytes.Equal(ok, blank) {
		t.Error("tray_blank must be the normal icon")
	}
}
