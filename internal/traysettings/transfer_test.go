package traysettings

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
)

// TestCHK_TRAY_44_ExportImportFile — the file is the export_config reply
// plus the tray block; Viber never goes out or in; limits and formats.
func TestCHK_TRAY_44_ExportImportFile(t *testing.T) {
	snd := trayalert.DefaultSoundPrefs(nil)
	snd.Master = 40
	snd.Error = trayalert.SoundProfile{Kind: trayalert.SoundFile, File: `C:\snd\err.wav`, Volume: 90, Loop: true}
	exp := model.ConfigExport{Format: model.ConfigFormatV1, ContainsLogins: true,
		Settings: &model.Settings{Viber: &model.ViberSettings{}},
		Profiles: []model.Profile{{ID: "p1", Name: "Склад"}}}
	b, err := ExportFile(exp, TrayBlock{Language: "en", Balloons: true, Sound: &snd, UIMode: UIExpert})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"viber"`) || !strings.Contains(string(b), `"format": "ec-tray/1"`) {
		t.Fatalf("export: %s", b)
	}
	f, err := ParseImport(b, i18n.RU)
	if err != nil {
		t.Fatal(err)
	}
	if f.Tray == nil || f.Tray.Language != "en" || !f.Tray.Balloons || f.Tray.Sound.Master != 40 || f.Tray.UIMode != UIExpert ||
		f.Tray.Sound.Error.File != `C:\snd\err.wav` || f.Config.Tray != nil || len(f.Config.Profiles) != 1 {
		t.Fatalf("parsed %+v / %+v", f.Tray, f.Config)
	}
	// a Viber block of a hand-edited file is not imported
	var raw map[string]any
	json.Unmarshal(b, &raw)
	raw["settings"] = map[string]any{"viber": map[string]any{"enabled": true}}
	b2, _ := json.Marshal(raw)
	if f2, err := ParseImport(b2, i18n.RU); err != nil || f2.Config.Settings == nil || f2.Config.Settings.Viber != nil {
		t.Fatalf("viber imported: %v", err)
	}
	for name, c := range map[string]struct {
		data []byte
		want string
	}{
		"too large":  {[]byte(`{"format":"ec-config/1","x":"` + strings.Repeat("a", TransferFileMax) + `"}`), "1024"},
		"not json":   {[]byte("hello"), "EC"},
		"format":     {[]byte(`{"format":"ec-config/2","profiles":[]}`), "ec-config/1"},
		"tray large": {[]byte(`{"format":"ec-config/1","profiles":[],"tray":"` + strings.Repeat("a", model.ImportTrayMax) + `"}`), "256"},
	} {
		for _, lang := range []string{i18n.RU, i18n.EN} {
			if _, err := ParseImport(c.data, lang); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s %s: %v", name, lang, err)
			}
		}
	}
	// a file without a tray block / with an unknown one: Tray nil
	if f, err := ParseImport([]byte(`{"format":"ec-config/1","profiles":[],"tray":{"format":"x"}}`), i18n.EN); err != nil || f.Tray != nil {
		t.Fatalf("unknown tray block: %v %+v", err, f)
	}
}

// TestCHK_TRAY_44_Preview — the dry run text, RU / EN, missing sound file.
func TestCHK_TRAY_44_Preview(t *testing.T) {
	snd := trayalert.DefaultSoundPrefs(nil)
	snd.Warning = trayalert.SoundProfile{Kind: trayalert.SoundFile, File: `D:\no.wav`}
	tray := &TrayBlock{Format: TrayFormat, Sound: &snd}
	res := model.ImportResult{DryRun: true, Added: []string{"a"}, Updated: []string{"b"}, Removed: []string{"c"}, NeedPassword: []string{"a"}}
	names := map[string]string{"a": "Альфа", "b": "Бета"}
	no := func(string) bool { return false }
	for _, lang := range []string{i18n.RU, i18n.EN} {
		p := Preview(res, names, tray, true, no, lang)
		for _, s := range []string{"Альфа", "Бета", "c", `D:\no.wav`, "100 %"} {
			if !strings.Contains(p, s) {
				t.Errorf("%s: %q missing in %q", lang, s, p)
			}
		}
		if strings.Count(p, "\r\n") != 5 {
			t.Errorf("%s: lines %q", lang, p)
		}
		if p := Preview(model.ImportResult{}, nil, tray, false, no, lang); !strings.Contains(p, i18n.T(i18n.KeyXferTrayKept, lang)) ||
			!strings.Contains(p, i18n.T(i18n.KeyXferNoChanges, lang)) || strings.Contains(p, `D:\no.wav`) {
			t.Errorf("%s: tray kept: %q", lang, p)
		}
	}
}

// TestCHK_TRAY_44_ErrorTexts — every import_config error has a RU and an
// EN text (no «unknown message»).
func TestCHK_TRAY_44_ErrorTexts(t *testing.T) {
	for _, k := range []string{model.MsgImportFormat, model.MsgImportMode, model.MsgImportDuplicateID, model.MsgImportNoConfig,
		model.MsgImportTrayTooLarge, model.MsgImportRollbackFailed, model.MsgMonitorProfileInvalid, model.MsgMonitorSettingsInvalid,
		model.MsgMonitorConfigConflict, model.MsgMonitorAccessDenied} {
		ru, en := i18n.Message(k, i18n.RU), i18n.Message(k, i18n.EN)
		if ru == "" || en == "" || ru == en || strings.Contains(en, k) {
			t.Errorf("%s: %q / %q", k, ru, en)
		}
	}
	e := NewEditor(i18n.EN, Admin)
	pr := e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeConflict, ErrorKey: model.MsgMonitorConfigConflict})
	if pr == nil || !pr.Reload {
		t.Errorf("conflict must offer a reload: %+v", pr)
	}
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgImportTrayTooLarge,
		ErrorArgs: []string{"tray", "validation.too_long", "262144"}})
	if pr == nil || !strings.Contains(pr.Text, "262144") {
		t.Errorf("tray too large: %+v", pr)
	}
}
