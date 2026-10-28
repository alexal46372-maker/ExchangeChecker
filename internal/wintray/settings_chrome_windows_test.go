//go:build windows

package wintray

import (
	"runtime"
	"strconv"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_65_LayoutContract — modern settings chrome geometry /
// palette (CHK-TRAY-65), mirrored from CHK-SETUP-14. Pure constants +
// theme helpers; no HWND.
func TestCHK_TRAY_65_LayoutContract(t *testing.T) {
	if settingsTabChromeH < 28 || settingsTabChromeH > 48 {
		t.Fatalf("tab chrome height %d want 28–48", settingsTabChromeH)
	}
	if setBarH < 56 {
		t.Fatalf("footer bar %d too small", setBarH)
	}
	trayui.SetTheme(trayui.ThemeLight)
	t.Cleanup(func() { trayui.SetTheme(trayui.ThemeLight) })
	if trayui.ChromePage() == 0x00F0F0F0 {
		t.Fatal("content bg must not be COLOR_BTNFACE gray")
	}
	if trayui.ChromePage() != trayui.Page {
		t.Fatal("ChromePage light must be Page (#FAFBFC)")
	}
	if trayui.ChromePanel() != trayui.Panel {
		t.Fatal("ChromePanel light must be Panel (#F1F5F9)")
	}
	if trayui.ChromeBrandDark() == 0 || trayui.ChromeAccent() == 0 {
		t.Fatal("brand/accent missing")
	}
	if trayui.ChromeBrandDark() == trayui.ChromePage() {
		t.Fatal("brand/content collapsed")
	}
	trayui.SetTheme(trayui.ThemeDark)
	if trayui.ChromePage() == trayui.Page {
		t.Fatal("dark ChromePage must differ from light Page")
	}
	trayui.SetTheme(trayui.ThemeLight)
	if len(pageActionIDs) < 3 {
		t.Fatal("pageActionIDs must include Test/Cancel/…")
	}
}

// TestCHK_TRAY_65_ChromeSmoke — Segoe UI (not DEFAULT_GUI_FONT) and
// solid brushes for settings chrome resolve (light + dark).
func TestCHK_TRAY_65_ChromeSmoke(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, theme := range []trayui.Theme{trayui.ThemeLight, trayui.ThemeDark} {
		trayui.SetTheme(theme)
		invalidateThemeBrushes()
		panel, page := ensureThemeBrushes()
		if panel == 0 || page == 0 {
			t.Fatalf("%s: chrome brushes missing", theme)
		}
		f := uiFont()
		if f == 0 {
			t.Fatalf("%s: uiFont missing", theme)
		}
		if f == winapi.GetStockObject(winapi.DEFAULT_GUI_FONT) {
			t.Fatalf("%s: uiFont must not be DEFAULT_GUI_FONT", theme)
		}
	}
	trayui.SetTheme(trayui.ThemeLight)
}

// TestCHK_TRAY_66_TabChromeFixedAndNoOverlap — tab strip is a frame
// child (not under the scroll host); its screen rect Y does not move
// when the host scrolls to bottom; host has WS_CLIPCHILDREN|CLIPSIBLINGS;
// visible children of the active panel do not overlap at 0/mid/max scroll.
func TestCHK_TRAY_66_TabChromeFixedAndNoOverlap(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newViberCfg()
	app := startCfgApp(t, svc.cfgService, true, nil)
	defer app.Exit()

	defer func() { testDPI = 0 }()
	for _, lang := range []string{i18n.RU, i18n.EN} {
		for _, dpi := range []int32{96, 120, 144} {
			testDPI = dpi
			w, err := newSettingsWindow(settingsConfig{lang: lang, mode: traysettings.Admin, session: app.session,
				hidden: true, uiMode: traysettings.UIExpert})
			if err != nil {
				t.Fatal(err)
			}
			w.Reload()
			pumpUntil(t, "loaded", func() bool { w.update(); return !w.busy && w.ed.HasProfile() })
			tag := lang + "/" + strconv.Itoa(int(dpi))

			tab := w.ctl[idSetTabs]
			if tab == 0 || w.host == 0 {
				w.close()
				t.Fatalf("%s: tabs/host missing", tag)
			}
			// tab parent ≠ scroll host: tab is among frame children, not host
			inHost, inFrame := false, false
			for _, h := range winapi.Children(w.host) {
				if h == tab {
					inHost = true
				}
			}
			for _, h := range winapi.Children(w.hwnd) {
				if h == tab {
					inFrame = true
				}
			}
			if inHost || !inFrame {
				w.close()
				t.Fatalf("%s: tab must be frame child, not host (inHost=%v inFrame=%v)", tag, inHost, inFrame)
			}
			st := winapi.WindowStyle(w.host)
			if st&winapi.WS_CLIPCHILDREN == 0 || st&winapi.WS_CLIPSIBLINGS == 0 {
				w.close()
				t.Fatalf("%s: host missing WS_CLIPCHILDREN|WS_CLIPSIBLINGS (style=%#x)", tag, st)
			}
			for i, pan := range w.pagePanel {
				if pan == 0 {
					continue
				}
				pst := winapi.WindowStyle(pan)
				if pst&winapi.WS_CLIPSIBLINGS == 0 {
					w.close()
					t.Fatalf("%s: pagePanel[%d] missing WS_CLIPSIBLINGS", tag, i)
				}
			}

			w.setPage(0)
			w.layoutFit()
			r0 := winapi.GetWindowRect(tab)
			// scroll to bottom of content
			w.scrollTo(0, 1<<20)
			r1 := winapi.GetWindowRect(tab)
			if r0.Top != r1.Top || r0.Bottom != r1.Bottom || r0.Left != r1.Left || r0.Right != r1.Right {
				w.close()
				t.Fatalf("%s: tab screen rect moved with scroll: before=%v after=%v", tag, r0, r1)
			}
			_, sy := w.ScrollPos()
			if sy == 0 && w.contentH > w.viewH {
				w.close()
				t.Fatalf("%s: expected non-zero scroll at bottom (contentH=%d viewH=%d)", tag, w.contentH, w.viewH)
			}

			for _, frac := range []float64{0, 0.5, 1} {
				y := int32(float64(max32(0, w.contentH-w.viewH)) * frac)
				w.scrollTo(0, y)
				if why := assertSamePageNoOverlap(w); why != "" {
					w.close()
					t.Fatalf("%s scroll=%d: %s", tag, y, why)
				}
				if why := assertHostChildrenClear(w); why != "" {
					w.close()
					t.Fatalf("%s scroll=%d host: %s", tag, y, why)
				}
			}
			w.close()
		}
	}
}

// TestCHK_TRAY_67_TestButtonVisible — after Databases→General→Databases,
// Simple↔Expert, Viber/Email and back, Test is visible, hit-testable,
// and its client rect sits fully in the visible footer band.
func TestCHK_TRAY_67_TestButtonVisible(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newViberCfg()
	app := startCfgApp(t, svc.cfgService, true, nil)
	defer app.Exit()
	w, err := newSettingsWindow(settingsConfig{lang: i18n.RU, mode: traysettings.Admin, session: app.session,
		hidden: false, uiMode: traysettings.UIExpert})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	w.Reload()
	pumpUntil(t, "loaded", func() bool { w.update(); return !w.busy && w.ed.HasProfile() })

	checkTest := func(tag string) {
		t.Helper()
		h := w.ctl[idSetTest]
		if h == 0 {
			t.Fatalf("%s: Test HWND missing", tag)
		}
		if !winapi.IsWindowVisible(h) {
			t.Fatalf("%s: Test not IsWindowVisible", tag)
		}
		if winapi.WindowStyle(h)&winapi.WS_VISIBLE == 0 {
			t.Fatalf("%s: Test missing WS_VISIBLE", tag)
		}
		r := winapi.GetWindowRect(h)
		cr := winapi.GetClientRect(w.hwnd)
		ox, oy := winapi.ClientOrigin(w.hwnd)
		// client-relative
		cl := winapi.RECT{Left: r.Left - ox, Top: r.Top - oy, Right: r.Right - ox, Bottom: r.Bottom - oy}
		if cl.Left < 0 || cl.Top < 0 || cl.Right > cr.Right || cl.Bottom > cr.Bottom {
			t.Fatalf("%s: Test rect %v outside client %v", tag, cl, cr)
		}
		if w.footerTop > 0 && cl.Top < w.footerTop-px(4) {
			t.Fatalf("%s: Test above footer band (top=%d footerTop=%d)", tag, cl.Top, w.footerTop)
		}
		// not clipped by host scroll — Test is a frame child
		for _, ch := range winapi.Children(w.host) {
			if ch == h {
				t.Fatalf("%s: Test must not be a host child", tag)
			}
		}
	}

	w.setPage(0)
	w.showPageActions(0)
	w.layoutFit()
	w.update()
	checkTest("start-databases")

	w.setPage(1)
	if winapi.IsWindowVisible(w.ctl[idSetTest]) {
		t.Fatal("Test must hide on General")
	}
	w.setPage(0)
	w.update()
	checkTest("back-from-general")

	for i := 0; i < 2; i++ {
		w.toggleUIMode() // Expert→Simple
		w.setPage(0)
		w.update()
		checkTest("after-simple")
		w.toggleUIMode() // Simple→Expert
		w.setPage(0)
		w.update()
		checkTest("after-expert")
	}

	pumpUntil(t, "viber", func() bool { w.update(); return w.viberAvailable() && winapi.TabItemCount(w.ctl[idSetTabs]) >= 3 })
	w.setPage(pageViber)
	if winapi.IsWindowVisible(w.ctl[idSetTest]) {
		t.Fatal("Test must hide on Viber")
	}
	w.setPage(0)
	w.update()
	checkTest("back-from-viber")

	if w.emailAvailable() {
		w.setPage(pageEmail)
		if winapi.IsWindowVisible(w.ctl[idSetTest]) {
			t.Fatal("Test must hide on Email")
		}
		w.setPage(0)
		w.update()
		checkTest("back-from-email")
	}

	// any scroll position: Test stays in footer (frame child)
	w.scrollTo(0, 1<<20)
	checkTest("scrolled-bottom")
	w.scrollTo(0, 0)
	checkTest("scrolled-top")
}

