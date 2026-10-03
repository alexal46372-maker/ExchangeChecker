package ipc

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_IPC_03_CloseNoInvalidRequest — Server.Close cancels a running
// test_profile before it disconnects the clients; the canceled test must
// not be answered (it was answered invalid_request "context canceled":
// linux -race CI run 37127399931). The hook widens the window between
// the cancel and the disconnect so the old race is deterministic.
func TestCHK_IPC_03_CloseNoInvalidRequest(t *testing.T) {
	h, s, l, _ := newV2(t)
	rw, _ := l.Dial()
	c := NewClient(rw, nil)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h.testBlock = make(chan struct{})
	h.testCtxOK = make(chan struct{})
	type reply struct {
		m   model.IPCMessage
		err error
	}
	res := make(chan reply, 1)
	go func() { m, err := c.TestProfile(ctx, "x", nil, ""); res <- reply{m, err} }()
	<-h.testReqs
	s.closeHook = func() {
		<-h.testCtxOK                      // the test returned ctx.Err()
		time.Sleep(150 * time.Millisecond) // a reply would be written now
	}
	s.Close()
	select {
	case r := <-res:
		if r.err == nil {
			t.Fatalf("reply after Close: %+v", r.m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client not released by Close")
	}
}

// TestCHK_IPC_03_CanceledErrorCode — a context cancellation is never
// reported as invalid_request.
func TestCHK_IPC_03_CanceledErrorCode(t *testing.T) {
	for _, err := range []error{context.Canceled, fmt.Errorf("query: %w", context.Canceled)} {
		m := errorReply(7, err, "")
		if m.Code != model.ErrCodeCanceled || m.ErrorKey != model.MsgMonitorCanceled {
			t.Errorf("%v: code %q key %q", err, m.Code, m.ErrorKey)
		}
	}
	if m := errorReply(7, errors.New("bad"), ""); m.Code != model.ErrCodeInvalidRequest {
		t.Errorf("plain error: %q", m.Code)
	}
}
