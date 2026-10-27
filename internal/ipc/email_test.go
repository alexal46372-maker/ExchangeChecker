package ipc

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type fakeEmailTester struct {
	lastID, lastAddr, lastPW string
}

func (f *fakeEmailTester) EmailTest(ctx context.Context, recipientID, addr, smtpPassword string) (model.EmailTestResult, error) {
	f.lastID, f.lastAddr, f.lastPW = recipientID, addr, smtpPassword
	return model.EmailTestResult{OK: true, Results: []model.EmailTestItem{{ID: recipientID, Addr: addr, OK: true}}}, nil
}

type emailCfgHandler struct {
	*cfgHandler
	smtpSaved string
	clearSmtp bool
}

func (h *emailCfgHandler) SaveSettingsEmail(st model.Settings, viberToken string, clearViber bool, smtpPassword string, clearSmtp bool, expectedRevision, account string) (string, error) {
	h.smtpSaved, h.clearSmtp = smtpPassword, clearSmtp
	return "rev-email", nil
}

func (h *emailCfgHandler) ExportConfigBoth(includeViber, includeEmail bool) (model.ConfigExport, error) {
	exp := model.ConfigExport{Format: model.ConfigFormatV1, Profiles: []model.Profile{}, ContainsEmailRecipients: includeEmail}
	if includeEmail {
		e := model.DefaultEmailSettings().ForExport()
		e.Recipients = []model.EmailRecipient{{ID: "e1", Addr: "a@b.co", Name: "Ann"}}
		st := model.DefaultSettings()
		st.Email = &e
		exp.Settings = &st
	} else {
		st := model.DefaultSettings()
		st.Email = nil
		st.Viber = nil
		exp.Settings = &st
	}
	return exp, nil
}

func (h *emailCfgHandler) ImportConfigBoth(in *model.ConfigExport, mode, expectedRevision string, dryRun, includeViber, includeEmail bool, account string) (model.ImportResult, error) {
	return model.ImportResult{Mode: mode, DryRun: dryRun, Revision: "r-imp"}, nil
}

// Also satisfy ConfigViberRecipientsTransfer via both methods? ExportConfigBoth is enough for CapExportEmail.
// Need ExportConfig for ConfigTransfer - cfgHandler may not have it. Embed xfer or implement.
func (h *emailCfgHandler) ExportConfig() (model.ConfigExport, error) {
	return h.ExportConfigBoth(false, false)
}
func (h *emailCfgHandler) ImportConfig(cfg *model.ConfigExport, mode, rev string, dry bool, account string) (model.ImportResult, error) {
	return h.ImportConfigBoth(cfg, mode, rev, dry, false, false, account)
}

func TestCHK_IPC_16_EmailTestAndCap(t *testing.T) {
	var admin int32 = 1
	v2, _, _, _ := newV2(t)
	h := &emailCfgHandler{cfgHandler: &cfgHandler{v2Handler: v2}}
	fake := &fakeEmailTester{}
	lg := &testLog{}
	s := NewServer(h, lg)
	s.Email = fake
	s.Authorize = func(io.ReadWriteCloser) (Peer, error) {
		return Peer{Admin: atomic.LoadInt32(&admin) == 1, Account: `HOST\alex`}, nil
	}
	l := NewMemListener()
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })

	rw, _ := l.Dial()
	t.Cleanup(func() { rw.Close() })
	var hello model.IPCMessage
	if err := ReadMessage(rw, &hello); err != nil {
		t.Fatal(err)
	}
	joined := "," + strings.Join(hello.Capabilities, ",") + ","
	if !strings.Contains(joined, ",email,") || !strings.Contains(joined, ",export_email_recipients,") {
		t.Fatalf("hello caps %v", hello.Capabilities)
	}
	c := &rawConn{t: t, rw: rw, pending: map[uint64]model.IPCMessage{}}

	c.send(model.IPCRequest{ID: 1, Cmd: model.CmdEmailTest, RecipientID: "e1", Addr: "one@off.co", SmtpPassword: "SECRET-PW"})
	r := c.reply(1)
	b, _ := json.Marshal(model.IPCMessage{Type: r.Type, EmailTest: r.EmailTest})
	if r.Type != model.MsgEmailTest || !r.EmailTest.OK || fake.lastID != "e1" || fake.lastAddr != "one@off.co" || fake.lastPW != "SECRET-PW" {
		t.Fatalf("email_test %s handler=%+v", b, fake)
	}
	if lg.has("SECRET-PW") {
		t.Fatal("password in the log")
	}
	if s := (model.IPCRequest{Cmd: model.CmdEmailTest, SmtpPassword: "SECRET-PW"}).String(); strings.Contains(s, "SECRET") {
		t.Fatalf("String leaked: %s", s)
	}

	st := model.DefaultSettings()
	c.send(model.IPCRequest{ID: 2, Cmd: model.CmdSaveSettings, Settings: &st, SmtpPassword: "NEW-SECRET", ExpectedRevision: "rev1"})
	if r := c.reply(2); r.Type != model.MsgOK || r.Revision != "rev-email" || h.smtpSaved != "NEW-SECRET" {
		t.Fatalf("save %+v smtp=%q", r, h.smtpSaved)
	}

	c.send(model.IPCRequest{ID: 3, Cmd: model.CmdExportConfig, IncludeEmailRecipients: true})
	r = c.reply(3)
	if r.Export == nil || !r.Export.ContainsEmailRecipients || r.Export.Settings == nil || r.Export.Settings.Email == nil {
		t.Fatalf("export %#v", r.Export)
	}

	atomic.StoreInt32(&admin, 0)
	c.send(model.IPCRequest{ID: 10, Cmd: model.CmdEmailTest})
	if r := c.reply(10); r.Code != model.ErrCodeAccessDenied {
		t.Fatalf("non-admin %+v", r)
	}
}
