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

type fakeViberMgr struct {
	mu    sync.Mutex
	calls []string
	v     model.ViberSettings
	tok   bool
}

func (f *fakeViberMgr) GetViber() (model.ViberView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return model.ViberView{Settings: f.v, HasToken: f.tok, Status: model.ViberStatus{}}, nil
}

func (f *fakeViberMgr) SaveViber(s model.ViberSettings, token string, clear bool, account string) (model.ViberView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "save:"+account)
	if err := s.Validate(); err != nil {
		return model.ViberView{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberInvalid, Message: err.Error()}
	}
	f.v, f.tok = s, token != "" || f.tok && !clear
	return model.ViberView{Settings: f.v, HasToken: f.tok}, nil
}

func (f *fakeViberMgr) ViberTest(ctx context.Context, receiver, token string) (model.ViberTestResult, error) {
	st := 6
	return model.ViberTestResult{Results: []model.ViberTestItem{{Receiver: receiver, OK: false, ErrorKey: model.MsgViberReceiverInvalid, ViberStatus: &st}}}, nil
}

// TestCHK_SVC_06_IPCViber — capability viber; get_viber / save_viber /
// viber_test admin only; the token is never in a reply or a log line;
// viber_test is asynchronous with the reply matched by id.
func TestCHK_SVC_06_IPCViber(t *testing.T) {
	var admin int32 = 1
	v2, _, _, _ := newV2(t)
	lg := &testLog{}
	s := NewServer(v2, lg)
	vm := &fakeViberMgr{v: model.DefaultViberSettings()}
	s.Viber = vm
	s.Authorize = func(io.ReadWriteCloser) (Peer, error) {
		return Peer{Admin: atomic.LoadInt32(&admin) == 1, Account: `HOST\alex`}, nil
	}
	l := NewMemListener()
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })

	rw, _ := l.Dial()
	t.Cleanup(func() { rw.Close() })
	var hello model.IPCMessage
	if err := ReadMessage(rw, &hello); err != nil || !strings.Contains(strings.Join(hello.Capabilities, ","), model.CapViber) {
		t.Fatalf("hello %+v %v", hello.Capabilities, err)
	}
	c := &rawConn{t: t, rw: rw, pending: map[uint64]model.IPCMessage{}}
	c.send(model.IPCRequest{ID: 1, Cmd: model.CmdGetViber})
	r := c.reply(1)
	b, _ := json.Marshal(r.Viber)
	if r.Type != model.MsgViber || string(b) != `{"settings":{"enabled":false,"receivers":[],"schedule":{"days":[1,2,3,4,5],"from":"08:30","to":"17:00"},"events":{"overdue":true,"recovered":true},"reminderMinutes":60,"senderName":"ExchangeChecker"},"hasToken":false,"inWindow":false,"status":{"queued":0,"sent":0,"dropped":0}}` {
		t.Fatalf("get_viber %s %+v", b, r)
	}
	nv := model.DefaultViberSettings()
	nv.Enabled, nv.Receivers = true, []string{"r1"}
	c.send(model.IPCRequest{ID: 2, Cmd: model.CmdSaveViber, Viber: &nv, ViberToken: "TOKEN-SECRET-1"})
	r = c.reply(2)
	all, _ := json.Marshal(r)
	if r.Type != model.MsgViber || r.Result != model.ResultSaved || r.Viber == nil || !r.Viber.HasToken || strings.Contains(string(all), "TOKEN-SECRET") {
		t.Fatalf("save_viber %s", all)
	}
	bad := nv
	bad.ReminderMinutes = 5
	c.send(model.IPCRequest{ID: 3, Cmd: model.CmdSaveViber, Viber: &bad})
	if r := c.reply(3); r.Type != model.MsgError || r.ErrorKey != model.MsgViberInvalid || r.Code != model.ErrCodeInvalidRequest {
		t.Fatalf("invalid %+v", r)
	}
	c.send(model.IPCRequest{ID: 4, Cmd: model.CmdViberTest, Receiver: "r1", ViberToken: "TOKEN-SECRET-2"})
	r = c.reply(4)
	b, _ = json.Marshal(r.ViberTest)
	if r.Type != model.MsgViberTest || string(b) != `{"results":[{"receiver":"r1","ok":false,"errorKey":"notify.viber_receiver_invalid","viberStatus":6}]}` {
		t.Fatalf("viber_test %s %+v", b, r)
	}
	atomic.StoreInt32(&admin, 0)
	for i, cmd := range []string{model.CmdGetViber, model.CmdSaveViber, model.CmdViberTest} {
		id := uint64(10 + i)
		c.send(model.IPCRequest{ID: id, Cmd: cmd, Viber: &nv, ViberToken: "TOKEN-SECRET-3"})
		if r := c.reply(id); r.Code != model.ErrCodeAccessDenied {
			t.Fatalf("%s non-admin %+v", cmd, r)
		}
	}
	if lg.has("TOKEN-SECRET") {
		t.Fatal("token in the log")
	}
	if s := (model.IPCRequest{Cmd: model.CmdSaveViber, ViberToken: "TOKEN-SECRET-4"}).String(); strings.Contains(s, "TOKEN-SECRET") {
		t.Fatalf("String %s", s)
	}
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if len(vm.calls) != 2 || vm.calls[0] != `save:HOST\alex` {
		t.Fatalf("calls %v", vm.calls)
	}
}
