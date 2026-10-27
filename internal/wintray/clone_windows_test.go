//go:build windows

package wintray

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_58_CloneButton — CapCloneProfile from ProfileCloner; admin
// settings show «Клонировать базу», click clones and focuses password.
func TestCHK_TRAY_58_CloneButton(t *testing.T) {
	svc := newCfgService()
	app := startCfgApp(t, svc, true, nil)
	defer app.Exit()
	if !app.session.Status().Has(model.CapCloneProfile) {
		t.Fatalf("caps %v", app.session.Status().Capabilities)
	}
	w := openAdmin(t, app)
	w.cfg.confirm = func(uintptr, string, string) bool { return true }
	pumpUntil(t, "clone visible", func() bool {
		w.update()
		return visible(w.ctl[idSetClone]) && winapi.IsWindowEnabled(w.ctl[idSetClone])
	})
	ru, en := i18n.T(i18n.KeySetClone, i18n.RU), i18n.T(i18n.KeySetClone, i18n.EN)
	got := winapi.GetWindowText(w.ctl[idSetClone])
	if got != ru && got != en {
		t.Fatalf("label %q", got)
	}
	srcID := w.ed.Base.ID
	nBefore := len(w.ed.Profiles)
	w.click(t, idSetClone)
	pumpUntil(t, "cloned", func() bool {
		return !w.busy && len(w.ed.Profiles) > nBefore && w.ed.Base.ID != srcID
	})
	if len(svc.clones) != 1 {
		t.Fatalf("clones %v", svc.clones)
	}
	if w.lastFocus != traysettings.FieldPassword {
		t.Fatalf("focus %v", w.lastFocus)
	}
}

// TestCHK_TRAY_58_CloneHiddenReadOnly — read-only hides the button.
func TestCHK_TRAY_58_CloneHiddenReadOnly(t *testing.T) {
	svc := newCfgService()
	app := startCfgApp(t, svc, true, &fakeElevator{})
	defer app.Exit()
	w, err := newSettingsWindow(settingsConfig{lang: i18n.RU, mode: traysettings.ReadOnly, session: app.session, hidden: true,
		uiMode: traysettings.UISimple, elevator: &fakeElevator{}})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	w.Reload()
	pumpUntil(t, "loaded", func() bool { return !w.busy && w.loaded })
	w.update()
	if visible(w.ctl[idSetClone]) {
		t.Fatal("clone visible in read-only")
	}
}

// TestCHK_TRAY_58_CloneStrings — RU+EN.
func TestCHK_TRAY_58_CloneStrings(t *testing.T) {
	for _, lang := range []string{i18n.RU, i18n.EN} {
		for _, k := range []string{i18n.KeySetClone, i18n.KeySetCloneConfirm, i18n.KeySetClonedNeedPw, i18n.KeyTipSetClone} {
			if i18n.T(k, lang) == k || i18n.T(k, lang) == "" {
				t.Errorf("%s %s missing", k, lang)
			}
		}
	}
}
