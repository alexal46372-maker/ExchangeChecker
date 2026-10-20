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
		if winapi.IsWindowEnabled(w.ctl[idSetNavProfiles]) || !winapi.IsWindowEnabled(w.ctl[idSetPage]) {
			t.Errorf("%s: databases page not marked", lang)
		}
		barLeft := func() int32 { return winapi.GetWindowRect(w.ctl[idSetStatus]).Left }
		for _, p := range []struct {
			id   int32
			page int
		}{{idSetPage, 1}, {idSetViber, pageViber}} {
			w.click(t, p.id)
			if w.page != p.page || winapi.IsWindowEnabled(w.ctl[p.id]) || !winapi.IsWindowEnabled(w.ctl[idSetNavProfiles]) {
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
