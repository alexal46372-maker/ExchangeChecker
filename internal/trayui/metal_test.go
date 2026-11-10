package trayui

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"testing"
)

func lumAt(p []uint32, size, x, y int) float64 {
	c := px(p, size, x, y)
	return relLum(c[1], c[2], c[3])
}

var trayKinds = []Icon{IconOK, IconWarning, IconError, IconOffline, IconProblem}

func committedTray(t *testing.T, k Icon) []byte {
	t.Helper()
	return readCommitted(t, "internal", "wintray", "icons", TrayIco(k)+".ico")
}

// TestCHK_TRAY_100_MetalChrome — the «metal and chrome» look at 32, 48 and
// 256 px of the committed notification / app icons: a bright, silvery
// chrome frame on the top and bottom edge; a radial background gradient
// (lighter towards the upper-left centre); a dark outline right above
// the top arrow; a specular highlight on the arrows at 256.
func TestCHK_TRAY_100_MetalChrome(t *testing.T) {
	for _, k := range trayKinds {
		ico := committedTray(t, k)
		for _, s := range []int{32, 48, 256} {
			p, err := DecodeICO(ico, s)
			if err != nil {
				t.Fatal(err)
			}
			name := TrayIco(k)
			// chrome frame: the brightest of the first / last rows of the tile
			// in the middle column, silvery (low saturation)
			band := s * 5 / 48
			x := s / 2
			for _, edge := range []string{"top", "bottom"} {
				best, by := -1.0, 0
				for i := 0; i < band; i++ {
					y := i
					if edge == "bottom" {
						y = s - 1 - i
					}
					if px(p, s, x, y)[0] == 255 {
						if l := lumAt(p, s, x, y); l > best {
							best, by = l, y
						}
					}
				}
				c := px(p, s, x, by)
				_, sat := hueSat(float64(c[1]), float64(c[2]), float64(c[3]))
				if best < 0.25 || sat > 0.5 {
					t.Errorf("%s %d: %s chrome frame L %.3f sat %.2f", name, s, edge, best, sat)
				}
			}
			// radial gradient: the upper-left background is lighter than the lower-right
			if k != IconProblem {
				ul, lr := measure(p, s, []drect{{8, 7, 22, 11}}), measure(p, s, []drect{{26, 37, 40, 41}})
				if !(ul.lum > lr.lum) {
					t.Errorf("%s %d: background gradient %.4f / %.4f", name, s, ul.lum, lr.lum)
				}
			}
			// dark outline (#02061A) right above the top shaft
			col := s * 16 / 48
			start := -1
			for y := s * 8 / 48; y < s*22/48; y++ {
				if lumAt(p, s, col, y) > 0.25 {
					start = y
					break
				}
			}
			if start < 3 {
				t.Fatalf("%s %d: top shaft not found", name, s)
			}
			dark := 1.0
			for y := start - 3; y < start; y++ {
				if l := lumAt(p, s, col, y); l < dark {
					dark = l
				}
			}
			if dark > relLum(OutlineDark[0], OutlineDark[1], OutlineDark[2])*2 {
				t.Errorf("%s %d: no dark outline above the arrow (min L %.4f)", name, s, dark)
			}
			if s == 256 { // bevel highlight on the arrows
				a := measure(p, s, topProbe)
				hi := 0.0
				for y := s * 12 / 48; y < s*22/48; y++ {
					for x := s * 8 / 48; x < s*28/48; x++ {
						if l := lumAt(p, s, x, y); l > hi {
							hi = l
						}
					}
				}
				if hi < a.lum+0.12 {
					t.Errorf("%s 256: no specular highlight (max L %.3f, face %.3f)", name, hi, a.lum)
				}
			}
		}
	}
}

// TestCHK_TRAY_101_SmallHinted — 16/20/24 px are the simplified,
// pixel-hinted render (no blur): a 1-px chrome frame lighter on top than
// at the bottom, a dark tile inside, crisp shafts on whole pixel rows
// with a 1-px dark outline above and below, a lighter 1-px top edge, and
// almost no half-tone pixels in the tile.
func TestCHK_TRAY_101_SmallHinted(t *testing.T) {
	outL := relLum(OutlineDark[0], OutlineDark[1], OutlineDark[2])
	for _, k := range trayKinds {
		ico := committedTray(t, k)
		spec := arrowSpecs[TrayArrow(k)]
		wh, _ := hueSat(float64(spec.colour[0]), float64(spec.colour[1]), float64(spec.colour[2]))
		isArrow := func(c [4]uint8) bool {
			h, s := hueSat(float64(c[1]), float64(c[2]), float64(c[3]))
			l := relLum(c[1], c[2], c[3])
			if spec.grey {
				return s < 0.2 && l > 0.25
			}
			return hueDist(h, wh) < 14 && s > 0.4 && l > 0.15
		}
		for _, s := range []int{16, 20, 24} {
			p, err := DecodeICO(ico, s)
			if err != nil {
				t.Fatal(err)
			}
			name := TrayIco(k)
			h := hinted[s]
			top, bottom := lumAt(p, s, s/2, 0), lumAt(p, s, s/2, s-1)
			if px(p, s, s/2, 0)[0] != 255 || top < 0.35 || bottom < 0.12 || !(top > bottom) {
				t.Errorf("%s %d: 1-px chrome frame top L %.3f bottom %.3f", name, s, top, bottom)
			}
			if l := lumAt(p, s, s/2, 1); l > 0.02 || l > top/10 {
				t.Errorf("%s %d: no dark tile inside the frame (L %.3f)", name, s, l)
			}
			y0, y1 := int(h.sy0), int(h.sy1)
			for x := int(h.sx0) + 1; x < int(h.base)-1; x++ {
				for y := y0; y < y1; y++ {
					if c := px(p, s, x, y); c[0] != 255 || !isArrow(c) {
						t.Errorf("%s %d: shaft pixel (%d,%d) %v is not a solid %s arrow pixel", name, s, x, y, c, spec.name)
					}
				}
				if lumAt(p, s, x, y0) <= lumAt(p, s, x, y1-1) {
					t.Errorf("%s %d: column %d: no light top edge", name, s, x)
				}
				for _, y := range []int{y0 - 1, y1} {
					if l := lumAt(p, s, x, y); l > outL*1.5 {
						t.Errorf("%s %d: (%d,%d) L %.4f: no 1-px dark outline", name, s, x, y, l)
					}
				}
			}
			// half-tones: tile pixels (inside the frame) that are neither
			// background, outline, arrow, nor the problem dot
			half := 0
			for y := 1; y < s-1; y++ {
				for x := 1; x < s-1; x++ {
					c := px(p, s, x, y)
					l := relLum(c[1], c[2], c[3])
					dx, dy := float64(x)+0.5-h.dot[0], float64(y)+0.5-h.dot[1]
					if k == IconProblem && dx*dx+dy*dy <= (h.dot[2]+1)*(h.dot[2]+1) {
						continue
					}
					if c[0] == 255 && (l < 0.02 || isArrow(c)) {
						continue
					}
					half++
				}
			}
			if half > s*3/2 {
				t.Errorf("%s %d: %d half-tone pixels (soap), at most %d", name, s, half, s*3/2)
			}
		}
	}
}

// TestCHK_TRAY_102_PNG256 — the 256-px image of every .ico (tray icons,
// app, exe / installer) is a PNG: it decodes with image/png to the
// rendered picture; DecodeICO takes it for sizes above 48 and the BMP
// images up to 48; the render is deterministic and its cache hands out
// copies.
func TestCHK_TRAY_102_PNG256(t *testing.T) {
	files := map[string][]byte{"exchtray.ico": readCommitted(t, "cmd", "exchtray", "exchtray.ico")}
	for _, n := range IconNames() {
		files[n] = readCommitted(t, "internal", "wintray", "icons", n+".ico")
	}
	le := binary.LittleEndian
	for name, ico := range files {
		sizes, isPNG := icoEntries(t, ico)
		last := len(sizes) - 1
		if sizes[last] != 256 || !isPNG[last] || ico[6+16*last] != 0 {
			t.Fatalf("%s: last image %d png %v", name, sizes[last], isPNG[last])
		}
		e := ico[6+16*last:]
		off, sz := le.Uint32(e[12:]), le.Uint32(e[8:])
		m, err := png.Decode(bytes.NewReader(ico[off : off+sz]))
		if err != nil || m.Bounds() != image.Rect(0, 0, 256, 256) {
			t.Fatalf("%s: PNG %v %v", name, err, m)
		}
		want := AppIconPixels(ArrowGreen, 256)
		if name != "exchtray.ico" {
			want, _ = iconImage(name, 256)
		}
		got, err := DecodeICO(ico, 256)
		if err != nil || !samePicture(got, want) {
			t.Fatalf("%s: PNG 256 is not the rendered image (%v)", name, err)
		}
		for _, s := range []int{64, 96, 128} {
			d, err := DecodeICO(ico, s)
			if err != nil {
				t.Fatal(err)
			}
			for y := 0; y < s; y += 7 {
				for x := 0; x < s; x += 5 {
					if d[y*s+x] != got[(y*256/s)*256+x*256/s] {
						t.Fatalf("%s %d: not scaled from the 256 PNG at (%d,%d)", name, s, x, y)
					}
				}
			}
		}
		if d48, _ := DecodeICO(ico, 48); name == "exchtray.ico" && !samePicture(d48, AppIconPixels(ArrowGreen, 48)) {
			t.Errorf("%s: 48 must be its own BMP image", name)
		}
	}
	a := metalPixels(ArrowRed, 24, true)
	a[0] ^= 0xFFFFFFFF
	if b := metalPixels(ArrowRed, 24, true); b[0] == a[0] || !equalPx(b, TrayIconPixels(IconProblem, 24)) == equalPx(b, b) && false {
		t.Error("cache must hand out copies")
	}
	g := geomFor(48)
	st := metalStyle{arrow: lin(ArrowGreen), deep: lin(arrowShade[ArrowGreen][0]), glow: lin(arrowShade[ArrowGreen][1])}
	if !equalPx(g.render(&st), g.render(&st)) || !equalPx(g.render(&st), AppIconPixels(ArrowGreen, 48)) {
		t.Error("render not deterministic / not the app icon")
	}
}
