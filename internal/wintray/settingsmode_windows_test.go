//go:build windows

package wintray

import (
	"runtime"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

type memUIMode struct{ m traysettings.UIMode }

func (s *memUIMode) Load() (traysettings.UIMode, error) { return s.m, nil }
func (s *memUIMode) Save(m traysettings.UIMode) error   { s.m = m; return nil }

// TestCHK_TRAY_41_SettingsModes — «Простой» hides the expert fields and
// requires server, database, login method and, for SQL, login and
// password (red via WM_CTLCOLOREDIT + asterisk); «Экспертный» shows
// everything and requires only what the model requires; the toggle is
// stored.
func TestCHK_TRAY_41_SettingsModes(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	app := startCfgApp(t, svc, true, nil)
	store := &memUIMode{}
	w, err := newSettingsWindow(settingsConfig{lang: i18n.RU, mode: traysettings.Admin, session: app.session, hidden: true,
		uiMode: loadUIMode(store), uiStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	if w.UIMode() != traysettings.UISimple || w.expertShown || winapi.GetWindowText(w.ctl[idSetUIMode]) != "Экспертный режим" ||
		w.tips.text(w.ctl[idSetUIMode]) == "" {
		t.Fatalf("simple by default: %v %q", w.UIMode(), winapi.GetWindowText(w.ctl[idSetUIMode]))
	}
	w.Reload()
	pumpUntil(t, "loaded", func() bool { return !w.busy && w.ed.HasProfile() })
	w.newProfile()
	w.ed.Form.Auth = model.AuthSQL
	w.update()
	for _, id := range []int32{idSetHost, idSetDatabase, idSetLogin, idSetPassword} {
		if !w.Missing()[id] || !strings.HasSuffix(winapi.GetWindowText(w.labels[id]), " *") {
			t.Errorf("simple: %d not highlighted (%q)", id, winapi.GetWindowText(w.labels[id]))
		}
	}
	if br, ok := w.requiredColor(0, w.ctl[idSetHost]); !ok || br == 0 {
		t.Error("WM_CTLCOLOREDIT brush")
	}
	if _, ok := w.requiredColor(0, w.ctl[idSetName]); ok {
		t.Error("name has a default: never red")
	}
	w.set(idSetHost, "srv")
	if w.Missing()[idSetHost] {
		t.Error("filled host still red")
	}
	// expert: password not required, the toggle is stored
	w.click(t, idSetUIMode)
	if store.m != traysettings.UIExpert || !w.expertShown || winapi.GetWindowText(w.ctl[idSetUIMode]) != "Простой режим" {
		t.Fatalf("expert %v", store.m)
	}
	if w.Missing()[idSetPassword] || strings.HasSuffix(winapi.GetWindowText(w.labels[idSetPassword]), " *") {
		t.Error("expert: password is not required by the model")
	}
	if !w.Missing()[idSetDatabase] || !w.Missing()[idSetLogin] {
		t.Error("expert: database / login required")
	}
	// windows login: no login / password
	w.ed.Form.Auth = model.AuthWindows
	w.update()
	if w.Missing()[idSetLogin] {
		t.Error("windows auth needs no login")
	}
	// CHK-TRAY-61: General stays visible in Simple (do not bounce off it)
	w.click(t, idSetPage)
	if w.page != 1 {
		t.Fatalf("want General page, got %d", w.page)
	}
	w.click(t, idSetUIMode)
	if w.page != 1 || w.UIMode() != traysettings.UISimple {
		t.Errorf("simple mode must keep General tab active, page=%d mode=%v", w.page, w.UIMode())
	}
	if winapi.TabItemCount(w.ctl[idSetTabs]) < 2 || w.tabPageAt(0) != 1 || w.tabPageAt(1) != 0 {
		t.Errorf("simple mode must keep General tab (CHK-TRAY-75 order), pages=%v", w.tabPages)
	}
}
