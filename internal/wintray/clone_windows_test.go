//go:build windows

package wintray

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_58_CloneButton — admin + CapCloneProfile shows the button;
// click clones, selects the new profile and focuses the password field.
func TestCHK_TRAY_58_CloneButton(t *testing.T) {
	for _, lang := range []string{i18n.RU, i18n.EN} {
		t.Run(lang, func(t *testing.T) {
			svc := newCfgService()
			app := startCfgApp(t, svc, true, nil)
			defer app.Exit()
			if !app.session.Supported(model.CmdCloneProfile) {
				t.Fatalf("missing clone_profile cap: %v", app.session.Status().Capabilities)
			}
			w, err := newSettingsWindow(settingsConfig{lang: lang, mode: traysettings.Admin, session: app.session, hidden: true,
				uiMode: traysettings.UISimple, confirm: func(uintptr, string, string) bool { return true }})
			if err != nil {
				t.Fatal(err)
			}
			defer w.close()
			w.Reload()
			pumpUntil(t, "loaded", func() bool { return !w.busy && w.ed.HasProfile() && w.ed.Revision != "" })
			pumpUntil(t, "clone visible", func() bool {
				w.update()
				return visible(w.ctl[idSetClone]) && winapi.IsWindowEnabled(w.ctl[idSetClone])
			})
			if got := winapi.GetWindowText(w.ctl[idSetClone]); got != i18n.T(i18n.KeySetClone, lang) {
				t.Fatalf("label %q want %q caps=%v", got, i18n.T(i18n.KeySetClone, lang), app.session.Status().Capabilities)
			}
			srcID := w.ed.Base.ID
			nBefore := len(w.ed.Profiles)
			w.click(t, idSetClone)
			pumpUntil(t, "cloned", func() bool {
				return !w.busy && len(w.ed.Profiles) > nBefore && w.ed.Base.ID != srcID
			})
			if len(svc.clones) != 1 || svc.clones[0] != srcID {
				t.Fatalf("clones %v want [%s]", svc.clones, srcID)
			}
			if w.lastFocus != traysettings.FieldPassword {
				t.Fatalf("focus %v want password", w.lastFocus)
			}
			if w.ed.Base.HasPassword {
				t.Fatal("clone must not copy password")
			}
		})
	}
}

// TestCHK_TRAY_58_CloneHiddenReadOnly — read-only settings hide the button.
func TestCHK_TRAY_58_CloneHiddenReadOnly(t *testing.T) {
	svc := newCfgService()
	app := startCfgApp(t, svc, false, &fakeElevator{})
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

// TestCHK_TRAY_58_CloneStrings — RU+EN labels and tips.
func TestCHK_TRAY_58_CloneStrings(t *testing.T) {
	for _, lang := range []string{i18n.RU, i18n.EN} {
		for _, k := range []string{i18n.KeySetClone, i18n.KeySetCloneConfirm, i18n.KeySetClonedNeedPw, i18n.KeyTipSetClone} {
			if i18n.T(k, lang) == k || i18n.T(k, lang) == "" {
				t.Errorf("%s %s missing", k, lang)
			}
		}
	}
	if model.CapCloneProfile != "clone_profile" || model.CmdCloneProfile != "clone_profile" {
		t.Fatal(model.CapCloneProfile, model.CmdCloneProfile)
	}
}
