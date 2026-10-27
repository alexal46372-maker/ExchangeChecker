package wintray

import (
	"os"
	"runtime"
	"strconv"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_08_RegistryLangStore — the per-user preference in HKCU:
// missing = as in Windows, ru/en stored as REG_SZ, "" deletes the value
// (a test key under HKCU\Software, removed afterwards).
func TestCHK_TRAY_08_RegistryLangStore(t *testing.T) {
	key := `Software\ExchangeCheckerTest-` + strconv.Itoa(os.Getpid())
	t.Cleanup(func() { winapi.RegDeleteKey(winapi.HKEY_CURRENT_USER, key) })
	s := RegistryLangStore{Key: key}
	if p, err := s.Load(); err != nil || p != "" {
		t.Fatalf("missing key: %q %v", p, err)
	}
	for _, pref := range []string{"en", "ru"} {
		if err := s.Save(pref); err != nil {
			t.Fatal(err)
		}
		if p, err := s.Load(); err != nil || p != pref {
			t.Errorf("load %q: %q %v", pref, p, err)
		}
	}
	if err := s.Save(""); err != nil {
		t.Fatal(err)
	}
	if v, err := winapi.RegGetString(winapi.HKEY_CURRENT_USER, key, LangValue); err != nil || v != "" {
		t.Errorf("value must be deleted: %q %v", v, err)
	}
	if err := winapi.RegSetString(winapi.HKEY_CURRENT_USER, key, LangValue, "de"); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Load(); p != "" {
		t.Errorf("unknown value must read as auto: %q", p)
	}
	if pref, lang := LoadLang(s); pref != "" || (lang != i18n.RU && lang != i18n.EN) {
		t.Errorf("LoadLang %q %q", pref, lang)
	}
	if ResolveLang("en") != i18n.EN || ResolveLang("ru") != i18n.RU {
		t.Error("explicit preference must win")
	}
}

type fakeLangStore struct{ saved []string }

func (f *fakeLangStore) Load() (string, error) { return "", nil }
func (f *fakeLangStore) Save(p string) error   { f.saved = append(f.saved, p); return nil }

// TestCHK_TRAY_08_SetLanguage — the menu choice is stored and applied at
// once: right menu (checked item), tooltip, alert window texts.
func TestCHK_TRAY_08_SetLanguage(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app := startCfgApp(t, newCfgService(), false, &fakeElevator{})
	store := &fakeLangStore{}
	app.opt.LangStore = store
	app.applyAlert(trayalert.View{Visible: true, Level: model.LevelWarning,
		Alerts: []model.AlertEvent{ptAlert("p1", "N1", "k1", model.LevelWarning)}})
	if app.awin == nil || winapi.GetWindowText(app.awin.ack) != i18n.T(i18n.KeyAlertAck, i18n.RU) {
		t.Fatal("alert window in Russian expected")
	}
	app.Command(trayui.CmdLangEN)
	if len(store.saved) != 1 || store.saved[0] != "en" || app.opt.Lang != i18n.EN || app.opt.LangPref != "en" {
		t.Fatalf("saved %v lang %q pref %q", store.saved, app.opt.Lang, app.opt.LangPref)
	}
	m := app.RightMenu()
	lang := menuSubByCmd(m, trayui.CmdLangEN)
	if m[0].Text != i18n.T(i18n.KeyMenuSettings, i18n.EN) || lang == nil || !lang[2].Checked || lang[0].Checked {
		t.Errorf("right menu %+v lang=%+v", m, lang)
	}
	if app.ind.Tooltip != trayui.Evaluate(app.status, i18n.EN).Tooltip {
		t.Errorf("tooltip %q", app.ind.Tooltip)
	}
	if winapi.GetWindowText(app.awin.ack) != i18n.T(i18n.KeyAlertAck, i18n.EN) ||
		winapi.GetWindowText(app.awin.close) != i18n.T(i18n.KeyAlertClose, i18n.EN) {
		t.Errorf("alert window texts %q / %q", winapi.GetWindowText(app.awin.ack), winapi.GetWindowText(app.awin.close))
	}
	app.Command(trayui.CmdLangAuto)
	auto := menuSubByCmd(app.RightMenu(), trayui.CmdLangAuto)
	if store.saved[1] != "" || app.opt.LangPref != "" || auto == nil || !auto[0].Checked {
		t.Errorf("auto: saved %v pref %q sub=%+v", store.saved, app.opt.LangPref, auto)
	}
}

// menuSubByCmd returns the Sub of the first item that contains cmd (CHK-TRAY-63 order).
func menuSubByCmd(m []trayui.Item, cmd int) []trayui.Item {
	for _, it := range m {
		for _, s := range it.Sub {
			if s.ID == cmd {
				return it.Sub
			}
		}
	}
	return nil
}
