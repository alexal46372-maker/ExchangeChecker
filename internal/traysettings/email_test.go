package traysettings

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func emailEditor(t *testing.T, hasPassword bool, tlsMode string) *Editor {
	t.Helper()
	s := model.DefaultSettings()
	s.Email.HasPassword = hasPassword
	s.Email.TLSMode = tlsMode
	s.Email.Host = "smtp.example.com"
	s.Email.Port = 587
	s.Email.From = "mon@example.com"
	s.Email.Recipients = []model.EmailRecipient{{ID: "ops", Addr: "ops@example.com", Name: "Дежурный"}}
	e := NewEditor(i18n.RU, Admin)
	e.LoadConfig(model.ConfigView{Settings: s}, "r1")
	if !e.CanEditEmail() || e.EmailUnsaved() {
		t.Fatal("email page not loaded")
	}
	return e
}

// TestCHK_TRAY_64_EmailForm — settings.email round trip, recipients
// "id addr name", tlsMode, schedule, events; smtpPassword write-only;
// tlsMode none ignores password; starttls/tls require it when enabled.
func TestCHK_TRAY_64_EmailForm(t *testing.T) {
	e := emailEditor(t, true, model.EmailTLSStartTLS)
	f := e.Email
	if !f.Days[0] || f.Days[5] || f.FromTime != "08:30" || f.ToTime != "17:00" ||
		f.TLSMode != model.EmailTLSStartTLS || f.Host != "smtp.example.com" || f.Port != "587" ||
		f.From != "mon@example.com" || f.Recipients != "ops ops@example.com Дежурный" {
		t.Fatalf("form %+v", f)
	}
	req, pr := e.EmailRequest()
	if pr != nil || req.Cmd != model.CmdSaveSettings || req.SmtpPassword != "" || req.ClearSmtpPassword ||
		req.ExpectedRevision != "r1" || req.Settings.Email == nil || req.Settings.Email.Status != nil {
		t.Fatalf("plain save %+v %v", req, pr)
	}
	b, _ := json.Marshal(req)
	if strings.Contains(string(b), `"hasPassword":true`) {
		t.Error("hasPassword is read-only")
	}
	e.Email.Enabled = true
	e.Email.Recipients = "ops ops@example.com Дежурный\r\n\r\nalert alerts@example.com"
	e.Email.Days = [7]bool{false, false, false, false, false, true, true}
	e.Email.Events = [4]bool{true, false, true, false}
	e.Email.Password = "secret"
	req, pr = e.EmailRequest()
	em := req.Settings.Email
	if pr != nil || req.SmtpPassword != "secret" || !em.Enabled || len(em.Recipients) != 2 ||
		em.Recipients[1].Addr != "alerts@example.com" || len(em.Schedule.Days) != 2 ||
		strings.Join(em.Events, ",") != "error,recovered" {
		t.Fatalf("edited %+v %+v %v", req, em, pr)
	}
	e.EmailDone(model.IPCMessage{Type: model.MsgOK, Revision: "r2"}, req)
	if e.Revision != "r2" || !e.Settings.Email.HasPassword || e.Email.Password != "" || e.EmailUnsaved() {
		t.Error("after save")
	}
	e.Email.ClearPassword = true
	req, _ = e.EmailRequest()
	if !req.ClearSmtpPassword || req.SmtpPassword != "" {
		t.Error("clearSmtpPassword")
	}
	e.EmailDone(model.IPCMessage{Type: model.MsgOK}, req)
	if e.Settings.Email.HasPassword || e.PasswordStateText() != i18n.T(i18n.KeyEmPasswordNone, i18n.RU) {
		t.Error("password cleared")
	}

	// tlsMode none: password not required / not sent when enabled
	e = emailEditor(t, false, model.EmailTLSNone)
	e.Email.Enabled = true
	e.Email.Password = "ignored"
	req, pr = e.EmailRequest()
	if pr != nil || req.SmtpPassword != "" || req.ClearSmtpPassword {
		t.Fatalf("none mode must ignore password: %+v %v", req, pr)
	}
	if e.PasswordStateText() != i18n.T(i18n.KeyEmPasswordNoneTLS, i18n.RU) {
		t.Errorf("none state %q", e.PasswordStateText())
	}

	// starttls enabled without password → focus password
	e = emailEditor(t, false, model.EmailTLSStartTLS)
	e.Email.Enabled = true
	if _, pr = e.EmailRequest(); pr == nil || pr.Field != FieldEmailPassword {
		t.Errorf("enabled without password: %+v", pr)
	}

	for _, c := range []struct {
		edit func(*EmailForm)
		want Field
	}{
		{func(f *EmailForm) { f.Recipients = "bad" }, FieldEmailRecipients},
		{func(f *EmailForm) { f.Recipients = "ok not-an-email" }, FieldEmailRecipients},
		{func(f *EmailForm) { f.Days = [7]bool{} }, FieldEmailDays},
		{func(f *EmailForm) { f.FromTime = "9:00" }, FieldEmailSchedFrom},
		{func(f *EmailForm) { f.FromTime, f.ToTime = "18:00", "09:00" }, FieldEmailSchedTo},
		{func(f *EmailForm) { f.TLSMode = "ssl" }, FieldEmailTLSMode},
		{func(f *EmailForm) { f.Enabled, f.Host = true, "" }, FieldEmailHost},
	} {
		e := emailEditor(t, true, model.EmailTLSStartTLS)
		c.edit(&e.Email)
		if _, pr := e.EmailRequest(); pr == nil || pr.Field != c.want || pr.Text == "" {
			t.Errorf("%+v: problem %+v, want %s", e.Email, pr, c.want)
		}
	}
	e = emailEditor(t, false, model.EmailTLSStartTLS)
	e.EmailRequest()
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorSettingsInvalid,
		ErrorArgs: []string{"email.events[0]", model.ValEnum, "x"}})
	if pr == nil || pr.Field != FieldEmailEvents || strings.Contains(pr.Text, "email.events") {
		t.Errorf("settings_invalid: %+v", pr)
	}
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgEmailNoPassword})
	if pr == nil || pr.Field != FieldEmailPassword {
		t.Errorf("no password: %+v", pr)
	}
	for _, k := range []string{model.MsgEmailNoPassword, model.MsgEmailNoRecipients, model.MsgEmailNoHost, model.MsgEmailNoFrom,
		model.MsgEmailAuthFailed, model.MsgEmailInvalidAddr, model.MsgEmailNetwork, model.MsgEmailTLS, model.MsgEmailFailed,
		model.MsgEmailInvalid, model.MsgEmailRateLimited} {
		ru, en := i18n.Message(k, i18n.RU), i18n.Message(k, i18n.EN)
		if ru == "" || en == "" || ru == en || strings.Contains(ru, k) {
			t.Errorf("%s: %q / %q", k, ru, en)
		}
	}
}

// TestCHK_TRAY_64_EmailTest — email_test {recipientId?, addr?, smtpPassword?}.
func TestCHK_TRAY_64_EmailTest(t *testing.T) {
	e := emailEditor(t, false, model.EmailTLSStartTLS)
	if _, pr := e.EmailTestRequest("", ""); pr == nil || pr.Field != FieldEmailPassword {
		t.Errorf("no password: %+v", pr)
	}
	e.Email.Password = "pw"
	req, pr := e.EmailTestRequest("ops", "")
	if pr != nil || req.Cmd != model.CmdEmailTest || req.RecipientID != "ops" || req.SmtpPassword != "pw" || !model.AdminCommand(req.Cmd) {
		t.Fatalf("test request %+v %v", req, pr)
	}
	e = emailEditor(t, true, model.EmailTLSStartTLS)
	req, pr = e.EmailTestRequest("", "oneoff@example.com")
	if pr != nil || req.SmtpPassword != "" || req.Addr != "oneoff@example.com" {
		t.Fatalf("saved password + addr: %+v %v", req, pr)
	}
	// tlsMode none: no password needed
	e = emailEditor(t, false, model.EmailTLSNone)
	req, pr = e.EmailTestRequest("ops", "")
	if pr != nil || req.SmtpPassword != "" {
		t.Fatalf("none test: %+v %v", req, pr)
	}
	txt := e.EmailTestText(&model.EmailTestResult{OK: false, Results: []model.EmailTestItem{
		{ID: "ops", Addr: "ops@example.com", OK: true},
		{ID: "zz", Addr: "zz@x.com", ErrorKey: model.MsgEmailInvalidAddr}}})
	if !strings.Contains(txt, "Дежурный (ops)") || !strings.Contains(txt, i18n.Message(model.MsgEmailInvalidAddr, i18n.RU)) ||
		!strings.HasPrefix(txt, i18n.T(i18n.KeyEmTestFailed, i18n.RU)) {
		t.Errorf("result text %q", txt)
	}
	if e.EmailTestText(nil) == "" {
		t.Error("nil result")
	}
	var m model.IPCMessage
	if err := json.Unmarshal([]byte(`{"type":"email_test","emailTest":{"ok":true,"results":[{"id":"ops","addr":"ops@example.com","ok":true}]}}`), &m); err != nil ||
		m.EmailTest == nil || !m.EmailTest.OK || len(m.EmailTest.Results) != 1 {
		t.Errorf("emailTest JSON %+v %v", m, err)
	}
	if EmailStatusText(&model.EmailSettings{Status: &model.ViberStatus{Sent: 3, Queued: 1}}, i18n.EN) != "Sent: 3, queued: 1, dropped: 0" {
		t.Error("status text")
	}
}

// TestCHK_TRAY_64_EmailExportFlags — includeEmailRecipients / strip password.
func TestCHK_TRAY_64_EmailExportFlags(t *testing.T) {
	exp := model.ConfigExport{Format: model.ConfigFormatV1, ContainsEmailRecipients: true,
		Settings: &model.Settings{Email: &model.EmailSettings{Enabled: true, Host: "h", Port: 25, TLSMode: model.EmailTLSNone,
			From: "a@b.c", HasPassword: true, Recipients: []model.EmailRecipient{{ID: "1", Addr: "a@b.c"}}}}}
	raw, err := ExportFile(exp, nil, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"containsEmailRecipients": true`) || strings.Contains(string(raw), "hasPassword") ||
		strings.Contains(string(raw), "smtpPassword") || !strings.Contains(string(raw), `"host": "h"`) {
		t.Errorf("export with email: %s", raw)
	}
	raw0, _ := ExportFile(exp, nil, false, false)
	if strings.Contains(string(raw0), `"email"`) || strings.Contains(string(raw0), `"containsEmailRecipients": true`) {
		t.Errorf("export without email must strip: %s", raw0)
	}
	er := ExportRequest(false, true)
	b, _ := json.Marshal(er)
	if !strings.Contains(string(b), `"includeEmailRecipients":true`) || er.Cmd != model.CmdExportConfig {
		t.Errorf("export request %s", b)
	}
	f, err := ParseImport(raw, i18n.RU)
	if err != nil {
		t.Fatal(err)
	}
	ir := ImportRequest(f, model.ImportMerge, true, "r1", false, true)
	b, _ = json.Marshal(ir)
	if !strings.Contains(string(b), `"includeEmailRecipients":true`) || ir.Config.Settings.Email == nil || ir.Config.Settings.Email.HasPassword {
		t.Errorf("import request %s", b)
	}
	ir0 := ImportRequest(f, model.ImportMerge, true, "r1", false, false)
	if ir0.IncludeEmailRecipients || (ir0.Config.Settings != nil && ir0.Config.Settings.Email != nil) {
		t.Errorf("import without email must strip: %+v", ir0.Config.Settings)
	}
}
