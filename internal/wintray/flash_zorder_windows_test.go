//go:build windows

package wintray

import (
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// darkShare is the share of dark pixels (every channel < 64) of a BGRA
// copy, rows [y0, y1) and columns [x0, x1) of a w-wide image.
func darkShare(px []byte, w, x0, y0, x1, y1 int32) float64 {
	n, d := 0, 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := (y*w + x) * 4
			if int(i)+2 >= len(px) {
				continue
			}
			n++
			if px[i] < 64 && px[i+1] < 64 && px[i+2] < 64 {
				d++
			}
		}
	}
	if n == 0 {
		return 1
	}
	return float64(d) / float64(n)
}

// ssWhiteRect: SS_WHITERECT (a STATIC filled with the window color).
const ssWhiteRect = 0x6

func rectsMeet(a, b winapi.RECT) bool {
	return a.Left < b.Right && b.Left < a.Right && a.Top < b.Bottom && b.Top < a.Bottom
}

// TestCHK_TRAY_95_OverlayBelowAlertAndPanel — the overlay really shown
// (not HiddenFlash) next to the shown alert window and the panel by the
// clock: on every blink phase the overlay is below both in the z-order,
// and the screen copy (BitBlt with CAPTUREBLT, layered windows included)
// shows the alert window and the panel unchanged between the black phase
// and the clear phase, while an ordinary window elsewhere is darkened in
// the black phase (the overlay is on the screen).
func TestCHK_TRAY_95_OverlayBelowAlertAndPanel(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &flashService{alertService: &alertService{}, known: map[uint64]bool{7: true}}
	al := ptAlert("p1", "Склад", "pt", model.LevelError)
	al.ID = 7
	svc.set(1, al)
	app, srv := startFlashApp(t, svc, &memFlashStore{on: true}, func(o *Options) {
		o.HiddenAlerts, o.HiddenPanel, o.HiddenFlash = false, false, false
	})
	pumpUntil(t, "alert window + panel shown", func() bool {
		p := app.Panel()
		return app.AlertWindow() != 0 && winapi.IsWindowVisible(app.AlertWindow()) &&
			p != nil && p.shown && winapi.IsWindowVisible(p.hwnd)
	})
	aw, pw := app.AlertWindow(), app.Panel().hwnd
	ar, pr := winapi.GetWindowRect(aw), winapi.GetWindowRect(pw)
	if rectsMeet(ar, pr) {
		t.Logf("alert %+v and panel %+v overlap", ar, pr)
	}
	// an ordinary (not topmost) white window where neither of them is
	work := workArea()
	var ref uintptr
	for _, pos := range [][2]int32{{work.Left + 10, work.Top + 10}, {work.Right - 210, work.Top + 10}, {work.Left + 10, work.Bottom - 170}} {
		r := winapi.RECT{Left: pos[0], Top: pos[1], Right: pos[0] + 200, Bottom: pos[1] + 160}
		if rectsMeet(r, ar) || rectsMeet(r, pr) {
			continue
		}
		h, err := winapi.CreateWindowEx(0, "STATIC", "", winapi.WS_POPUP|winapi.WS_VISIBLE|ssWhiteRect,
			r.Left, r.Top, 200, 160, 0, 0, winapi.GetModuleHandle())
		if err != nil || h == 0 {
			t.Fatalf("reference window: %v", err)
		}
		ref = h
		break
	}
	if ref == 0 {
		t.Fatal("no free place for the reference window")
	}
	defer winapi.DestroyWindow(ref)
	winapi.SetWindowPos(ref, 0, 0, 0, 0, 0, winapi.SWP_NOMOVE|winapi.SWP_NOSIZE|winapi.SWP_NOACTIVATE|winapi.SWP_SHOWWINDOW)
	pumpN(6)

	srv.Flash(model.FlashEvent{ProfileID: "p1", PointKey: "pt", EventID: 7, Level: model.LevelError})
	pumpUntil(t, "flash", func() bool { return app.Flash() != nil && app.Flash().timer })
	fw := app.Flash()
	defer app.stopFlash()

	type shot struct {
		w, h int32
		px   []byte
	}
	grab := func(what string, hw uintptr) shot {
		t.Helper()
		w, h, px, ok := winapi.CaptureScreenRect(winapi.GetWindowRect(hw))
		if !ok {
			t.Fatalf("%s: screen copy failed", what)
		}
		return shot{w, h, px}
	}
	// the body of the alert window (its title band blinks on its own) and
	// the inside of the panel frame
	alertBody := func(s shot) float64 { return darkShare(s.px, s.w, 0, s.h/2, s.w, s.h) }
	b := panelBorderPx() + 2
	panelIn := func(s shot) float64 { return darkShare(s.px, s.w, b, b, s.w-b, s.h-b) }
	refAll := func(s shot) float64 { return darkShare(s.px, s.w, 4, 4, s.w-4, s.h-4) }

	for cycle := 0; cycle < 2; cycle++ {
		pumpUntil(t, "black phase", func() bool { return fw.on })
		winapi.DwmFlush()
		if !winapi.IsWindowVisible(fw.hwnd) {
			t.Fatal("overlay not shown in the black phase")
		}
		if !winapi.IsAbove(aw, fw.hwnd) || !winapi.IsAbove(pw, fw.hwnd) {
			t.Fatalf("cycle %d: overlay above the alert window (%v) or the panel (%v)", cycle,
				!winapi.IsAbove(aw, fw.hwnd), !winapi.IsAbove(pw, fw.hwnd))
		}
		if winapi.WindowExStyle(fw.hwnd)&winapi.WS_EX_TOPMOST == 0 {
			t.Error("overlay lost topmost")
		}
		aOn, pOn, rOn := grab("alert on", aw), grab("panel on", pw), grab("ref on", ref)
		pumpUntil(t, "clear phase", func() bool { return !fw.on })
		winapi.DwmFlush()
		aOff, pOff, rOff := grab("alert off", aw), grab("panel off", pw), grab("ref off", ref)
		if d := refAll(rOn); d < 0.8 {
			t.Fatalf("cycle %d: the overlay is not on the screen (reference dark %.2f in the black phase)", cycle, d)
		}
		if d := refAll(rOff); d > 0.2 {
			t.Fatalf("cycle %d: reference dark %.2f in the clear phase", cycle, d)
		}
		if on, off := alertBody(aOn), alertBody(aOff); on > 0.6 || on-off > 0.05 {
			t.Fatalf("cycle %d: the overlay covers the alert window (dark %.2f black phase, %.2f clear)", cycle, on, off)
		}
		if on, off := panelIn(pOn), panelIn(pOff); on > 0.6 || on-off > 0.05 {
			t.Fatalf("cycle %d: the overlay covers the panel (dark %.2f black phase, %.2f clear)", cycle, on, off)
		}
	}
}

// TestCHK_TRAY_95_FlashSwitchOffAtOnce — «Мигание экрана» off stops a
// running flash at once: the checkbox on «Общие» of the settings window
// (saveScreenFlash → the tray), and the TrayReloadPrefs message
// (reloadTrayPrefs: the switch changed by the elevated settings process);
// on again resumes it while the flash is still active.
func TestCHK_TRAY_95_FlashSwitchOffAtOnce(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &flashService{alertService: &alertService{}, known: map[uint64]bool{7: true}}
	store := &memFlashStore{on: true}
	app, srv := startFlashApp(t, svc, store, func(o *Options) { o.HiddenSettings = true })
	srv.Flash(model.FlashEvent{ProfileID: "p1", EventID: 7, Level: model.LevelError})
	pumpUntil(t, "flash", func() bool { return app.Flash() != nil && app.Flash().timer })
	fw := app.Flash()

	if app.OpenSettings() == 0 || app.settings == nil {
		t.Fatal("settings window")
	}
	w := app.settings
	pumpUntil(t, "settings loaded", func() bool { return !w.busy })
	w.setPage(1)
	pumpN(4)
	if !w.check(idGenFlash) {
		t.Fatal("«Мигание экрана» must start on")
	}
	w.click(t, idGenFlash) // off
	if on, _ := store.Load(); on {
		t.Fatal("switch not saved")
	}
	if fw.timer || fw.on || app.FlashActive() != 1 {
		t.Fatalf("off in the settings did not stop the flash at once: timer %v on %v active %d", fw.timer, fw.on, app.FlashActive())
	}
	w.click(t, idGenFlash) // on: the flash is still active → resumes
	if !fw.timer {
		t.Fatal("on in the settings did not resume the active flash")
	}

	// the elevated settings process saved «off» and broadcast TrayReloadPrefs
	store.Save(false)
	winapi.PostMessage(app.hwnd, trayReloadPrefsMsg(), 0, 0)
	pumpUntil(t, "reload prefs stops", func() bool { return !fw.timer && !fw.on })
	if app.FlashActive() != 1 {
		t.Errorf("active flashes %d", app.FlashActive())
	}
	store.Save(true)
	winapi.PostMessage(app.hwnd, trayReloadPrefsMsg(), 0, 0)
	pumpUntil(t, "reload prefs resumes", func() bool { return fw.timer })
}
