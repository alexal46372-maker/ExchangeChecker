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

// TestCHK_TRAY_47_Navigation — the settings window has a left
// navigation (PLAN §12 p.142): «Базы данных» / «Общие настройки» /
// «Уведомления Viber» and the mode switch, RU / EN with tooltips; the
// current page is disabled; hidden buttons leave no gap, neither in the
// navigation («Простой») nor in the bottom bar (the test buttons of the
// databases page on the other pages).
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
		pumpUntil(t, "loaded", func() bool { w.update(); return !w.busy && w.ed.HasProfile() && visible(w.ctl[idSetViber]) })
		nav := []struct {
			id       int32
			key, tip string
		}{{idSetNavProfiles, i18n.KeySetNavProfiles, i18n.KeyTipSetNavProfiles}, {idSetPage, i18n.KeySetNavGeneral, i18n.KeyTipSetPage},
			{idSetViber, i18n.KeySetNavViber, i18n.KeyTipVbPage}}
		for _, n := range nav {
			if h := w.ctl[n.id]; winapi.GetWindowText(h) != i18n.T(n.key, lang) || w.tips.text(h) != i18n.T(n.tip, lang) || i18n.T(n.tip, lang) == "" {
				t.Errorf("%s: nav %q tip %q", lang, winapi.GetWindowText(h), w.tips.text(h))
			}
		}
		gap := px(ui.P4)
		r := func(id int32) winapi.RECT { return w.hostRect[w.ctl[id]] }
		for i, id := range []int32{idSetPage, idSetViber, idSetUIMode} {
			prev := []int32{idSetNavProfiles, idSetPage, idSetViber}[i]
			if r(id).Top-r(prev).Bottom != gap || r(id).Left != r(prev).Left {
				t.Errorf("%s: nav button %d not right below the previous one: %+v %+v", lang, id, r(prev), r(id))
			}
		}
		if !navChecked(w.ctl[idSetNavProfiles]) || navChecked(w.ctl[idSetPage]) {
			t.Errorf("%s: databases page not marked", lang)
		}
		for _, id := range []int32{idSetNavProfiles, idSetPage, idSetViber} {
			if !winapi.IsWindowEnabled(w.ctl[id]) {
				t.Errorf("%s: nav %d disabled (the current page is pressed, not greyed)", lang, id)
			}
		}
		barLeft := func() int32 { return winapi.GetWindowRect(w.ctl[idSetStatus]).Left }
		for _, p := range []struct {
			id   int32
			page int
		}{{idSetPage, 1}, {idSetViber, pageViber}} {
			w.click(t, p.id)
			if w.page != p.page || !navChecked(w.ctl[p.id]) || navChecked(w.ctl[idSetNavProfiles]) {
				t.Errorf("%s: page %d after nav %d", lang, w.page, p.id)
			}
			// the first visible bar button starts at the margin
			if x := winapi.GetWindowRect(w.ctl[idSetReload]).Left; x != barLeft() || visible(w.ctl[idSetTest]) {
				t.Errorf("%s page %d: «Перечитать» at %d, bar starts at %d (gap of hidden buttons)", lang, p.page, x, barLeft())
			}
		}
		w.click(t, idSetNavProfiles)
		if w.page != 0 || winapi.GetWindowRect(w.ctl[idSetTest]).Left != barLeft() {
			t.Errorf("%s: back to the databases page", lang)
		}
		w.toggleUIMode() // «Простой»: no general / Viber buttons, no gap
		if visible(w.ctl[idSetPage]) || visible(w.ctl[idSetViber]) || r(idSetUIMode).Top-r(idSetNavProfiles).Bottom != gap {
			t.Errorf("%s: simple mode navigation %+v %+v", lang, r(idSetNavProfiles), r(idSetUIMode))
		}
		w.toggleUIMode()
		if r(idSetUIMode).Top <= r(idSetViber).Bottom {
			t.Errorf("%s: expert mode navigation not restored", lang)
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


// TestCHK_TRAY_47_PageClear — switching section buttons (profiles /
// general / Viber / simple↔expert) clears the previous right-panel
// controls before showing the next (Alex: fields overlapped without a
// clear). Inactive page roots stay Hidden and their HWNDs are not WS_VISIBLE.
func TestCHK_TRAY_47_PageClear(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newViberCfg()
	app := startCfgApp(t, svc.cfgService, true, nil)
	w, err := newSettingsWindow(settingsConfig{lang: i18n.RU, mode: traysettings.Admin, session: app.session, hidden: true,
		uiMode: traysettings.UIExpert})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { w.close(); app.Exit() }()
	w.Reload()
	pumpUntil(t, "loaded", func() bool { w.update(); return !w.busy && w.ed.HasProfile() && visible(w.ctl[idSetViber]) })

	assertPage := func(want int, see, hide []int32) {
		t.Helper()
		if w.page != want {
			t.Fatalf("page=%d want %d", w.page, want)
		}
		for _, id := range see {
			if h := w.ctl[id]; h == 0 || !visible(h) {
				t.Errorf("page %d: control %d should be visible", want, id)
			}
		}
		for _, id := range hide {
			if h := w.ctl[id]; h != 0 && visible(h) {
				t.Errorf("page %d: control %d still visible (overlay)", want, id)
			}
		}
		// right-panel page roots: inactive pages must stay Hidden (wizard clear)
		for i, n := range w.pages {
			if n == nil {
				continue
			}
			wantHidden := i != want
			ui.Walk(n, func(c *ui.Ctl) {
				if c == nil || c.H == 0 {
					return
				}
				if wantHidden && !c.Hidden {
					t.Errorf("page %d: pages[%d] ctl not Hidden (overlay risk)", want, i)
				}
				if wantHidden && visible(c.H) {
					t.Errorf("page %d: pages[%d] hwnd still WS_VISIBLE (overlay)", want, i)
				}
			})
		}
	}

	assertPage(0, []int32{idSetHost, idSetName}, []int32{idGenPoll, idVbToken})
	w.click(t, idSetPage)
	assertPage(1, []int32{idGenPoll, idGenBeep}, []int32{idSetHost, idVbToken})
	w.click(t, idSetViber)
	assertPage(pageViber, []int32{idVbToken, idVbEnabled}, []int32{idSetHost, idGenPoll})
	w.click(t, idSetNavProfiles)
	assertPage(0, []int32{idSetHost}, []int32{idGenPoll, idVbToken})

	// simple ↔ expert on the profiles page: expert-only fields clear
	w.toggleUIMode()
	if w.UIMode() != traysettings.UISimple || visible(w.ctl[idSetPort]) || visible(w.ctl[idSetEncrypt]) {
		t.Errorf("simple: expert fields still visible")
	}
	if !visible(w.ctl[idSetHost]) {
		t.Errorf("simple: host should stay visible")
	}
	w.toggleUIMode()
	if w.UIMode() != traysettings.UIExpert || !visible(w.ctl[idSetPort]) {
		t.Errorf("expert: port not restored")
	}
	// simple→expert then jump to Viber: profiles expert fields must clear
	w.click(t, idSetViber)
	assertPage(pageViber, []int32{idVbToken}, []int32{idSetHost, idSetPort, idGenPoll})
}
