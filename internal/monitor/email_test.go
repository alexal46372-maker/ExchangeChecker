package monitor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func TestCHK_CFG_09_SaveGetExportImportEmail(t *testing.T) {
	e := newCfgEnv(t)
	m := e.m
	pwCalls := 0
	m.opt.OnEmailPassword = func() { pwCalls++ }
	m.opt.EmailStatus = func() *model.ViberStatus {
		return &model.ViberStatus{Queued: 2, Sent: 3}
	}

	st := model.DefaultSettings()
	st.Email.Enabled = true
	st.Email.Host, st.Email.Port = "smtp.example.com", 587
	st.Email.TLSMode, st.Email.From = model.EmailTLSStartTLS, "bot@example.com"
	st.Email.Recipients = []model.EmailRecipient{{ID: "e1", Addr: "a@b.co", Name: "Ann"}}

	wantCode(t, "enabled without pw", errOf(m.SaveSettingsEmail(st, "", false, "", false, e.rev(), "a")),
		model.ErrCodeInvalidRequest, model.MsgEmailNoPassword)

	if _, err := m.SaveSettingsEmail(st, "", false, "SMTP-SECRET", false, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	if pw, _ := e.st.LoadEmailPassword(); pw != "SMTP-SECRET" || pwCalls != 1 {
		t.Fatalf("pw %q calls %d", pw, pwCalls)
	}
	raw, _ := e.st.ReadFileRaw(config.SettingsFile)
	if strings.Contains(string(raw), "SMTP-SECRET") {
		t.Fatalf("settings.json leaked password: %s", raw)
	}

	view, _, err := m.GetConfig()
	if err != nil || view.Settings.Email == nil || !view.Settings.Email.HasPassword ||
		view.Settings.Email.Status == nil || view.Settings.Email.Status.Sent != 3 {
		t.Fatalf("view %#v err=%v", view.Settings.Email, err)
	}
	jb, _ := json.Marshal(view.Settings.Email)
	if !strings.Contains(string(jb), `"hasPassword":true`) {
		t.Fatalf("hasPassword always in views: %s", jb)
	}

	// default export omits email
	x, err := m.ExportConfig()
	if err != nil || x.Settings.Email != nil || x.ContainsEmailRecipients {
		t.Fatalf("default export: %#v", x)
	}

	x, err = m.ExportConfigBoth(false, true)
	if err != nil || x.Settings.Email == nil || !x.ContainsEmailRecipients ||
		x.Settings.Email.HasPassword || x.Settings.Email.Status != nil ||
		len(x.Settings.Email.Recipients) != 1 {
		t.Fatalf("include export: %#v", x.Settings.Email)
	}
	eb, _ := json.Marshal(x.Settings.Email)
	if strings.Contains(string(eb), "hasPassword") || strings.Contains(string(eb), "smtpPassword") {
		t.Fatalf("export secrets: %s", eb)
	}

	// import with flag applies recipients; password untouched
	imp := model.DefaultSettings()
	imp.Email = &model.EmailSettings{
		Enabled: false, Host: "other.example", Port: 465, TLSMode: model.EmailTLSImplicit,
		From: "x@y.z", Recipients: []model.EmailRecipient{{ID: "e2", Addr: "c@d.co", Name: "Bob"}},
		Schedule:    model.ViberSchedule{Days: []int{1, 2, 3, 4, 5}, From: "09:00", To: "18:00"},
		Events:      model.ViberEventKinds(),
		HasPassword: true, // ignored
	}
	cfg := &model.ConfigExport{Format: model.ConfigFormatV1, Settings: &imp, Profiles: []model.Profile{}}
	if _, err := m.ImportConfigBoth(cfg, model.ImportMerge, e.rev(), false, false, true, "a"); err != nil {
		t.Fatal(err)
	}
	cur, _ := e.st.LoadSettings()
	if cur.Email.Host != "other.example" || len(cur.Email.Recipients) != 1 || cur.Email.Recipients[0].ID != "e2" {
		t.Fatalf("imported %#v", cur.Email)
	}
	if pw, _ := e.st.LoadEmailPassword(); pw != "SMTP-SECRET" {
		t.Fatalf("password changed: %q", pw)
	}

	// import without flag keeps stored email
	imp2 := model.DefaultSettings()
	imp2.Email = &model.EmailSettings{Host: "ignored.example", Port: 25, TLSMode: model.EmailTLSNone,
		From: "i@g.n", Recipients: []model.EmailRecipient{}, Schedule: model.DefaultViberSettings().Schedule, Events: model.ViberEventKinds()}
	cfg2 := &model.ConfigExport{Format: model.ConfigFormatV1, Settings: &imp2, Profiles: []model.Profile{}}
	if _, err := m.ImportConfigBoth(cfg2, model.ImportMerge, e.rev(), false, false, false, "a"); err != nil {
		t.Fatal(err)
	}
	cur, _ = e.st.LoadSettings()
	if cur.Email.Host != "other.example" {
		t.Fatalf("kept email: %#v", cur.Email)
	}
}

func TestCHK_CFG_09_ClearSmtpPassword(t *testing.T) {
	e := newCfgEnv(t)
	st := model.DefaultSettings()
	st.Email.Host, st.Email.Port, st.Email.From = "h", 25, "a@b.co"
	st.Email.TLSMode = model.EmailTLSNone
	if _, err := e.m.SaveSettingsEmail(st, "", false, "P", false, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.SaveSettingsEmail(st, "", false, "", true, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	if e.st.HasEmailPassword() {
		t.Fatal("cleared")
	}
}
