package traysettings

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func viberEditor(t *testing.T, hasToken bool) *Editor {
	t.Helper()
	s := model.DefaultSettings()
	s.Viber.HasToken = hasToken
	s.Viber.Recipients = []model.ViberRecipient{{ID: "abc=", Name: "Дежурный"}}
	e := NewEditor(i18n.RU, Admin)
	e.LoadConfig(model.ConfigView{Settings: s}, "r1")
	if !e.CanEditViber() || e.ViberUnsaved() {
		t.Fatal("viber page not loaded")
	}
	return e
}

// TestCHK_TRAY_43_ViberForm — the «Viber» page: settings.viber round
// trip, recipients "ID name" per line, schedule, events, reminder 0 or
// 15..1440, the token write-only (viberToken only when typed,
// clearViberToken), service errors focus their field.
func TestCHK_TRAY_43_ViberForm(t *testing.T) {
	e := viberEditor(t, true)
	f := e.Viber
	if !f.Days[0] || f.Days[5] || f.From != "08:30" || f.To != "17:00" || f.Reminder != "60" || f.Events != [4]bool{true, true, true, true} ||
		f.Recipients != "abc= Дежурный" {
		t.Fatalf("form %+v", f)
	}
	// no change: the token is not sent
	req, pr := e.ViberRequest()
	if pr != nil || req.Cmd != model.CmdSaveSettings || req.ViberToken != "" || req.ClearViberToken || req.ExpectedRevision != "r1" ||
		req.Settings.Viber == nil || req.Settings.Viber.Status != nil {
		t.Fatalf("plain save %+v %v", req, pr)
	}
	b, _ := json.Marshal(req)
	if strings.Contains(string(b), "hasToken\":true") {
		t.Error("hasToken is read-only: sent as false/ignored only")
	}
	e.Viber.Enabled = true
	e.Viber.Recipients = "u1 Иван\r\n\r\nu2"
	e.Viber.Days = [7]bool{false, false, false, false, false, true, true}
	e.Viber.Events = [4]bool{true, false, true, false}
	e.Viber.Reminder = "0"
	e.Viber.Token = "tok-123"
	req, pr = e.ViberRequest()
	v := req.Settings.Viber
	if pr != nil || req.ViberToken != "tok-123" || !v.Enabled || len(v.Recipients) != 2 || v.Recipients[0] != (model.ViberRecipient{ID: "u1", Name: "Иван"}) ||
		v.Recipients[1].ID != "u2" || len(v.Schedule.Days) != 2 || v.Schedule.Days[0] != 6 ||
		strings.Join(v.Events, ",") != "error,recovered" || v.ReminderMinutes != 0 {
		t.Fatalf("edited %+v %+v %v", req, v, pr)
	}
	e.ViberDone(model.IPCMessage{Type: model.MsgOK, Revision: "r2"}, req)
	if e.Revision != "r2" || !e.Settings.Viber.HasToken || e.Viber.Token != "" || e.ViberUnsaved() {
		t.Error("after save")
	}
	e.Viber.ClearToken = true
	req, _ = e.ViberRequest()
	if !req.ClearViberToken || req.ViberToken != "" {
		t.Error("clearViberToken")
	}
	e.ViberDone(model.IPCMessage{Type: model.MsgOK}, req)
	if e.Settings.Viber.HasToken || e.TokenStateText() != i18n.T(i18n.KeyVbTokenNone, i18n.RU) {
		t.Error("token cleared")
	}

	// local errors focus their field
	for _, c := range []struct {
		edit func(*ViberForm)
		want Field
	}{
		{func(f *ViberForm) { f.Recipients = "ok\nbad\x01id" }, FieldViberRecipients},
		{func(f *ViberForm) { f.Reminder = "10" }, FieldViberReminder},
		{func(f *ViberForm) { f.Reminder = "x" }, FieldViberReminder},
		{func(f *ViberForm) { f.Days = [7]bool{} }, FieldViberDays},
		{func(f *ViberForm) { f.From = "9:00" }, FieldViberFrom},
		{func(f *ViberForm) { f.From, f.To = "18:00", "09:00" }, FieldViberTo},
		{func(f *ViberForm) { f.Token = "a b" }, FieldViberToken},
		{func(f *ViberForm) { f.Enabled, f.Recipients = true, "" }, FieldViberRecipients},
	} {
		e := viberEditor(t, false)
		c.edit(&e.Viber)
		if _, pr := e.ViberRequest(); pr == nil || pr.Field != c.want || pr.Text == "" {
			t.Errorf("%+v: problem %+v, want %s", e.Viber, pr, c.want)
		}
	}
	// service errors
	e = viberEditor(t, false)
	e.ViberRequest()
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorSettingsInvalid,
		ErrorArgs: []string{"viber.events[0]", model.ValEnum, "x"}})
	if pr == nil || pr.Field != FieldViberEvents || strings.Contains(pr.Text, "viber.events") {
		t.Errorf("settings_invalid: %+v", pr)
	}
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgViberNoToken})
	if pr == nil || pr.Field != FieldViberToken || pr.Text != i18n.Message(model.MsgViberNoToken, i18n.RU) {
		t.Errorf("no token: %+v", pr)
	}
	pr = e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgViberNoToken, ErrorArgs: []string{"viberToken"}})
	if pr == nil || pr.Field != FieldViberToken {
		t.Errorf("no token with args: %+v", pr)
	}
	for _, k := range []string{model.MsgViberNoToken, model.MsgViberNoRecipients, model.MsgViberAuthFailed, model.MsgViberReceiverInvalid,
		model.MsgViberRateLimited, model.MsgViberNetwork, model.MsgViberTLS, model.MsgViberFailed, model.MsgViberInvalid} {
		ru, en := i18n.Message(k, i18n.RU), i18n.Message(k, i18n.EN)
		if ru == "" || en == "" || ru == en || strings.Contains(ru, k) {
			t.Errorf("%s: %q / %q", k, ru, en)
		}
	}
}

// TestCHK_TRAY_43_ViberTest — viber_test {recipientId?, viberToken?}: the
// typed token is used once, without a token at all the tray asks for it;
// the result names each recipient with its error text.
func TestCHK_TRAY_43_ViberTest(t *testing.T) {
	e := viberEditor(t, false)
	if _, pr := e.ViberTestRequest(""); pr == nil || pr.Field != FieldViberToken {
		t.Errorf("no token: %+v", pr)
	}
	e.Viber.Token = "tok"
	req, pr := e.ViberTestRequest("abc=")
	if pr != nil || req.Cmd != model.CmdViberTest || req.RecipientID != "abc=" || req.ViberToken != "tok" || !model.AdminCommand(req.Cmd) {
		t.Fatalf("test request %+v %v", req, pr)
	}
	e = viberEditor(t, true)
	req, pr = e.ViberTestRequest("")
	if pr != nil || req.ViberToken != "" || req.RecipientID != "" {
		t.Fatalf("saved token: %+v %v", req, pr)
	}
	txt := e.ViberTestText(&model.ViberTestResult{OK: false, Results: []model.ViberTestItem{{ID: "abc=", OK: true},
		{ID: "zz", ErrorKey: model.MsgViberReceiverInvalid}}})
	if !strings.Contains(txt, "Дежурный (abc=): доставлено") || !strings.Contains(txt, "zz: "+i18n.Message(model.MsgViberReceiverInvalid, i18n.RU)) ||
		!strings.HasPrefix(txt, i18n.T(i18n.KeyVbTestFailed, i18n.RU)) {
		t.Errorf("result text %q", txt)
	}
	if e.ViberTestText(nil) == "" {
		t.Error("nil result")
	}
	// the reply JSON of the service
	var m model.IPCMessage
	if err := json.Unmarshal([]byte(`{"type":"viber_test","viberTest":{"ok":true,"results":[{"id":"abc=","ok":true}]}}`), &m); err != nil ||
		m.ViberTest == nil || !m.ViberTest.OK || len(m.ViberTest.Results) != 1 {
		t.Errorf("viberTest JSON %+v %v", m, err)
	}
	if ViberStatusText(&model.ViberSettings{Status: &model.ViberStatus{Sent: 3, Queued: 1}}, i18n.EN) != "Sent: 3, queued: 1, dropped: 0" {
		t.Error("status text")
	}
}
