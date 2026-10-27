//go:build windows

package wintray

import (
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
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
		nTabs := winapi.TabItemCount(w.ctl[idSetTabs])
		if nTabs != 1 || len(w.tabPages) != 1 || w.tabPageAt(0) != 0 {
			t.Errorf("%s: simple mode must keep only Databases tab, got win=%d pages=%v", lang, nTabs, w.tabPages)
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

// TestCHK_TRAY_47_PageClear — HARD clear (Conchita @400d1ae): one child
// panel HWND per tab. After every switch, enum Children(host): the only
// visible page tree is the active panel; any GetWindowRect overlap of two
// visible host-siblings fails. Start Simple, Simple↔Expert, all tab pairs.
// Do not whack-a-mole pageCtl alone — that left CI green with live ghosts.
func TestCHK_TRAY_47_PageClear(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newViberCfg()
	app := startCfgApp(t, svc.cfgService, true, nil)
	// Start Simple — the live Win failure path (#6 / #8 missed ghosts).
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
		if why := assertHostChildrenClear(w); why != "" {
			t.Fatalf("%s host-enum: %s", tag, why)
		}
		for i := range w.pagePanel {
			if w.pagePanel[i] == 0 {
				t.Fatalf("%s: pagePanel[%d] missing", tag, i)
			}
		}
	}

	check("simple-start")
	if w.UIMode() != traysettings.UISimple || visible(w.ctl[idSetPort]) {
		t.Fatalf("want Simple with expert fields hidden")
	}

	// Simple ↔ Expert several times (mandatory); expert fields stay in panel[0]
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

// assertHostChildrenClear enums Children(host): only chrome + the active
// pagePanel may be IsWindowVisible; any other visible page panel / any
// GetWindowRect overlap of two visible direct siblings fails.
func assertHostChildrenClear(w *settingsWindow) string {
	if w.host == 0 {
		return "no host"
	}
	active := w.pagePanel[w.page]
	var visibleSiblings []struct {
		h uintptr
		r winapi.RECT
	}
	for _, h := range winapi.Children(w.host) {
		if h == 0 || !winapi.IsWindowVisible(h) {
			continue
		}
		// inactive page panels must not be visible
		for i, pan := range w.pagePanel {
			if pan == h && i != w.page {
				return "inactive pagePanel visible among host children"
			}
		}
		// any direct host child that belongs to an inactive page (orphan)
		if pg := w.pageOfHWND(h); pg >= 0 && pg != w.page && h != active {
			return "host child belongs to inactive page but IsWindowVisible"
		}
		visibleSiblings = append(visibleSiblings, struct {
			h uintptr
			r winapi.RECT
		}{h, winapi.GetWindowRect(h)})
	}
	for a := 0; a < len(visibleSiblings); a++ {
		for b := a + 1; b < len(visibleSiblings); b++ {
			ra, rb := visibleSiblings[a].r, visibleSiblings[b].r
			if rectsOverlap(ra, rb) {
				// chrome may sit beside the active panel without overlap;
				// overlap of two visible siblings is always a fail (Conchita)
				return "overlap: GetWindowRect of two visible host siblings"
			}
		}
	}
	// active panel tree must not expose inactive-page ghosts via IsWindowVisible
	if active != 0 {
		for i, pan := range w.pagePanel {
			if i == w.page || pan == 0 {
				continue
			}
			if winapi.IsWindowVisible(pan) {
				return "inactive panel IsWindowVisible"
			}
			for _, ch := range winapi.Children(pan) {
				if winapi.IsWindowVisible(ch) {
					return "ghost under inactive panel (Children sweep)"
				}
			}
		}
	}
	return ""
}
