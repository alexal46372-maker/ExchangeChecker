//go:build windows

package ipc

import (
	"io"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// testPipeSDDL is PipeSDDL with Everyone instead of Interactive Users (a
// CI runner process may lack the INTERACTIVE group) and the same
// 0x12018b mask, so a restricted / non-admin client is checked against
// exactly the non-admin access of the production DACL.
const testPipeSDDL = "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x12018b;;;WD)"

func startPipeCfgServer(t *testing.T) (*cfgHandler, string, *testLog) {
	t.Helper()
	name := testPipeName()
	l, err := ListenPipe(name, testPipeSDDL)
	if err != nil {
		t.Fatalf("ListenPipe: %v", err)
	}
	v2, _, _, _ := newV2(t)
	h := &cfgHandler{v2Handler: v2}
	lg := &testLog{}
	s := NewServer(h, lg) // no Authorize: the real pipe ClientIdentity
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })
	return h, name, lg
}

func pipeRaw(t *testing.T, rw io.ReadWriteCloser) *rawConn {
	t.Helper()
	t.Cleanup(func() { rw.Close() })
	readHello(t, rw)
	return &rawConn{t: t, rw: rw, pending: map[uint64]model.IPCMessage{}}
}

// dialAs opens the pipe on a locked OS thread impersonating tok (static
// SQOS tracking: the identity is captured at CreateFile), then reverts.
func dialAs(t *testing.T, name string, tok syscall.Token) io.ReadWriteCloser {
	t.Helper()
	type res struct {
		rw  io.ReadWriteCloser
		err error
	}
	ch := make(chan res, 1)
	go func() {
		runtime.LockOSThread() // never unlocked: the thread dies with the goroutine
		if err := winapi.ImpersonateLoggedOnUser(tok); err != nil {
			ch <- res{nil, err}
			return
		}
		rw, err := DialPipe(name, 5*time.Second)
		if rerr := winapi.RevertToSelf(); rerr != nil && err == nil {
			err = rerr
		}
		ch <- res{rw, err}
	}()
	r := <-ch
	if r.err != nil {
		t.Fatalf("dial as restricted token: %v", r.err)
	}
	return r.rw
}

var adminReqs = func() []model.IPCRequest {
	st := model.DefaultSettings()
	p := model.Profile{ID: "p1"}
	return []model.IPCRequest{
		{Cmd: model.CmdGetConfig},
		{Cmd: model.CmdSaveProfile, Profile: &p, Password: "pw"},
		{Cmd: model.CmdDeleteProfile, ProfileID: "p1"},
		{Cmd: model.CmdSaveSettings, Settings: &st},
	}
}

func expectDenied(t *testing.T, c *rawConn, h *cfgHandler, who string) {
	t.Helper()
	for i, r := range adminReqs() {
		r.ID = uint64(100 + i)
		if m := c.call(r); m.Type != model.MsgError || m.Code != model.ErrCodeAccessDenied || m.ErrorKey != model.MsgMonitorAccessDenied {
			t.Errorf("%s: %s not denied: %+v", who, r.Cmd, m)
		}
	}
	if m := c.call(model.IPCRequest{ID: 200, Cmd: model.CmdGetStatus}); m.ClientElevated == nil || *m.ClientElevated {
		t.Errorf("%s: clientElevated %v", who, m.ClientElevated)
	}
	if calls := h.Calls(); len(calls) != 0 {
		t.Errorf("%s: handler called: %v", who, calls)
	}
}

// TestCHK_IPC_06_PipeElevatedAdmin — over a real pipe an elevated
// administrator client (the test process) passes ImpersonateNamedPipeClient
// + CheckTokenMembership(BA): clientElevated true, admin commands run, the
// account reaches the handler. Skipped (with the reason) when the runner
// process is not an elevated administrator.
func TestCHK_IPC_06_PipeElevatedAdmin(t *testing.T) {
	isAdmin, err := winapi.ProcessIsAdmin()
	if err != nil || !isAdmin {
		t.Skipf("runner process is not an elevated administrator (CheckTokenMembership(BA) = %v, %v): admin-positive pipe check cannot run here", isAdmin, err)
	}
	h, name, _ := startPipeCfgServer(t)
	rw, err := DialPipe(name, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := pipeRaw(t, rw)
	if m := c.call(model.IPCRequest{ID: 1, Cmd: model.CmdGetStatus}); m.ClientElevated == nil || !*m.ClientElevated {
		t.Fatalf("clientElevated of an elevated admin: %v", m.ClientElevated)
	}
	if m := c.call(model.IPCRequest{ID: 2, Cmd: model.CmdGetConfig}); m.Type != model.MsgConfig || m.Revision != "rev1" {
		t.Fatalf("get_config: %+v", m)
	}
	st := model.DefaultSettings()
	if m := c.call(model.IPCRequest{ID: 3, Cmd: model.CmdSaveSettings, Settings: &st}); m.Type != model.MsgOK {
		t.Fatalf("save_settings: %+v", m)
	}
	calls := h.Calls()
	if len(calls) != 2 || !strings.HasPrefix(calls[1], "save_settings:") || calls[1] == "save_settings:" || !strings.Contains(calls[1], `\`) {
		t.Fatalf("calls / account: %v", calls)
	}
	t.Logf("client account: %s", strings.TrimPrefix(calls[1], "save_settings:"))
}

// TestCHK_IPC_06_PipeRestrictedTokenDenied — a client whose token has
// BUILTIN\Administrators as deny-only (CreateRestrictedToken, the shape of
// a UAC-filtered admin token) connects with the non-admin access mask of
// the production DACL (also verifies DialPipe no longer asks for
// GENERIC_WRITE) and gets access_denied on every admin command.
func TestCHK_IPC_06_PipeRestrictedTokenDenied(t *testing.T) {
	h, name, lg := startPipeCfgServer(t)
	tok, err := winapi.DenyOnlyToken(winapi.SIDAdministrators)
	if err != nil {
		t.Fatalf("DenyOnlyToken: %v", err)
	}
	defer tok.Close()
	c := pipeRaw(t, dialAs(t, name, tok))
	expectDenied(t, c, h, "restricted token")
	if !lg.has("refused") {
		t.Error("refusal not logged")
	}
}

// TestCHK_IPC_06_PipeAnonymousRefused — a client opening the pipe with
// SQOS SecurityAnonymous cannot be identified (impersonation yields an
// anonymous token): every admin command is refused.
func TestCHK_IPC_06_PipeAnonymousRefused(t *testing.T) {
	h, name, _ := startPipeCfgServer(t)
	n, _ := syscall.UTF16PtrFromString(name)
	hd, err := syscall.CreateFile(n, clientAccess, 0, nil, syscall.OPEN_EXISTING,
		winapi.FileFlagOverlapped|winapi.SecuritySQOSPresent /* | SECURITY_ANONYMOUS (0) */, 0)
	if err != nil {
		t.Fatalf("CreateFile anonymous: %v", err)
	}
	pc, err := newPipeConn(hd, false)
	if err != nil {
		syscall.CloseHandle(hd)
		t.Fatal(err)
	}
	expectDenied(t, pipeRaw(t, pc), h, "anonymous SQOS")
}

// TestCHK_IPC_06_ImpersonateBeforeFirstRead — documents why clientElevated
// is not in hello: ImpersonateNamedPipeClient before the server has read
// anything from the client is expected to fail (ERROR_CANNOT_IMPERSONATE).
// If this Windows version allows it, the test logs that (the design stays:
// the reply to the first request is equally early for the tray). After
// the first read it must succeed.
func TestCHK_IPC_06_ImpersonateBeforeFirstRead(t *testing.T) {
	name := testPipeName()
	l, err := ListenPipe(name, testPipeSDDL)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	acc := make(chan io.ReadWriteCloser, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			t.Error(err)
			acc <- nil
			return
		}
		acc <- c
	}()
	cli, err := DialPipe(name, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	srv := <-acc
	if srv == nil {
		t.FailNow()
	}
	defer srv.Close()
	pc := srv.(*pipeConn)
	_, before := pc.ClientIdentity()
	if before != nil {
		t.Logf("before the first read: %v (expected; errno 1368 = ERROR_CANNOT_IMPERSONATE: %v)", before,
			strings.Contains(before.Error(), winapi.ErrorCannotImpersonate.Error()))
	} else {
		t.Logf("NOTE: ImpersonateNamedPipeClient succeeded BEFORE the first read on this Windows version")
	}
	if err := WriteRequest(cli, model.IPCRequest{ID: 1, Cmd: model.CmdPing}); err != nil {
		t.Fatal(err)
	}
	var req model.IPCRequest
	if err := ReadFrame(srv, &req, model.IPCMaxRequest); err != nil || req.Cmd != model.CmdPing {
		t.Fatalf("read: %v %+v", err, req)
	}
	who, err := pc.ClientIdentity()
	if err != nil {
		t.Fatalf("ClientIdentity after the first read: %v", err)
	}
	if who.Account == "" {
		t.Error("no account")
	}
	t.Logf("after the first read: admin=%v account=%s", who.Admin, who.Account)
}
