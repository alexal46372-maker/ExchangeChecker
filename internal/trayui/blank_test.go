package trayui

import (
	"bytes"
	"math"
	"sort"
	"testing"
)

// TestCHK_TRAY_103_BlankBlackArrows — phase 16 B: the «off» phase of the
// blinking tray icon (tray_blank) has black arrows in a light chrome
// outline, no longer the green tray_ok. Measured on the committed
// internal/wintray/icons/tray_blank.ico at every size: the pixels are
// classified by the same geometry as the render (tile and arrow SDF at the
// pixel centre); the arrow body is black (median WCAG luminance ≤ 0.02),
// the outline is light and neutral (mean luminance ≥ 0.40, saturation ≤
// 0.25), and the outline contrasts with the tile background and with the
// arrow body (WCAG ≥ 7). state_blank (the empty card icon) is unchanged.
func TestCHK_TRAY_103_BlankBlackArrows(t *testing.T) {
	if TrayArrow(IconBlank) != ArrowBlack || TrayArrow(IconOK) != ArrowGreen {
		t.Fatalf("TrayArrow blank %v ok %v", TrayArrow(IconBlank), TrayArrow(IconOK))
	}
	if TrayIco(IconBlank) != IcoTrayBlank || StateIco(IconBlank) != IcoStateBlank {
		t.Fatal("tray_blank / state_blank names")
	}
	blank := readCommitted(t, "internal", "wintray", "icons", IcoTrayBlank+".ico")
	ok := readCommitted(t, "internal", "wintray", "icons", IcoTrayOK+".ico")
	if bytes.Equal(blank, ok) {
		t.Fatal("tray_blank.ico must differ from tray_ok.ico")
	}
	if gen, err := IconICO(IcoTrayBlank); err != nil || !bytes.Equal(gen, blank) {
		t.Fatalf("tray_blank.ico stale (go run ./tools/mkres): %v", err)
	}
	for _, s := range IconSizes {
		d, err := DecodeICO(blank, s)
		if err != nil {
			t.Fatal(err)
		}
		if !samePicture(d, TrayIconPixels(IconBlank, s)) {
			t.Fatalf("%d: committed image is not the render", s)
		}
		if equalPx(TrayIconPixels(IconBlank, s), TrayIconPixels(IconOK, s)) {
			t.Fatalf("%d: blank == ok", s)
		}
		g := geomFor(s)
		gap := 1.5 // background: away from the outline
		if g.small {
			gap = 0.6
		}
		var outL, outR, outG, outB, bgL float64
		var nOut, nBg int
		var body []float64
		for y := 0; y < s; y++ {
			for x := 0; x < s; x++ {
				c := px(d, s, x, y)
				if c[0] != 255 {
					continue
				}
				fx, fy := float64(x)+0.5, float64(y)+0.5
				td, _, _ := g.tileSDF(fx, fy)
				if td < g.frame+1 {
					continue
				}
				ad, _, _, _ := g.arrowSDF(fx, fy)
				l := relLum(c[1], c[2], c[3])
				switch {
				case ad < 0 && ad > -g.outline:
					outL += l
					outR, outG, outB = outR+float64(c[1]), outG+float64(c[2]), outB+float64(c[3])
					nOut++
				case ad >= 0.5:
					body = append(body, l)
				case ad < -g.outline-gap:
					bgL += l
					nBg++
				}
			}
		}
		if nOut < 8 || nBg < 3 || len(body) < 8 {
			t.Fatalf("%d: classes out %d bg %d body %d", s, nOut, nBg, len(body))
		}
		outL, bgL = outL/float64(nOut), bgL/float64(nBg)
		sort.Float64s(body)
		med := body[len(body)/2]
		_, sat := hueSat(outR/float64(nOut), outG/float64(nOut), outB/float64(nOut))
		t.Logf("%d: outline L %.3f sat %.2f, bg L %.4f, body median L %.4f; contrast outline/bg %.1f, outline/body %.1f",
			s, outL, sat, bgL, med, wcag(outL, bgL), wcag(outL, med))
		if med > 0.02 {
			t.Errorf("%d: arrows not black (median L %.4f)", s, med)
		}
		if outL < 0.40 || sat > 0.25 {
			t.Errorf("%d: outline not light chrome (L %.3f sat %.2f)", s, outL, sat)
		}
		if c := wcag(outL, bgL); c < 7 {
			t.Errorf("%d: outline/background contrast %.1f < 7", s, c)
		}
		if c := wcag(outL, med); c < 7 {
			t.Errorf("%d: outline/arrow contrast %.1f < 7", s, c)
		}
		if math.IsNaN(outL) {
			t.Fatal("nan")
		}
	}
}
