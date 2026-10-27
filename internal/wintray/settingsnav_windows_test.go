//go:build windows

package wintray

import (
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// TestCHK_TRAY_47_Navigation — section tabs (SysTabControl32): Databases /
// General / Viber; Simple/Expert is a separate mode button; in Simple the
// General (+ Viber) tabs are absent; current tab tracks setPage.
func TestCHK_TRAY_47_Navigation(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, lang := range []string{i18n.RU, i18n.EN} {
		svc := newViberCfg()
		app := startCfgApp(t, svc.cfgService, true, nil)
		w, err := newSettingsWindow(settingsConfig{lang: lang, mode: traysettings.Admin, session: app.session, hidden: true,
			uiMode: traysettings.UIExpert})
		if err != nil {
			t.Fatal(err)
		}
		w.Reload()
		pumpUntil(t, "loaded", func() bool { w.update(); return !w.busy && w.ed.HasProfile() && w.viberAvailable() })
		if w.ctl[idSetTabs] == 0 || winapi.TabItemCount(w.ctl[idSetTabs]) < 2 {
			t.Fatalf("%s: tabs missing or too few (%d)", lang, winapi.TabItemCount(w.ctl[idSetTabs]))
		}
		if !w.navTabSelected(0) {
			t.Errorf("%s: databases tab not selected", lang)
		}
		barLeft := func() int32 { return winapi.GetWindowRect(w.ctl[idSetStatus]).Left }
		for _, p := range []struct {
			page int
		}{{1}, {pageViber}} {
			w.setPage(p.page)
			if w.page != p.page || !w.navTabSelected(p.page) {
				t.Errorf("%s: page %d tab not selected", lang, p.page)
			}
			if x := winapi.GetWindowRect(w.ctl[idSetReload]).Left; x != barLeft() || visible(w.ctl[idSetTest]) {
				t.Errorf("%s page %d: «Перечитать» at %d, bar starts at %d", lang, p.page, x, barLeft())
			}
		}
		w.setPage(0)
		if w.page != 0 || winapi.GetWindowRect(w.ctl[idSetTest]).Left != barLeft() {
			t.Errorf("%s: back to the databases page", lang)
		}
		w.toggleUIMode() // Simple: only Databases tab
		if winapi.TabItemCount(w.ctl[idSetTabs]) != 1 || w.tabPageAt(0) != 0 {
			t.Errorf("%s: simple mode must keep only Databases tab, got %d", lang, winapi.TabItemCount(w.ctl[idSetTabs]))
		}
		if !visible(w.ctl[idSetUIMode]) {
			t.Errorf("%s: mode button must stay", lang)
		}
		w.toggleUIMode()
		if winapi.TabItemCount(w.ctl[idSetTabs]) < 2 {
			t.Errorf("%s: expert tabs not restored", lang)
		}
		w.close()
		app.Exit()
	}
}

// TestCHK_TRAY_47_Cues — every single-line edit box of the databases
// page and the Viber token shows a cue (PLAN §12 p.50), RU and EN.
func TestCHK_TRAY_47_Cues(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, lang := range []string{i18n.RU, i18n.EN} {
		svc := newViberCfg()
		app := startCfgApp(t, svc.cfgService, true, nil)
		w, err := newSettingsWindow(settingsConfig{lang: lang, mode: traysettings.Admin, session: app.session, hidden: true,
			uiMode: traysettings.UIExpert})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []int32{idSetName, idSetHost, idSetInstance, idSetPort, idSetDatabase, idSetLogin, idSetPassword,
			idSetWarn, idSetError, idSetConnTO, idSetQueryTO, idVbToken} {
			if w.cues[id] == "" {
				t.Errorf("%s: control %d without a cue", lang, id)
			}
		}
		if w.cues[idSetName] != i18n.T(i18n.KeyCueName, lang) {
			t.Errorf("%s: name cue %q", lang, w.cues[idSetName])
		}
		w.close()
		app.Exit()
	}
}

// TestCHK_TRAY_47_PageClear — HARD clear: start in Simple (Alex repro),
// flip Simple↔Expert several times, then every distinct tab pair A→B
// back and forth. Inactive pageCtl must be Hidden and not WS_VISIBLE;
// never two page trees visible at once (Gap:-1 overlap).
func TestCHK_TRAY_47_PageClear(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newViberCfg()
	app := startCfgApp(t, svc.cfgService, true, nil)
	// Start Simple — the live Win failure path (#6 missed this).
	w, err := newSettingsWindow(settingsConfig{lang: i18n.RU, mode: traysettings.Admin, session: app.session,
		hidden: false, // visible window when possible
		uiMode: traysettings.UISimple})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { w.close(); app.Exit() }()
	w.Reload()
	pumpUntil(t, "loaded", func() bool { w.update(); return !w.busy && w.ed.HasProfile() })

	check := func(tag string) {
		t.Helper()
		if why := w.assertPageClear(); why != "" {
			t.Fatalf("%s page=%d mode=%v: %s", tag, w.page, w.UIMode(), why)
		}
		// mandatory: inactive pageCtl HWNDs
		for i, hs := range w.pageCtl {
			if i == w.page {
				continue
			}
			for _, h := range hs {
				if h == 0 {
					continue
				}
				if winapi.WindowStyle(h)&winapi.WS_VISIBLE != 0 {
					t.Fatalf("%s: pageCtl[%d] hwnd still WS_VISIBLE", tag, i)
				}
				if c := w.ctlOf[h]; c != nil && !c.Hidden {
					t.Fatalf("%s: pageCtl[%d] Ctl.Hidden=false", tag, i)
				}
			}
			if n := w.pages[i]; n != nil {
				ui.Walk(n, func(c *ui.Ctl) {
					if c == nil || c.H == 0 {
						return
					}
					if !c.Hidden {
						t.Errorf("%s: pages[%d] not Hidden", tag, i)
					}
					if visible(c.H) {
						t.Errorf("%s: pages[%d] still WS_VISIBLE", tag, i)
					}
				})
			}
		}
	}

	check("simple-start")
	if w.UIMode() != traysettings.UISimple || visible(w.ctl[idSetPort]) {
		t.Fatalf("want Simple with expert fields hidden")
	}

	// Simple ↔ Expert several times (mandatory)
	for i := 0; i < 3; i++ {
		w.toggleUIMode()
		check("to-expert")
		if w.UIMode() != traysettings.UIExpert || !visible(w.ctl[idSetHost]) {
			t.Fatalf("expert flip %d: host not visible", i)
		}
		if visible(w.ctl[idGenPoll]) || visible(w.ctl[idVbToken]) {
			t.Fatalf("expert flip %d: other page controls visible (overlap)", i)
		}
		w.toggleUIMode()
		check("to-simple")
		if w.UIMode() != traysettings.UISimple || visible(w.ctl[idSetPort]) || visible(w.ctl[idGenPoll]) {
			t.Fatalf("simple flip %d: expert/general still visible", i)
		}
	}

	// Expert: all distinct tab pairs A→B, then back, several rounds
	w.toggleUIMode()
	pumpUntil(t, "viber tab", func() bool { w.update(); return w.viberAvailable() && winapi.TabItemCount(w.ctl[idSetTabs]) >= 3 })
	pages := []int{0, 1, pageViber}
	for round := 0; round < 2; round++ {
		for _, a := range pages {
			for _, b := range pages {
				if a == b {
					continue
				}
				w.setPage(a)
				check("pair-a")
				w.setPage(b)
				check("pair-b")
				if w.page != b || !w.navTabSelected(b) {
					t.Fatalf("round %d %d→%d: page=%d", round, a, b, w.page)
				}
				// active page sample control visible; previous not
				switch b {
				case 0:
					if !visible(w.ctl[idSetHost]) || visible(w.ctl[idGenPoll]) || visible(w.ctl[idVbToken]) {
						t.Fatalf("databases not exclusive after %d→%d", a, b)
					}
				case 1:
					if !visible(w.ctl[idGenPoll]) || visible(w.ctl[idSetHost]) || visible(w.ctl[idVbToken]) {
						t.Fatalf("general not exclusive after %d→%d", a, b)
					}
				case pageViber:
					if !visible(w.ctl[idVbToken]) || visible(w.ctl[idSetHost]) || visible(w.ctl[idGenPoll]) {
						t.Fatalf("viber not exclusive after %d→%d", a, b)
					}
				}
			}
		}
	}
}
