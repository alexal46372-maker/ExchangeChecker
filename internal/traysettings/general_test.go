package traysettings

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func generalEditor(mode Mode) *Editor {
	e := NewEditor(mode)
	s := model.DefaultSettings()
	s.Beep.Enabled = model.OptUnset // old file: reads as true
	if mode == Admin {
		e.LoadConfig(model.ConfigView{Settings: s, Profiles: []model.Profile{savedSQL()}}, "rev1")
	} else {
		e.LoadSnapshot(model.StatusSnapshot{ConfigRevision: "rev1", Settings: &s})
	}
	return e
}

// TestCHK_TRAY_09_GeneralSettings — the general page: loaded with the
// profiles (one revision), save_settings with expectedRevision, beep.enabled
// always explicit, trayAutostart, local validation focus, unsaved edits,
// the reply revision used next.
func TestCHK_TRAY_09_GeneralSettings(t *testing.T) {
	e := generalEditor(Admin)
	g := e.General
	if !g.BeepEnabled || !g.Autostart || g.PollSec != "60" || g.Gap != "700" || e.GeneralUnsaved() || e.Unsaved() {
		t.Fatalf("loaded %+v unsaved %v", g, e.GeneralUnsaved())
	}
	req, pr := e.SettingsRequest()
	if pr != nil || req.Cmd != model.CmdSaveSettings || req.ExpectedRevision != "rev1" || req.Settings == nil {
		t.Fatalf("request %+v %+v", req, pr)
	}
	b, _ := json.Marshal(req)
	if !strings.Contains(string(b), `"enabled":true`) {
		t.Errorf("beep.enabled must be explicit: %s", b)
	}
	e.General.BeepEnabled, e.General.Autostart, e.General.PollSec = false, false, "120"
	if !e.GeneralUnsaved() || !e.Unsaved() || e.ProfileUnsaved() {
		t.Error("general edits must be unsaved, profile not")
	}
	req, pr = e.SettingsRequest()
	b, _ = json.Marshal(req)
	if pr != nil || req.Settings.Beep.Enabled != model.OptFalse || req.Settings.TrayAutostart || req.Settings.PollIntervalSec != 120 ||
		!strings.Contains(string(b), `"enabled":false`) || !strings.Contains(string(b), `"trayAutostart":false`) {
		t.Errorf("edited request %s %+v", b, pr)
	}
	if st := e.SettingsDone(model.IPCMessage{Type: model.MsgOK, Revision: "rev2"}, *req.Settings); st != "Общие настройки сохранены." ||
		e.Revision != "rev2" || e.GeneralUnsaved() || e.General.BeepEnabled {
		t.Errorf("done: %q rev %q unsaved %v", st, e.Revision, e.GeneralUnsaved())
	}

	for _, c := range []struct {
		set   func(*GeneralForm)
		field Field
	}{
		{func(g *GeneralForm) { g.PollSec = "abc" }, FieldPollInterval},
		{func(g *GeneralForm) { g.PollSec = "3" }, FieldPollInterval},
		{func(g *GeneralForm) { g.ErrorMin = g.WarnMin }, FieldErrorMinutes},
		{func(g *GeneralForm) { g.WarnFreq = "20" }, FieldWarnFreq},
		{func(g *GeneralForm) { g.Gap = "" }, FieldGap},
	} {
		e.General = FromSettings(e.Settings)
		c.set(&e.General)
		if _, pr := e.SettingsRequest(); pr == nil || pr.Field != c.field || pr.Text == "" {
			t.Errorf("%s: %+v", c.field, pr)
		}
	}
	e.General = FromSettings(e.Settings)
	e.Revision = ""
	if _, pr := e.SettingsRequest(); pr == nil || !pr.Reload {
		t.Errorf("no revision: %+v", pr)
	}
	// service validation error: focus on the general field, localized name
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorSettingsInvalid,
		ErrorArgs: []string{"beep.gapMs", model.ValRange, "0", "60000", "70000"}})
	if pr.Field != FieldGap || !IsGeneral(pr.Field) || !strings.Contains(pr.Text, "Пауза между сигналами") {
		t.Errorf("settings_invalid %+v", pr)
	}

	r := generalEditor(ReadOnly)
	if !r.HasSettings || r.CanEditSettings() || !r.General.BeepEnabled || r.GeneralUnsaved() {
		t.Errorf("read-only general %+v", r.General)
	}
	if _, pr := r.SettingsRequest(); pr == nil {
		t.Error("read-only must not save settings")
	}
}

// TestCHK_TRAY_09_NoSettingsInvalidConfig — get_status without settings
// (configError / monitor.config_invalid, empty revision): no general
// settings to show or save, also after an earlier valid snapshot.
func TestCHK_TRAY_09_NoSettingsInvalidConfig(t *testing.T) {
	bad := model.StatusSnapshot{ConfigError: "settings.json: bad", ConfigErrorKey: model.MsgMonitorConfigInvalid}
	e := NewEditor(ReadOnly)
	e.LoadSnapshot(bad)
	if e.HasSettings || e.CanEditSettings() || e.Revision != "" || e.General != (GeneralForm{}) {
		t.Errorf("invalid config: has %v rev %q general %+v", e.HasSettings, e.Revision, e.General)
	}
	r := generalEditor(ReadOnly)
	r.LoadSnapshot(bad)
	if r.HasSettings || r.General.PollSec != "" {
		t.Errorf("stale settings kept: %+v", r.General)
	}
	a := NewEditor(Admin)
	a.LoadSnapshot(bad)
	if _, pr := a.SettingsRequest(); pr == nil || !pr.Reload || a.GeneralUnsaved() {
		t.Errorf("admin without settings: %+v", pr)
	}
}

// TestCHK_TRAY_09_AutostartErrorTexts — every trayAutostartError code
// has RU and EN text; no error = "".
func TestCHK_TRAY_09_AutostartErrorTexts(t *testing.T) {
	for _, c := range []string{model.AutostartErrAccessDenied, model.AutostartErrRegistry, model.AutostartErrTrayPath, model.AutostartErrSettings} {
		ru := AutostartErrorText(c)
		if ru == "" || strings.Contains(ru, c) {
			t.Errorf("%s: %q", c, ru)
		}
	}
	if AutostartErrorText("") != "" || !strings.Contains(AutostartErrorText("x"), "(x)") {
		t.Error("empty / unknown code")
	}
}
