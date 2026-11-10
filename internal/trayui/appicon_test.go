package trayui

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// Helpers of the icon tests (phase 15, «metal and chrome»). They measure
// the decoded pixels of the committed .ico files — the bytes the tray,
// the exe resources and the installer really use.

func px(p []uint32, size, x, y int) [4]uint8 {
	v := p[y*size+x]
	return [4]uint8{uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), uint8(v)}
}

func equalPx(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// samePicture: a decoded .ico image is the rendered one: the same alpha
// everywhere, the same colour where opaque (straight alpha in the file
// rounds semi-transparent edge pixels).
func samePicture(dec, want []uint32) bool {
	if len(dec) != len(want) {
		return false
	}
	for i := range dec {
		if dec[i]>>24 != want[i]>>24 || (dec[i]>>24 == 255 && dec[i] != want[i]) {
			return false
		}
	}
	return true
}

// relLum: WCAG relative luminance of an opaque pixel.
func relLum(r, g, b uint8) float64 {
	return 0.2126*s2l(r) + 0.7152*s2l(g) + 0.0722*s2l(b)
}

func wcag(a, b float64) float64 {
	if a < b {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
}

// hueSat: HSV hue (degrees) and saturation of an sRGB colour.
func hueSat(r, g, b float64) (float64, float64) {
	mx := math.Max(r, math.Max(g, b))
	mn := math.Min(r, math.Min(g, b))
	if mx <= 0 {
		return 0, 0
	}
	d := mx - mn
	if d == 0 {
		return 0, 0
	}
	var h float64
	switch mx {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, d / mx
}

func hueDist(a, b float64) float64 {
	d := math.Abs(a - b)
	if d > 180 {
		d = 360 - d
	}
	return d
}

// rect on the 48×48 design grid (u0, v0, u1, v1), mapped as u·size/48.
type drect [4]float64

// The probes: the middle of both arrow shafts, and the background above
// the top shaft (left) and below the bottom shaft (right).
var (
	arrowProbe = []drect{{12, 16, 24, 18}, {24, 30, 36, 32}}
	bgProbe    = []drect{{8, 7, 22, 11}, {26, 37, 40, 41}}
	// problem icon: the dot covers the bottom right
	topProbe   = []drect{{12, 16, 24, 18}}
	topBgProbe = []drect{{8, 7, 22, 11}}
)

type region struct {
	lum     float64 // mean WCAG luminance
	r, g, b float64 // mean sRGB
	n       int
	opaque  bool
}

func measure(p []uint32, size int, rs []drect) region {
	var m region
	m.opaque = true
	for _, r := range rs {
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				u, v := (float64(x)+0.5)*48/float64(size), (float64(y)+0.5)*48/float64(size)
				if u < r[0] || u > r[2] || v < r[1] || v > r[3] {
					continue
				}
				c := px(p, size, x, y)
				if c[0] != 255 {
					m.opaque = false
				}
				m.lum += relLum(c[1], c[2], c[3])
				m.r, m.g, m.b = m.r+float64(c[1]), m.g+float64(c[2]), m.b+float64(c[3])
				m.n++
			}
		}
	}
	if m.n > 0 {
		f := 1 / float64(m.n)
		m.lum, m.r, m.g, m.b = m.lum*f, m.r*f, m.g*f, m.b*f
	}
	return m
}

func readCommitted(t *testing.T, rel ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, rel...)...))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// icoEntries: the image sizes of an .ico directory and whether each image is PNG.
func icoEntries(t *testing.T, ico []byte) (sizes []int, isPNG []bool) {
	t.Helper()
	le := binary.LittleEndian
	if len(ico) < 6 || le.Uint16(ico) != 0 || le.Uint16(ico[2:]) != 1 {
		t.Fatal("not an .ico")
	}
	n := int(le.Uint16(ico[4:]))
	for i := 0; i < n; i++ {
		e := ico[6+16*i:]
		w, h := int(e[0]), int(e[1])
		if w != h {
			t.Fatalf("entry %d: %d×%d", i, w, h)
		}
		if w == 0 {
			w = 256
		}
		if le.Uint16(e[4:]) != 1 || le.Uint16(e[6:]) != 32 {
			t.Fatalf("entry %d: planes/bpp %d/%d", i, le.Uint16(e[4:]), le.Uint16(e[6:]))
		}
		sz, off := le.Uint32(e[8:]), le.Uint32(e[12:])
		if int(off+sz) > len(ico) {
			t.Fatalf("entry %d out of bounds", i)
		}
		sizes = append(sizes, w)
		isPNG = append(isPNG, bytes.HasPrefix(ico[off:], pngMagic))
	}
	return
}

func checkSizes(t *testing.T, name string, ico []byte, want []int) {
	t.Helper()
	sizes, isPNG := icoEntries(t, ico)
	if len(sizes) != len(want) {
		t.Fatalf("%s: sizes %v, want %v", name, sizes, want)
	}
	for i := range want {
		if sizes[i] != want[i] || isPNG[i] != (want[i] >= 256) {
			t.Errorf("%s: image %d size %d png %v, want %d (PNG only for 256)", name, i, sizes[i], isPNG[i], want[i])
		}
	}
}

// arrowSpec: the expected look of arrows of a colour on the dark tile.
type arrowSpec struct {
	name     string
	colour   [3]uint8
	grey     bool    // neutral (saturation ≤ 0.12) instead of a hue
	contrast float64 // WCAG arrow / background, at least
}

var arrowSpecs = map[[3]uint8]arrowSpec{
	ArrowGreen:  {"зелёный", ArrowGreen, false, 10},
	ArrowYellow: {"жёлтый", ArrowYellow, false, 9},
	ArrowRed:    {"красный", ArrowRed, false, 4.5},
	ArrowGrey:   {"серый", ArrowGrey, true, 10},
}

// checkArrows: hue of the arrows and WCAG contrast arrows / background of
// the decoded image p (size 32 or 256).
func checkArrows(t *testing.T, what string, p []uint32, size int, spec arrowSpec, probe, bgp []drect) {
	t.Helper()
	a, bg := measure(p, size, probe), measure(p, size, bgp)
	if !a.opaque || !bg.opaque || a.n < 4 || bg.n < 4 {
		t.Fatalf("%s %d: probes not opaque (%d, %d px)", what, size, a.n, bg.n)
	}
	h, s := hueSat(a.r, a.g, a.b)
	wh, _ := hueSat(float64(spec.colour[0]), float64(spec.colour[1]), float64(spec.colour[2]))
	if spec.grey {
		if s > 0.12 {
			t.Errorf("%s %d: grey arrows saturation %.2f (rgb %.0f/%.0f/%.0f)", what, size, s, a.r, a.g, a.b)
		}
	} else if hueDist(h, wh) > 10 || s < 0.5 {
		t.Errorf("%s %d: %s arrows hue %.1f° sat %.2f, want %.1f° ±10, sat ≥ 0.5", what, size, spec.name, h, s, wh)
	}
	if c := wcag(a.lum, bg.lum); c < spec.contrast {
		t.Errorf("%s %d: %s arrows contrast %.2f < %.1f (L %.3f / %.4f)", what, size, spec.name, c, spec.contrast, a.lum, bg.lum)
	}
	// the background is the dark blue of the palette (BgCore → BgEdge)
	bh, bs := hueSat(bg.r, bg.g, bg.b)
	if bg.lum > relLum(BgCore[0], BgCore[1], BgCore[2])*1.6 || hueDist(bh, 228) > 25 || bs < 0.5 {
		t.Errorf("%s %d: background L %.4f hue %.0f sat %.2f is not the dark blue tile", what, size, bg.lum, bh, bs)
	}
}

// TestCHK_TRAY_89_AppIconGreenArrows — the main icon (phase 15 «metal and
// chrome»): green arrows (hue of #3DFF7A, WCAG ≥ 10 to the dark-blue
// tile) at 32 and 256 px of the committed cmd/exchtray/exchtray.ico (the
// exe / installer icon, ExeIconSizes 16/32/48/256 with a PNG 256) and of
// internal/wintray/icons/app.ico (window icons); both are the generated
// bytes and show the same picture.
func TestCHK_TRAY_89_AppIconGreenArrows(t *testing.T) {
	ico := readCommitted(t, "cmd", "exchtray", "exchtray.ico")
	if !bytes.Equal(ico, ExeICO()) {
		t.Fatal("cmd/exchtray/exchtray.ico is not trayui.ExeICO (run go run ./tools/mkres)")
	}
	checkSizes(t, "exchtray.ico", ico, []int{16, 32, 48, 256})
	if len(ExeIconSizes) != 4 {
		t.Errorf("ExeIconSizes %v", ExeIconSizes)
	}
	app := readCommitted(t, "internal", "wintray", "icons", IcoApp+".ico")
	if want, _ := IconICO(IcoApp); !bytes.Equal(app, want) {
		t.Fatal("internal/wintray/icons/app.ico stale (go run ./tools/mkres)")
	}
	checkSizes(t, "app.ico", app, IconSizes)
	for _, s := range []int{32, 256} {
		d, err := DecodeICO(ico, s)
		if err != nil {
			t.Fatal(err)
		}
		checkArrows(t, "exchtray.ico", d, s, arrowSpecs[ArrowGreen], arrowProbe, bgProbe)
		w, err := DecodeICO(app, s)
		if err != nil || !equalPx(w, d) {
			t.Errorf("%d: app.ico and exchtray.ico differ (%v)", s, err)
		}
	}
	for _, s := range ExeIconSizes {
		d, err := DecodeICO(ico, s)
		if err != nil || !samePicture(d, AppIconPixels(ArrowGreen, s)) {
			t.Errorf("exchtray.ico %d is not AppIconPixels(ArrowGreen): %v", s, err)
		}
	}
	if a, _ := iconImage(IcoApp, 32); !equalPx(a, AppIconPixels(ArrowGreen, 32)) {
		t.Error("IcoApp must be the green-arrow app icon")
	}
}

// TestCHK_TRAY_90_TrayIconArrows — the notification icons (committed
// internal/wintray/icons/tray_*.ico) are the app icon with state arrows:
// green OK (and the blink «off» phase tray_blank = tray_ok, logic
// unchanged), yellow warning, red error, grey offline; problem = grey
// arrows and a red dot in a white ring. Hue and WCAG contrast at 32 and
// 256 px: green ≥ 10, yellow ≥ 9, grey ≥ 10, red ≥ 4.5.
func TestCHK_TRAY_90_TrayIconArrows(t *testing.T) {
	want := map[Icon][3]uint8{IconOK: ArrowGreen, IconBlank: ArrowGreen, IconWarning: ArrowYellow,
		IconError: ArrowRed, IconOffline: ArrowGrey, IconProblem: ArrowGrey}
	names := map[string]bool{}
	for k, arrow := range want {
		if TrayArrow(k) != arrow {
			t.Errorf("TrayArrow(%d) %v", k, TrayArrow(k))
		}
		name := TrayIco(k)
		names[name] = true
		ico := readCommitted(t, "internal", "wintray", "icons", name+".ico")
		if gen, err := IconICO(name); err != nil || !bytes.Equal(ico, gen) {
			t.Fatalf("%s.ico stale (go run ./tools/mkres): %v", name, err)
		}
		checkSizes(t, name, ico, IconSizes)
		probe, bgp := arrowProbe, bgProbe
		if k == IconProblem {
			probe, bgp = topProbe, topBgProbe
		}
		for _, s := range []int{32, 256} {
			d, err := DecodeICO(ico, s)
			if err != nil {
				t.Fatal(err)
			}
			checkArrows(t, name, d, s, arrowSpecs[arrow], probe, bgp)
			if k == IconProblem { // red dot (design 37,37) in a white ring
				c := px(d, s, s*37/48, s*37/48)
				h, sat := hueSat(float64(c[1]), float64(c[2]), float64(c[3]))
				if c[0] != 255 || hueDist(h, 0) > 10 || sat < 0.5 {
					t.Errorf("problem %d: dot %v", s, c)
				}
				w := px(d, s, s*37/48, s*37/48-s*10/48) // on the ring, above the dot
				if relLum(w[1], w[2], w[3]) < 0.6 {
					t.Errorf("problem %d: white ring %v", s, w)
				}
			}
		}
	}
	if len(names) != 6 {
		t.Errorf("tray icon names %v", names)
	}
	for _, s := range IconSizes {
		if !equalPx(TrayIconPixels(IconBlank, s), TrayIconPixels(IconOK, s)) ||
			!equalPx(TrayIconPixels(IconOK, s), AppIconPixels(ArrowGreen, s)) {
			t.Errorf("%d: blink off phase / OK must be the normal app icon", s)
		}
		if equalPx(TrayIconPixels(IconOffline, s), TrayIconPixels(IconProblem, s)) {
			t.Errorf("%d: problem icon has no dot", s)
		}
	}
	ok, _ := IconICO(IcoTrayOK)
	blank, _ := IconICO(IcoTrayBlank)
	if !bytes.Equal(ok, blank) {
		t.Error("tray_blank.ico must stay the normal icon (tray_ok.ico)")
	}
}
