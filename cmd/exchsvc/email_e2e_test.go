package main

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
)

type mailbox struct {
	mu  sync.Mutex
	got []string
}

func (b *mailbox) send(ctx context.Context, host string, port int, tlsMode, from, password, to, subject, body string) error {
	b.mu.Lock()
	b.got = append(b.got, to)
	b.mu.Unlock()
	return nil
}

func (b *mailbox) has(addr string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, a := range b.got {
		if a == addr {
			return true
		}
	}
	return false
}

// TestCHK_SVC_09_EmailRecipientE2E — the tray editor's save_settings
// payload (one recipient) through the real service: IPC save_settings →
// settings.json → reload → email_test and an alert reach the recipient
// via the SMTP hook; after a service restart the recipient is still there.
func TestCHK_SVC_09_EmailRecipientE2E(t *testing.T) {
	emailE2E(t, "")
}

func emailE2E(t *testing.T, noProfiles string) {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	p := model.NewProfile("db1")
	p.Name, p.Server, p.Database, p.AuthMode = "Склад", "srv", "db", model.AuthWindows
	p.Mapping = model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	if noProfiles == "" {
		if err := st.SaveProfiles([]model.Profile{p}); err != nil {
			t.Fatal(err)
		}
	}
	box := &mailbox{}
	start := func() *svc {
		return startSvcWith(t, dir, 0, nil, &fakeEvents{}, func(ac *appConfig) {
			ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Admin: true, Account: `TEST\admin`}, nil }
			ac.EmailSendHook = box.send
		})
	}
	s := start()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := s.c.Call(ctx, model.CmdGetConfig)
	if err != nil || cfg.Config == nil {
		t.Fatalf("get_config %+v %v", cfg, err)
	}
	ed := traysettings.NewEditor(model.LangRU, traysettings.Admin)
	ed.LoadConfig(*cfg.Config, cfg.Revision)
	if !ed.CanEditEmail() {
		t.Fatal("email page not editable")
	}
	ed.Email.Enabled = true
	ed.Email.Host, ed.Email.Port, ed.Email.TLSMode = "relay.example", "25", model.EmailTLSNone
	ed.Email.From = "bot@example.com"
	ed.Email.Recipients = "alex alex@example.com Алекс"
	for i := range ed.Email.Days {
		ed.Email.Days[i] = true
	}
	ed.Email.FromTime, ed.Email.ToTime = "00:00", "23:59"
	for i := range ed.Email.Events {
		ed.Email.Events[i] = true
	}
	req, pr := ed.EmailRequest()
	if pr != nil {
		t.Fatalf("tray request: %+v", pr)
	}
	if req.Settings.Email == nil || len(req.Settings.Email.Recipients) != 1 {
		t.Fatalf("tray payload %+v", req.Settings.Email)
	}
	if m, err := s.c.Do(ctx, req); err != nil || m.Type != model.MsgOK {
		t.Fatalf("save_settings %+v %v", m, err)
	}
	cur, err := st.LoadSettings()
	if err != nil || len(cur.Email.Recipients) != 1 || cur.Email.Recipients[0].Addr != "alex@example.com" {
		t.Fatalf("persisted %+v %v", cur.Email, err)
	}
	test := func(s *svc, id string) {
		t.Helper()
		var m model.IPCMessage
		for i := 0; i < 50; i++ {
			m, err = s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdEmailTest, RecipientID: id})
			if err == nil && m.Type == model.MsgEmailTest {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if err != nil || m.Type != model.MsgEmailTest || m.EmailTest == nil || !m.EmailTest.OK {
			t.Fatalf("email_test %q: %+v %v", id, m, err)
		}
	}
	test(s, "")
	if !box.has("alex@example.com") {
		t.Fatalf("not delivered: %v", box.got)
	}
	s.stop()

	// restart: settings.json reloaded, recipient still known; an alert
	// (first poll, lag far above the error threshold) is mailed too
	box.got = nil
	s = startSvcWith(t, dir, 600, nil, &fakeEvents{}, func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Admin: true, Account: `TEST\admin`}, nil }
		ac.EmailSendHook = box.send
	})
	defer s.stop()
	deadline := time.Now().Add(8 * time.Second)
	for !box.has("alex@example.com") && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !box.has("alex@example.com") {
		t.Fatalf("alert not mailed: %v", box.got)
	}
	test(s, "alex")
	_ = strings.TrimSpace
}

// TestCHK_SVC_09_EmailTestBeforeFirstPoll — after a service restart the
// first poll is still running (slow / unreachable SQL Server): email_test
// and the notifier must already know the saved recipients from
// settings.json, not report notify.email_no_recipients.
func TestCHK_SVC_09_EmailTestBeforeFirstPoll(t *testing.T) {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	p := model.NewProfile("db1")
	p.Name, p.Server, p.Database, p.AuthMode = "Склад", "srv", "db", model.AuthWindows
	p.Mapping = model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	if err := st.SaveProfiles([]model.Profile{p}); err != nil {
		t.Fatal(err)
	}
	set := model.DefaultSettings()
	set.Email.Enabled = true
	set.Email.Host, set.Email.Port, set.Email.TLSMode, set.Email.From = "relay.example", 25, model.EmailTLSNone, "bot@example.com"
	set.Email.Recipients = []model.EmailRecipient{{ID: "alex", Addr: "alex@example.com", Name: "Алекс"}}
	if err := st.SaveSettings(set); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	defer close(release)
	box := &mailbox{}
	s := startSvcWith(t, dir, 0, nil, &fakeEvents{}, func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Admin: true, Account: `TEST\admin`}, nil }
		ac.EmailSendHook = box.send
		ac.NewReader = func(pr model.Profile, pw string, opt sqlcheck.Options) (sqlcheck.ReadCloser, error) {
			<-release // first poll never finishes during the test
			return nil, context.Canceled
		}
	})
	defer s.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdEmailTest, RecipientID: "alex"})
	if err != nil || m.Type != model.MsgEmailTest || m.EmailTest == nil || !m.EmailTest.OK {
		t.Fatalf("email_test before first poll: %+v %v", m, err)
	}
	if !box.has("alex@example.com") {
		t.Fatalf("not delivered: %v", box.got)
	}
}

// TestCHK_SVC_09_EmailRecipientWithoutID — save_settings with a recipient
// that has only an address (no id): the service assigns a stable id,
// persists it and delivers email_test to that address.
func TestCHK_SVC_09_EmailRecipientWithoutID(t *testing.T) {
	dir := t.TempDir()
	box := &mailbox{}
	s := startSvcWith(t, dir, 0, nil, &fakeEvents{}, func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Admin: true, Account: `TEST\admin`}, nil }
		ac.EmailSendHook = box.send
	})
	defer s.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, err := s.c.Call(ctx, model.CmdGetConfig)
	if err != nil || cfg.Config == nil {
		t.Fatal(err)
	}
	set := cfg.Config.Settings
	e := *set.Email
	e.Enabled, e.Host, e.Port, e.TLSMode, e.From = true, "relay.example", 25, model.EmailTLSNone, "bot@example.com"
	e.Recipients = []model.EmailRecipient{{Addr: " Alex@Example.com "}}
	set.Email = &e
	if m, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSaveSettings, Settings: &set, ExpectedRevision: cfg.Revision}); err != nil || m.Type != model.MsgOK {
		t.Fatalf("save %+v %v", m, err)
	}
	st := config.NewStore(dir)
	cur, err := st.LoadSettings()
	if err != nil || len(cur.Email.Recipients) != 1 || cur.Email.Recipients[0].ID != "alex@example.com" {
		t.Fatalf("persisted %+v %v", cur.Email, err)
	}
	m, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdEmailTest, RecipientID: "alex@example.com"})
	if err != nil || m.EmailTest == nil || !m.EmailTest.OK || !box.has("Alex@Example.com") {
		t.Fatalf("email_test %+v %v got=%v", m, err, box.got)
	}
}

// TestCHK_SVC_09_EmailRecipientListAndErrors — an addr list split by
// , ; whitespace becomes separate recipients; enabled with no recipients is
// refused with notify.email_no_recipients; email_test of an unknown id is
// notify.email_recipient_not_found (not «no recipients»).
func TestCHK_SVC_09_EmailRecipientListAndErrors(t *testing.T) {
	dir := t.TempDir()
	box := &mailbox{}
	s := startSvcWith(t, dir, 0, nil, &fakeEvents{}, func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Admin: true, Account: `TEST\admin`}, nil }
		ac.EmailSendHook = box.send
	})
	defer s.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, err := s.c.Call(ctx, model.CmdGetConfig)
	if err != nil || cfg.Config == nil {
		t.Fatal(err)
	}
	set := cfg.Config.Settings
	e := *set.Email
	e.Enabled, e.Host, e.Port, e.TLSMode, e.From = true, "relay.example", 25, model.EmailTLSNone, "bot@example.com"
	e.Recipients = []model.EmailRecipient{}
	set.Email = &e
	m, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSaveSettings, Settings: &set, ExpectedRevision: cfg.Revision})
	if err != nil || m.Type != model.MsgError || m.ErrorKey != model.MsgEmailNoRecipients {
		t.Fatalf("enabled without recipients: %+v %v", m, err)
	}
	e.Recipients = []model.EmailRecipient{{Addr: "a@b.ru, c@d.ru;e@f.ru"}}
	if m, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSaveSettings, Settings: &set, ExpectedRevision: cfg.Revision}); err != nil || m.Type != model.MsgOK {
		t.Fatalf("save list %+v %v", m, err)
	}
	cur, _ := config.NewStore(dir).LoadSettings()
	if len(cur.Email.Recipients) != 3 || cur.Email.Recipients[2].Addr != "e@f.ru" || cur.Email.Recipients[1].ID != "c@d.ru" {
		t.Fatalf("split %+v", cur.Email.Recipients)
	}
	m, _ = s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdEmailTest, RecipientID: "nobody"})
	if m.Type != model.MsgError || m.ErrorKey != model.MsgEmailRecipientNotFound {
		t.Fatalf("unknown id %+v", m)
	}
	if !strings.Contains(model.MonitorMessage(model.MsgEmailNoRecipients, model.LangRU), "Нет сохранённых получателей") {
		t.Fatal("RU text")
	}
}
