//go:build windows

package wintray

import (
	"runtime"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_60_BasesChromeOnlyOnDatabases — profile list + Add / Delete /
// Clone / Discover live only inside the Databases tab panel. On General /
// Viber they must not be WS_VISIBLE / IsWindowVisible (CHK-TRAY-60).
func TestCHK_TRAY_60_BasesChromeOnlyOnDatabases(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newViberCfg()
	app := startCfgApp(t, svc.cfgService, true, nil)
	w, err := newSettingsWindow(settingsConfig{lang: i18n.RU, mode: traysettings.Admin, session: app.session,
		hidden: true, uiMode: traysettings.UIExpert})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { w.close(); app.Exit() }()
	w.Reload()
	pumpUntil(t, "loaded", func() bool { w.update(); return !w.busy && w.ed.HasProfile() && w.viberAvailable() })

	bases := []int32{idSetList, idSetAdd, idSetDelete, idSetClone, idSetDiscover}
	w.setPage(0)
	for _, id := range bases {
		if w.ctl[id] == 0 {
			t.Fatalf("bases control %d missing on Databases", id)
		}
		if id != idSetDiscover && !visible(w.ctl[id]) {
			t.Fatalf("bases control %d not visible on Databases", id)
		}
	}
	if !w.underPagePanel(w.ctl[idSetList]) || !w.underPagePanel(w.ctl[idSetAdd]) {
		t.Fatal("list/Add must be parented under pagePanel[0], not left chrome")
	}
	if why := assertSamePageNoOverlap(w); why != "" {
		t.Fatal("Databases same-page:", why)
	}

	for _, page := range []int{1, pageViber} {
		w.setPage(page)
		if why := w.assertPageClear(); why != "" {
			t.Fatalf("page %d clear: %s", page, why)
		}
		if why := assertBasesChromeHidden(w); why != "" {
			t.Fatalf("page %d: %s", page, why)
		}
		if why := assertSamePageNoOverlap(w); why != "" {
			t.Fatalf("page %d same-page: %s", page, why)
		}
		// hard: Children(active panel) must not include Bases chrome
		active := w.pagePanel[page]
		for _, id := range bases {
			h := w.ctl[id]
			if h == 0 {
				continue
			}
			if underHWND(active, h) {
				t.Fatalf("page %d: bases HWND %d is under active panel", page, id)
			}
			if winapi.IsWindowVisible(h) {
				t.Fatalf("page %d: bases HWND %d IsWindowVisible", page, id)
			}
		}
	}
}

// TestCHK_TRAY_61_WindowCommonAndSimpleGeneral — Export/Import + Simple/Expert
// are window-common (footer bar, visible on every tab). Simple keeps the
// General tab; Viber may hide. Theme combo is gone from General.
func TestCHK_TRAY_61_WindowCommonAndSimpleGeneral(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newViberCfg()
	app := startCfgApp(t, svc.cfgService, true, nil)
	w, err := newSettingsWindow(settingsConfig{lang: i18n.RU, mode: traysettings.Admin, session: app.session,
		hidden: true, uiMode: traysettings.UIExpert})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { w.close(); app.Exit() }()
	w.Reload()
	pumpUntil(t, "loaded", func() bool { w.update(); return !w.busy && w.ed.HasProfile() && w.viberAvailable() })

	if w.ctl[idGenTheme] != 0 {
		t.Fatal("idGenTheme must be removed from General (CHK-TRAY-61)")
	}
	common := []int32{idSetUIMode, idSetExport, idSetImport}
	for _, page := range []int{0, 1, pageViber} {
		w.setPage(page)
		for _, id := range common {
			h := w.ctl[id]
			if h == 0 {
				t.Fatalf("page %d: common control %d missing", page, id)
			}
			if !visible(h) {
				t.Fatalf("page %d: common control %d not visible", page, id)
			}
			if w.underPagePanel(h) {
				t.Fatalf("page %d: common control %d must be window bar, not tab panel", page, id)
			}
		}
	}

	// Simple: General tab always present; Viber may hide
	w.setPage(1)
	w.toggleUIMode()
	if w.UIMode() != traysettings.UISimple {
		t.Fatal("want Simple")
	}
	if w.page != 1 {
		t.Fatalf("Simple must keep General active, page=%d", w.page)
	}
	n := winapi.TabItemCount(w.ctl[idSetTabs])
	if n < 2 || w.tabPageAt(0) != 1 || w.tabPageAt(1) != 0 {
		t.Fatalf("Simple tabs want General+Databases (CHK-TRAY-75), got %d pages=%v", n, w.tabPages)
	}
	for _, p := range w.tabPages {
		if p == pageViber {
			t.Fatal("Viber must be expert-only in Simple")
		}
	}
	// basic General items stay; expert gen fields hide
	if !visible(w.ctl[idGenLang]) || !visible(w.ctl[idGenWarn]) {
		t.Fatal("Simple General: language / warn threshold must stay")
	}
	if visible(w.ctl[idGenPoll]) {
		t.Fatal("Simple General: expert poll must hide")
	}
	if w.ctl[idGenWarnFreq] != 0 && visible(w.ctl[idGenWarnFreq]) {
		t.Fatal("Simple General: sound freq dup must stay removed/hidden (CHK-TRAY-74)")
	}
	// Export/Import/UIMode still common
	for _, id := range common {
		if !visible(w.ctl[id]) {
			t.Fatalf("Simple: common %d must stay visible", id)
		}
	}
}

// TestCHK_TRAY_62_SoundInGeneral — alarm profiles live in General «Звук»
// (warning+error); tray menu has no CmdSound (CHK-TRAY-62).
func TestCHK_TRAY_62_SoundInGeneral(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	store := &memSoundStore{}
	svc := newViberCfg()
	app := startCfgApp(t, svc.cfgService, true, nil)
	app.opt.SoundStore = store
	app.loadSoundPrefs()
	w, err := newSettingsWindow(settingsConfig{lang: i18n.RU, mode: traysettings.Admin, session: app.session,
		hidden: true, uiMode: traysettings.UIExpert,
		stores: trayStores{Sound: store},
		onSoundSave: app.SaveSoundPrefs, onSoundTest: app.testSound, onSoundStop: app.stopTestSound,
		soundPrefs: func() trayalert.SoundPrefs { return app.SoundPrefs() },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { w.close(); app.Exit() }()
	w.Reload()
	pumpUntil(t, "loaded", func() bool { w.update(); return !w.busy && w.ed.HasProfile() })

	w.setPage(1)
	if w.sndWarn.freq == 0 || w.sndErr.freq == 0 || w.sndSave == 0 {
		t.Fatal("sound section controls missing on General")
	}
	if !w.underPagePanel(w.sndWarn.freq) || !w.underPagePanel(w.sndSave) {
		t.Fatal("sound controls must be under General page panel")
	}
	if winapi.GetWindowText(w.sndWarn.vol) != "80" || winapi.GetWindowText(w.sndErr.vol) != "100" {
		t.Fatalf("defaults warn=%q err=%q", winapi.GetWindowText(w.sndWarn.vol), winapi.GetWindowText(w.sndErr.vol))
	}
	// invalid → not saved
	winapi.SetWindowText(w.sndErr.freq, "5")
	winapi.SendMessage(w.sndSave, winapi.BM_CLICK, 0, 0)
	if store.saves != 0 || !strings.Contains(winapi.GetWindowText(w.sndInfo), "Ошибка") {
		t.Fatalf("invalid saved: %q saves=%d", winapi.GetWindowText(w.sndInfo), store.saves)
	}
	winapi.SetWindowText(w.sndErr.freq, "1500")
	winapi.SetWindowText(w.sndErr.vol, "60")
	winapi.SendMessage(w.sndSave, winapi.BM_CLICK, 0, 0)
	if store.saves != 1 || app.SoundPrefs().Error.FreqHz != 1500 || app.SoundPrefs().Error.Volume != 60 {
		t.Fatalf("save failed saves=%d prefs=%+v", store.saves, app.SoundPrefs())
	}
	// menu: no CmdSound; Theme present (CHK-TRAY-63 also)
	var sound, theme bool
	for _, it := range app.RightMenu() {
		if it.ID == trayui.CmdSound {
			sound = true
		}
		for _, s := range it.Sub {
			if s.ID == trayui.CmdThemeLight || s.ID == trayui.CmdThemeDark {
				theme = true
			}
		}
	}
	if sound {
		t.Fatal("CmdSound must not appear in tray menu")
	}
	if !theme {
		t.Fatal("Theme submenu missing from tray menu")
	}
}
