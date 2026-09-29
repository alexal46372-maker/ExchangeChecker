package monitor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

const (
	savedPW = "Saved-Pa$$w0rd"
	reqPW   = "Req-S3cr3t!"
)

// fakeTest is an injectable TestFunc recording its calls.
type fakeTest struct {
	mu      sync.Mutex
	calls   []testCall
	info    TestInfo
	err     func(pw string) error
	block   chan struct{} // blocks (ignoring ctx) until closed
	started chan struct{}
}

type testCall struct {
	p  model.Profile
	pw string
}

func (f *fakeTest) Test(ctx context.Context, p model.Profile, pw string) (TestInfo, error) {
	f.mu.Lock()
	f.calls = append(f.calls, testCall{p, pw})
	block, started, info, errf := f.block, f.started, f.info, f.err
	f.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if block != nil {
		<-block
	}
	if errf != nil {
		return TestInfo{}, errf(pw)
	}
	return info, nil
}

func (f *fakeTest) n() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }
func (f *fakeTest) last() testCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return testCall{}
	}
	return f.calls[len(f.calls)-1]
}

// fakeAfter is the injectable timer of the hard test timeout.
type fakeAfter struct {
	mu    sync.Mutex
	chans []chan time.Time
	durs  []time.Duration
}

func (f *fakeAfter) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan time.Time, 1)
	f.chans = append(f.chans, ch)
	f.durs = append(f.durs, d)
	return ch
}

func (f *fakeAfter) fire(i int) {
	f.mu.Lock()
	ch := f.chans[i]
	f.mu.Unlock()
	ch <- time.Time{}
}

func sqlProf(id string) model.Profile {
	p := prof(id)
	p.AuthMode, p.Login, p.Server = model.AuthSQL, "u", `srv\INST`
	return p
}

type testEnv struct {
	m   *Monitor
	ft  *fakeTest
	fa  *fakeAfter
	clk *fakeClock
	lg  *memLog
	f   *fakeFactory
}

func newTestEnv(t *testing.T, saved []model.Profile, interval time.Duration) *testEnv {
	t.Helper()
	e := &testEnv{ft: &fakeTest{info: TestInfo{Version: "10.50.1600.1", ProductLevel: "RTM", Edition: "Standard Edition", CompatLevel: 100}},
		fa: &fakeAfter{}, clk: &fakeClock{t: t0}, lg: &memLog{}, f: newFactory()}
	pws := map[string]string{}
	for _, p := range saved {
		pws[p.ID] = savedPW
		e.f.set(p.ID, behavior{points: lagPoints(map[string]int{"P": 1})})
	}
	opt := baseOpt(e.f, NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: saved, Passwords: pws}), e.clk)
	opt.Test, opt.TestAfter, opt.Log, opt.CommandInterval = e.ft.Test, e.fa.After, e.lg, interval
	e.m = New(opt)
	e.m.reloadConfig(true)
	return e
}

func (e *testEnv) run(req model.IPCRequest) (model.TestProfileResult, error) {
	return e.m.TestProfile(context.Background(), req)
}

// TestCHK_IPC_03_TestProfile — saved profile by ID (saved password), an
// unsaved profile with its own password, server information in the
// result, encrypt=disable and TLS 1.0 go unchanged through the same test,
// errors: unknown ID -> not_found, missing / invalid profile ->
// invalid_request; Windows authentication only for the saved profile with
// its saved parameters (else category config, no connection).
func TestCHK_IPC_03_TestProfile(t *testing.T) {
	win := prof("w")
	e := newTestEnv(t, []model.Profile{sqlProf("s"), win}, -1)
	r, err := e.run(model.IPCRequest{ProfileID: "s"})
	if err != nil || !r.OK || r.Category != model.TestOK || r.ErrorKey != model.MsgTestOK {
		t.Fatalf("saved: %+v %v", r, err)
	}
	if r.ServerVersion != "10.50.1600.1" || r.ProductLevel != "RTM" || r.Edition != "Standard Edition" || r.CompatLevel != 100 || r.Encrypt != "disable" {
		t.Errorf("server info %+v", r)
	}
	if c := e.ft.last(); c.pw != savedPW || c.p.ID != "s" {
		t.Errorf("saved call %+v", c.p.ID)
	}
	if _, err := e.run(model.IPCRequest{ProfileID: "nope"}); codeOf(err) != model.ErrCodeNotFound {
		t.Errorf("unknown id: %v", err)
	}
	if _, err := e.run(model.IPCRequest{}); codeOf(err) != model.ErrCodeInvalidRequest {
		t.Errorf("empty request: %v", err)
	}
	bad := sqlProf("n")
	bad.Server = ""
	if _, err := e.run(model.IPCRequest{Profile: &bad, Password: reqPW}); codeOf(err) != model.ErrCodeInvalidRequest {
		t.Errorf("invalid profile: %v", err)
	}
	// unsaved profile (no ID, no mapping yet) with its own password
	n := sqlProf("")
	n.Mapping = model.Mapping{}
	n.Server, n.Database = "other-host", "db"
	if r, err := e.run(model.IPCRequest{Profile: &n, Password: reqPW}); err != nil || !r.OK {
		t.Fatalf("unsaved: %+v %v", r, err)
	}
	if c := e.ft.last(); c.pw != reqPW || c.p.Server != "other-host" {
		t.Errorf("unsaved call %+v", c.p.Server)
	}
	// encrypt=disable and TLS 1.0 (SQL 2008 preset with encryption on)
	d := sqlProf("s")
	d.Encrypt = model.EncryptDisable
	if r, _ := e.run(model.IPCRequest{Profile: &d, Password: reqPW}); r.Encrypt != "disable" || e.ft.last().p.EffectiveEncrypt() != model.EncryptDisable {
		t.Errorf("disable: %+v", r)
	}
	t10 := sqlProf("s")
	t10.Encrypt, t10.TLSMinVersion = model.EncryptOptional, model.TLSMin10
	if r, _ := e.run(model.IPCRequest{Profile: &t10, Password: reqPW}); r.Encrypt != "optional" || r.TLSMin != "1.0" || e.ft.last().p.EffectiveTLSMinVersion() != "1.0" {
		t.Errorf("tls 1.0: %+v", r)
	}
	// Windows authentication
	calls := e.ft.n()
	if r, err := e.run(model.IPCRequest{ProfileID: "w"}); err != nil || !r.OK {
		t.Errorf("saved windows: %+v %v", r, err)
	}
	same := win
	same.Name = "renamed" // not a connection parameter
	if r, err := e.run(model.IPCRequest{Profile: &same}); err != nil || !r.OK || e.ft.last().p.Name != win.Name {
		t.Errorf("windows, saved parameters: %+v %v", r, err)
	}
	if e.ft.n() != calls+2 {
		t.Fatalf("windows calls %d", e.ft.n()-calls)
	}
	for name, mut := range map[string]func(*model.Profile){
		"new profile": func(p *model.Profile) { p.ID = "new" },
		"other host":  func(p *model.Profile) { p.Server = "evil" },
		"other db":    func(p *model.Profile) { p.Database = "x" },
		"encrypt":     func(p *model.Profile) { p.Encrypt = model.EncryptOn },
		"login":       func(p *model.Profile) { p.Login = `DOM\x` },
	} {
		p := win
		mut(&p)
		r, err := e.run(model.IPCRequest{Profile: &p, Password: reqPW})
		if err != nil || r.OK || r.Category != model.TestConfig || r.ErrorKey != model.MsgTestWindowsUnsaved {
			t.Errorf("windows %s: %+v %v", name, r, err)
		}
	}
	if e.ft.n() != calls+2 {
		t.Errorf("unsaved windows profiles must not connect: %d calls", e.ft.n()-calls-2)
	}
	if model.MonitorMessage(model.MsgTestWindowsUnsaved, "ru") != "Сохраните профиль, чтобы проверить Windows-аутентификацию." ||
		model.MonitorMessage(model.MsgTestWindowsUnsaved, model.LangEN) == "" {
		t.Error("windows message RU/EN")
	}
}

// TestCHK_IPC_03_SavedPasswordReuse — an empty password reuses the saved
// one only for the same host, instance, port, login and auth mode with an
// encryption not weaker than saved; host / instance / port / login / auth
// mode swaps and every weaker encryption are rejected with
// invalid_request + "enter the password" and make no connection;
// identical and stronger settings are accepted.
func TestCHK_IPC_03_SavedPasswordReuse(t *testing.T) {
	saved := sqlProf("s")
	saved.Encrypt, saved.TLSMinVersion = model.EncryptOn, model.TLSMin11
	winSaved := prof("w")
	winSaved.Server, winSaved.Login = `srv\INST`, "u"
	e := newTestEnv(t, []model.Profile{saved, winSaved}, -1)
	reject := map[string]func(*model.Profile){
		"host swap":           func(p *model.Profile) { p.Server = `evil\INST` },
		"instance swap":       func(p *model.Profile) { p.Server = `srv\OTHER` },
		"no instance":         func(p *model.Profile) { p.Server = `srv` },
		"port swap":           func(p *model.Profile) { p.Port = 1444 },
		"login swap":          func(p *model.Profile) { p.Login = "sa" },
		"new profile id":      func(p *model.Profile) { p.ID = "other" },
		"encrypt disable":     func(p *model.Profile) { p.Encrypt = model.EncryptDisable },
		"encrypt default":     func(p *model.Profile) { p.Encrypt = model.EncryptDefault },
		"encrypt optional":    func(p *model.Profile) { p.Encrypt = model.EncryptOptional },
		"trust server cert":   func(p *model.Profile) { p.TrustServerCert = true },
		"lower tls min":       func(p *model.Profile) { p.TLSMinVersion = model.TLSMin10 },
		"mssql2008 tls floor": func(p *model.Profile) { p.TLSMinVersion = ""; p.ServerPreset = model.ServerPresetMSSQL2008 },
	}
	for name, mut := range reject {
		p := saved
		mut(&p)
		n := e.ft.n()
		_, err := e.run(model.IPCRequest{Profile: &p})
		var ie *model.IPCError
		if !errors.As(err, &ie) || ie.Code != model.ErrCodeInvalidRequest || ie.Key != model.MsgTestPasswordRequired {
			t.Errorf("%s: %v", name, err)
		}
		if e.ft.n() != n {
			t.Errorf("%s: connected with the saved password", name)
		}
	}
	// auth mode swap: saved Windows profile, request SQL auth with the
	// same server and login and no password
	sw := winSaved
	sw.AuthMode = model.AuthSQL
	if _, err := e.run(model.IPCRequest{Profile: &sw}); codeOf(err) != model.ErrCodeInvalidRequest {
		t.Errorf("auth mode swap: %v", err)
	}
	accept := map[string]func(*model.Profile){
		"identical":        func(*model.Profile) {},
		"host case":        func(p *model.Profile) { p.Server = `SRV\inst` },
		"other database":   func(p *model.Profile) { p.Database = "other" },
		"stronger strict":  func(p *model.Profile) { p.Encrypt = model.EncryptStrict },
		"higher tls min":   func(p *model.Profile) { p.TLSMinVersion = model.TLSMin12 },
		"longer timeouts":  func(p *model.Profile) { p.ConnectTimeoutSec = 30 },
		"renamed / mapped": func(p *model.Profile) { p.Name = "x"; p.Mapping = model.Mapping{} },
	}
	for name, mut := range accept {
		p := saved
		mut(&p)
		r, err := e.run(model.IPCRequest{Profile: &p})
		if err != nil || !r.OK || e.ft.last().pw != savedPW {
			t.Errorf("%s: %+v %v", name, r, err)
		}
	}
	// an explicit password is always used as given
	p := saved
	p.Server = "elsewhere"
	if _, err := e.run(model.IPCRequest{Profile: &p, Password: reqPW}); err != nil || e.ft.last().pw != reqPW {
		t.Errorf("explicit password: %v", err)
	}
	if model.MonitorMessage(model.MsgTestPasswordRequired, "ru") != "Введите пароль." || model.MonitorMessage(model.MsgTestPasswordRequired, model.LangEN) != "Enter the password." {
		t.Error("password message RU/EN")
	}
}

// TestCHK_IPC_03_TimeoutBusyNotBlocking — the hard timeout (15 s, fake
// timer) answers timeout even if the driver ignores its context; at most
// 2 tests run at a time (3rd = busy); a timed-out test still hanging in
// the driver counts against MaxAbandoned; polling of the databases
// continues while tests hang.
func TestCHK_IPC_03_TimeoutBusyNotBlocking(t *testing.T) {
	e := newTestEnv(t, []model.Profile{sqlProf("s"), sqlProf("t")}, -1)
	e.ft.block = make(chan struct{})
	e.ft.started = make(chan struct{}, 4)
	type res struct {
		r   model.TestProfileResult
		err error
	}
	out := make(chan res, 4)
	for _, id := range []string{"s", "t"} {
		id := id
		go func() { r, err := e.run(model.IPCRequest{ProfileID: id}); out <- res{r, err} }()
		<-e.ft.started
	}
	if _, err := e.run(model.IPCRequest{ProfileID: "s"}); !errors.Is(err, ErrTestBusy) || codeOf(err) != model.ErrCodeBusy {
		t.Fatalf("3rd test: %v", err)
	}
	// polling is not blocked by the hanging tests
	e.m.pollAll(context.Background())
	if !profileByID(e.m.Snapshot(), "s").Polled {
		t.Fatal("poll blocked by tests")
	}
	e.fa.mu.Lock()
	if len(e.fa.durs) != 2 || e.fa.durs[0] != DefaultTestTimeout || DefaultTestTimeout != 15*time.Second {
		t.Errorf("timeouts %v", e.fa.durs)
	}
	e.fa.mu.Unlock()
	e.clk.Add(DefaultTestTimeout)
	e.fa.fire(0)
	e.fa.fire(1)
	for i := 0; i < 2; i++ {
		r := <-out
		if r.err != nil || r.r.OK || r.r.Category != model.TestTimeout || r.r.ErrorKey != model.MsgTestTimeout || r.r.DurationMs != 15000 {
			t.Errorf("timeout result %+v %v", r.r, r.err)
		}
	}
	if run, ab := e.m.tester.Busy(); run != 0 || ab != 2 {
		t.Errorf("running %d abandoned %d", run, ab)
	}
	if _, err := e.run(model.IPCRequest{ProfileID: "s"}); codeOf(err) != model.ErrCodeBusy {
		t.Errorf("abandoned tests still in the driver must keep the tester busy: %v", err)
	}
	close(e.ft.block)
	waitFor(t, func() bool { _, ab := e.m.tester.Busy(); return ab == 0 })
	e.ft.mu.Lock()
	e.ft.block, e.ft.started = nil, nil
	e.ft.mu.Unlock()
	if r, err := e.run(model.IPCRequest{ProfileID: "s"}); err != nil || !r.OK {
		t.Errorf("after the driver returned: %+v %v", r, err)
	}
}

// TestCHK_IPC_03_RateLimited — test_profile is under the command rate
// limit (IPCCommandInterval): a second test within 10 s gets
// rate_limited without connecting; after 10 s it runs again.
func TestCHK_IPC_03_RateLimited(t *testing.T) {
	e := newTestEnv(t, []model.Profile{sqlProf("s")}, 0) // default 10 s
	if _, err := e.run(model.IPCRequest{ProfileID: "s"}); err != nil {
		t.Fatal(err)
	}
	n := e.ft.n()
	other := sqlProf("x")
	other.Server = "10.0.0.2"
	other.Port = 22
	for _, req := range []model.IPCRequest{{ProfileID: "s"}, {Profile: &other, Password: reqPW}} {
		_, err := e.run(req)
		var ie *model.IPCError
		if !errors.As(err, &ie) || ie.Code != model.ErrCodeRateLimited || ie.Key != model.MsgMonitorRateLimited {
			t.Errorf("rate limit: %v", err)
		}
	}
	if e.ft.n() != n {
		t.Error("rate-limited request connected")
	}
	e.clk.Add(9 * time.Second)
	if _, err := e.run(model.IPCRequest{ProfileID: "s"}); codeOf(err) != model.ErrCodeRateLimited {
		t.Errorf("after 9 s: %v", err)
	}
	e.clk.Add(time.Second)
	if r, err := e.run(model.IPCRequest{Profile: &other, Password: reqPW}); err != nil || !r.OK {
		t.Errorf("after 10 s: %+v %v", r, err)
	}
}

type sqlNumErr struct{ n int32 }

func (e sqlNumErr) Error() string         { return fmt.Sprintf("mssql: error %d", e.n) }
func (e sqlNumErr) SQLErrorNumber() int32 { return e.n }

// TestCHK_IPC_03_CategoriesNoPassword — failure categories (network,
// login, database, TLS with its sql.* hint, timeout, config, other) and
// the password never appears in the result, in the log or in any file of
// the config directory, even when the driver error contained it.
func TestCHK_IPC_03_CategoriesNoPassword(t *testing.T) {
	dir := t.TempDir()
	e := newTestEnv(t, []model.Profile{sqlProf("s")}, -1)
	e.m.opt.Counters = OpenFileCounterStore(dir, e.lg)
	e.m.opt.Acks = OpenFileAckStore(dir, e.lg)
	conn := func(err error) error { return &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "connect", Err: err} }
	cases := []struct {
		err       func(pw string) error
		cat, key  string
		wantInMsg string
	}{
		{func(pw string) error {
			return conn(&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused " + pw)})
		}, model.TestNetwork, model.MsgTestNetwork, "refused ***"},
		{func(pw string) error { return conn(fmt.Errorf("login failed (%s): %w", pw, sqlNumErr{18456})) }, model.TestLogin, model.MsgTestLogin, "login failed (***)"},
		{func(string) error { return conn(sqlNumErr{4060}) }, model.TestDatabase, model.MsgTestDatabase, "4060"},
		{func(string) error {
			return &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "connect", Err: errors.New("tls: handshake"), Key: sqlcheck.MsgTLSEnableTLS10}
		}, model.TestTLS, sqlcheck.MsgTLSEnableTLS10, "handshake"},
		{func(string) error {
			return &sqlcheck.Error{Kind: sqlcheck.KindTimeout, Op: "connect", Err: context.DeadlineExceeded}
		}, model.TestTimeout, model.MsgTestTimeout, "deadline"},
		{func(string) error {
			return &sqlcheck.Error{Kind: sqlcheck.KindConfig, Op: "open", Err: errors.New("bad")}
		}, model.TestConfig, model.MsgTestConfig, "bad"},
		{func(pw string) error { return errors.New("weird " + pw) }, model.TestOther, model.MsgTestOther, "weird ***"},
	}
	p := sqlProf("new")
	for _, c := range cases {
		e.ft.err = c.err
		r, err := e.run(model.IPCRequest{Profile: &p, Password: reqPW})
		if err != nil || r.OK || r.Category != c.cat || r.ErrorKey != c.key || !strings.Contains(r.Error, c.wantInMsg) {
			t.Errorf("%s: %+v %v", c.cat, r, err)
		}
		if strings.Contains(r.Error, reqPW) {
			t.Errorf("%s: password in the result", c.cat)
		}
	}
	// the saved password (by ID) is scrubbed too
	e.ft.err = func(pw string) error { return errors.New("echo " + pw) }
	if r, _ := e.run(model.IPCRequest{ProfileID: "s"}); strings.Contains(r.Error, savedPW) || !strings.Contains(r.Error, "***") {
		t.Errorf("saved password leaked: %q", r.Error)
	}
	// a request with a password is masked when formatted
	req := model.IPCRequest{Cmd: model.CmdTestProfile, Profile: &p, Password: reqPW}
	for _, s := range []string{fmt.Sprintf("%v", req), fmt.Sprintf("%+v", req), fmt.Sprintf("%#v", req), req.String()} {
		if strings.Contains(s, reqPW) {
			t.Errorf("formatted request contains the password: %s", s)
		}
	}
	// poll + ack so both state files exist, then scan logs and files
	e.m.pollAll(context.Background())
	e.lg.mu.Lock()
	for _, l := range e.lg.lines {
		if strings.Contains(l, reqPW) || strings.Contains(l, savedPW) {
			t.Errorf("password in the log: %s", l)
		}
	}
	if len(e.lg.lines) == 0 {
		t.Error("tests not logged")
	}
	e.lg.mu.Unlock()
	filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			b, _ := os.ReadFile(path)
			if strings.Contains(string(b), reqPW) || strings.Contains(string(b), savedPW) {
				t.Errorf("password written to %s", path)
			}
		}
		return nil
	})
}
