//go:build windows

package wintray

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// chromeCfg announces CapViber + CapEmail + CapAlarmTest so CHK-TRAY-66/67
// cover every tab and Alarm page-action (forced CapEmail / Alarm).
type chromeCfg struct {
	*cfgService
	viber            model.ViberSettings
	email            model.EmailSettings
	hasToken, hasPass bool
}

func newChromeCfg() *chromeCfg {
	v := model.DefaultViberSettings()
	v.Recipients = []model.ViberRecipient{{ID: "abc=", Name: "Дежурный"}}
	v.Status = &model.ViberStatus{Sent: 1}
	e := model.DefaultEmailSettings()
	e.Host, e.Port, e.From = "smtp.example.com", 587, "mon@example.com"
	e.TLSMode = model.EmailTLSStartTLS
	e.Recipients = []model.EmailRecipient{{ID: "ops", Addr: "ops@example.com", Name: "Ops"}}
	f := &chromeCfg{cfgService: newCfgService(), viber: v, email: e, hasToken: true, hasPass: true}
	f.cfgService.handler = f
	return f
}

func (f *chromeCfg) settings() model.Settings {
	s := model.DefaultSettings()
	v := f.viber
	v.HasToken = f.hasToken
	s.Viber = &v
	e := f.email
	e.HasPassword = f.hasPass
	s.Email = &e
	return s
}

func (f *chromeCfg) GetConfig() (model.ConfigView, string, error) {
	cv, rev, err := f.cfgService.GetConfig()
	cv.Settings = f.settings()
	return cv, rev, err
}

func (f *chromeCfg) Snapshot() model.StatusSnapshot {
	snap := f.cfgService.Snapshot()
	s := f.settings()
	snap.Settings = &s
	return snap
}

func (f *chromeCfg) ViberTest(ctx context.Context, recipientID, token string) (model.ViberTestResult, error) {
	return model.ViberTestResult{OK: true}, nil
}

func (f *chromeCfg) EmailTest(ctx context.Context, recipientID, addr, smtpPassword string) (model.EmailTestResult, error) {
	return model.EmailTestResult{OK: true}, nil
}

func (f *chromeCfg) AlarmTest(ctx context.Context, e model.AlertEvent, viber bool, account string) (model.AlarmTestResult, error) {
	return model.AlarmTestResult{OK: true}, nil
}

func startChromeApp(t *testing.T) (*App, *chromeCfg) {
	t.Helper()
	svc := newChromeCfg()
	app := startCfgApp(t, svc.cfgService, true, nil)
	return app, svc
}

func openChromeSettings(t *testing.T, app *App, lang string, dpi int32, hidden bool) *settingsWindow {
	t.Helper()
	testDPI = dpi
	w, err := newSettingsWindow(settingsConfig{lang: lang, mode: traysettings.Admin, session: app.session,
		hidden: hidden, uiMode: traysettings.UIExpert})
	if err != nil {
		t.Fatal(err)
	}
	w.Reload()
	pumpUntil(t, "chrome caps", func() bool {
		w.update()
		return !w.busy && w.ed.HasProfile() && w.viberAvailable() && w.emailAvailable() &&
			winapi.TabItemCount(w.ctl[idSetTabs]) >= 4
	})
	return w
}

func chromePages(w *settingsWindow) []int {
	pages := []int{0, 1, pageViber}
	if w.emailAvailable() {
		pages = append(pages, pageEmail)
	}
	return pages
}

// assertTabsNotInHostRect — tabs must never be scrolled via hostRect.
func assertTabsNotInHostRect(w *settingsWindow) string {
	tab := w.ctl[idSetTabs]
	if tab == 0 {
		return "no tabs"
	}
	if _, ok := w.hostRect[tab]; ok {
		return "tabs present in hostRect"
	}
	for _, h := range winapi.Children(w.host) {
		if h == tab {
			return "tabs is host child"
		}
	}
	return ""
}

// assertScrollChromeFlags — scrollTo/setPage must RedrawWindow with the
// full invalidate+erase+children+updatenow combo (source contract).
func assertScrollChromeFlags(t *testing.T) {
	t.Helper()
	for _, name := range []string{"settingsfit_windows.go", "settings_windows.go", "settingsmode_windows.go"} {
		b, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		want := []byte("RDW_INVALIDATE|winapi.RDW_ERASE|winapi.RDW_ALLCHILDREN|winapi.RDW_UPDATENOW")
		if !bytes.Contains(b, want) {
			t.Fatalf("%s: missing RedrawWindow flags RDW_INVALIDATE|ERASE|ALLCHILDREN|UPDATENOW", name)
		}
	}
	combo := winapi.RDW_INVALIDATE | winapi.RDW_ERASE | winapi.RDW_ALLCHILDREN | winapi.RDW_UPDATENOW
	if combo != 0x0001|0x0004|0x0080|0x0100 {
		t.Fatalf("RDW combo %#x", combo)
	}
}

// assertPixelGhostAfterScroll — inactive panels stay invisible after scroll;
// CaptureWindow of the tab strip is stable across scroll (no ghost bleed).
func assertPixelGhostAfterScroll(t *testing.T, w *settingsWindow, tag string) {
	t.Helper()
	tab := w.ctl[idSetTabs]
	if why := w.assertPageClear(); why != "" {
		t.Fatalf("%s pageClear: %s", tag, why)
	}
	if why := assertHostChildrenClear(w); why != "" {
		t.Fatalf("%s host: %s", tag, why)
	}
	if why := assertSamePageNoOverlap(w); why != "" {
		t.Fatalf("%s overlap: %s", tag, why)
	}
	cw, ch, before, ok := winapi.CaptureWindow(tab)
	if !ok || cw <= 0 || ch <= 0 || len(before) == 0 {
		return // PrintWindow may fail off-screen; HWND asserts above still hold
	}
	w.scrollTo(0, 1<<20)
	_, _, after, ok2 := winapi.CaptureWindow(tab)
	if ok2 && len(after) == len(before) && !bytes.Equal(before, after) {
		// allow tiny dither; fail only if >2% pixels differ
		diff := 0
		for i := range before {
			if before[i] != after[i] {
				diff++
			}
		}
		if diff*100 > len(before)*2 {
			t.Fatalf("%s: tab pixels changed after scroll (%d/%d)", tag, diff, len(before))
		}
	}
	if why := w.assertPageClear(); why != "" {
		t.Fatalf("%s after-scroll pageClear: %s", tag, why)
	}
}

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

// TestCHK_TRAY_65_SettingsChromeErase — every erase branch + dark +
// contentErase + CTLCOLOR brush ≠ COLOR_BTNFACE.
func TestCHK_TRAY_65_SettingsChromeErase(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _ := startChromeApp(t)
	defer app.Exit()
	defer func() { testDPI = 0 }()
	testDPI = 96
	w := openChromeSettings(t, app, i18n.RU, 96, true)
	defer w.close()
	trayui.SetTheme(trayui.ThemeLight)
	t.Cleanup(func() { trayui.SetTheme(trayui.ThemeLight) })

	hdc := winapi.GetDC(w.hwnd)
	if hdc == 0 {
		t.Fatal("GetDC")
	}
	defer winapi.ReleaseDC(w.hwnd, hdc)

	// branch: footerTop valid + tabChromeH > 0 (brand accent)
	w.layoutFit()
	if w.footerTop <= 0 || w.tabChromeH <= 0 {
		t.Fatalf("footerTop=%d tabChromeH=%d", w.footerTop, w.tabChromeH)
	}
	if r := w.settingsChromeErase(hdc); r != 1 {
		t.Fatalf("erase want 1 got %d", r)
	}
	// branch: footerTop fallback (≤0)
	savedFT, savedTH := w.footerTop, w.tabChromeH
	w.footerTop = 0
	if r := w.settingsChromeErase(hdc); r != 1 {
		t.Fatalf("erase footerTop=0: %d", r)
	}
	// branch: footerTop past bottom
	rc := winapi.GetClientRect(w.hwnd)
	w.footerTop = rc.Bottom + 10
	if r := w.settingsChromeErase(hdc); r != 1 {
		t.Fatalf("erase footerTop>bottom: %d", r)
	}
	// branch: no brand accent
	w.footerTop, w.tabChromeH = savedFT, 0
	if r := w.settingsChromeErase(hdc); r != 1 {
		t.Fatalf("erase tabChromeH=0: %d", r)
	}
	w.footerTop, w.tabChromeH = savedFT, savedTH

	// dark theme erase
	trayui.SetTheme(trayui.ThemeDark)
	invalidateThemeBrushes()
	if r := w.settingsChromeErase(hdc); r != 1 {
		t.Fatalf("dark erase: %d", r)
	}
	trayui.SetTheme(trayui.ThemeLight)
	invalidateThemeBrushes()

	// contentErase on host + active pagePanel
	if w.host == 0 {
		t.Fatal("no host")
	}
	hdcH := winapi.GetDC(w.host)
	if hdcH == 0 {
		t.Fatal("GetDC host")
	}
	if r := contentErase(hdcH, w.host); r != 1 {
		t.Fatalf("contentErase host: %d", r)
	}
	winapi.ReleaseDC(w.host, hdcH)
	pan := w.pagePanel[w.page]
	if pan != 0 {
		hdcP := winapi.GetDC(pan)
		if hdcP != 0 {
			if r := contentErase(hdcP, pan); r != 1 {
				t.Fatalf("contentErase panel: %d", r)
			}
			winapi.ReleaseDC(pan, hdcP)
		}
	}

	// CTLCOLOR brushes must be theme chrome, not COLOR_BTNFACE stock path
	panel, page := ensureThemeBrushes()
	brStatic, ok := themeCtlColor(winapi.WM_CTLCOLORSTATIC, hdc, 0)
	if !ok || brStatic == 0 || brStatic != panel {
		t.Fatalf("CTLCOLORSTATIC brush %#x ok=%v want panel %#x", brStatic, ok, panel)
	}
	brEdit, ok := themeCtlColor(winapi.WM_CTLCOLOREDIT, hdc, 0)
	if !ok || brEdit == 0 || brEdit != page {
		t.Fatalf("CTLCOLOREDIT brush %#x ok=%v want page %#x", brEdit, ok, page)
	}
	if trayui.ChromePage() == 0x00F0F0F0 || trayui.ChromePanel() == 0x00F0F0F0 {
		t.Fatal("CTLCOLOR chrome must not be COLOR_BTNFACE")
	}
	stock := winapi.GetStockObject(winapi.DEFAULT_GUI_FONT)
	if brStatic == stock || brEdit == stock {
		t.Fatal("CTLCOLOR returned stock object")
	}
}

// TestCHK_TRAY_66_AllTabsScrollNoOverlap — pages 0/1/Viber/Email × RU/EN ×
// 96/120/144; force CapEmail; scroll 0/mid/max; tab rect fixed; no ghosts.
func TestCHK_TRAY_66_AllTabsScrollNoOverlap(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _ := startChromeApp(t)
	defer app.Exit()
	defer func() { testDPI = 0 }()
	assertScrollChromeFlags(t)

	for _, lang := range []string{i18n.RU, i18n.EN} {
		for _, dpi := range []int32{96, 120, 144} {
			testDPI = dpi
			w := openChromeSettings(t, app, lang, dpi, true)
			tag := lang + "/" + strconv.Itoa(int(dpi))
			tab := w.ctl[idSetTabs]
			if tab == 0 || w.host == 0 {
				w.close()
				t.Fatalf("%s: tabs/host missing", tag)
			}
			st := winapi.WindowStyle(w.host)
			if st&winapi.WS_CLIPCHILDREN == 0 || st&winapi.WS_CLIPSIBLINGS == 0 {
				w.close()
				t.Fatalf("%s: host missing CLIPCHILDREN|CLIPSIBLINGS %#x", tag, st)
			}
			fst := winapi.WindowStyle(w.hwnd)
			if fst&winapi.WS_CLIPCHILDREN == 0 {
				w.close()
				t.Fatalf("%s: frame missing WS_CLIPCHILDREN %#x", tag, fst)
			}
			tst := winapi.WindowStyle(tab)
			if tst&winapi.WS_CLIPSIBLINGS == 0 {
				w.close()
				t.Fatalf("%s: tab missing WS_CLIPSIBLINGS %#x", tag, tst)
			}
			for i, pan := range w.pagePanel {
				if pan == 0 {
					continue
				}
				if winapi.WindowStyle(pan)&winapi.WS_CLIPSIBLINGS == 0 {
					w.close()
					t.Fatalf("%s: pagePanel[%d] missing WS_CLIPSIBLINGS", tag, i)
				}
			}

			for _, page := range chromePages(w) {
				w.setPage(page)
				w.layoutFit()
				if why := assertTabsNotInHostRect(w); why != "" {
					w.close()
					t.Fatalf("%s page=%d: %s", tag, page, why)
				}
				r0 := winapi.GetWindowRect(tab)
				for _, frac := range []float64{0, 0.5, 1} {
					y := int32(float64(max32(0, w.contentH-w.viewH)) * frac)
					w.scrollTo(0, y)
					r1 := winapi.GetWindowRect(tab)
					if r0.Top != r1.Top || r0.Bottom != r1.Bottom || r0.Left != r1.Left || r0.Right != r1.Right {
						w.close()
						t.Fatalf("%s page=%d scroll=%d: tab rect moved %v → %v", tag, page, y, r0, r1)
					}
					if why := assertTabsNotInHostRect(w); why != "" {
						w.close()
						t.Fatalf("%s page=%d scroll=%d: %s", tag, page, y, why)
					}
					if why := assertSamePageNoOverlap(w); why != "" {
						w.close()
						t.Fatalf("%s page=%d scroll=%d: %s", tag, page, y, why)
					}
					if why := assertHostChildrenClear(w); why != "" {
						w.close()
						t.Fatalf("%s page=%d scroll=%d host: %s", tag, page, y, why)
					}
					if why := w.assertPageClear(); why != "" {
						w.close()
						t.Fatalf("%s page=%d scroll=%d clear: %s", tag, page, y, why)
					}
				}
				assertPixelGhostAfterScroll(t, w, tag+"/p"+strconv.Itoa(page))
				w.scrollTo(0, 0)
			}
			w.close()
		}
	}
}

// TestCHK_TRAY_66_ScrollPosPerTab — per-tab scrollPos survives leave/return;
// layoutFit does not reset after an explicit restore. (setPage's trailing
// update may EN_SETFOCUS→scrollIntoView and move the live ScrollPos; the
// durable contract is the scrollPos[] slot + layoutFit.)
func TestCHK_TRAY_66_ScrollPosPerTab(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _ := startChromeApp(t)
	defer app.Exit()
	defer func() { testDPI = 0 }()
	w := openChromeSettings(t, app, i18n.RU, 96, true)
	defer w.close()

	w.setPage(0)
	w.layoutFit()
	maxY := max32(0, w.contentH-w.viewH)
	if maxY < 8 {
		t.Skip("content not tall enough to scroll")
	}
	want0 := maxY / 2
	w.scrollTo(0, want0)
	if _, y0 := w.ScrollPos(); y0 != want0 || w.scrollPos[0].y != want0 {
		t.Fatalf("scroll0 got %d slot %d want %d", y0, w.scrollPos[0].y, want0)
	}

	w.setPage(1)
	if w.scrollPos[0].y != want0 {
		t.Fatalf("leave page0: scrollPos[0]=%d want %d", w.scrollPos[0].y, want0)
	}
	w.layoutFit()
	max1 := max32(0, w.contentH-w.viewH)
	want1 := int32(0)
	if max1 >= 3 {
		want1 = max1 / 3
	}
	w.scrollTo(0, want1)
	if w.scrollPos[1].y != want1 {
		t.Fatalf("scrollPos[1]=%d want %d", w.scrollPos[1].y, want1)
	}
	if w.scrollPos[0].y != want0 {
		t.Fatalf("page1 scroll clobbered scrollPos[0]: %d", w.scrollPos[0].y)
	}

	w.setPage(0)
	if w.scrollPos[1].y != want1 {
		t.Fatalf("leave page1: scrollPos[1]=%d want %d", w.scrollPos[1].y, want1)
	}
	// Re-apply restored slot (setPage already scrollTo'd it before update;
	 // focus scrollIntoView may have moved live Y) and prove layoutFit keeps it.
	w.scrollTo(0, want0)
	w.layoutFit()
	if _, yFit := w.ScrollPos(); yFit != want0 || w.scrollPos[0].y != want0 {
		t.Fatalf("layoutFit reset page0: live=%d slot=%d want %d", yFit, w.scrollPos[0].y, want0)
	}

	w.setPage(1)
	if w.scrollPos[0].y != want0 {
		t.Fatalf("second leave page0 lost slot: %d", w.scrollPos[0].y)
	}
	w.scrollTo(0, want1)
	w.layoutFit()
	if _, y1b := w.ScrollPos(); y1b != want1 || w.scrollPos[1].y != want1 {
		t.Fatalf("layoutFit reset page1: live=%d slot=%d want %d", y1b, w.scrollPos[1].y, want1)
	}
}

// TestCHK_TRAY_66_LayoutFitWrap144EN — 144 dpi EN footer wrap; host band
// sits in [tabChromeH, footerTop); tabs ∉ hostRect after layoutFit/scrollTo.
func TestCHK_TRAY_66_LayoutFitWrap144EN(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _ := startChromeApp(t)
	defer app.Exit()
	defer func() { testDPI = 0 }()
	w := openChromeSettings(t, app, i18n.EN, 144, true)
	defer w.close()

	w.setPage(0)
	w.layoutFit()
	if w.tabChromeH <= 0 || w.footerTop <= w.tabChromeH {
		t.Fatalf("band tabChromeH=%d footerTop=%d", w.tabChromeH, w.footerTop)
	}
	_, oy := winapi.ClientOrigin(w.hwnd)
	hr := winapi.GetWindowRect(w.host)
	hostTop := hr.Top - oy
	hostBot := hr.Bottom - oy
	if hostTop < w.tabChromeH-2 || hostBot > w.footerTop+2 {
		t.Fatalf("host band [%d,%d) want within [%d,%d)", hostTop, hostBot, w.tabChromeH, w.footerTop)
	}
	if why := assertTabsNotInHostRect(w); why != "" {
		t.Fatal("after layoutFit:", why)
	}
	cr := winapi.GetClientRect(w.hwnd)
	// wrapped footer: footer band taller than single-row setBarH at 144
	barH := cr.Bottom - w.footerTop
	if barH < px(setBarH) {
		t.Fatalf("footer height %d < setBarH %d", barH, px(setBarH))
	}
	w.scrollTo(0, 1<<20)
	if why := assertTabsNotInHostRect(w); why != "" {
		t.Fatal("after scrollTo:", why)
	}
	rTab := winapi.GetWindowRect(w.ctl[idSetTabs])
	if rTab.Bottom-oy > w.tabChromeH+px(8) {
		t.Fatalf("tab bottom %d below chrome band %d", rTab.Bottom-oy, w.tabChromeH)
	}
}

// TestCHK_TRAY_66_TabChromeFixedAndNoOverlap — kept name for CI greps;
// delegates coverage to AllTabs (page0 subset still exercised here lightly).
func TestCHK_TRAY_66_TabChromeFixedAndNoOverlap(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _ := startChromeApp(t)
	defer app.Exit()
	defer func() { testDPI = 0 }()
	w := openChromeSettings(t, app, i18n.RU, 96, true)
	defer w.close()
	w.setPage(0)
	w.layoutFit()
	tab := w.ctl[idSetTabs]
	r0 := winapi.GetWindowRect(tab)
	w.scrollTo(0, 1<<20)
	r1 := winapi.GetWindowRect(tab)
	if r0 != r1 {
		t.Fatalf("tab moved %v → %v", r0, r1)
	}
	if why := assertSamePageNoOverlap(w); why != "" {
		t.Fatal(why)
	}
}

func checkTestFooter(t *testing.T, w *settingsWindow, tag string) {
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
	cl := winapi.RECT{Left: r.Left - ox, Top: r.Top - oy, Right: r.Right - ox, Bottom: r.Bottom - oy}
	if cl.Left < 0 || cl.Top < 0 || cl.Right > cr.Right || cl.Bottom > cr.Bottom {
		t.Fatalf("%s: Test rect %v outside client %v", tag, cl, cr)
	}
	if w.footerTop > 0 && cl.Top < w.footerTop-px(4) {
		t.Fatalf("%s: Test above footer band (top=%d footerTop=%d)", tag, cl.Top, w.footerTop)
	}
	for _, ch := range winapi.Children(w.host) {
		if ch == h {
			t.Fatalf("%s: Test must not be a host child", tag)
		}
	}
}

// TestCHK_TRAY_67_PageActionsContract — exact pageActionIDs×4, isPageAction,
// IsWindowEnabled Test with/without profile, Cancel/Poll, Alarm cap,
// hideInactive skip, toggleUIMode without setPage, forced CapEmail.
func TestCHK_TRAY_67_PageActionsContract(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if len(pageActionIDs) != 4 {
		t.Fatalf("pageActionIDs len %d want 4", len(pageActionIDs))
	}
	wantIDs := []int32{idSetTest, idSetCancelTest, idSetAlarmTest, idSetPoll}
	for i, id := range wantIDs {
		if pageActionIDs[i] != id {
			t.Fatalf("pageActionIDs[%d]=%d want %d", i, pageActionIDs[i], id)
		}
	}

	app, _ := startChromeApp(t)
	defer app.Exit()
	defer func() { testDPI = 0 }()
	for _, dpi := range []int32{96, 120, 144} {
		testDPI = dpi
		w := openChromeSettings(t, app, i18n.RU, dpi, false)
		tag := "dpi" + strconv.Itoa(int(dpi))

		if !w.emailAvailable() {
			w.close()
			t.Fatalf("%s: CapEmail not forced", tag)
		}
		if !app.session.Status().Has(model.CapAlarmTest) && !app.session.Supported(model.CmdAlarmTest) {
			w.close()
			t.Fatalf("%s: CapAlarmTest missing", tag)
		}

		w.setPage(0)
		w.showPageActions(0)
		w.layoutFit()
		w.update()

		for _, id := range pageActionIDs {
			h := w.ctl[id]
			if h == 0 {
				w.close()
				t.Fatalf("%s: missing page action %d", tag, id)
			}
			if !w.isPageAction(h) {
				w.close()
				t.Fatalf("%s: isPageAction false for %d", tag, id)
			}
		}
		// hideInactive must skip page actions (Test stays visible on Databases)
		w.hideInactiveTabControls()
		if !winapi.IsWindowVisible(w.ctl[idSetTest]) {
			w.close()
			t.Fatalf("%s: hideInactive hid Test", tag)
		}

		// EnableWindow: profile selected → Test on; none → off
		if !w.ed.HasProfile() {
			w.close()
			t.Fatalf("%s: need profile", tag)
		}
		w.update()
		if !winapi.IsWindowEnabled(w.ctl[idSetTest]) {
			w.close()
			t.Fatalf("%s: Test disabled with profile selected", tag)
		}
		if winapi.IsWindowEnabled(w.ctl[idSetCancelTest]) {
			w.close()
			t.Fatalf("%s: Cancel enabled while not testing", tag)
		}
		if !winapi.IsWindowEnabled(w.ctl[idSetPoll]) {
			w.close()
			t.Fatalf("%s: Poll disabled for saved profile", tag)
		}
		// Alarm visible + enabled under CapAlarmTest on Databases
		if !winapi.IsWindowVisible(w.ctl[idSetAlarmTest]) {
			w.close()
			t.Fatalf("%s: AlarmTest hidden with CapAlarmTest", tag)
		}
		if !winapi.IsWindowEnabled(w.ctl[idSetAlarmTest]) {
			w.close()
			t.Fatalf("%s: AlarmTest disabled", tag)
		}

		w.ed.Select(-1)
		w.fill()
		w.update()
		if w.ed.HasProfile() {
			w.close()
			t.Fatalf("%s: Select(-1) still HasProfile", tag)
		}
		if winapi.IsWindowEnabled(w.ctl[idSetTest]) {
			w.close()
			t.Fatalf("%s: Test enabled with no profile", tag)
		}
		if winapi.IsWindowEnabled(w.ctl[idSetPoll]) {
			w.close()
			t.Fatalf("%s: Poll enabled with no profile", tag)
		}
		w.ed.Select(0)
		w.fill()
		w.update()
		if !winapi.IsWindowEnabled(w.ctl[idSetTest]) {
			w.close()
			t.Fatalf("%s: Test still disabled after re-select", tag)
		}

		checkTestFooter(t, w, tag+"/start")

		// tab / showPageActions round-trips
		w.setPage(1)
		for _, id := range pageActionIDs {
			if winapi.IsWindowVisible(w.ctl[id]) {
				w.close()
				t.Fatalf("%s: page action %d visible on General", tag, id)
			}
		}
		w.setPage(0)
		w.update()
		checkTestFooter(t, w, tag+"/back-general")

		// toggleUIMode without setPage — showPageActions must keep Test
		w.toggleUIMode() // Expert→Simple
		if w.page != 0 {
			w.close()
			t.Fatalf("%s: toggleUIMode changed page to %d", tag, w.page)
		}
		w.update()
		checkTestFooter(t, w, tag+"/after-simple")
		w.toggleUIMode() // Simple→Expert
		w.update()
		checkTestFooter(t, w, tag+"/after-expert")

		w.setPage(pageViber)
		if winapi.IsWindowVisible(w.ctl[idSetTest]) {
			w.close()
			t.Fatalf("%s: Test visible on Viber", tag)
		}
		w.setPage(0)
		w.update()
		checkTestFooter(t, w, tag+"/back-viber")

		w.setPage(pageEmail)
		if winapi.IsWindowVisible(w.ctl[idSetTest]) {
			w.close()
			t.Fatalf("%s: Test visible on Email", tag)
		}
		w.setPage(0)
		w.update()
		checkTestFooter(t, w, tag+"/back-email")

		// any scroll: Test stays in footer
		for _, y := range []int32{0, 1 << 20} {
			w.scrollTo(0, y)
			checkTestFooter(t, w, tag+"/scroll"+strconv.Itoa(int(y)))
		}
		w.close()
	}
}

// TestCHK_TRAY_67_HitTestFooter — WindowFromPoint at Test center == Test;
// still true after scroll to bottom. DPI matrix 96/120/144.
func TestCHK_TRAY_67_HitTestFooter(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _ := startChromeApp(t)
	defer app.Exit()
	defer func() { testDPI = 0 }()

	for _, dpi := range []int32{96, 120, 144} {
		testDPI = dpi
		w := openChromeSettings(t, app, i18n.RU, dpi, false)
		tag := "dpi" + strconv.Itoa(int(dpi))
		w.setPage(0)
		w.showPageActions(0)
		w.layoutFit()
		w.update()
		winapi.SetForegroundWindow(w.hwnd)

		hit := func(label string) {
			t.Helper()
			h := w.ctl[idSetTest]
			r := winapi.GetWindowRect(h)
			pt := winapi.POINT{X: (r.Left + r.Right) / 2, Y: (r.Top + r.Bottom) / 2}
			got := winapi.WindowFromPoint(pt)
			if got != h {
				t.Fatalf("%s %s: WindowFromPoint=%#x want Test %#x at %v", tag, label, got, h, pt)
			}
			checkTestFooter(t, w, tag+"/"+label)
		}
		hit("top")
		w.scrollTo(0, 1<<20)
		hit("bottom")
		w.scrollTo(0, 0)
		hit("top2")
		w.close()
	}
}

// TestCHK_TRAY_67_TestButtonVisible — retained name; covers round-trips
// at default dpi via PageActionsContract + HitTest.
func TestCHK_TRAY_67_TestButtonVisible(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _ := startChromeApp(t)
	defer app.Exit()
	defer func() { testDPI = 0 }()
	w := openChromeSettings(t, app, i18n.RU, 96, false)
	defer w.close()
	w.setPage(0)
	w.showPageActions(0)
	w.layoutFit()
	w.update()
	checkTestFooter(t, w, "start")
	if !winapi.IsWindowEnabled(w.ctl[idSetTest]) {
		t.Fatal("Test disabled with profile")
	}
	w.setPage(1)
	if winapi.IsWindowVisible(w.ctl[idSetTest]) {
		t.Fatal("Test visible on General")
	}
	w.setPage(0)
	checkTestFooter(t, w, "back")
}
