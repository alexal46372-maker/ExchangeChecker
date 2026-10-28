//go:build windows

package wintray

import (
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

func openUXSettings(t *testing.T) *settingsWindow {
	t.Helper()
	runtime.LockOSThread()
	app, _ := startChromeApp(t)
	return openChromeSettings(t, app, i18n.RU, 96, true)
}

// openUXSettingsWithSound opens chrome settings with sound preview/save hooks
// so CHK-TRAY-81/82 can exercise real start/stop/persist paths (not label-only).
func openUXSettingsWithSound(t *testing.T, onTest func(trayalert.SoundProfile, int), onStop func(), onSave func(trayalert.SoundPrefs) error) (*App, *settingsWindow) {
	t.Helper()
	runtime.LockOSThread()
	app, _ := startChromeApp(t)
	testDPI = 96
	w, err := newSettingsWindow(settingsConfig{
		lang: i18n.RU, mode: traysettings.Admin, session: app.session,
		hidden: true, uiMode: traysettings.UIExpert,
		onSoundTest: onTest,
		onSoundStop: onStop,
		onSoundSave: onSave,
		soundPrefs:  func() trayalert.SoundPrefs { return trayalert.DefaultSoundPrefs(nil) },
	})
	if err != nil {
		t.Fatal(err)
	}
	w.Reload()
	pumpUntil(t, "chrome caps", func() bool {
		w.update()
		return !w.busy && w.ed.HasProfile() && w.viberAvailable() && w.emailAvailable() &&
			winapi.TabItemCount(w.ctl[idSetTabs]) >= 4
	})
	return app, w
}

// TestCHK_TRAY_74_NoSoundDupInGeneral — upper General has no freq/dur/gap;
// BeepEnabled checkbox lives in «Звук».
func TestCHK_TRAY_74_NoSoundDupInGeneral(t *testing.T) {
	for _, e := range genEdits {
		switch e.id {
		case idGenWarnFreq, idGenWarnDur, idGenErrFreq, idGenErrDur, idGenGap:
			t.Fatalf("genEdits must not include sound dup id=%d field=%s", e.id, e.field)
		}
	}
	w := openUXSettings(t)
	defer w.close()
	if w.ctl[idGenWarnFreq] != 0 || w.ctl[idGenWarnDur] != 0 || w.ctl[idGenErrFreq] != 0 ||
		w.ctl[idGenErrDur] != 0 || w.ctl[idGenGap] != 0 {
		t.Fatal("upper General still has sound freq/dur/gap HWNDs")
	}
	if w.ctl[idGenBeep] == 0 {
		t.Fatal("BeepEnabled checkbox missing (should be in «Звук»)")
	}
	if pg := w.pageOfHWND(w.ctl[idGenBeep]); pg != 1 {
		t.Fatalf("BeepEnabled page=%d want General(1)", pg)
	}
}

// TestCHK_TRAY_75_TabOrderGeneralFirst — tabs: General, Databases, ….
func TestCHK_TRAY_75_TabOrderGeneralFirst(t *testing.T) {
	w := openUXSettings(t)
	defer w.close()
	if len(w.tabPages) < 2 || w.tabPageAt(0) != 1 || w.tabPageAt(1) != 0 {
		t.Fatalf("want General then Databases, got %v", w.tabPages)
	}
	// Tab order is General-first; open on Databases (working tab).
	if w.page != 0 || !w.navTabSelected(0) {
		t.Fatalf("default page=%d want Databases(0)", w.page)
	}
}

// TestCHK_TRAY_76_HostFlushUnderTabs — no pad gap under SysTabControl.
func TestCHK_TRAY_76_HostFlushUnderTabs(t *testing.T) {
	w := openUXSettings(t)
	defer w.close()
	w.layoutFit()
	tab, host := w.ctl[idSetTabs], w.host
	if tab == 0 || host == 0 {
		t.Fatal("missing tab/host")
	}
	tr, hr := winapi.GetWindowRect(tab), winapi.GetWindowRect(host)
	if gap := hr.Top - tr.Bottom; gap > 2 {
		t.Fatalf("white strip under tabs: gap=%d tab=%v host=%v chromeH=%d", gap, tr, hr, w.tabChromeH)
	}
}

// TestCHK_TRAY_77_TabsFillHostWidth — General content width tracks host.
func TestCHK_TRAY_77_TabsFillHostWidth(t *testing.T) {
	w := openUXSettings(t)
	defer w.close()
	w.setPage(1)
	w.layoutFit()
	if w.pageW <= 0 || w.viewW <= 0 {
		t.Fatalf("pageW=%d viewW=%d", w.pageW, w.viewW)
	}
	if w.pageW+px(24) < w.viewW {
		t.Fatalf("General pageW=%d too narrow vs viewW=%d", w.pageW, w.viewW)
	}
}

// TestCHK_TRAY_78_DBListNotScrolled — left list chrome is host-pinned; form scrolls.
func TestCHK_TRAY_78_DBListNotScrolled(t *testing.T) {
	w := openUXSettings(t)
	defer w.close()
	w.setPage(0)
	lv := w.ctl[idSetList]
	if lv == 0 || !w.pinnedHost[lv] {
		t.Fatalf("list pinned=%v hwnd=%v", w.pinnedHost[lv], lv)
	}
	if w.underPagePanel(lv) {
		t.Fatal("pinned list must be host child, not under pagePanel")
	}
	for _, id := range []int32{idSetAdd, idSetDelete, idSetClone} {
		if h := w.ctl[id]; h != 0 && !w.pinnedHost[h] {
			t.Fatalf("id %d not pinned", id)
		}
	}
	if h := w.ctl[idSetHost]; h != 0 && w.pinnedHost[h] {
		t.Fatal("host field must scroll with right panel")
	}
	before := winapi.GetWindowRect(lv)
	hostBefore := winapi.GetWindowRect(w.ctl[idSetHost])
	w.scrollTo(0, 1<<20)
	_, sy := w.ScrollPos()
	after := winapi.GetWindowRect(lv)
	hostAfter := winapi.GetWindowRect(w.ctl[idSetHost])
	if after.Top != before.Top {
		t.Fatalf("list scrolled with sy=%d: before=%v after=%v", sy, before, after)
	}
	if sy > 0 && hostAfter.Top == hostBefore.Top {
		t.Fatalf("host field did not scroll sy=%d: %v", sy, hostBefore)
	}
}

// TestCHK_TRAY_80_PollColumnNotInChecks — ListView poll column; no checks checkbox.
func TestCHK_TRAY_80_PollColumnNotInChecks(t *testing.T) {
	w := openUXSettings(t)
	defer w.close()
	if w.ctl[idSetEnabled] != 0 {
		t.Fatal("idSetEnabled must leave «Опрос и пороги»")
	}
	lv := w.ctl[idSetList]
	if lv == 0 {
		t.Fatal("missing DB list")
	}
	// ListView speaks LVM_*; LISTBOX would ignore / return 0 differently — at least HWND exists
	// and checkbox helpers round-trip.
	winapi.ListViewSetChecked(lv, 0, true)
	if !winapi.ListViewChecked(lv, 0) && winapi.SendMessage(lv, winapi.LVM_GETITEMCOUNT, 0, 0) > 0 {
		t.Fatal("ListView checkbox round-trip failed")
	}
	if i18n.T(i18n.KeySetColPoll, i18n.RU) != "Опрос" || i18n.T(i18n.KeySetColPoll, i18n.EN) != "Poll" {
		t.Fatal("KeySetColPoll RU/EN")
	}
}

// TestCHK_TRAY_81_SoundStopLabel — «Прослушать» really starts preview for
// Warning and Error, Stop halts it (onSoundStop), label returns to «Прослушать».
// setSoundTestLabel alone is not enough (CHK-TRAY-81).
func TestCHK_TRAY_81_SoundStopLabel(t *testing.T) {
	if i18n.T(i18n.KeySoundStop, i18n.RU) != "Остановить" {
		t.Fatalf("KeySoundStop RU=%q", i18n.T(i18n.KeySoundStop, i18n.RU))
	}
	listen := i18n.T(i18n.KeySoundTest, i18n.RU)
	stop := i18n.T(i18n.KeySoundStop, i18n.RU)

	var started []trayalert.SoundProfile
	var stops int
	previewing := false
	_, w := openUXSettingsWithSound(t,
		func(p trayalert.SoundProfile, _ int) {
			started = append(started, p)
			previewing = true
			// Do not auto-end: keep «playing» until stopSoundPreview / onSoundStop.
		},
		func() {
			stops++
			previewing = false
		},
		nil,
	)
	defer w.close()

	if w.sndWarn.test == 0 || w.sndErr.test == 0 {
		t.Fatal("missing Прослушать buttons")
	}

	// Stay on this goroutine (LockOSThread) — no t.Run: Win32 BM_CLICK hangs off-thread.
	for _, tc := range []struct {
		name string
		id   int32
		ctl  profileCtl
	}{
		{"Warning", idSndWarn + idSndTest, w.sndWarn},
		{"Error", idSndErr + idSndTest, w.sndErr},
	} {
		started = nil
		stops = 0
		previewing = false
		btn := tc.ctl.test
		if got := winapi.GetWindowText(btn); got != listen {
			t.Fatalf("%s initial label=%q want %q", tc.name, got, listen)
		}
		w.click(t, tc.id)
		if len(started) != 1 {
			t.Fatalf("%s previewSound/onSoundTest not called: started=%d", tc.name, len(started))
		}
		if !previewing {
			t.Fatalf("%s preview flag false after start", tc.name)
		}
		if got := winapi.GetWindowText(btn); got != stop {
			t.Fatalf("%s playing label=%q want %q", tc.name, got, stop)
		}
		// previewSound preemptively calls stopSoundPreview (onSoundStop) before start;
		// assert an *additional* stop on «Остановить».
		stopsAtPlay := stops
		w.click(t, tc.id) // «Остановить»
		if stops != stopsAtPlay+1 {
			t.Fatalf("%s onSoundStop after halt=%d want %d", tc.name, stops, stopsAtPlay+1)
		}
		if previewing {
			t.Fatalf("%s preview still running after stop", tc.name)
		}
		if got := winapi.GetWindowText(btn); got != listen {
			t.Fatalf("%s after stop label=%q want %q", tc.name, got, listen)
		}
	}
}

// TestCHK_TRAY_82_NoSoundSaveButton — no idSndSave; window Save / persistSoundSection writes prefs.
func TestCHK_TRAY_82_NoSoundSaveButton(t *testing.T) {
	var saved []trayalert.SoundPrefs
	_, w := openUXSettingsWithSound(t, nil, nil, func(p trayalert.SoundPrefs) error {
		saved = append(saved, p)
		return nil
	})
	defer w.close()
	if w.ctl[idSndSave] != 0 || w.sndSave != 0 {
		t.Fatal("«Сохранить» must leave the Звук frame")
	}
	if w.sndWarn.freq == 0 || w.sndErr.freq == 0 {
		t.Fatal("missing sound profile fields")
	}

	winapi.SetWindowText(w.sndWarn.freq, "1234")
	winapi.SetWindowText(w.sndErr.freq, "2345")
	if bad := w.persistSoundSection(); bad != "" {
		t.Fatalf("persistSoundSection: %s", bad)
	}
	if len(saved) != 1 {
		t.Fatalf("onSoundSave via persistSoundSection: calls=%d want 1", len(saved))
	}
	if saved[0].Warning.FreqHz != 1234 || saved[0].Error.FreqHz != 2345 {
		t.Fatalf("saved prefs warn=%d err=%d", saved[0].Warning.FreqHz, saved[0].Error.FreqHz)
	}

	// Window Save path: saveSettings calls persistSoundSection first (CHK-TRAY-82).
	saved = nil
	winapi.SetWindowText(w.sndWarn.freq, "1500")
	w.saveSettings() // same entry as idSetSave; persist is synchronous
	if len(saved) != 1 {
		t.Fatalf("saveSettings onSoundSave calls=%d want 1", len(saved))
	}
	if saved[0].Warning.FreqHz != 1500 {
		t.Fatalf("saveSettings prefs warn freq=%d want 1500", saved[0].Warning.FreqHz)
	}
}

// TestCHK_TRAY_79_MenuLanguageUnderTheme_Smoke — Language directly under Theme.
func TestCHK_TRAY_79_MenuLanguageUnderTheme_Smoke(t *testing.T) {
	m := trayui.BuildTrayRightMenu(trayui.RightMenuOpts{Lang: i18n.RU, Theme: trayui.ThemeLight})
	themeAt, langAt := -1, -1
	for i, it := range m {
		if len(it.Sub) == 2 && it.Sub[0].ID == trayui.CmdThemeLight {
			themeAt = i
		}
		if len(it.Sub) >= 2 && it.Sub[0].ID == trayui.CmdLangAuto {
			langAt = i
		}
	}
	if themeAt < 0 || langAt != themeAt+1 {
		t.Fatalf("theme@%d lang@%d menu=%+v", themeAt, langAt, m)
	}
}
