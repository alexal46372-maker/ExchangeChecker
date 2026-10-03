package ipc

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// cfgHandler adds a fake ConfigManager to v2Handler.
type cfgHandler struct {
	*v2Handler
	mu    sync.Mutex
	calls []string
	err   error // returned by the next call
}

func (h *cfgHandler) record(c string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, c)
	err := h.err
	h.err = nil
	return err
}

func (h *cfgHandler) Calls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls...)
}

func (h *cfgHandler) GetConfig() (model.ConfigView, string, error) {
	if err := h.record("get_config"); err != nil {
		return model.ConfigView{}, "", err
	}
	return model.ConfigView{Settings: model.DefaultSettings(), Profiles: []model.Profile{{ID: "p1", HasPassword: true}}}, "rev1", nil
}

func (h *cfgHandler) SaveProfile(p model.Profile, password string, clear bool, exp, account string) (model.SavedProfile, error) {
	if err := h.record("save_profile:" + p.ID + ":" + password + ":" + exp + ":" + account); err != nil {
		return model.SavedProfile{}, err
	}
	p.HasPassword = password != ""
	return model.SavedProfile{Profile: p, Revision: "rev2", PollScheduled: p.Enabled}, nil
}

func (h *cfgHandler) DeleteProfile(id, exp, account string) (string, error) {
	if err := h.record("delete_profile:" + id + ":" + account); err != nil {
		return "", err
	}
	return "rev3", nil
}

func (h *cfgHandler) SaveSettings(st model.Settings, exp, account string) (string, error) {
	if err := h.record("save_settings:" + account); err != nil {
		return "", err
	}
	return "rev4", nil
}

type rawConn struct {
	t  *testing.T
	rw io.ReadWriteCloser
	// replies read but not yet asked for, by id
	pending map[uint64]model.IPCMessage
}

func dialRaw(t *testing.T, l *MemListener) *rawConn {
	t.Helper()
	rw, err := l.Dial()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rw.Close() })
	readHello(t, rw)
	return &rawConn{t: t, rw: rw, pending: map[uint64]model.IPCMessage{}}
}

func (c *rawConn) send(req model.IPCRequest) {
	c.t.Helper()
	if err := WriteRequest(c.rw, req); err != nil {
		c.t.Fatal(err)
	}
}

// reply returns the reply with id (pushes skipped).
func (c *rawConn) reply(id uint64) model.IPCMessage {
	c.t.Helper()
	if m, ok := c.pending[id]; ok {
		delete(c.pending, id)
		return m
	}
	for {
		var m model.IPCMessage
		if err := ReadMessage(c.rw, &m); err != nil {
			c.t.Fatalf("read reply %d: %v", id, err)
		}
		if m.ID == id {
			return m
		}
		if m.ID != 0 {
			c.pending[m.ID] = m
		}
	}
}

func (c *rawConn) call(req model.IPCRequest) model.IPCMessage {
	c.t.Helper()
	c.send(req)
	return c.reply(req.ID)
}

func newCfgServer(t *testing.T, admin *int32, authCalls *int32, authErr error) (*cfgHandler, *MemListener, *testLog) {
	t.Helper()
	v2, _, _, _ := newV2(t) // only for the handler; its server is unused
	h := &cfgHandler{v2Handler: v2}
	lg := &testLog{}
	s := NewServer(h, lg)
	if admin != nil {
		s.Authorize = func(io.ReadWriteCloser) (Peer, error) {
			atomic.AddInt32(authCalls, 1)
			if authErr != nil {
				return Peer{}, authErr
			}
			return Peer{Admin: atomic.LoadInt32(admin) == 1, Account: `HOST\alex`}, nil
		}
	}
	l := NewMemListener()
	go s.Serve(l)
	t.Cleanup(func() { s.Close() })
	return h, l, lg
}

// CHK-IPC-06: admin commands are refused (access_denied +
// monitor.access_denied_admin) for a non-admin client and the handler is
// never called; the check runs on EVERY admin command (no cache), while
// clientElevated of get_status / ping is computed once per connection.
func TestCHK_IPC_06_AdminCommandsGate(t *testing.T) {
	var admin, auth int32
	h, l, lg := newCfgServer(t, &admin, &auth, nil)
	rw, _ := l.Dial()
	defer rw.Close()
	var hello model.IPCMessage
	if err := ReadMessage(rw, &hello); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{model.CapConfigRead, model.CapConfigWrite, model.CapCancel} {
		if !strings.Contains(strings.Join(hello.Capabilities, ","), c) {
			t.Errorf("capability %s missing: %v", c, hello.Capabilities)
		}
	}
	if hello.ClientElevated != nil {
		t.Error("clientElevated must not be in hello")
	}
	c := &rawConn{t: t, rw: rw, pending: map[uint64]model.IPCMessage{}}
	st := model.DefaultSettings()
	p := model.Profile{ID: "p1", Enabled: true}
	reqs := []model.IPCRequest{
		{Cmd: model.CmdGetConfig},
		{Cmd: model.CmdSaveProfile, Profile: &p, Password: "secret"},
		{Cmd: model.CmdDeleteProfile, ProfileID: "p1"},
		{Cmd: model.CmdSaveSettings, Settings: &st},
	}
	id := uint64(0)
	for _, r := range reqs {
		id++
		r.ID = id
		m := c.call(r)
		if m.Type != model.MsgError || m.Code != model.ErrCodeAccessDenied || m.ErrorKey != model.MsgMonitorAccessDenied ||
			strings.Contains(m.Error, "secret") {
			t.Errorf("%s as non-admin: %+v", r.Cmd, m)
		}
	}
	if len(h.Calls()) != 0 {
		t.Fatalf("handler called for a non-admin: %v", h.Calls())
	}
	if !lg.has("refused") {
		t.Error("refusal not logged")
	}
	id++
	m := c.call(model.IPCRequest{ID: id, Cmd: model.CmdGetStatus})
	if m.ClientElevated == nil || *m.ClientElevated {
		t.Fatalf("get_status clientElevated: %+v", m.ClientElevated)
	}
	before := atomic.LoadInt32(&auth)
	// now an admin: admin commands see it at once (no cache) ...
	atomic.StoreInt32(&admin, 1)
	id++
	m = c.call(model.IPCRequest{ID: id, Cmd: model.CmdGetConfig})
	if m.Type != model.MsgConfig || m.Revision != "rev1" || m.Config == nil || len(m.Config.Profiles) != 1 {
		t.Fatalf("get_config as admin: %+v", m)
	}
	id++
	m = c.call(model.IPCRequest{ID: id, Cmd: model.CmdSaveProfile, Profile: &p, Password: "pw", ExpectedRevision: "rev1"})
	if m.Type != model.MsgOK || m.Result != model.ResultSaved || m.ProfileID != "p1" || m.Profile == nil || !m.Profile.HasPassword ||
		!m.Reloaded || !m.PollScheduled || m.Revision != "rev2" {
		t.Fatalf("save_profile: %+v", m)
	}
	id++
	m = c.call(model.IPCRequest{ID: id, Cmd: model.CmdDeleteProfile, ProfileID: "p1"})
	if m.Type != model.MsgOK || m.Result != model.ResultDeleted || m.ProfileID != "p1" || !m.Reloaded || m.Revision != "rev3" {
		t.Fatalf("delete_profile: %+v", m)
	}
	id++
	m = c.call(model.IPCRequest{ID: id, Cmd: model.CmdSaveSettings, Settings: &st})
	if m.Type != model.MsgOK || m.Result != model.ResultSaved || !m.Reloaded || m.Revision != "rev4" {
		t.Fatalf("save_settings: %+v", m)
	}
	want := []string{"get_config", `save_profile:p1:pw:rev1:HOST\alex`, `delete_profile:p1:HOST\alex`, `save_settings:HOST\alex`}
	if !reflect.DeepEqual(h.Calls(), want) {
		t.Fatalf("calls %v, want %v", h.Calls(), want)
	}
	if n := atomic.LoadInt32(&auth) - before; n != 4 {
		t.Errorf("authorize called %d times for 4 admin commands (must re-check every time)", n)
	}
	// ... but clientElevated of this connection stays the cached display value
	id++
	if m = c.call(model.IPCRequest{ID: id, Cmd: model.CmdPing}); m.ClientElevated == nil || *m.ClientElevated {
		t.Errorf("ping clientElevated changed within the connection: %v", m.ClientElevated)
	}
	// a new connection sees the new value
	c2 := dialRaw(t, l)
	if m = c2.call(model.IPCRequest{ID: 1, Cmd: model.CmdPing}); m.ClientElevated == nil || !*m.ClientElevated {
		t.Errorf("new connection clientElevated: %v", m.ClientElevated)
	}
	// revoke: refused again immediately
	atomic.StoreInt32(&admin, 0)
	id++
	if m = c.call(model.IPCRequest{ID: id, Cmd: model.CmdGetConfig}); m.Code != model.ErrCodeAccessDenied {
		t.Errorf("revoked admin: %+v", m)
	}
}

// CHK-IPC-06: handler errors keep code / errorKey / errorArgs; missing
// profile / settings are invalid_request; a plain error gets save_failed.
func TestCHK_IPC_06_AdminErrors(t *testing.T) {
	admin, auth := int32(1), int32(0)
	h, l, _ := newCfgServer(t, &admin, &auth, nil)
	c := dialRaw(t, l)
	m := c.call(model.IPCRequest{ID: 1, Cmd: model.CmdSaveProfile})
	if m.Code != model.ErrCodeInvalidRequest || m.ErrorKey != model.MsgMonitorProfileInvalid || !reflect.DeepEqual(m.ErrorArgs, []string{"profile", model.ValRequired}) {
		t.Errorf("no profile: %+v", m)
	}
	m = c.call(model.IPCRequest{ID: 2, Cmd: model.CmdSaveSettings})
	if m.Code != model.ErrCodeInvalidRequest || m.ErrorKey != model.MsgMonitorSettingsInvalid {
		t.Errorf("no settings: %+v", m)
	}
	h.err = &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorProfileInvalid, Message: "port", Args: []string{"port", model.ValRange, "0", "65535", "70000"}}
	p := model.Profile{ID: "x"}
	m = c.call(model.IPCRequest{ID: 3, Cmd: model.CmdSaveProfile, Profile: &p})
	if m.Code != model.ErrCodeInvalidRequest || len(m.ErrorArgs) != 5 || m.ErrorArgs[0] != "port" {
		t.Errorf("args lost: %+v", m)
	}
	h.err = &model.IPCError{Code: model.ErrCodeConflict, Key: model.MsgMonitorConfigConflict, Message: "conflict"}
	if m = c.call(model.IPCRequest{ID: 4, Cmd: model.CmdDeleteProfile, ProfileID: "x", ExpectedRevision: "old"}); m.Code != model.ErrCodeConflict ||
		m.ErrorKey != model.MsgMonitorConfigConflict {
		t.Errorf("conflict: %+v", m)
	}
	h.err = errors.New("disk on fire")
	if m = c.call(model.IPCRequest{ID: 5, Cmd: model.CmdGetConfig}); m.Code != model.ErrCodeSaveFailed || m.ErrorKey != model.MsgMonitorConfigSaveFailed {
		t.Errorf("plain error: %+v", m)
	}
	// every error reply carries code and errorKey
	if m = c.call(model.IPCRequest{ID: 6, Cmd: "frobnicate"}); m.Code != model.ErrCodeUnknownCommand || m.ErrorKey != model.MsgMonitorUnknownCommand {
		t.Errorf("unknown: %+v", m)
	}
}

// CHK-IPC-06: an Authorize error and a transport that cannot identify the
// client both mean "not an administrator".
func TestCHK_IPC_06_UnidentifiedClientDenied(t *testing.T) {
	admin, auth := int32(1), int32(0)
	h, l, _ := newCfgServer(t, &admin, &auth, errors.New("ERROR_CANNOT_IMPERSONATE"))
	c := dialRaw(t, l)
	if m := c.call(model.IPCRequest{ID: 1, Cmd: model.CmdGetConfig}); m.Code != model.ErrCodeAccessDenied {
		t.Errorf("authorize error: %+v", m)
	}
	if m := c.call(model.IPCRequest{ID: 2, Cmd: model.CmdGetStatus}); m.ClientElevated == nil || *m.ClientElevated {
		t.Errorf("clientElevated with authorize error: %v", m.ClientElevated)
	}
	h2, l2, _ := newCfgServer(t, nil, nil, nil) // MemListener, no Authorize
	c2 := dialRaw(t, l2)
	if m := c2.call(model.IPCRequest{ID: 1, Cmd: model.CmdSaveSettings, Settings: &model.Settings{}}); m.Code != model.ErrCodeAccessDenied {
		t.Errorf("no identifier: %+v", m)
	}
	if len(h.Calls())+len(h2.Calls()) != 0 {
		t.Error("handler called")
	}
}

// CHK-IPC-07: cancel {ref} stops the caller's running test_profile
// without dropping the connection: ok canceled for the cancel, error
// canceled (monitor.canceled) for the test, its ctx is canceled; unknown /
// finished refs -> not_found + monitor.unknown_request; a duplicate
// running id is refused; another connection cannot cancel it.
func TestCHK_IPC_07_Cancel(t *testing.T) {
	h, _, l, _ := newV2(t)
	h.testBlock = make(chan struct{})
	h.testCtxOK = make(chan struct{})
	c := dialRaw(t, l)
	other := dialRaw(t, l)
	c.send(model.IPCRequest{ID: 5, Cmd: model.CmdTestProfile, ProfileID: "db1"})
	<-h.testReqs // running
	if m := c.call(model.IPCRequest{ID: 5, Cmd: model.CmdTestProfile, ProfileID: "db1"}); m.Code != model.ErrCodeInvalidRequest ||
		m.ErrorKey != model.MsgMonitorBadRequest {
		t.Errorf("duplicate running id: %+v", m)
	}
	if m := other.call(model.IPCRequest{ID: 1, Cmd: model.CmdCancel, Ref: 5}); m.Code != model.ErrCodeNotFound || m.ErrorKey != model.MsgMonitorUnknownRequest {
		t.Errorf("cancel from another connection: %+v", m)
	}
	if m := c.call(model.IPCRequest{ID: 6, Cmd: model.CmdCancel, Ref: 5}); m.Type != model.MsgOK || m.Result != model.ResultCanceled {
		t.Fatalf("cancel: %+v", m)
	}
	<-h.testCtxOK
	if m := c.reply(5); m.Type != model.MsgError || m.Code != model.ErrCodeCanceled || m.ErrorKey != model.MsgMonitorCanceled {
		t.Fatalf("canceled test reply: %+v", m)
	}
	if m := c.call(model.IPCRequest{ID: 7, Cmd: model.CmdPing}); m.Type != model.MsgPong {
		t.Fatalf("connection dropped after cancel: %+v", m)
	}
	for _, ref := range []uint64{5, 99} {
		if m := c.call(model.IPCRequest{ID: 8 + ref, Cmd: model.CmdCancel, Ref: ref}); m.Code != model.ErrCodeNotFound || m.ErrorKey != model.MsgMonitorUnknownRequest {
			t.Errorf("cancel ref %d: %+v", ref, m)
		}
	}
	// a finished test: not_found, its result was delivered normally
	h.testBlock = nil
	if m := c.call(model.IPCRequest{ID: 20, Cmd: model.CmdTestProfile, ProfileID: "db1"}); m.Type != model.MsgTestResult {
		t.Fatalf("test: %+v", m)
	}
	<-h.testReqs
	if m := c.call(model.IPCRequest{ID: 21, Cmd: model.CmdCancel, Ref: 20}); m.Code != model.ErrCodeNotFound {
		t.Errorf("cancel finished: %+v", m)
	}
}

// CHK-IPC-07: Client.DoCancel sends cancel {ref} when its ctx ends; the
// service cancels the test and the connection keeps working.
func TestCHK_IPC_07_ClientDoCancel(t *testing.T) {
	h, _, l, _ := newV2(t)
	h.testBlock = make(chan struct{})
	h.testCtxOK = make(chan struct{})
	rw, _ := l.Dial()
	cl := NewClient(rw, nil)
	defer cl.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := cl.DoCancel(ctx, model.IPCRequest{Cmd: model.CmdTestProfile, ProfileID: "db1"})
		done <- err
	}()
	<-h.testReqs
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("DoCancel: %v", err)
	}
	select {
	case <-h.testCtxOK:
	case <-time.After(10 * time.Second):
		t.Fatal("service test not canceled")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	if m, err := cl.Call(ctx2, model.CmdPing); err != nil || m.Type != model.MsgPong {
		t.Fatalf("ping after cancel: %+v %v", m, err)
	}
}
