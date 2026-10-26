package ipc

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type alarmOnly struct{ fakeHandler }

type nopAlarm struct{}

func (nopAlarm) AlarmTest(ctx context.Context, e model.AlertEvent, viber bool, account string) (model.AlarmTestResult, error) {
	return model.AlarmTestResult{}, nil
}

// TestCHK_IPC_11_ClientHello — client_hello stores the client's
// capabilities (unknown ignored, echo of the accepted ones; limits 64
// items / 64 chars -> invalid_request); capability client_hello; the test
// alarm push reaches only connections that declared alarm_test (a client
// without client_hello — tray 0.10 — never gets it).
func TestCHK_IPC_11_ClientHello(t *testing.T) {
	srv := NewServer(&alarmOnly{fakeHandler{snap: testSnapshot()}}, nil)
	srv.AlarmTest = nopAlarm{}
	srv.Authorize = func(io.ReadWriteCloser) (Peer, error) { return Peer{Admin: true}, nil }
	l := NewMemListener()
	go srv.Serve(l)
	defer srv.Close()
	dial := func() (*Client, chan model.IPCMessage) {
		rw, err := l.Dial()
		if err != nil {
			t.Fatal(err)
		}
		ch := make(chan model.IPCMessage, 64)
		c := NewClient(rw, func(m model.IPCMessage) { ch <- m })
		t.Cleanup(func() { c.Close() })
		if h := <-ch; !strings.Contains(","+strings.Join(h.Capabilities, ",")+",", ",client_hello,") {
			t.Fatalf("caps %v", h.Capabilities)
		}
		return c, ch
	}
	newC, newP := dial()
	oldC, oldP := dial()
	_ = oldC
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	many := make([]string, 65)
	for i := range many {
		many[i] = "x"
	}
	for _, caps := range [][]string{many, {strings.Repeat("a", 65)}} {
		if r, _ := newC.Do(ctx, model.IPCRequest{Cmd: model.CmdClientHello, Capabilities: caps}); r.Code != model.ErrCodeInvalidRequest {
			t.Errorf("limits %+v", r)
		}
	}
	r, err := newC.Do(ctx, model.IPCRequest{Cmd: model.CmdClientHello, Capabilities: []string{"zzz", "alarm_test", "alarm_test"}})
	b, _ := json.Marshal(r)
	if err != nil || string(b) != `{"id":`+itoa(r.ID)+`,"type":"client_hello","capabilities":["alarm_test"]}` {
		t.Fatalf("reply %s %v", b, err)
	}
	if r, err := newC.Do(ctx, model.IPCRequest{Cmd: model.CmdAlarmTest, Level: "warning"}); err != nil || r.AlarmTest == nil {
		t.Fatalf("alarm_test %+v %v", r, err)
	}
	got := func(ch chan model.IPCMessage) bool {
		deadline := time.After(300 * time.Millisecond)
		for {
			select {
			case m := <-ch:
				if m.Type == model.MsgAlert && m.Alert != nil && m.Alert.Test {
					return true
				}
			case <-deadline:
				return false
			}
		}
	}
	if !got(newP) {
		t.Error("declared client got no test push")
	}
	if got(oldP) {
		t.Error("old client got the test push")
	}
}

func itoa(n uint64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
