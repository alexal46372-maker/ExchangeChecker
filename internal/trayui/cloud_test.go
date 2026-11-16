package trayui

import "testing"

// TestCHK_TRAY_92_CloudGeometry — phase 16 A: the speech-bubble panel by
// the clock, pure geometry (the tray uses it for SetWindowRgn / FrameRgn).
// For a taskbar at the bottom / top / left / right, the icon in the middle
// and at both ends of the screen, 96 and 144 dpi metrics, and an unknown
// icon: the body lies in the work area (nothing cut off), the window covers
// body + tail, the tail tip is on the icon edge facing the body and on the
// icon centre line (the bubble touches the icon), the tail is at least
// Tail long, its base lies on the straight part of the body edge (not on a
// round corner) and the mask is filled near the tip; the frame is exactly b
// pixels deep at the body sides; the colours are red / yellow / black.
func TestCHK_TRAY_92_CloudGeometry(t *testing.T) {
	const sw, sh = 1366, 768
	for _, scale := range []int32{96, 144} {
		s := func(v int32) int32 { return (v*scale + 48) / 96 }
		m := CloudMetrics{Tail: s(PanelTail), TailHalf: s(PanelTailHalf), Radius: s(PanelRadius), Gap: s(PanelGap)}
		bw, bh := s(380), s(64)
		type tc struct {
			name string
			work CloudRect
			icon CloudRect
			ok   bool
			side TailSide
		}
		var cases []tc
		for _, x := range []int32{sw - s(24), sw / 2, s(4)} {
			cases = append(cases,
				tc{"bottom", CloudRect{0, 0, sw, sh - s(40)}, CloudRect{x - s(12), sh - s(32), x + s(12), sh - s(8)}, true, TailDown},
				tc{"top", CloudRect{0, s(40), sw, sh}, CloudRect{x - s(12), s(8), x + s(12), s(32)}, true, TailUp})
		}
		for _, y := range []int32{sh - s(24), sh / 2, s(10)} {
			cases = append(cases,
				tc{"right", CloudRect{0, 0, sw - s(60), sh}, CloudRect{sw - s(42), y - s(12), sw - s(18), y + s(12)}, true, TailRight},
				tc{"left", CloudRect{s(60), 0, sw, sh}, CloudRect{s(18), y - s(12), s(42), y + s(12)}, true, TailLeft})
		}
		cases = append(cases, tc{"unknown", CloudRect{0, 0, sw, sh - s(40)}, CloudRect{}, false, TailDown})
		for _, k := range cases {
			c := PanelCloud(bw, bh, k.icon, k.ok, k.work, m)
			tag := k.name
			if c.Side != k.side {
				t.Fatalf("%s %d: side %d want %d", tag, scale, c.Side, k.side)
			}
			body := CloudRect{c.Body.L + c.Win.L, c.Body.T + c.Win.T, c.Body.R + c.Win.L, c.Body.B + c.Win.T}
			if body.W() != bw || body.H() != bh || body.L < k.work.L || body.T < k.work.T || body.R > k.work.R || body.B > k.work.B {
				t.Errorf("%s %d: body %+v outside work %+v", tag, scale, body, k.work)
			}
			if c.Win.L < 0 || c.Win.T < 0 || c.Win.R > sw || c.Win.B > sh {
				t.Errorf("%s %d: window %+v off the screen", tag, scale, c.Win)
			}
			for _, p := range c.Tail {
				if p.X < 0 || p.Y < 0 || p.X >= c.Win.W() || p.Y >= c.Win.H() {
					t.Errorf("%s %d: tail point %+v outside the window %+v", tag, scale, p, c.Win)
				}
			}
			tip := c.TipScreen
			if tip.X != c.Tail[2].X+c.Win.L || tip.Y != c.Tail[2].Y+c.Win.T {
				t.Errorf("%s %d: tip %+v / %+v", tag, scale, tip, c.Tail[2])
			}
			cx, cy := k.icon.L+k.icon.W()/2, k.icon.T+k.icon.H()/2
			var long int32
			switch {
			case !k.ok:
				if tip.Y != k.work.B-1 || body.R > k.work.R-m.Gap {
					t.Errorf("%s %d: tip %+v body %+v (bottom-right corner, gap %d)", tag, scale, tip, body, m.Gap)
				}
				long = tip.Y - (body.B - 1)
			case k.side == TailDown:
				if tip.Y != k.icon.T || tip.X != cx {
					t.Errorf("%s %d: tip %+v not on the top edge centre of icon %+v", tag, scale, tip, k.icon)
				}
				long = tip.Y - (body.B - 1)
			case k.side == TailUp:
				if tip.Y != k.icon.B-1 || tip.X != cx {
					t.Errorf("%s %d: tip %+v not on the bottom edge of icon %+v", tag, scale, tip, k.icon)
				}
				long = body.T - tip.Y
			case k.side == TailRight:
				if tip.X != k.icon.L || tip.Y != cy {
					t.Errorf("%s %d: tip %+v not on the left edge of icon %+v", tag, scale, tip, k.icon)
				}
				long = tip.X - (body.R - 1)
			case k.side == TailLeft:
				if tip.X != k.icon.R-1 || tip.Y != cy {
					t.Errorf("%s %d: tip %+v not on the right edge of icon %+v", tag, scale, tip, k.icon)
				}
				long = body.L - tip.X
			}
			if long < m.Tail-1 {
				t.Errorf("%s %d: tail %d px < %d", tag, scale, long, m.Tail)
			}
			// the base on the straight part of the edge
			b0, b1 := c.Tail[0], c.Tail[1]
			if k.side == TailDown || k.side == TailUp {
				if b0.X < c.Body.L+m.Radius || b1.X > c.Body.R-m.Radius || b1.X-b0.X != 2*m.TailHalf {
					t.Errorf("%s %d: tail base %+v %+v on a round corner of %+v", tag, scale, b0, b1, c.Body)
				}
			} else if b0.Y < c.Body.T+m.Radius || b1.Y > c.Body.B-m.Radius || b1.Y-b0.Y != 2*m.TailHalf {
				t.Errorf("%s %d: tail base %+v %+v on a round corner of %+v", tag, scale, b0, b1, c.Body)
			}
			// filled up to ≤ 2 px from the tip
			found := false
			tp := c.Tail[2]
			for dy := int32(-2); dy <= 2 && !found; dy++ {
				for dx := int32(-2); dx <= 2; dx++ {
					if c.CloudMask(tp.X+dx, tp.Y+dy) {
						found = true
						break
					}
				}
			}
			if !found {
				t.Errorf("%s %d: the tail is empty near its tip %+v", tag, scale, tp)
			}
			if c.CloudMask(c.Body.L, c.Body.T) || c.CloudMask(c.Body.R-1, c.Body.B-1) {
				t.Errorf("%s %d: square corners", tag, scale)
			}
			// frame depth b at the body sides
			// frame depth b at the body sides without the tail
			b := s(PanelBorder)
			mx, my := (c.Body.L+c.Body.R)/2, (c.Body.T+c.Body.B)/2
			at := func(k int32) [2][2]int32 { // two opposite edge pixels k deep
				if k2 := k; c.Side == TailDown || c.Side == TailUp {
					return [2][2]int32{{c.Body.L + k2, my}, {c.Body.R - 1 - k2, my}}
				}
				return [2][2]int32{{mx, c.Body.T + k}, {mx, c.Body.B - 1 - k}}
			}
			for k := int32(0); k < b; k++ {
				for _, q := range at(k) {
					if !c.CloudFrame(q[0], q[1], b) {
						t.Errorf("%s %d: frame missing %d px deep at %v", tag, scale, k, q)
					}
				}
			}
			for _, q := range at(b) {
				if c.CloudFrame(q[0], q[1], b) {
					t.Errorf("%s %d: frame deeper than %d at %v", tag, scale, b, q)
				}
			}
		}
	}
	if PanelBorder != 3 || PanelFrameColor(true, false) != PanelFrameRed || PanelFrameColor(false, false) != PanelFrameYellow ||
		PanelFrameColor(true, true) != PanelFrameBlack || PanelFrameColor(false, true) != PanelFrameBlack {
		t.Error("frame width / colours")
	}
	if PanelFrameYellow != 0x0000D8FF || PanelFrameRed != 0x00303BFF {
		t.Error("COLORREF values")
	}
}
