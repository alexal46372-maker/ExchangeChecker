package ipc

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type fakeViberTester struct{}

func (fakeViberTester) ViberTest(ctx context.Context, recipientID, token string) (model.ViberTestResult, error) {
	return model.ViberTestResult{OK: false, Results: []model.ViberTestItem{{ID: recipientID, OK: false, ErrorKey: model.MsgViberReceiverInvalid}}}, nil
}

// viberCfgHandler: a ConfigManager with ViberSettingsSaver.
type viberCfgHandler struct {
	*cfgHandler
	vmu   sync.Mutex
	saved []string
}

func (h *viberCfgHandler) SaveSettingsViber(st model.Settings, token string, clear bool, exp, account string) (string, error) {
	h.vmu.Lock()
	defer h.vmu.Unlock()
	en := st.Viber != nil && st.Viber.Enabled
	h.saved = append(h.saved, strings.Join([]string{token, map[bool]string{true: "clear", false: "keep"}[clear], exp, account,
		map[bool]string{true: "on", false: "off"}[en]}, "|"))
	return "rev9", nil
}

// TestCHK_SVC_06_IPCViber — capability viber; viber_test admin only,
// asynchronous, exact reply shape; save_settings carries viberToken /
// clearViberToken to the handler; the token is never in a reply or a log
// line.
func TestCHK_SVC_06_IPCViber(t *testing.T) {
	var admin int32 = 1
	v2, _, _, _ := newV2(t)
	h := &viberCfgHandler{cfgHandler: &cfgHandler{v2Handler: v2}}
	lg := &testLog{}
	s := NewServer(h, lg)
	s.Viber = fakeViberTester{}
	s.Authorize = func(io.ReadWriteCloser) (Peer, error) {
		return Peer{Admin: atomic.LoadInt32(&admin) == 1, Account: `HOST\alex`}, nil
	}
	l := NewMemListener()
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })

	rw, _ := l.Dial()
	t.Cleanup(func() { rw.Close() })
	var hello model.IPCMessage
	if err := ReadMessage(rw, &hello); err != nil || !strings.Contains(","+strings.Join(hello.Capabilities, ",")+",", ",viber,") {
		t.Fatalf("hello %+v %v", hello.Capabilities, err)
	}
	c := &rawConn{t: t, rw: rw, pending: map[uint64]model.IPCMessage{}}
	st := model.DefaultSettings()
	st.Viber.Enabled, st.Viber.Recipients = true, []model.ViberRecipient{{ID: "r1", Name: "Иван"}}
	c.send(model.IPCRequest{ID: 2, Cmd: model.CmdSaveSettings, Settings: &st, ViberToken: "TOKEN-SECRET-1", ExpectedRevision: "rev1"})
	r := c.reply(2)
	all, _ := json.Marshal(r)
	if r.Type != model.MsgOK || r.Revision != "rev9" || strings.Contains(string(all), "TOKEN-SECRET") {
		t.Fatalf("save_settings %s", all)
	}
	c.send(model.IPCRequest{ID: 3, Cmd: model.CmdSaveSettings, Settings: &st, ClearViberToken: true, ExpectedRevision: "rev9"})
	if r := c.reply(3); r.Type != model.MsgOK {
		t.Fatalf("clear %+v", r)
	}
	c.send(model.IPCRequest{ID: 4, Cmd: model.CmdViberTest, RecipientID: "r1", ViberToken: "TOKEN-SECRET-2"})
	r = c.reply(4)
	b, _ := json.Marshal(model.IPCMessage{Type: r.Type, ViberTest: r.ViberTest})
	if string(b) != `{"type":"viber_test","viberTest":{"ok":false,"results":[{"id":"r1","ok":false,"errorKey":"notify.viber_receiver_invalid"}]}}` {
		t.Fatalf("viber_test %s", b)
	}
	atomic.StoreInt32(&admin, 0)
	c.send(model.IPCRequest{ID: 10, Cmd: model.CmdViberTest, ViberToken: "TOKEN-SECRET-3"})
	if r := c.reply(10); r.Code != model.ErrCodeAccessDenied {
		t.Fatalf("non-admin %+v", r)
	}
	c.send(model.IPCRequest{ID: 11, Cmd: model.CmdSaveSettings, Settings: &st, ViberToken: "TOKEN-SECRET-3"})
	if r := c.reply(11); r.Code != model.ErrCodeAccessDenied {
		t.Fatalf("non-admin save %+v", r)
	}
	if lg.has("TOKEN-SECRET") {
		t.Fatal("token in the log")
	}
	if s := (model.IPCRequest{Cmd: model.CmdSaveSettings, ViberToken: "TOKEN-SECRET-4"}).String(); strings.Contains(s, "TOKEN-SECRET") {
		t.Fatalf("String %s", s)
	}
	h.vmu.Lock()
	defer h.vmu.Unlock()
	if strings.Join(h.saved, ";") != `TOKEN-SECRET-1|keep|rev1|HOST\alex|on;|clear|rev9|HOST\alex|on` {
		t.Fatalf("saved %v", h.saved)
	}
}
