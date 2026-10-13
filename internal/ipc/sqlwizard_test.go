package ipc

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type wizHandler struct {
	fakeHandler
	started  chan struct{}
	canceled chan struct{}
}

func (h *wizHandler) SQLBrowse(ctx context.Context, req model.IPCRequest) (model.SQLBrowseResult, error) {
	if req.Host == "slow" {
		h.started <- struct{}{}
		<-ctx.Done()
		h.canceled <- struct{}{}
		return model.SQLBrowseResult{}, ctx.Err()
	}
	var res model.SQLBrowseResult
	for i := 0; i < 2000; i++ { // > 64 KiB: cut by the server (size)
		res.Instances = append(res.Instances, model.SQLServerInstance{Host: strings.Repeat("h", 60), Address: "10.0.0.1",
			Instance: "SQL" + strings.Repeat("x", 10)})
	}
	return res, nil
}

func (h *wizHandler) SQLDatabases(ctx context.Context, req model.IPCRequest) (model.SQLDatabasesResult, error) {
	info := model.SQLErrorInfo{Code: "login_failed", Category: model.SQLConnLogin, ErrorKey: "x", SQLNumber: 18456}
	return model.SQLDatabasesResult{}, &model.IPCError{Code: model.ErrCodeFailed, Key: model.MsgMonitorConnError,
		Message: "login failed", Info: &info}
}

func wizServer(t *testing.T, admin *int32) (*wizHandler, func() *Client) {
	t.Helper()
	h := &wizHandler{fakeHandler: fakeHandler{snap: testSnapshot()}, started: make(chan struct{}, 1), canceled: make(chan struct{}, 1)}
	srv := NewServer(h, nil)
	srv.Authorize = func(io.ReadWriteCloser) (Peer, error) {
		return Peer{Admin: atomic.LoadInt32(admin) == 1, Account: `HOST\alex`}, nil
	}
	l := NewMemListener()
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return h, func() *Client {
		rw, err := l.Dial()
		if err != nil {
			t.Fatal(err)
		}
		hello := make(chan model.IPCMessage, 64)
		c := NewClient(rw, func(m model.IPCMessage) { hello <- m })
		t.Cleanup(func() { c.Close() })
		select {
		case m := <-hello:
			caps := strings.Join(m.Capabilities, ",")
			for _, k := range []string{model.CapSQLBrowse, model.CapSQLDatabases, model.CapSQLDiagnose, model.CapSQLGrantScript} {
				if !strings.Contains(caps, k) {
					t.Errorf("capability %s missing: %s", k, caps)
				}
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no hello")
		}
		return c
	}
}

// TestCHK_IPC_10_WizardIPC — admin gating of all four sql_* commands,
// truncation to 64 KiB, errorInfo, sql_diagnose / sql_grant_script
// replies and their validation, cancel {ref} of sql_browse.
func TestCHK_IPC_10_WizardIPC(t *testing.T) {
	var admin int32
	h, dial := wizServer(t, &admin)
	c := dial()
	for _, cmd := range []string{model.CmdSQLBrowse, model.CmdSQLDatabases, model.CmdSQLDiagnose, model.CmdSQLGrantScript} {
		if m := do(t, c, model.IPCRequest{Cmd: cmd, Code: "login_failed"}); m.Type != model.MsgError || m.Code != model.ErrCodeAccessDenied {
			t.Errorf("%s as a user: %+v", cmd, m)
		}
	}
	atomic.StoreInt32(&admin, 1)
	m := do(t, c, model.IPCRequest{Cmd: model.CmdSQLBrowse})
	if m.Type != model.MsgSQLBrowse || m.SQLBrowse == nil || !m.Truncated || m.TruncatedReason != model.TruncatedSize ||
		!m.SQLBrowse.Truncated || len(m.SQLBrowse.Instances) == 0 || len(m.SQLBrowse.Instances) == 2000 {
		t.Fatalf("browse truncation: type %s truncated %v %q", m.Type, m.Truncated, m.TruncatedReason)
	}
	m = do(t, c, model.IPCRequest{Cmd: model.CmdSQLDatabases, Profile: &model.Profile{Server: "srv"}})
	if m.Type != model.MsgError || m.Code != model.ErrCodeFailed || m.ErrorInfo == nil || m.ErrorInfo.Code != "login_failed" || m.ErrorInfo.SQLNumber != 18456 {
		t.Errorf("errorInfo: %+v", m)
	}
	m = do(t, c, model.IPCRequest{Cmd: model.CmdSQLDiagnose, Code: "login_failed"})
	if m.Type != model.MsgSQLDiagnosis || m.SQLDiagnosis == nil || m.SQLDiagnosis.Key == "" {
		t.Errorf("diagnose by code: %+v", m)
	}
	m = do(t, c, model.IPCRequest{Cmd: model.CmdSQLDiagnose, Category: model.SQLConnLogin})
	if m.Type != model.MsgSQLDiagnosis || m.SQLDiagnosis == nil {
		t.Errorf("diagnose by category: %+v", m)
	}
	if m = do(t, c, model.IPCRequest{Cmd: model.CmdSQLDiagnose}); m.Type != model.MsgError || m.Code != model.ErrCodeInvalidRequest {
		t.Errorf("diagnose without input: %+v", m)
	}
	m = do(t, c, model.IPCRequest{Cmd: model.CmdSQLGrantScript, Principal: "ec_monitor", Database: "trade"})
	if m.Type != model.MsgSQLGrantScript || m.SQLGrantScript == nil || !strings.Contains(m.SQLGrantScript.SQL, "ec_monitor") {
		t.Errorf("grant script: %+v", m)
	}
	if m = do(t, c, model.IPCRequest{Cmd: model.CmdSQLGrantScript, Principal: "x];DROP", Database: ""}); m.Type != model.MsgError ||
		m.Code != model.ErrCodeInvalidRequest || len(m.ErrorArgs) < 2 {
		t.Errorf("grant script validation: %+v", m)
	}
	// cancel {ref}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.DoCancel(ctx, model.IPCRequest{Cmd: model.CmdSQLBrowse, Host: "slow"})
		done <- err
	}()
	select {
	case <-h.started:
	case <-time.After(5 * time.Second):
		t.Fatal("browse not started")
	}
	cancel()
	select {
	case <-h.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("browse not canceled")
	}
	<-done
	if m := do(t, c, model.IPCRequest{Cmd: model.CmdPing}); m.Type == model.MsgError {
		t.Errorf("connection after cancel: %+v", m)
	}
}
