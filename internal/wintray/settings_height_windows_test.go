//go:build windows

package wintray

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_108_SettingsWindowHeight — phase 16 G (Кончита 16.11.2026):
// the settings window opened through the real constructor is no taller
// than 560 logical px and no taller than 3/4 of the work area (1024×768
// with a 40 px taskbar, 1366×768, 1920×1080; 96/120/144 dpi); the page
// content that does not fit scrolls vertically in the host while the tab
// strip and the footer («Сохранить», «Закрыть», the status) stay in place
// and inside the client; the window lies in the work area; nothing
// scrolls horizontally because of the vertical bar.
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
			limit := min32(px(setMaxOuterH), (work.Bottom-work.Top)*3/4)
			if h > limit {
				t.Errorf("%s: window height %d > limit %d (560 logical / 75%% of %d)", tag, h, limit, work.Bottom-work.Top)
			}
			if dpi == 96 && work.Bottom >= 1000 && (h < 540 || h > 560) {
				t.Errorf("%s: height %d, want 540–560 logical px", tag, h)
			}
			if !contains(work, r) {
				t.Errorf("%s: window %+v outside the work area", tag, r)
			}
			scrolled := false
			for _, p := range chromePages(w) {
				w.setPage(p)
				w.layoutFit()
				if w.contentH > w.viewH {
					scrolled = true
				}
				if w.contentW > w.viewW {
					t.Errorf("%s page %d: horizontal scroll %d > %d", tag, p, w.contentW, w.viewW)
				}
				tab := winapi.GetWindowRect(w.ctl[idSetTabs])
				save := winapi.GetWindowRect(w.ctl[idSetSave])
				cls := winapi.GetWindowRect(w.ctl[idSetClose])
				w.scrollTo(0, 1<<20)
				if w.contentH > w.viewH {
					if _, sy := w.ScrollPos(); sy != w.contentH-w.viewH {
						t.Errorf("%s page %d: scrolled to %d, want %d", tag, p, sy, w.contentH-w.viewH)
					}
				}
				if winapi.GetWindowRect(w.ctl[idSetTabs]) != tab || winapi.GetWindowRect(w.ctl[idSetSave]) != save || winapi.GetWindowRect(w.ctl[idSetClose]) != cls {
					t.Errorf("%s page %d: tabs / footer moved with the scroll", tag, p)
				}
				ox, oy := winapi.ClientOrigin(w.hwnd)
				cr := winapi.GetClientRect(w.hwnd)
				client := winapi.RECT{Left: ox, Top: oy, Right: ox + cr.Right, Bottom: oy + cr.Bottom}
				for _, id := range []int32{idSetSave, idSetClose, idSetStatus, idSetTabs} {
					if hc := w.ctl[id]; hc != 0 && !contains(client, winapi.GetWindowRect(hc)) {
						t.Errorf("%s page %d: control %d outside the client", tag, p, id)
					}
				}
				hr := winapi.GetWindowRect(w.host)
				if hr.Top-oy < w.tabChromeH-2 || hr.Bottom-oy > w.footerTop+2 {
					t.Errorf("%s page %d: host [%d,%d) outside tabs %d / footer %d", tag, p, hr.Top-oy, hr.Bottom-oy, w.tabChromeH, w.footerTop)
				}
				w.scrollTo(0, 0)
			}
			if !scrolled {
				t.Errorf("%s: no page scrolls — the old full height", tag)
			}
			w.close()
		}
	}
}
