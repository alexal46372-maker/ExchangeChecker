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
	b, err := ExportFile(exp, &TrayBlock{Language: "en", Balloons: true, Sound: &snd, UIMode: UIExpert, Theme: "dark"}, false, false)
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
	// ParseImport keeps non-secret viber for optional includeViberRecipients;
	// ImportRequest without the flag strips it (service keeps stored).
	var raw map[string]any
	json.Unmarshal(b, &raw)
	raw["settings"] = map[string]any{"viber": map[string]any{"enabled": true, "recipients": []any{map[string]any{"id": "1", "name": "A"}}}}
	b2, _ := json.Marshal(raw)
	f2, err := ParseImport(b2, i18n.RU)
	if err != nil || f2.Config.Settings == nil || f2.Config.Settings.Viber == nil || !f2.Config.Settings.Viber.Enabled {
		t.Fatalf("parse keeps viber: %v %+v", err, f2)
	}
	req := ImportRequest(f2, model.ImportMerge, true, "", false, false)
	if req.IncludeViberRecipients || req.Config.Settings == nil || req.Config.Settings.Viber != nil {
		t.Fatalf("ImportRequest without flag must strip viber: %+v", req)
	}
	req2 := ImportRequest(f2, model.ImportMerge, true, "", true, false)
	if !req2.IncludeViberRecipients || req2.Config.Settings.Viber == nil || req2.Config.Settings.Viber.HasToken {
		t.Fatalf("ImportRequest with flag: %+v", req2)
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


// TestCHK_TRAY_57_RequestJSON — includeViberRecipients on export/import
// request JSON; Theme in the tray block; default export still omits Viber.
func TestCHK_TRAY_57_RequestJSON(t *testing.T) {
	snd := trayalert.DefaultSoundPrefs(nil)
	exp := model.ConfigExport{Format: model.ConfigFormatV1, ContainsViberRecipients: true,
		Settings: &model.Settings{Viber: &model.ViberSettings{Enabled: true, HasToken: true,
			Recipients: []model.ViberRecipient{{ID: "u1", Name: "Ann"}}, Status: &model.ViberStatus{}}}}
	b, err := ExportFile(exp, &TrayBlock{Language: "ru", Balloons: true, Sound: &snd, UIMode: UISimple, Theme: "dark"}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"recipients"`) || !strings.Contains(s, `"theme": "dark"`) || strings.Contains(s, `"hasToken": true`) {
		t.Fatalf("export with viber: %s", b)
	}
	b0, err := ExportFile(exp, &TrayBlock{Language: "en"}, false, false)
	if err != nil || strings.Contains(string(b0), `"viber"`) || strings.Contains(string(b0), `"containsViberRecipients": true`) {
		t.Fatalf("export without flag: %s / %v", b0, err)
	}
	er := ExportRequest(true, false)
	raw, _ := json.Marshal(er)
	if !strings.Contains(string(raw), `"includeViberRecipients":true`) || er.Cmd != model.CmdExportConfig {
		t.Fatalf("ExportRequest: %s", raw)
	}
	er0 := ExportRequest(false, false)
	raw0, _ := json.Marshal(er0)
	if strings.Contains(string(raw0), "includeViberRecipients") {
		t.Fatalf("omit empty: %s", raw0)
	}
	f, err := ParseImport(b, i18n.EN)
	if err != nil || f.Tray == nil || f.Tray.Theme != "dark" {
		t.Fatalf("parse theme: %v %+v", err, f)
	}
	ir := ImportRequest(f, model.ImportMerge, false, "r1", true, false)
	raw, _ = json.Marshal(ir)
	if !strings.Contains(string(raw), `"includeViberRecipients":true`) || ir.Config.Settings.Viber == nil {
		t.Fatalf("ImportRequest: %s", raw)
	}
	// no required restart string for the tray-block path (CHK-TRAY-57)
	for _, lang := range []string{i18n.RU, i18n.EN} {
		s := i18n.T(i18n.KeyXferTrayApplied, lang)
		if strings.Contains(s, "перезапуск") || strings.Contains(strings.ToLower(s), "restart") {
			t.Errorf("%s: restart text still required: %q", lang, s)
		}
	}
}
