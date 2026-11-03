package trayui

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
)

// TestCHK_TRAY_96_VolumeLevels — 0 crossed out, 1..33 one wave, 34..66 two,
// 67..99 three, 100 shouting; each picture differs.
func TestCHK_TRAY_96_VolumeLevels(t *testing.T) {
	cases := map[int]SpeakerLevel{-5: SpeakerMute, 0: SpeakerMute, 1: SpeakerWave1, 33: SpeakerWave1, 34: SpeakerWave2, 66: SpeakerWave2, 67: SpeakerWave3, 99: SpeakerWave3, 100: SpeakerShout, 150: SpeakerShout}
	for v, want := range cases {
		if got := VolumeLevel(v); got != want {
			t.Errorf("VolumeLevel(%d)=%d want %d", v, got, want)
		}
	}
	seen := map[string]SpeakerLevel{}
	for l := SpeakerMute; l <= SpeakerShout; l++ {
		p := SpeakerPixels(l, 32)
		key := ""
		lit := 0
		for _, c := range p {
			key += string(rune(c&0xFFFFFF + 1))
			if c>>24 != 0 {
				lit++
			}
		}
		if lit == 0 {
			t.Fatalf("level %d empty", l)
		}
		if o, dup := seen[key]; dup {
			t.Errorf("level %d draws like %d", l, o)
		}
		seen[key] = l
	}
	// more waves = more lit pixels right of the cone
	right := func(l SpeakerLevel) int {
		n, p := 0, SpeakerPixels(l, 32)
		for y := 0; y < 32; y++ {
			for x := 17; x < 32; x++ {
				if p[y*32+x]>>24 != 0 {
					n++
				}
			}
		}
		return n
	}
	if !(right(SpeakerWave1) < right(SpeakerWave2) && right(SpeakerWave2) < right(SpeakerWave3) && right(SpeakerWave3) < right(SpeakerShout)) {
		t.Errorf("waves %d %d %d %d", right(SpeakerWave1), right(SpeakerWave2), right(SpeakerWave3), right(SpeakerShout))
	}
}

// TestCHK_TRAY_96_VolumeFromX — the track maps ends to 0 / 100, clamps.
func TestCHK_TRAY_96_VolumeFromX(t *testing.T) {
	for _, c := range [][2]int{{100, 0}, {50, 0}, {300, 100}, {500, 100}, {200, 50}} {
		if got := VolumeFromX(c[0], 100, 301); got != c[1] {
			t.Errorf("x=%d: %d want %d", c[0], got, c[1])
		}
	}
	if VolumeFromX(5, 10, 10) != 0 {
		t.Error("empty track")
	}
}

// TestCHK_TRAY_96_MenuSlider — the right menu has the Volume slider item
// (no ellipsis: it is not a window any more).
func TestCHK_TRAY_96_MenuSlider(t *testing.T) {
	n := 0
	for _, it := range Phase10Items() {
		if it.ID == CmdVolume {
			n++
			if !it.Volume || it.Text != "Громкость" {
				t.Errorf("volume item %+v", it)
			}
		}
	}
	if n != 1 || i18n.T(i18n.KeyMenuVolume) != "Громкость" {
		t.Fatalf("volume items %d", n)
	}
}
