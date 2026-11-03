//go:build windows

package wintray

import (
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_92_PanelRedFrame — the non-closable error panel by the
// clock has a very thick bright red frame (panelBorder logical px, no thin
// WS_BORDER): captured pixels of all four bands are PanelFrameRGB, the
// text sits inside the frame, WM_CLOSE still does not close it.
func TestCHK_TRAY_92_PanelRedFrame(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _, _ := startP10App(t, &alertService{}, &testBeeper{}, &memSoundStore{})
	app.problems = []trayalert.Problem{{Kind: trayalert.ProblemError, Text: "Склад / Exchange — ЦБ: 45 мин без обмена"}}
	app.updatePanel()
	p := app.Panel()
	if p == nil || !winapi.IsWindow(p.hwnd) {
		t.Fatal("panel not created")
	}
	if st := winapi.WindowStyle(p.hwnd); st&winapi.WS_BORDER != 0 {
		t.Errorf("thin WS_BORDER left: %x", st)
	}
	if panelBorder < 6 || panelBorderPx() < panelBorder {
		t.Fatalf("frame %d / %d px is not very thick", panelBorder, panelBorderPx())
	}
	winapi.ShowWindow(p.hwnd, winapi.SW_SHOWNOACTIVATE)
	defer winapi.ShowWindow(p.hwnd, winapi.SW_HIDE)
	winapi.RedrawWindow(p.hwnd, winapi.RDW_INVALIDATE|winapi.RDW_ERASE|winapi.RDW_UPDATENOW|winapi.RDW_ALLCHILDREN)
	pumpN(5)
	winapi.DwmFlush()
	w, h, px, ok := winapi.CaptureWindow(p.hwnd)
	if !ok {
		t.Fatal("capture")
	}
	at := func(x, y int32) uint32 {
		i := (y*w + x) * 4
		return uint32(px[i+2]) | uint32(px[i+1])<<8 | uint32(px[i])<<16 // BGRA → COLORREF
	}
	b := panelBorderPx()
	for _, pt := range [][2]int32{{1, 1}, {b - 1, h / 2}, {w - 2, h / 2}, {w / 2, h - 2}, {w / 2, b - 1}, {w - b, h - b}} {
		if c := at(pt[0], pt[1]); c != PanelFrameRGB {
			t.Errorf("frame pixel %v = %06x want %06x", pt, c, PanelFrameRGB)
		}
	}
	if c := at(b+2, b+2); c == PanelFrameRGB {
		t.Errorf("inside the frame is red too")
	}
	tr := winapi.GetWindowRect(p.text)
	wr := winapi.GetWindowRect(p.hwnd)
	if tr.Left-wr.Left < b || tr.Top-wr.Top < b || wr.Right-tr.Right < b || wr.Bottom-tr.Bottom < b {
		t.Errorf("text %+v overlaps the frame of %+v", tr, wr)
	}
	winapi.SendMessage(p.hwnd, winapi.WM_CLOSE, 0, 0)
	if !winapi.IsWindow(p.hwnd) {
		t.Fatal("panel closed by WM_CLOSE")
	}
}
