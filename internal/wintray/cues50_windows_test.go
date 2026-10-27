//go:build windows

package wintray

import (
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// settingsCueID: UIField → settings/Viber/e-mail edit control.
var settingsCueID = map[traysettings.Field]int32{
	traysettings.FieldName: idSetName, traysettings.FieldHost: idSetHost, traysettings.FieldInstance: idSetInstance,
	traysettings.FieldPort: idSetPort, traysettings.FieldDatabase: idSetDatabase, traysettings.FieldLogin: idSetLogin,
	traysettings.FieldPassword: idSetPassword, traysettings.FieldProfWarn: idSetWarn, traysettings.FieldProfError: idSetError,
	traysettings.FieldConnTimeout: idSetConnTO, traysettings.FieldQueryTimeout: idSetQueryTO,
	traysettings.FieldProfStalled: idSetStalled, traysettings.FieldStalledMinutes: idGenStalled,
	traysettings.FieldViberToken: idVbToken,
	traysettings.FieldEmailHost: idEmHost, traysettings.FieldEmailPort: idEmPort,
	traysettings.FieldEmailFrom: idEmFrom, traysettings.FieldEmailPassword: idEmPassword,
}

// TestCHK_TRAY_50_Cues — every UIField with Cue is set on its edit box,
// RU+EN (PLAN §12 p.145): settings + Viber via newViberCfg, Discovery via
// newDiscService; red highlight only for Missing required fields.
func TestCHK_TRAY_50_Cues(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	done, _ := useComCtl6()
	defer done()
	for _, lang := range []string{i18n.RU, i18n.EN} {
		svc := newViberCfg()
		app := startCfgApp(t, svc.cfgService, true, nil)
		w, err := newSettingsWindow(settingsConfig{lang: lang, mode: traysettings.Admin, session: app.session, hidden: true,
			uiMode: traysettings.UIExpert})
		if err != nil {
			t.Fatal(err)
		}
		w.Reload()
		pumpUntil(t, "loaded", func() bool { w.update(); return !w.busy && w.ed.HasProfile() && w.ctl[idVbToken] != 0 })
		for _, f := range traysettings.UIFields {
			want := f.CueText(lang)
			if want == "" || f.ID == traysettings.FieldDiscTime || f.ID == traysettings.FieldDiscTolerance {
				continue
			}
			id, ok := settingsCueID[f.ID]
			if !ok {
				t.Errorf("%s: field %s has cue %q but no settings control", lang, f.ID, want)
				continue
			}
			if got := w.cues[id]; got != want {
				t.Errorf("%s: %s cue %q, want %q", lang, f.ID, got, want)
			}
		}
		w.click(t, idSetAdd)
		PumpPending()
		w.updateRequired()
		stored := !w.ed.IsNew && w.ed.Base.HasPassword && !w.ed.Form.ClearPassword
		miss := map[traysettings.Field]bool{}
		for _, m := range traysettings.Missing(w.uiMode, w.ed.Form, stored) {
			miss[m] = true
		}
		for id, on := range w.Missing() {
			if !on {
				continue
			}
			f := reqFields[id]
			if !miss[f] {
				t.Errorf("%s: %s highlighted but not Missing", lang, f)
			}
			for _, u := range traysettings.UIFields {
				if u.ID == f && u.HasDefault {
					t.Errorf("%s: HasDefault %s highlighted", lang, f)
				}
			}
		}
		w.close()
		app.Exit()

		ds := newDiscService()
		app2 := startCfgApp(t, ds.cfgService, true, nil)
		w2, err := newSettingsWindow(settingsConfig{lang: lang, mode: traysettings.Admin, session: app2.session, hidden: true,
			uiMode: traysettings.UIExpert})
		if err != nil {
			t.Fatal(err)
		}
		w2.Reload()
		pumpUntil(t, "disc-ready", func() bool {
			w2.update()
			return !w2.busy && w2.ed.HasProfile() && winapi.IsWindowEnabled(w2.ctl[idSetDiscover])
		})
		w2.click(t, idSetDiscover)
		if w2.disc == nil {
			t.Fatalf("%s: discovery not opened", lang)
		}
		for _, f := range []traysettings.UIField{
			{ID: traysettings.FieldDiscTime, Cue: i18n.KeyCueDiscTime},
			{ID: traysettings.FieldDiscTolerance, Cue: i18n.KeySetTimeoutCue, CueDefault: 1440},
		} {
			want := f.CueText(lang)
			id := map[traysettings.Field]int32{traysettings.FieldDiscTime: idDiscTime, traysettings.FieldDiscTolerance: idDiscTol}[f.ID]
			if got := winapi.CueBanner(w2.disc.ctl[id]); got != want {
				t.Errorf("%s: discovery %s cue %q, want %q", lang, f.ID, got, want)
			}
		}
		w2.disc.close()
		w2.close()
		app2.Exit()
	}
}
