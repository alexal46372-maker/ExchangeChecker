package trayui

// Phase 16 A (CHK-TRAY-92): the problem panel by the clock is a comic
// speech bubble — a rounded rectangle (the body) with a tail whose tip
// touches the tray icon. PanelCloud is the pure geometry the tray window
// uses for its region (CreateRoundRectRgn + CreatePolygonRgn, SetWindowRgn)
// and its frame (FrameRgn); CloudMask / CloudFrame rasterize the same
// geometry for the tests and the previews.

// CloudRect is a pixel rectangle (right / bottom exclusive).
type CloudRect struct{ L, T, R, B int32 }

// W and H are the width and the height.
func (r CloudRect) W() int32 { return r.R - r.L }
func (r CloudRect) H() int32 { return r.B - r.T }

// Contains reports whether the pixel (x, y) is inside.
func (r CloudRect) Contains(x, y int32) bool { return x >= r.L && x < r.R && y >= r.T && y < r.B }

// CloudPt is a pixel point.
type CloudPt struct{ X, Y int32 }

// TailSide is where the tail points (towards the icon).
type TailSide int

// Tail sides: down (taskbar at the bottom), up (top), left, right.
const (
	TailDown TailSide = iota
	TailUp
	TailLeft
	TailRight
)

// CloudMetrics are the pixel sizes of the bubble (DPI-scaled by the caller).
type CloudMetrics struct {
	Tail     int32 // minimum tail length (body edge → tip)
	TailHalf int32 // half width of the tail base
	Radius   int32 // corner radius of the body
	Gap      int32 // distance from the work-area corner when the icon is unknown
}

// Cloud is the placed bubble.
type Cloud struct {
	Win    CloudRect  // screen rectangle of the window
	Body   CloudRect  // the body, window coordinates
	Tail   [3]CloudPt // tail base 1, base 2, tip (window coordinates)
	Side   TailSide
	Radius int32
	// TipScreen is the tail tip on the screen (on the icon edge).
	TipScreen CloudPt
}

func clamp32(v, lo, hi int32) int32 {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// PanelCloud places a body of bodyW × bodyH pixels right next to the tray
// icon (screen rectangle icon, iconOK = known) inside the work area, with
// the tail tip on the icon edge facing the body (the bubble touches the
// icon), so nothing is cut at the screen edges: the body is clamped into the
// work area and the tail base slides along the straight part of the body
// edge while the tip stays on the icon centre line. An unknown icon: the
// bottom-right corner of the work area, the tail down to its edge.
func PanelCloud(bodyW, bodyH int32, icon CloudRect, iconOK bool, work CloudRect, m CloudMetrics) Cloud {
	c := Cloud{Radius: m.Radius}
	var side TailSide
	var tip CloudPt
	margin := int32(0) // next to a known icon the body may reach the work-area edge
	if !iconOK || icon.W() <= 0 || icon.H() <= 0 {
		side, margin = TailDown, m.Gap
		tip = CloudPt{work.R - m.Gap - m.Radius - 2*m.TailHalf, work.B - 1}
	} else {
		cx, cy := icon.L+icon.W()/2, icon.T+icon.H()/2
		switch {
		case icon.T >= work.B-2:
			side = TailDown
		case icon.B <= work.T+2:
			side = TailUp
		case icon.L >= work.R-2:
			side = TailRight
		case icon.R <= work.L+2:
			side = TailLeft
		case cy > (work.T+work.B)/2: // icon inside the work area (overflow, auto-hide)
			side = TailDown
		default:
			side = TailUp
		}
		switch side {
		case TailDown:
			tip = CloudPt{cx, icon.T}
		case TailUp:
			tip = CloudPt{cx, icon.B - 1}
		case TailRight:
			tip = CloudPt{icon.L, cy}
		case TailLeft:
			tip = CloudPt{icon.R - 1, cy}
		}
	}
	c.Side = side
	var body CloudRect
	var base1, base2 CloudPt
	slide := func(center, lo, hi int32) int32 { // tail base centre on the straight edge
		a, b := lo+m.Radius+m.TailHalf, hi-m.Radius-m.TailHalf
		if b < a {
			return (lo + hi) / 2
		}
		return clamp32(center, a, b)
	}
	switch side {
	case TailDown, TailUp:
		l := clamp32(tip.X-bodyW/2, work.L+margin, work.R-margin-bodyW)
		if side == TailDown {
			b := tip.Y - m.Tail + 1
			if b > work.B {
				b = work.B
			}
			body = CloudRect{l, b - bodyH, l + bodyW, b}
			bx := slide(tip.X, body.L, body.R)
			base1, base2 = CloudPt{bx - m.TailHalf, body.B - 2}, CloudPt{bx + m.TailHalf, body.B - 2}
		} else {
			t := tip.Y + m.Tail
			if t < work.T {
				t = work.T
			}
			body = CloudRect{l, t, l + bodyW, t + bodyH}
			bx := slide(tip.X, body.L, body.R)
			base1, base2 = CloudPt{bx - m.TailHalf, body.T + 1}, CloudPt{bx + m.TailHalf, body.T + 1}
		}
	default:
		t := clamp32(tip.Y-bodyH/2, work.T+margin, work.B-margin-bodyH)
		if side == TailRight {
			r := tip.X - m.Tail + 1
			if r > work.R {
				r = work.R
			}
			body = CloudRect{r - bodyW, t, r, t + bodyH}
			by := slide(tip.Y, body.T, body.B)
			base1, base2 = CloudPt{body.R - 2, by - m.TailHalf}, CloudPt{body.R - 2, by + m.TailHalf}
		} else {
			l := tip.X + m.Tail
			if l < work.L {
				l = work.L
			}
			body = CloudRect{l, t, l + bodyW, t + bodyH}
			by := slide(tip.Y, body.T, body.B)
			base1, base2 = CloudPt{body.L + 1, by - m.TailHalf}, CloudPt{body.L + 1, by + m.TailHalf}
		}
	}
	win := body
	for _, p := range []CloudPt{base1, base2, tip} {
		if p.X < win.L {
			win.L = p.X
		}
		if p.X+1 > win.R {
			win.R = p.X + 1
		}
		if p.Y < win.T {
			win.T = p.Y
		}
		if p.Y+1 > win.B {
			win.B = p.Y + 1
		}
	}
	c.Win = win
	c.TipScreen = tip
	loc := func(p CloudPt) CloudPt { return CloudPt{p.X - win.L, p.Y - win.T} }
	c.Body = CloudRect{body.L - win.L, body.T - win.T, body.R - win.L, body.B - win.T}
	c.Tail = [3]CloudPt{loc(base1), loc(base2), loc(tip)}
	return c
}

// inRoundRect: the pixel centre (x+0.5, y+0.5) in the rounded rectangle
// (corner ellipses of radius r).
func inRoundRect(b CloudRect, r int32, x, y int32) bool {
	if !b.Contains(x, y) {
		return false
	}
	fx, fy := float64(x)+0.5, float64(y)+0.5
	rr := float64(r)
	cx, cy := fx, fy
	switch {
	case fx < float64(b.L)+rr:
		cx = float64(b.L) + rr
	case fx > float64(b.R)-rr:
		cx = float64(b.R) - rr
	}
	switch {
	case fy < float64(b.T)+rr:
		cy = float64(b.T) + rr
	case fy > float64(b.B)-rr:
		cy = float64(b.B) - rr
	}
	dx, dy := fx-cx, fy-cy
	return dx*dx+dy*dy <= rr*rr
}

// inTriangle: the pixel centre inside the triangle (either winding).
func inTriangle(t [3]CloudPt, x, y int32) bool {
	px, py := float64(x)+0.5, float64(y)+0.5
	sign := func(a, b CloudPt) float64 {
		return (px-float64(b.X))*(float64(a.Y)-float64(b.Y)) - (float64(a.X)-float64(b.X))*(py-float64(b.Y))
	}
	if (t[1].X-t[0].X)*(t[2].Y-t[0].Y)-(t[1].Y-t[0].Y)*(t[2].X-t[0].X) == 0 {
		return false // degenerate
	}
	d1, d2, d3 := sign(t[0], t[1]), sign(t[1], t[2]), sign(t[2], t[0])
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

// CloudMask reports whether the window pixel (x, y) belongs to the bubble.
func (c Cloud) CloudMask(x, y int32) bool {
	return inRoundRect(c.Body, c.Radius, x, y) || inTriangle(c.Tail, x, y)
}

// CloudFrame reports whether the window pixel (x, y) is on the frame of
// width b (FrameRgn: region pixels with an outside pixel within b-1 pixels
// horizontally or vertically).
func (c Cloud) CloudFrame(x, y, b int32) bool {
	if !c.CloudMask(x, y) {
		return false
	}
	for k := int32(1); k <= b; k++ {
		if !c.CloudMask(x-k, y) || !c.CloudMask(x+k, y) || !c.CloudMask(x, y-k) || !c.CloudMask(x, y+k) {
			return true
		}
	}
	return false
}

// Frame colours of the panel (COLORREF 0x00BBGGRR): the worst problem —
// bright red for an error / no connection, bright yellow for a warning;
// black in the «off» phase of the icon blink (the frame blinks with it).
const (
	PanelFrameRed    = 0x00303BFF // #FF3B30
	PanelFrameYellow = 0x0000D8FF // #FFD800
	PanelFrameBlack  = 0x00000000
)

// Logical sizes of the bubble (96 dpi; the tray scales them): the frame
// 3 px (phase 16: instead of 8), the tail, the corner radius, the margin.
const (
	PanelBorder   = 3
	PanelTail     = 14
	PanelTailHalf = 10
	PanelRadius   = 12
	PanelGap      = 8
)

// PanelFrameColor is the frame colour: red = the worst problem is an error
// (else yellow); off = the «off» blink phase (black).
func PanelFrameColor(red, off bool) uint32 {
	switch {
	case off:
		return PanelFrameBlack
	case red:
		return PanelFrameRed
	}
	return PanelFrameYellow
}
