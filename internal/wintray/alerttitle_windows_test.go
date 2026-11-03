//go:build windows

package wintray

import (
	"runtime"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_93_AlertTitleBlink — the title strip of the alert window
// blinks bright red / black fast (alertTitleBlinkMs) while the window is
// shown: the captured strip pixel alternates AlertTitleRed / black, the
// real WM_TIMER flips it, white bold title text, and hiding stops it.
func TestCHK_TRAY_93_AlertTitleBlink(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	aw, err := newAlertWindow(0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer winapi.DestroyWindow(aw.hwnd)
	if alertTitleBlinkMs > 300 {
		t.Fatalf("blink %d ms is not fast", alertTitleBlinkMs)
	}
	v := trayalert.View{Visible: true, Level: model.LevelError}
	aw.apply(v, false)
	if !aw.titleBlink || aw.TitleColor() != AlertTitleRed {
		t.Fatalf("blink not started: %v %06x", aw.titleBlink, aw.TitleColor())
	}
	if got := winapi.GetWindowText(aw.title); got == "" || got != trayalert.Title(v) {
		t.Errorf("strip text %q", got)
	}
	winapi.ShowWindow(aw.hwnd, winapi.SW_SHOWNOACTIVATE)
	strip := func() uint32 {
		winapi.RedrawWindow(aw.hwnd, winapi.RDW_INVALIDATE|winapi.RDW_ERASE|winapi.RDW_UPDATENOW|winapi.RDW_ALLCHILDREN)
		pumpN(3)
		winapi.DwmFlush()
		w, _, px, ok := winapi.CaptureWindow(aw.hwnd)
		if !ok {
			t.Fatal("capture")
		}
		wr, tr := winapi.GetWindowRect(aw.hwnd), winapi.GetWindowRect(aw.title)
		x, y := tr.Left-wr.Left+2, tr.Top-wr.Top+2
		i := (y*w + x) * 4
		return uint32(px[i+2]) | uint32(px[i+1])<<8 | uint32(px[i])<<16
	}
	if c := strip(); c != AlertTitleRed {
		t.Errorf("red phase pixel %06x", c)
	}
	aw.titleTick()
	if c := strip(); c != AlertTitleBlack {
		t.Errorf("black phase pixel %06x", c)
	}
	n := aw.titleFlips
	pumpFor(4 * alertTitleBlinkMs * time.Millisecond)
	if aw.titleFlips-n < 2 {
		t.Errorf("WM_TIMER flips %d", aw.titleFlips-n)
	}
	aw.apply(trayalert.View{Visible: false}, false)
	if aw.titleBlink {
		t.Error("hidden window still blinks")
	}
}
