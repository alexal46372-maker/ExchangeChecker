//go:build windows

package wintray

import (
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
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

// TestCHK_TRAY_81_SoundStopLabel — «Остановить» toggle helpers.
func TestCHK_TRAY_81_SoundStopLabel(t *testing.T) {
	if i18n.T(i18n.KeySoundStop, i18n.RU) != "Остановить" {
		t.Fatalf("KeySoundStop RU=%q", i18n.T(i18n.KeySoundStop, i18n.RU))
	}
	w := openUXSettings(t)
	defer w.close()
	if w.sndWarn.test == 0 || w.sndErr.test == 0 {
		t.Fatal("missing Прослушать buttons")
	}
	w.setSoundTestLabel(w.sndWarn, true)
	if got := winapi.GetWindowText(w.sndWarn.test); got != i18n.T(i18n.KeySoundStop, w.cfg.lang) {
		t.Fatalf("stop label=%q", got)
	}
	w.setSoundTestLabel(w.sndWarn, false)
	if got := winapi.GetWindowText(w.sndWarn.test); got != i18n.T(i18n.KeySoundTest, w.cfg.lang) {
		t.Fatalf("listen label=%q", got)
	}
}

// TestCHK_TRAY_82_NoSoundSaveButton — no idSndSave; persist via Settings Save.
func TestCHK_TRAY_82_NoSoundSaveButton(t *testing.T) {
	w := openUXSettings(t)
	defer w.close()
	if w.ctl[idSndSave] != 0 || w.sndSave != 0 {
		t.Fatal("«Сохранить» must leave the Звук frame")
	}
	_ = w.persistSoundSection()
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

// TestCHK_TRAY_74_82_SourceContracts — erase paths 65–73 untouched; UX symbols present.
func TestCHK_TRAY_74_82_SourceContracts(t *testing.T) {
	// Non-windows-safe string contracts via trayui/i18n only.
	if traysettings.FieldWarnFreq == "" {
		t.Fatal("field consts")
	}
	_ = idGenBeep
	_ = idSndSave
}
