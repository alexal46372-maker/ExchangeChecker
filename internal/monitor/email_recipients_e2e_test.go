package monitor

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
	"github.com/alexal46372-maker/ExchangeChecker/internal/notify"
)

// TestCHK_TRAY_87_SingleAddressSaveThenTest — the real path of «Тест» on
// the e-mail page: the tray editor reads a recipients box with ONE bare
// address (no id), sends save_settings through ipc.Server to the
// Monitor (settings written by config.Store), the notify.EmailNotifier
// gets the saved settings from the snapshot (as exchsvc wires it), and
// email_test then reaches that address — no «Не заданы получатели».
func TestCHK_TRAY_87_SingleAddressSaveThenTest(t *testing.T) {
	cs := config.NewStore(t.TempDir())
	cs.Protector = plainProtector{}
	var mu sync.Mutex
	var sent []string
	email := notify.NewEmail(notify.EmailOptions{Store: cs, Now: time.Now,
		SendHook: func(ctx context.Context, host string, port int, tlsMode, from, password, to, subject, body string) error {
			mu.Lock()
			sent = append(sent, to)
			mu.Unlock()
			return nil
		}})
	t.Cleanup(email.Close)
	opt := baseOpt(newFactory(), FileConfig{Store: cs}, &fakeClock{t: t0})
	opt.Log, opt.CommandInterval = &memLog{}, -1
	opt.EmailStatus, opt.OnEmailPassword = email.Status, email.ReloadPassword
	opt.OnSnapshot = func(s model.StatusSnapshot) { email.Snapshot(s) }
	m := New(opt)
	m.reloadConfig(true)
	srv := ipc.NewServer(m, &memLog{})
	srv.Email = email
	srv.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Admin: true, Account: `HOST\admin`}, nil }
	l := ipc.NewMemListener()
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	rw, err := l.Dial()
	if err != nil {
		t.Fatal(err)
	}
	var hello model.IPCMessage
	if err := ipc.ReadMessage(rw, &hello); err != nil {
		t.Fatal(err)
	}
	c := ipc.NewClient(rw, nil)
	t.Cleanup(func() { c.Close() })
	e := &chainEnv{t: t, m: m, c: c}

	ed := e.editor()
	if !ed.CanEditEmail() {
		t.Fatalf("email page not editable: %+v", ed.Settings.Email)
	}
	ed.Email.Enabled = true
	ed.Email.Host, ed.Email.Port, ed.Email.From = "smtp.example.com", "25", "mon@example.com"
	ed.Email.TLSMode = model.EmailTLSNone
	ed.Email.Recipients = "alex@mail.ru"
	for i := range ed.Email.Days {
		ed.Email.Days[i] = true
	}
	save, pr := ed.EmailRequest()
	if pr != nil {
		t.Fatalf("save request: %+v", pr)
	}
	if rs := save.Settings.Email.Recipients; len(rs) != 1 || rs[0].Addr != "alex@mail.ru" || rs[0].ID != "alex" {
		t.Fatalf("recipients sent %+v", rs)
	}
	sm := e.do(save)
	if sm.Type == model.MsgError {
		t.Fatalf("save_settings refused: %+v", sm.Error)
	}
	ed.EmailDone(sm, save)
	// the service really stored the recipient
	st, err := cs.LoadSettings()
	if err != nil || st.Email == nil || len(st.Email.Recipients) != 1 || st.Email.Recipients[0].Addr != "alex@mail.ru" {
		t.Fatalf("stored %+v %v", st.Email, err)
	}
	test, pr := ed.EmailTestRequest("", "")
	if pr != nil {
		t.Fatalf("test request: %+v", pr)
	}
	tm := e.do(test)
	if tm.Type == model.MsgError {
		txt := ed.Reply(tm).Text
		if strings.Contains(txt, model.MonitorMessage(model.MsgEmailNoRecipients)) {
			t.Fatalf("email_test still sees no recipients: %q", txt)
		}
		t.Fatalf("email_test error %+v: %q", tm.Error, txt)
	}
	if tm.EmailTest == nil || !tm.EmailTest.OK || len(tm.EmailTest.Results) != 1 || tm.EmailTest.Results[0].Addr != "alex@mail.ru" {
		t.Fatalf("email_test %+v", tm.EmailTest)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0] != "alex@mail.ru" {
		t.Fatalf("sent %v", sent)
	}
}
