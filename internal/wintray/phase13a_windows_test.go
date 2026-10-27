//go:build windows

package wintray

import (
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
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
	if n < 2 || w.tabPageAt(0) != 0 || w.tabPageAt(1) != 1 {
		t.Fatalf("Simple tabs want Databases+General, got %d pages=%v", n, w.tabPages)
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
	if visible(w.ctl[idGenPoll]) || visible(w.ctl[idGenWarnFreq]) {
		t.Fatal("Simple General: expert poll/freq must hide")
	}
	// Export/Import/UIMode still common
	for _, id := range common {
		if !visible(w.ctl[id]) {
			t.Fatalf("Simple: common %d must stay visible", id)
		}
	}
}
