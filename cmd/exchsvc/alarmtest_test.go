package main

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_IPC_11_AlarmTest — alarm_test (admin, capability alarm_test):
// the test alert is pushed with test:true and no id, is not in
// events_since, status.alerts / problems or the Event Log; viber:true
// without a token gives viber {ok:false, results:[]}; one
// per 10 s (rate_limited + retryAfterMs); level checked; non-admin denied.
func TestCHK_IPC_11_AlarmTest(t *testing.T) {
	dir := t.TempDir()
	ev := &fakeEvents{}
	var admin int32 = 1
	s := startSvcWith(t, dir, 5, nil, ev, func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) {
			return ipc.Peer{Admin: atomic.LoadInt32(&admin) == 1, Account: `TEST\admin`}, nil
		}
	})
	defer s.stop()
	hello := s.waitPush(t, func(m model.IPCMessage) bool { return m.Type == model.MsgHello })
	if !strings.Contains(","+strings.Join(hello.Capabilities, ",")+",", ",alarm_test,") {
		t.Fatalf("caps %v", hello.Capabilities)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if r, _ := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdAlarmTest, Level: "critical"}); r.Code != model.ErrCodeInvalidRequest || r.ErrorKey != model.MsgAlarmTestLevel {
		t.Fatalf("bad level %+v", r)
	}
	r, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdAlarmTest, Level: model.AlarmLevelError, Viber: true})
	if err != nil || r.Type != model.MsgAlarmTest || r.AlarmTest == nil {
		t.Fatalf("alarm_test %+v %v", r, err)
	}
	b, _ := json.Marshal(r.AlarmTest)
	if string(b) != `{"ok":true,"viber":{"ok":false,"results":[]}}` {
		t.Fatalf("reply %s", b)
	}
	push := s.waitPush(t, func(m model.IPCMessage) bool { return m.Type == model.MsgAlert && m.Alert != nil && m.Alert.Test })
	pb, _ := json.Marshal(push.Alert)
	if push.Alert.ID != 0 || push.Alert.EventID != 0 || push.Alert.To != model.LevelError || push.Alert.ProfileID != model.AlarmTestProfileID ||
		!strings.Contains(string(pb), `"test":true`) {
		t.Fatalf("push %s", pb)
	}
	ev.mu.Lock()
	for _, m := range ev.got {
		if strings.Contains(m, "alarm-test") || strings.Contains(m, "Тест") {
			t.Errorf("test alarm in the Event Log: %q", m)
		}
	}
	ev.mu.Unlock()
	// rate limit
	r, _ = s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdAlarmTest, Level: model.AlarmLevelWarning})
	if r.Code != model.ErrCodeRateLimited || r.RetryAfterMs <= 0 || r.RetryAfterMs > 10000 {
		t.Fatalf("rate limit %+v", r)
	}
	// not in events_since / status
	es, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdEventsSince})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es.Events {
		if e.Test || e.ProfileID == model.AlarmTestProfileID {
			t.Fatalf("test alert in events_since %+v", e)
		}
	}
	st, _ := s.c.CallTimeout(model.CmdGetStatus, 5*time.Second)
	sb, _ := json.Marshal(st.Status)
	if strings.Contains(string(sb), model.AlarmTestProfileID) {
		t.Fatalf("test alert in status %s", sb)
	}
	atomic.StoreInt32(&admin, 0)
	if r, _ := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdAlarmTest, Level: model.AlarmLevelWarning}); r.Code != model.ErrCodeAccessDenied {
		t.Fatalf("non-admin %+v", r)
	}
}
