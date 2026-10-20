package trayui

import (
	"bytes"
	"testing"
)

// TestCHK_TRAY_49_Icons — every icon is an .ico with all IconSizes,
// 32-bit, the same bytes on every call (deterministic), decodes back to
// its image at every size (and scales to a size it does not have).
func TestCHK_TRAY_49_Icons(t *testing.T) {
	names := IconNames()
	if len(names) != 11 {
		t.Fatalf("names %v", names)
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
		for _, s := range IconSizes {
			p, err := DecodeICO(a, s)
			if err != nil || len(p) != s*s {
				t.Fatalf("%s %d: %v", n, s, err)
			}
			want, _ := iconImage(n, s)
			opaque := 0
			for i := range p {
				if p[i]>>24 == 255 {
					opaque++
					if p[i] != want[i] {
						t.Fatalf("%s %d: pixel %d %08x, want %08x", n, s, i, p[i], want[i])
					}
				}
			}
			if n != IcoStateBlank && opaque < s*s/10 {
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
}
