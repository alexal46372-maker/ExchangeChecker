//go:build windows

package wintray

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// smCYMaxTrack: SM_CYMAXTRACK of GetSystemMetrics.
const smCYMaxTrack = 60

// openModeSettings opens the settings window (admin) in uiMode, like
// openChromeSettings.
func openModeSettings(t *testing.T, app *App, dpi int32, mode traysettings.UIMode) *settingsWindow {
	t.Helper()
	testDPI = dpi
	w, err := newSettingsWindow(settingsConfig{mode: traysettings.Admin, session: app.session, hidden: true, uiMode: mode})
	if err != nil {
		t.Fatal(err)
	}
	w.Reload()
	pumpUntil(t, "settings loaded", func() bool {
		w.update()
		return !w.busy && w.ed.HasProfile() && winapi.TabItemCount(w.ctl[idSetTabs]) >= 2
	})
	return w
}

// settingsPages: the tabs of the current mode.
func settingsPages(w *settingsWindow) []int {
	if w.uiMode == traysettings.UISimple {
		return []int{0, 1}
	}
	return chromePages(w)
}

// checkButtonsVisible — every shown push button of the settings window:
// footer buttons inside the client; the DB list chrome (pinned, not
// scrolled) inside the host view as it is; page buttons inside the view
// once scrolled to (scrollIntoView). Returns the checked buttons.
func checkButtonsVisible(t *testing.T, tag string, w *settingsWindow) map[uintptr]bool {
	t.Helper()
	ox, oy := winapi.ClientOrigin(w.hwnd)
	cr := winapi.GetClientRect(w.hwnd)
	client := winapi.RECT{Left: ox, Top: oy, Right: ox + cr.Right, Bottom: oy + cr.Bottom}
	checked := map[uintptr]bool{}
	var walk func(h uintptr, underHost bool)
	walk = func(parent uintptr, underHost bool) {
		for _, h := range winapi.Children(parent) {
			if winapi.WindowStyle(h)&winapi.WS_VISIBLE == 0 {
				continue
			}
			inHost := underHost || h == w.host
			if winapi.GetClassName(h) == "Button" {
				if bt := winapi.WindowStyle(h) & winapi.BS_TYPEMASK; bt == 0 || bt == 1 { // push buttons
					checked[h] = true
					if inHost && !w.pinnedHost[h] {
						w.scrollIntoView(h)
					}
					hx, hy := winapi.ClientOrigin(w.host)
					view := winapi.RECT{Left: hx, Top: hy, Right: hx + w.viewW, Bottom: hy + w.viewH}
					r := winapi.GetWindowRect(h)
					area, what := client, "client"
					if inHost {
						area, what = view, "host view"
					}
					if r.Bottom > area.Bottom || r.Top < area.Top || r.Right > area.Right || r.Left < area.Left || r.Bottom-r.Top <= 0 {
						t.Errorf("%s: button %q %+v outside the %s %+v (pinned %v, scroll %d)", tag, winapi.GetWindowText(h), r, what, area, w.pinnedHost[h], w.scrollY)
					}
					if inHost && !w.pinnedHost[h] {
						w.scrollTo(0, 0)
					}
				}
			}
			walk(h, inHost)
		}
	}
	walk(w.hwnd, false)
	return checked
}

// TestCHK_TRAY_110_SettingsButtonsVisible — phase 17 (Alex 17.11.2026):
// the settings window has no fixed height cap any more (phase 16 G hid
// the buttons under the DB list in expert mode); it is as high as its
// tallest tab, limited only by the work area. Every tab in simple and
// expert mode (both opened that way and switched with «Режим»), at 96 and
// 120 dpi, on a 1920×1080 and a 1024×768 work area (40 px taskbar): the
// bottom edge of every shown push button is inside the client — the
// footer as is, «Добавить» / «Удалить» / «Клонировать» / «Найти серверы…»
// under the DB list inside the host view without scrolling (the list gets
// shorter when the view is low), page buttons once scrolled to.
func TestCHK_TRAY_110_SettingsButtonsVisible(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() { testDPI = 0; testWorkArea = nil }()
	app, _ := startChromeApp(t)
	defer app.Exit()
	works := []winapi.RECT{{Right: 1920, Bottom: 1040}, {Right: 1024, Bottom: 728}}
	for _, dpi := range []int32{96, 120} {
		for _, wa := range works {
			work := wa
			testWorkArea = &work
			for _, mode := range []traysettings.UIMode{traysettings.UIExpert, traysettings.UISimple} {
				w := openModeSettings(t, app, dpi, mode)
				for pass := 0; pass < 2; pass++ {
					tag := fmt.Sprintf("%d dpi %dx%d mode %v pass %d", dpi, work.Right, work.Bottom+40, w.uiMode, pass)
					r := winapi.GetWindowRect(w.hwnd)
					if !contains(work, r) {
						t.Errorf("%s: window %+v outside the work area", tag, r)
					}
					for _, p := range settingsPages(w) {
						w.setPage(p)
						w.layoutFit()
						got := checkButtonsVisible(t, fmt.Sprintf("%s page %d", tag, p), w)
						if p == 0 {
							ids := []int32{idSetAdd, idSetDelete, idSetSave, idSetClose}
							if w.uiMode == traysettings.UIExpert {
								ids = append(ids, idSetDiscover)
							}
							for _, id := range ids {
								if h := w.ctl[id]; h == 0 || !got[h] {
									t.Errorf("%s page 0: button %d not shown / not checked", tag, id)
								}
							}
							if lr := winapi.GetWindowRect(w.ctl[idSetList]); lr.Bottom-lr.Top < px(pinnedListMinH)-1 {
								t.Errorf("%s: DB list only %d px high", tag, lr.Bottom-lr.Top)
							}
						}
					}
					w.toggleUIMode() // the other mode in the same window
					pumpN(3)
				}
				w.close()
			}
		}
	}
}

// TestCHK_TRAY_108_SettingsWindowHeight — phase 16 G, revised in phase 17
// (CHK-TRAY-110): no fixed cap — the window is as high as its tallest tab
// (expert, every control shown) and never higher than the work area
// (1024×768, 1366×768, 1920×1080; 96/120/144 dpi); on a large work area no
// tab needs to scroll vertically; on a small one the content scrolls while
// the tab strip and the footer stay in place and inside the client; no
// horizontal scroll because of the vertical bar.
func TestCHK_TRAY_108_SettingsWindowHeight(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() { testDPI = 0; testWorkArea = nil }()
	app, _ := startChromeApp(t)
	defer app.Exit()
	works := []winapi.RECT{{Right: 1024, Bottom: 728}, {Right: 1366, Bottom: 728}, {Right: 1920, Bottom: 1040}}
	for _, dpi := range []int32{96, 120, 144} {
		for _, wa := range works {
			work := wa
			testWorkArea = &work
			tag := fmt.Sprintf("%d dpi %dx%d", dpi, work.Right, work.Bottom)
			w := openChromeSettings(t, app, i18n.RU, dpi, true)
			r := winapi.GetWindowRect(w.hwnd)
			h := r.Bottom - r.Top
			if !contains(work, r) || h > work.Bottom-work.Top {
				t.Errorf("%s: window %+v outside the work area", tag, r)
			}
			// not clamped by the work area nor by Windows (a top-level window
			// is never taller than the real screen + its frame: the CI
			// runner's 1024×768 screen limits the 1920×1080 model)
			maxTrack := winapi.GetSystemMetrics(smCYMaxTrack)
			full := h < work.Bottom-work.Top && (maxTrack == 0 || h < maxTrack)
			for _, p := range chromePages(w) {
				w.setPage(p)
				w.layoutFit()
				if full && w.contentH > w.viewH {
					t.Errorf("%s page %d: scrolls (%d > %d) although the window is not clamped (height %d)", tag, p, w.contentH, w.viewH, h)
				}
				if w.contentW > w.viewW {
					t.Errorf("%s page %d: horizontal scroll %d > %d", tag, p, w.contentW, w.viewW)
				}
				tab := winapi.GetWindowRect(w.ctl[idSetTabs])
				save := winapi.GetWindowRect(w.ctl[idSetSave])
				w.scrollTo(0, 1<<20)
				if winapi.GetWindowRect(w.ctl[idSetTabs]) != tab || winapi.GetWindowRect(w.ctl[idSetSave]) != save {
					t.Errorf("%s page %d: tabs / footer moved with the scroll", tag, p)
				}
				_, oy := winapi.ClientOrigin(w.hwnd)
				hr := winapi.GetWindowRect(w.host)
				if hr.Top-oy < w.tabChromeH-2 || hr.Bottom-oy > w.footerTop+2 {
					t.Errorf("%s page %d: host [%d,%d) outside tabs %d / footer %d", tag, p, hr.Top-oy, hr.Bottom-oy, w.tabChromeH, w.footerTop)
				}
				w.scrollTo(0, 0)
			}
			if dpi == 96 && work.Bottom >= 1000 && maxTrack >= 1080 && !full {
				t.Errorf("%s: the window is clamped on a 1920×1080 screen (height %d)", tag, h)
			}
			w.close()
		}
	}
}
