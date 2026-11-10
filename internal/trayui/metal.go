package trayui

import (
	"math"
	"sync"
)

// The «metal and chrome» application icon (phase 15, Alex's decision
// 10.11.2026, CHK-TRAY-89/90/100–102): a deep-blue tile (radial gradient
// BgCore → BgEdge) in a chrome frame, two exchange arrows (→ on top, ←
// below) with a dark outline, a bevel, a vertical gradient and a specular
// highlight. It is rendered here in float64 (standard library only, no
// CGO), so tools/mkres writes the same bytes on every run of the same
// toolchain (Go 1.20.14, amd64): mkres -check and the CI double run.
//
// 32/40/48/256 px: the full render (bevelled chrome frame, shadow, glow,
// bevelled arrows); the arrow geometry is snapped to whole pixels up to
// 48 px. 16/20/24 px: a simplified, pixel-hinted render from per-size
// tables (1-px chrome frame lighter on top, 1-px dark outline, gradient
// arrows with a 1-px light top edge), no blur.

// Palette of the metal icon (sRGB).
var (
	BgCore      = [3]uint8{0x07, 0x1A, 0x55} // tile centre
	BgEdge      = [3]uint8{0x03, 0x0B, 0x2C} // tile edge
	RimBlue     = [3]uint8{0x4F, 0x8D, 0xFF} // chrome frame tint
	OutlineDark = [3]uint8{0x02, 0x06, 0x1A} // outline around the arrows
)

// Arrow colours of the application icon (phase 15 palette).
var (
	ArrowGreen  = [3]uint8{0x3D, 0xFF, 0x7A} // main icon / tray «норма»
	ArrowYellow = [3]uint8{0xFF, 0xC6, 0x1A} // tray: warning
	ArrowRed    = [3]uint8{0xFF, 0x4F, 0x4F} // tray: error, the problem dot
	ArrowGrey   = [3]uint8{0xD2, 0xD8, 0xE2} // tray: no service / unknown
)

// arrowShade: the darker bottom of the gradient and the glow of an
// arrow colour.
var arrowShade = map[[3]uint8][2][3]uint8{
	ArrowGreen:  {{0x00, 0xA8, 0x3A}, {0x2B, 0xFF, 0x6A}},
	ArrowYellow: {{0xE0, 0x8A, 0x00}, {0xFF, 0xC2, 0x1A}},
	ArrowRed:    {{0xC8, 0x10, 0x1A}, {0xFF, 0x30, 0x30}},
	ArrowGrey:   {{0x7C, 0x86, 0x96}, {0xB0, 0xC0, 0xD8}},
}

type pt struct{ x, y float64 }

// metalGeom: the geometry of one size in pixels.
type metalGeom struct {
	size    int
	ss      int     // samples per pixel side
	k, off  float64 // design unit → px: px = off + u*k
	rad     float64 // tile corner radius
	inset   float64 // transparent margin around the tile
	frame   float64 // chrome frame width
	outline float64 // dark outline width
	bevel   float64 // arrow bevel width (0: flat, hinted sizes)
	glow    float64 // glow strength around the arrows
	shadow  bool    // contact shadow + drop shadow
	small   bool    // simplified hinted render (16/20/24)
	arrows  [2][]pt
	band    [2][2]float64 // top / bottom y of each arrow (gradient)
	dot     [4]float64    // problem dot: cx, cy, white radius, red radius
}

// hinted: per-size pixel tables of the top arrow (→) of the small icons:
// shaft y0/y1, shaft x0, head base x, tip x, head y0/y1; the bottom arrow
// (←) is the same arrow turned by 180° around the centre. Dot: cx, cy,
// white radius, red radius.
var hinted = map[int]struct {
	sy0, sy1, sx0, base, tip, hy0, hy1 float64
	rad                                float64
	dot                                [4]float64
}{
	16: {4, 6, 3, 9, 14, 2, 8, 3, [4]float64{12, 12, 3.6, 2.6}},
	20: {5, 7, 3, 12, 18, 2, 10, 3.5, [4]float64{15, 15, 4.6, 3.4}},
	24: {5, 8, 4, 14, 21, 2, 11, 4, [4]float64{18, 18, 5.5, 4.2}},
}

// designArrow: the top arrow (→) on the 48×48 grid, as trayui.appShapes
// of phase 13: shaft 8..29 × 14..20, head (29,7)–(42,17)–(29,27).
const (
	dSy0, dSy1, dSx0, dBase, dTip, dHy0, dHy1 = 14, 20, 8, 29, 42, 7, 27
)

func arrowPoly(sy0, sy1, sx0, base, tip, hy0, hy1 float64) []pt {
	cy := (hy0 + hy1) / 2
	return []pt{{sx0, sy0}, {base, sy0}, {base, hy0}, {tip, cy}, {base, hy1}, {base, sy1}, {sx0, sy1}}
}

func turn(p []pt, s float64) []pt {
	out := make([]pt, len(p))
	for i, q := range p {
		out[i] = pt{s - q.x, s - q.y}
	}
	return out
}

func geomFor(size int) metalGeom {
	S := float64(size)
	g := metalGeom{size: size}
	if h, ok := hinted[size]; ok {
		g.small, g.ss = true, 4
		g.k, g.off = S/48, 0
		g.rad, g.frame, g.outline = h.rad, 1, 1
		top := arrowPoly(h.sy0, h.sy1, h.sx0, h.base, h.tip, h.hy0, h.hy1)
		g.arrows = [2][]pt{top, turn(top, S)}
		g.band = [2][2]float64{{h.hy0, h.hy1}, {S - h.hy1, S - h.hy0}}
		g.dot = h.dot
		return g
	}
	switch {
	case size >= 128:
		g.inset, g.ss = math.Round(S*12/256), 2
	case size >= 40:
		g.inset, g.ss = 1, 4
	default:
		g.inset, g.ss = 0, 4
	}
	g.off = g.inset
	g.k = (S - 2*g.inset) / 48
	g.rad = 8.5 * g.k
	g.frame = math.Max(2, 3.0*g.k)
	g.outline = math.Max(1, 1.1*g.k)
	g.bevel = math.Max(1.2, 2.3*g.k)
	g.shadow = size >= 40
	g.glow = 0.10
	if size >= 48 {
		g.glow = 0.16
	}
	px := func(u float64) float64 { return g.off + u*g.k }
	sy0, sy1, sx0, base, tip, hy0, hy1 := px(dSy0), px(dSy1), px(dSx0), px(dBase), px(dTip), px(dHy0), px(dHy1)
	if size <= 48 { // whole pixels: crisp shafts
		sy0, sy1, sx0, base, tip = math.Round(sy0), math.Round(sy1), math.Round(sx0), math.Round(base), math.Round(tip)
		c := (sy0 + sy1) / 2
		hh := math.Round((hy1 - hy0) / 2)
		hy0, hy1 = c-hh, c+hh
	}
	top := arrowPoly(sy0, sy1, sx0, base, tip, hy0, hy1)
	g.arrows = [2][]pt{top, turn(top, S)}
	g.band = [2][2]float64{{hy0, hy1}, {S - hy1, S - hy0}}
	g.dot = [4]float64{px(37), px(37), 11 * g.k, 9 * g.k}
	return g
}

// polySDF: signed distance to the polygon (positive inside) and the unit
// gradient of that distance (pointing inwards).
func polySDF(p []pt, x, y float64) (float64, float64, float64) {
	best := math.MaxFloat64
	var gx, gy float64
	inside := false
	n := len(p)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		a, b := p[j], p[i]
		ex, ey := b.x-a.x, b.y-a.y
		wx, wy := x-a.x, y-a.y
		t := (wx*ex + wy*ey) / (ex*ex + ey*ey)
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
		dx, dy := wx-ex*t, wy-ey*t
		d := dx*dx + dy*dy
		if d < best {
			best = d
			l := math.Sqrt(d)
			if l > 1e-9 {
				gx, gy = dx/l, dy/l
			} else { // on the edge: the edge normal (sign fixed below)
				el := math.Sqrt(ex*ex + ey*ey)
				gx, gy = ey/el, -ex/el
			}
		}
		if (a.y > y) != (b.y > y) && x < a.x+(y-a.y)*(b.x-a.x)/(b.y-a.y) {
			inside = !inside
		}
	}
	d := math.Sqrt(best)
	if inside {
		return d, gx, gy // away from the edge = inwards
	}
	return -d, -gx, -gy
}

// tileSDF: signed distance to the rounded tile (positive inside) and the
// unit outward direction.
func (g *metalGeom) tileSDF(x, y float64) (float64, float64, float64) {
	S := float64(g.size)
	c := S / 2
	h := c - g.inset - g.rad
	qx, qy := math.Abs(x-c)-h, math.Abs(y-c)-h
	sx, sy := 1.0, 1.0
	if x < c {
		sx = -1
	}
	if y < c {
		sy = -1
	}
	if qx > 0 && qy > 0 {
		l := math.Sqrt(qx*qx + qy*qy)
		return g.rad - l, sx * qx / l, sy * qy / l
	}
	if qx > qy {
		return g.rad - qx, sx, 0
	}
	return g.rad - qy, 0, sy
}

func (g *metalGeom) arrowSDF(x, y float64) (d, gx, gy float64, which int) {
	d, gx, gy = polySDF(g.arrows[0], x, y)
	d2, gx2, gy2 := polySDF(g.arrows[1], x, y)
	if d2 > d {
		return d2, gx2, gy2, 1
	}
	return d, gx, gy, 0
}

func s2l(c uint8) float64 {
	v := float64(c) / 255
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func l2s(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	if v <= 0.0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	return uint8(v*255 + 0.5)
}

type rgb struct{ r, g, b float64 }

func lin(c [3]uint8) rgb { return rgb{s2l(c[0]), s2l(c[1]), s2l(c[2])} }

func (a rgb) mix(b rgb, t float64) rgb {
	return rgb{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
}
func (a rgb) mul(t float64) rgb { return rgb{a.r * t, a.g * t, a.b * t} }
func (a rgb) add(b rgb) rgb     { return rgb{a.r + b.r, a.g + b.g, a.b + b.b} }

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func smooth(e0, e1, x float64) float64 {
	t := clamp01((x - e0) / (e1 - e0))
	return t * t * (3 - 2*t)
}

// key light from the top left (y down), its half vector with the view.
var (
	keyL   = norm3(-0.55, -0.7, 0.6)
	keyH   = norm3(-0.55, -0.7, 1.6)
	rimH   = norm3(0.7, 0.6, 1.35)
	whiteL = rgb{1, 1, 1}
)

func norm3(x, y, z float64) [3]float64 {
	l := math.Sqrt(x*x + y*y + z*z)
	return [3]float64{x / l, y / l, z / l}
}

func dot3(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

// tilt: the normal of a surface leaning by angle th towards (ox, oy).
func tilt(ox, oy, th float64) [3]float64 {
	s, c := math.Sin(th), math.Cos(th)
	return [3]float64{ox * s, oy * s, c}
}

// chromeEnv: the brightness a chrome surface of normal n reflects (bright
// sky above, a dark horizon line, dim ground below).
func chromeEnv(n [3]float64) float64 {
	ry := 2*n[2]*n[1] - 0 // reflected view vector (0,0,1): r = 2(n·z)n − z
	rx := 2 * n[2] * n[0]
	sky := 0.55 + 0.45*clamp01(-ry)
	ground := 0.10 + 0.12*clamp01(ry)
	e := ground + (sky-ground)*smooth(0.12, -0.08, ry)
	e -= 0.10 * math.Exp(-(ry-0.02)*(ry-0.02)/0.002) // horizon line
	return e + 0.08*rx
}

type metalStyle struct {
	arrow, deep, glow rgb
	problem           bool
}

// sample: linear colour and coverage of one sample point.
func (g *metalGeom) sample(st *metalStyle, x, y float64) (rgb, float64) {
	td, ox, oy := g.tileSDF(x, y)
	if td < 0 {
		if !g.shadow {
			return rgb{}, 0
		}
		sd, _, _ := g.tileSDF(x, y-g.k*1.2) // drop shadow below the tile
		a := 0.45 * math.Pow(clamp01(1+sd/(g.k*2.2)), 2)
		return rgb{}, a
	}
	S := float64(g.size)
	u, v := x/S, y/S
	// background: radial depth gradient
	r := math.Sqrt((u-0.45)*(u-0.45) + (v-0.40)*(v-0.40))
	t := math.Pow(clamp01(r/0.75), 1.3)
	col := lin(BgCore).mix(lin(BgEdge), t)
	ad, agx, agy, which := g.arrowSDF(x, y)
	if ad < 0 { // around the arrows: shadow, ambient occlusion, glow, outline
		if g.shadow {
			sd, _, _, _ := g.arrowSDF(x-0.9*g.k, y-1.1*g.k)
			col = col.mul(1 - 0.55*smooth(-1.4*g.k, 0.6*g.k, sd))
			col = col.mul(1 - 0.30*math.Exp(ad/(2.2*g.k)))
		}
		if g.glow > 0 {
			col = col.add(st.glow.mul(g.glow * math.Exp((ad+g.outline)/(1.3*g.k)) * smooth(g.outline, g.outline+1, -ad)))
		}
		if ad > -g.outline {
			col = lin(OutlineDark)
		}
	} else { // the arrow
		b := g.band[which]
		bnd := clamp01((y - b[0]) / (b[1] - b[0]))
		body := st.arrow.mix(st.deep, 0.42*bnd)
		if g.small {
			lit := body.mul(0.98)
			if d, _, _, _ := g.arrowSDF(x, y-1); d < 0 { // 1-px light top edge
				lit = lit.mix(whiteL, 0.22)
			} else if d, _, _, _ := g.arrowSDF(x, y+1); d < 0 { // darker bottom edge
				lit = lit.mix(st.deep, 0.45)
			}
			col = lit
		} else {
			s := clamp01(ad / g.bevel)
			n := tilt(-agx, -agy, (1-s)*(1-s)*0.95)
			diff := clamp01(dot3(n, keyL))
			lit := body.mul(0.55 + 0.75*diff)
			lit = lit.mul(0.93 + 0.12*math.Sin(n[1]*4.0-n[0]*2.0+v*9.0)) // brushed metal
			lit = lit.add(st.arrow.mul(0.18 * clamp01(ad/(3*g.k))))      // inner glow
			spec := math.Pow(clamp01(dot3(n, keyH)), 18)
			rs := math.Pow(clamp01(dot3(n, rimH)), 30)
			lit = lit.add(rgb{1, 0.97, 0.9}.mul(1.3 * spec)).add(rgb{0.5, 0.8, 1.0}.mul(0.30 * rs))
			col = lit
		}
	}
	// chrome frame
	if td < g.frame {
		s := td / g.frame
		var e float64
		if g.small {
			e = 0.92 - 0.62*(y/S) // light top, darker bottom
			if td > 0.75*g.frame {
				e *= 0.8
			}
		} else {
			n := tilt(ox, oy, (0.5-s)*1.7)
			e = chromeEnv(n) + 0.9*math.Pow(clamp01(dot3(n, keyH)), 40)
			if s > 0.86 { // a dark groove on the inside
				e *= 0.35
			}
		}
		col = rgb{0.58, 0.64, 0.78}.mul(e).add(lin(RimBlue).mul(0.22 * e))
	}
	if st.problem { // red dot in a white ring (bottom right)
		dx, dy := x-g.dot[0], y-g.dot[1]
		dd := math.Sqrt(dx*dx + dy*dy)
		if dd <= g.dot[2] {
			col = rgb{0.92, 0.94, 0.97}
			if dd <= g.dot[3] {
				red := lin(ArrowRed)
				col = red.mul(1.05 - 0.35*clamp01(dd/g.dot[3]))
				hx, hy := x-(g.dot[0]-0.33*g.dot[3]), y-(g.dot[1]-0.38*g.dot[3])
				if h := 1 - math.Sqrt(hx*hx+hy*hy)/(0.33*g.dot[3]); h > 0 && !g.small {
					col = col.mix(whiteL, 0.7*h)
				}
			}
		}
	}
	return col, 1
}

func (g *metalGeom) render(st *metalStyle) []uint32 {
	n := g.size
	out := make([]uint32, n*n)
	ss := g.ss
	inv := 1 / float64(ss*ss)
	for py := 0; py < n; py++ {
		for px := 0; px < n; px++ {
			var acc rgb
			var a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := float64(px) + (float64(sx)+0.5)/float64(ss)
					y := float64(py) + (float64(sy)+0.5)/float64(ss)
					c, ca := g.sample(st, x, y)
					acc = acc.add(c.mul(ca))
					a += ca
				}
			}
			if a <= 0 {
				continue
			}
			alpha := a * inv
			c := acc.mul(1 / a)
			out[py*n+px] = argb(uint8(alpha*255+0.5), l2s(c.r), l2s(c.g), l2s(c.b))
		}
	}
	return out
}

type metalKey struct {
	arrow   [3]uint8
	size    int
	problem bool
}

var metalCache struct {
	sync.Mutex
	m map[metalKey][]uint32
}

// metalPixels: the size×size metal icon (top-down premultiplied BGRA)
// with arrows of colour arrow, optionally with the problem dot. Results
// are cached (the render is deterministic); callers get a copy.
func metalPixels(arrow [3]uint8, size int, problem bool) []uint32 {
	k := metalKey{arrow, size, problem}
	metalCache.Lock()
	p, ok := metalCache.m[k]
	metalCache.Unlock()
	if !ok {
		sh, known := arrowShade[arrow]
		if !known { // an unknown colour: darker bottom, same glow
			sh = [2][3]uint8{{arrow[0] * 3 / 5, arrow[1] * 3 / 5, arrow[2] * 3 / 5}, arrow}
		}
		st := metalStyle{arrow: lin(arrow), deep: lin(sh[0]), glow: lin(sh[1]), problem: problem}
		g := geomFor(size)
		p = g.render(&st)
		metalCache.Lock()
		if metalCache.m == nil {
			metalCache.m = map[metalKey][]uint32{}
		}
		metalCache.m[k] = p
		metalCache.Unlock()
	}
	return append([]uint32(nil), p...)
}
