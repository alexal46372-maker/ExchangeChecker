package traysettings

import (
	"encoding/json"
	"reflect"
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
	e := NewEditor(Admin)
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
	if e.Settings.Email.HasPassword || e.PasswordStateText() != i18n.T(i18n.KeyEmPasswordNone) {
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
	if e.PasswordStateText() != i18n.T(i18n.KeyEmPasswordNoneTLS) {
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
		ru := i18n.Message(k)
		if ru == "" || strings.Contains(ru, k) {
			t.Errorf("%s: %q", k, ru)
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
	if !strings.Contains(txt, "Дежурный (ops)") || !strings.Contains(txt, i18n.Message(model.MsgEmailInvalidAddr)) ||
		!strings.HasPrefix(txt, i18n.T(i18n.KeyEmTestFailed)) {
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
	if EmailStatusText(&model.EmailSettings{Status: &model.ViberStatus{Sent: 3, Queued: 1}}) != "Отправлено: 3, в очереди: 1, отброшено: 0" {
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
	f, err := ParseImport(raw)
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

// TestCHK_TRAY_87_ParseRecipients — the recipients box (phase 13): a bare
// address gets a generated id; commas, semicolons, spaces and new lines
// separate addresses; ids never contain a separator, are unique and pass
// model.ValidEmailID; "id addr name" keeps working; a wrong address is a
// Russian text naming it.
func TestCHK_TRAY_87_ParseRecipients(t *testing.T) {
	type r = model.EmailRecipient
	cases := []struct {
		in   string
		want []r
	}{
		{"alex@mail.ru", []r{{ID: "alex", Addr: "alex@mail.ru"}}},
		{"a@b.ru;c@d.ru", []r{{ID: "a", Addr: "a@b.ru"}, {ID: "c", Addr: "c@d.ru"}}},
		{"a@b.ru, c@d.ru", []r{{ID: "a", Addr: "a@b.ru"}, {ID: "c", Addr: "c@d.ru"}}},
		{"a@b.ru c@d.ru", []r{{ID: "a", Addr: "a@b.ru"}, {ID: "c", Addr: "c@d.ru"}}},
		{"a@b.ru\r\nc@d.ru\n\n", []r{{ID: "a", Addr: "a@b.ru"}, {ID: "c", Addr: "c@d.ru"}}},
		{" a@b.ru ;; , c@d.ru; ", []r{{ID: "a", Addr: "a@b.ru"}, {ID: "c", Addr: "c@d.ru"}}},
		{"ops ops@example.com Дежурный инженер", []r{{ID: "ops", Addr: "ops@example.com", Name: "Дежурный инженер"}}},
		{"ops ops@example.com\r\nalex@mail.ru", []r{{ID: "ops", Addr: "ops@example.com"}, {ID: "alex", Addr: "alex@mail.ru"}}},
		// same local part: deduplicated, also against an explicit id
		{"info@a.ru, info@b.ru", []r{{ID: "info", Addr: "info@a.ru"}, {ID: "info-2", Addr: "info@b.ru"}}},
		{"info@a.ru\ninfo x@y.ru", []r{{ID: "info-2", Addr: "info@a.ru"}, {ID: "info", Addr: "x@y.ru"}}},
		{"Ivan.Petrov+mon@Mail.ru", []r{{ID: "ivan.petrov-mon", Addr: "Ivan.Petrov+mon@Mail.ru"}}},
		// the same address twice is one recipient (not alex + alex-2)
		{"alex@mail.ru, alex@mail.ru", []r{{ID: "alex", Addr: "alex@mail.ru"}}},
		{"alex@mail.ru\nAlex@Mail.RU; alex@mail.ru", []r{{ID: "alex", Addr: "alex@mail.ru"}}},
		{"ops ops@x.ru Дежурный\nops@x.ru, alex@mail.ru", []r{{ID: "ops", Addr: "ops@x.ru", Name: "Дежурный"}, {ID: "alex", Addr: "alex@mail.ru"}}},
		{"alex@mail.ru\nboss alex@mail.ru", []r{{ID: "alex", Addr: "alex@mail.ru"}}},
		{"", []r{}},
	}
	for _, c := range cases {
		got, fe := ParseEmailRecipients(c.in)
		if fe != nil {
			t.Errorf("%q: %+v", c.in, fe)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %+v want %+v", c.in, got, c.want)
		}
		ids := map[string]bool{}
		for _, x := range got {
			if strings.ContainsAny(x.ID, ",; ") || !model.ValidEmailID(x.ID) || ids[x.ID] {
				t.Errorf("%q: bad id %q", c.in, x.ID)
			}
			ids[x.ID] = true
		}
		// the saved list re-renders and re-parses to the same recipients
		e := model.DefaultEmailSettings()
		e.Recipients = got
		back, fe := ParseEmailRecipients(FromEmail(e).Recipients)
		if fe != nil || !reflect.DeepEqual(back, got) {
			t.Errorf("%q: round trip %+v %v", c.in, back, fe)
		}
	}
	if s := FromEmail(model.EmailSettings{Recipients: []model.EmailRecipient{{ID: "alex", Addr: "alex@mail.ru"}}}).Recipients; s != "alex@mail.ru" {
		t.Errorf("bare render %q", s)
	}
	// ids are at most model.EmailMaxIDLen bytes, also with a suffix
	long := strings.Repeat("a", 100)
	got, fe := ParseEmailRecipients(long + "@x.ru, " + long + "@y.ru")
	if fe != nil || len(got) != 2 || len(got[0].ID) != model.EmailMaxIDLen || len(got[1].ID) != model.EmailMaxIDLen ||
		got[0].ID == got[1].ID || !strings.HasSuffix(got[1].ID, "-2") || !model.ValidEmailID(got[1].ID) {
		t.Errorf("long ids %+v %v", got, fe)
	}
	// ids from non-ASCII / empty local parts
	for addr, want := range map[string]string{"иван@почта.рф": "mail", "a,b@c.ru": "a-b", "x;y@c.ru": "x-y"} {
		if id := EmailIDFromAddr(addr); id != want || !model.ValidEmailID(id) {
			t.Errorf("EmailIDFromAddr(%q) = %q, want %q", addr, id, want)
		}
	}
	// invalid addresses: an error naming the address, in Russian, through
	// the real EmailRequest of the editor
	for _, bad := range []string{"alex", "alex@", "@mail.ru", "a@b.ru, мусор", "a@b.ru Иван"} {
		if _, fe := ParseEmailRecipients(bad); fe == nil || !strings.HasPrefix(fe.Field, "email.recipients[") {
			t.Errorf("%q accepted: %+v", bad, fe)
		}
		ed := NewEditor(Admin)
		s := model.DefaultSettings()
		ed.LoadConfig(model.ConfigView{Settings: s}, "rev1")
		ed.Email.Recipients = bad
		_, pr := ed.EmailRequest()
		if pr == nil || pr.Field != FieldEmailRecipients || !strings.Contains(pr.Text, "Неверный адрес e-mail получателя") {
			t.Fatalf("%q: problem %+v", bad, pr)
		}
		word := strings.Fields(strings.NewReplacer(",", " ", ";", " ").Replace(bad))
		if last := word[len(word)-1]; !strings.Contains(pr.Text, "«"+last+"»") {
			t.Errorf("%q: text %q does not name %q", bad, pr.Text, last)
		}
	}
}
