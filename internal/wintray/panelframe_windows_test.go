//go:build windows

package wintray

import (
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// panelShot captures the shown panel (classic WM_PRINT, full content, then
// the screen copy; the first capture with any painted pixel).
func panelShot(t *testing.T, hwnd uintptr) (int32, int32, []byte) {
	t.Helper()
	winapi.ShowWindow(hwnd, winapi.SW_SHOWNOACTIVATE)
	winapi.RedrawWindow(hwnd, winapi.RDW_INVALIDATE|winapi.RDW_ERASE|winapi.RDW_UPDATENOW|winapi.RDW_ALLCHILDREN)
	pumpN(5)
	winapi.DwmFlush()
	for _, capf := range []func(uintptr) (int32, int32, []byte, bool){
		func(hw uintptr) (int32, int32, []byte, bool) { return winapi.CaptureWindowFlags(hw, 0) },
		winapi.CaptureWindow,
		winapi.CaptureScreen,
	} {
		w, h, px, ok := capf(hwnd)
		if !ok {
			continue
		}
		for j := 0; j+3 < len(px); j += 4 {
			if px[j]|px[j+1]|px[j+2] != 0 {
				return w, h, px
			}
		}
	}
	t.Fatal("capture: no painted pixels")
	return 0, 0, nil
}

// TestCHK_TRAY_92_PanelRedFrame — phase 16 A: the non-closable problem
// panel by the clock (real path: ipc.Server status → App) is a speech
// bubble: a window region (rounded body + tail, GetWindowRgn = complex,
// the round corners outside) equal to trayui.PanelCloud of the measured
// body and the real work area (the tray icon is unknown in tests: the tail
// goes down to the work-area edge; the placement next to the icon is
// TestCHK_TRAY_92_CloudGeometry), the window rectangle is the bubble's and
// lies in the work area / on the screen; the frame is thin (3 px logical,
// DPI-scaled, no WS_BORDER) along the inside of the region on the body
// sides and on the tail; its colour is the worst problem — red for an
// error, bright yellow for a warning — and it blinks to black and back in
// the same blinkTick as the icon; the text lies inside the frame; WM_CLOSE
// does not close it.
func TestCHK_TRAY_92_PanelRedFrame(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &alertService{}
	svc.set(1, ptAlert("a", "Склад", "k1", model.LevelError))
	app, srv, _ := startP10App(t, svc, &testBeeper{}, &memSoundStore{})
	pumpUntil(t, "panel", func() bool {
		p := app.Panel()
		return app.Status().State == trayipc.StateConnected && p != nil && p.shown && len(p.lines) == 1 &&
			app.Blink() == trayalert.BlinkRed
	})
	p := app.Panel()
	if !winapi.IsWindow(p.hwnd) {
		t.Fatal("panel not created")
	}
	if st := winapi.WindowStyle(p.hwnd); st&winapi.WS_BORDER != 0 {
		t.Errorf("thin WS_BORDER left: %x", st)
	}
	b := panelBorderPx()
	if panelBorder != 3 || b < 3 || b > px(4)+1 {
		t.Fatalf("frame %d logical / %d px, want 3 (DPI-scaled)", panelBorder, b)
	}
	defer winapi.ShowWindow(p.hwnd, winapi.SW_HIDE)
	// geometry: the window and its region are the bubble
	c := p.cloud
	work, _ := winapi.WorkArea()
	if c.Win.W() <= 0 || c.Side != trayui.TailDown {
		t.Fatalf("cloud %+v", c)
	}
	if wr := winapi.GetWindowRect(p.hwnd); wr.Left != c.Win.L || wr.Top != c.Win.T || wr.Right != c.Win.R || wr.Bottom != c.Win.B {
		t.Errorf("window %+v, bubble %+v", wr, c.Win)
	}
	body := trayui.CloudRect{L: c.Body.L + c.Win.L, T: c.Body.T + c.Win.T, R: c.Body.R + c.Win.L, B: c.Body.B + c.Win.T}
	if body.L < work.Left || body.T < work.Top || body.R > work.Right || body.B > work.Bottom {
		t.Errorf("body %+v outside the work area %+v", body, work)
	}
	if c.Win.B > work.Bottom || c.TipScreen.Y != work.Bottom-1 {
		t.Errorf("tail tip %+v, work bottom %d", c.TipScreen, work.Bottom)
	}
	rgn := winapi.CreateRectRgn(0, 0, 0, 0)
	defer winapi.DeleteObject(rgn)
	if k := winapi.GetWindowRgn(p.hwnd, rgn); k != winapi.COMPLEXREGION {
		t.Fatalf("window region type %d, want complex (bubble)", k)
	}
	if winapi.PtInRegion(rgn, c.Body.L, c.Body.T) || winapi.PtInRegion(rgn, c.Body.R-1, c.Body.T) {
		t.Error("round corners are inside the region")
	}
	if !winapi.PtInRegion(rgn, (c.Body.L+c.Body.R)/2, (c.Body.T+c.Body.B)/2) {
		t.Error("body centre outside the region")
	}
	// the Windows region = the geometry of the previews (trayui.CloudMask),
	// up to the rasterization of the round corners / slanted tail edges
	diff, n := 0, 0
	for y := int32(0); y < c.Win.H(); y++ {
		for x := int32(0); x < c.Win.W(); x++ {
			n++
			if winapi.PtInRegion(rgn, x, y) != c.CloudMask(x, y) {
				diff++
			}
		}
	}
	if diff*100 > n*3 {
		t.Errorf("region differs from trayui.CloudMask in %d of %d pixels", diff, n)
	}
	t.Logf("cloud %+v; region vs CloudMask: %d / %d pixels differ", c, diff, n)

	type probe struct {
		x0, y0, dx, dy int32
		tail           bool
	}
	// frame samples of the current bubble: from the body centre to each
	// side (the bottom one beside the tail base) and down the tail to its
	// tip; the last pixel inside the window region on each line
	samples := func() (trayui.Cloud, []probe, [][2]int32) {
		c := p.cloud
		rg := winapi.CreateRectRgn(0, 0, 0, 0)
		defer winapi.DeleteObject(rg)
		if winapi.GetWindowRgn(p.hwnd, rg) != winapi.COMPLEXREGION {
			t.Fatal("no bubble region")
		}
		cx, cy := (c.Body.L+c.Body.R)/2, (c.Body.T+c.Body.B)/2
		bx := c.Body.L + c.Radius + b + 2 // bottom: left of the tail base
		if bx >= c.Tail[0].X-b {
			bx = c.Body.R - c.Radius - b - 2
		}
		probes := []probe{{cx, cy, -1, 0, false}, {cx, cy, 1, 0, false}, {cx, cy, 0, -1, false}, {bx, cy, 0, 1, false},
			{c.Tail[2].X, cy, 0, 1, true}}
		edges := make([][2]int32, len(probes))
		for i, pr := range probes {
			x, y := pr.x0, pr.y0
			for winapi.PtInRegion(rg, x+pr.dx, y+pr.dy) {
				x, y = x+pr.dx, y+pr.dy
			}
			edges[i] = [2]int32{x, y}
		}
		if e := edges[4]; c.Tail[2].Y-e[1] > 2 {
			t.Errorf("the tail ends at y %d, %d px before its tip %+v", e[1], c.Tail[2].Y-e[1], c.Tail[2])
		}
		return c, probes, edges
	}
	check := func(what string, want uint32) {
		t.Helper()
		c, probes, edges := samples()
		w, h, pix := panelShot(t, p.hwnd)
		if w != c.Win.W() || h != c.Win.H() {
			t.Fatalf("%s: capture %dx%d, bubble %dx%d", what, w, h, c.Win.W(), c.Win.H())
		}
		at := func(x, y int32) uint32 {
			i := (y*w + x) * 4
			return uint32(pix[i+2]) | uint32(pix[i+1])<<8 | uint32(pix[i])<<16 // BGRA → COLORREF
		}
		for i, pr := range probes {
			e := edges[i]
			depth := b
			if pr.tail {
				depth = 1 // the tip is all frame
			}
			for k := int32(0); k < depth; k++ {
				if got := at(e[0]-k*pr.dx, e[1]-k*pr.dy); got != want {
					t.Errorf("%s: frame pixel %d deep at %v (dir %d,%d) = %06x want %06x", what, k, e, pr.dx, pr.dy, got, want)
				}
			}
			if !pr.tail {
				if got := at(e[0]-(b+1)*pr.dx, e[1]-(b+1)*pr.dy); got == want {
					t.Errorf("%s: the frame is thicker than %d px at %v", what, b, e)
				}
			}
		}
	}
	if app.PanelFrameColor() != PanelFrameRGB {
		t.Fatalf("error frame colour %06x", app.PanelFrameColor())
	}
	check("error", PanelFrameRGB)
	// the blink: the same tick switches the icon and the frame
	n0 := p.frameRepaints
	app.blinkTick()
	if app.ShownIcon() != trayui.IconBlank || app.PanelFrameColor() != trayui.PanelFrameBlack || p.frameRepaints != n0+1 {
		t.Fatalf("off phase: icon %v frame %06x repaints %d", app.ShownIcon(), app.PanelFrameColor(), p.frameRepaints-n0)
	}
	check("error, off phase", trayui.PanelFrameBlack)
	app.blinkTick()
	if app.ShownIcon() != trayui.IconError || app.PanelFrameColor() != PanelFrameRGB {
		t.Fatal("on phase again")
	}
	check("error, on again", PanelFrameRGB)
	// text inside the frame
	tr, wr := winapi.GetWindowRect(p.text), winapi.GetWindowRect(p.hwnd)
	if tr.Left-wr.Left < c.Body.L+b || tr.Top-wr.Top < c.Body.T+b || wr.Left+c.Body.R-tr.Right < b || wr.Top+c.Body.B-tr.Bottom < b {
		t.Errorf("text %+v overlaps the frame of the body %+v (window %+v)", tr, c.Body, wr)
	}
	// a warning: bright yellow, blinking to black too
	srv.PublishStatus(svc.set(2, ptAlert("a", "Склад", "k1", model.LevelWarning)))
	pumpUntil(t, "warning", func() bool { return app.Blink() == trayalert.BlinkYellow && p.shown })
	if PanelFrameYellow != 0x0000D8FF || app.PanelFrameColor() != PanelFrameYellow {
		t.Fatalf("warning frame colour %06x", app.PanelFrameColor())
	}
	check("warning", PanelFrameYellow)
	app.blinkTick()
	check("warning, off phase", trayui.PanelFrameBlack)
	app.blinkTick()
	check("warning, on again", PanelFrameYellow)
	if !p.shown || !winapi.IsWindowVisible(p.hwnd) {
		t.Error("the panel was hidden during the capture")
	}
	winapi.SendMessage(p.hwnd, winapi.WM_CLOSE, 0, 0)
	if !winapi.IsWindow(p.hwnd) {
		t.Fatal("panel closed by WM_CLOSE")
	}
}
