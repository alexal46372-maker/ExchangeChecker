//go:build windows

package wintray

import (
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// settingsCueID: UIField → settings/Viber edit control (Discovery below).
var settingsCueID = map[traysettings.Field]int32{
	traysettings.FieldName: idSetName, traysettings.FieldHost: idSetHost, traysettings.FieldInstance: idSetInstance,
	traysettings.FieldPort: idSetPort, traysettings.FieldDatabase: idSetDatabase, traysettings.FieldLogin: idSetLogin,
	traysettings.FieldPassword: idSetPassword, traysettings.FieldProfWarn: idSetWarn, traysettings.FieldProfError: idSetError,
	traysettings.FieldConnTimeout: idSetConnTO, traysettings.FieldQueryTimeout: idSetQueryTO,
	traysettings.FieldViberToken: idVbToken, traysettings.FieldViberReminder: idVbReminder,
}

// TestCHK_TRAY_50_Cues — every UIField with Cue that belongs to the
// settings window is set on its edit box, RU+EN (PLAN §12 p.145);
// Discovery cues of FieldDiscTime / FieldDiscTolerance are on the dialog;
// red highlight only for Missing required fields.
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
			if want == "" {
				continue
			}
			switch f.ID {
			case traysettings.FieldDiscTime, traysettings.FieldDiscTolerance:
				continue // checked after Discovery opens
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
		// Discovery
		if !w.discoveryAvailable() {
			t.Fatalf("%s: discovery unavailable", lang)
		}
		w.click(t, idSetDiscover)
		pumpUntil(t, "discovery", func() bool { return w.disc != nil })
		for _, f := range []traysettings.UIField{
			{ID: traysettings.FieldDiscTime, Cue: i18n.KeyCueDiscTime},
			{ID: traysettings.FieldDiscTolerance, Cue: i18n.KeySetTimeoutCue, CueDefault: 1440},
		} {
			want := f.CueText(lang)
			id := map[traysettings.Field]int32{traysettings.FieldDiscTime: idDiscTime, traysettings.FieldDiscTolerance: idDiscTol}[f.ID]
			if got := winapi.CueBanner(w.disc.ctl[id]); got != want {
				t.Errorf("%s: discovery %s cue %q, want %q", lang, f.ID, got, want)
			}
		}
		w.disc.close()
		// required highlight
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
	}
}
