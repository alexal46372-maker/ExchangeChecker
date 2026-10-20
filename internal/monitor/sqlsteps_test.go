package monitor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// TestCHK_IPC_12_SQLBrowseModes — sql_browse v2: mode / port / cidr reach
// sqlcheck.Browse with a Progress callback; validation.sql_browse_cidr
// passes through (invalid_request + monitor.bad_request, args [cidr,
// key]); per-mode limits (local 3 s, broadcast 15 s, subnet 15 s, keys
// independent); subnet runs with the 15 s IPC deadline; a running browse
// answers busy + retryAfterMs.
func TestCHK_IPC_12_SQLBrowseModes(t *testing.T) {
	clk := &fakeClock{t: t0}
	m := New(baseOpt(newFactory(), NewMemConfig(Config{Settings: model.DefaultSettings()}), clk))
	var mu sync.Mutex
	var got []model.SQLBrowseRequest
	var deadlines []time.Duration
	block := make(chan struct{})
	started := make(chan struct{}, 1)
	m.explorer.BrowseWith = func(ctx context.Context, req model.SQLBrowseRequest, opt sqlcheck.BrowseOptions) (model.SQLBrowseResult, error) {
		dl, _ := ctx.Deadline()
		mu.Lock()
		got = append(got, req)
		deadlines = append(deadlines, time.Until(dl))
		mu.Unlock()
		if opt.Progress != nil {
			opt.Progress(model.SQLBrowseProgress{Phase: "ssrp", Done: 1, Total: 3})
		}
		if req.Host == "slow" {
			started <- struct{}{}
			<-block
		}
		return model.SQLBrowseResult{Mode: req.EffectiveMode(), Sent: 3, SendErrorCount: 20,
			SendErrors: make([]model.SQLBrowseSendErr, 20), Advice: []string{model.MsgSQLBrowseAdviceEnterServer}}, nil
	}
	ctx := context.Background()
	var events []model.SQLBrowseProgress
	prog := func(p model.SQLBrowseProgress) { events = append(events, p) }
	_, err := m.SQLBrowseProgress(ctx, model.IPCRequest{Mode: "subnet", CIDR: "10.0.0.0/16"}, prog)
	wizBadRequest(t, "cidr", err, "cidr", model.ValSQLBrowseCIDR)
	_, err = m.SQLBrowseProgress(ctx, model.IPCRequest{Mode: "nope"}, prog)
	wizBadRequest(t, "mode", err, "mode", model.ValEnum)
	res, err := m.SQLBrowseProgress(ctx, model.IPCRequest{Mode: "subnet", CIDR: "192.168.1.0/24", Port: 1444}, prog)
	if err != nil || res.Mode != "subnet" || len(res.SendErrors) != model.MaxSQLBrowseSendErrors || res.SendErrorCount != 20 || len(events) != 1 {
		t.Fatalf("subnet %+v %v %v", res, err, events)
	}
	if got[0].CIDR != "192.168.1.0/24" || got[0].Port != 1444 || deadlines[0] < 14*time.Second || deadlines[0] > 15*time.Second {
		t.Errorf("subnet req %+v deadline %v", got[0], deadlines[0])
	}
	limited := func(what string, err error, ms int64) {
		t.Helper()
		var ie *model.IPCError
		if !errors.As(err, &ie) || ie.Code != model.ErrCodeRateLimited || ie.RetryAfterMs != ms {
			t.Errorf("%s: %#v", what, err)
		}
	}
	_, err = m.SQLBrowseProgress(ctx, model.IPCRequest{Mode: "subnet"}, nil)
	limited("subnet again", err, 15000)
	if _, err = m.SQLBrowseProgress(ctx, model.IPCRequest{Mode: "local"}, nil); err != nil {
		t.Fatal("local after subnet:", err)
	}
	if _, err = m.SQLBrowseProgress(ctx, model.IPCRequest{Mode: "broadcast"}, nil); err != nil {
		t.Fatal("broadcast after local:", err)
	}
	clk.Add(time.Second)
	_, err = m.SQLBrowseProgress(ctx, model.IPCRequest{Mode: "local"}, nil)
	limited("local again", err, 2000)
	_, err = m.SQLBrowseProgress(ctx, model.IPCRequest{}, nil) // auto = broadcast
	limited("auto = broadcast", err, 14000)
	// busy with retryAfterMs
	done := make(chan error, 1)
	go func() {
		_, err := m.SQLBrowseProgress(ctx, model.IPCRequest{Mode: "host", Host: "slow"}, nil)
		done <- err
	}()
	<-started
	clk.Add(1500 * time.Millisecond) // past the any-host 1 s limit
	_, err = m.SQLBrowseProgress(ctx, model.IPCRequest{Mode: "host", Host: "other"}, nil)
	var ie *model.IPCError
	if !errors.As(err, &ie) || ie.Code != model.ErrCodeBusy || ie.RetryAfterMs <= 0 {
		t.Errorf("busy %#v", err)
	}
	close(block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestCHK_IPC_13_SQLTestSteps — sql_test_steps: server required and
// validated; the saved profile and its saved password are handed to
// sqlcheck.CheckServer (never from IPC); steps are forwarded; unknown
// profile; one at a time (busy + retryAfterMs); the log never carries the
// password, the login or the stage detail.
func TestCHK_IPC_13_SQLTestSteps(t *testing.T) {
	clk := &fakeClock{t: t0}
	p := model.NewProfile("db1")
	p.Name, p.Server, p.Database, p.AuthMode, p.Login = "Склад", "srv", "db", model.AuthSQL, "exch_login"
	p.Mapping = model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	var logb strings.Builder
	var logMu sync.Mutex
	opt := baseOpt(newFactory(), NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{p},
		Passwords: map[string]string{p.ID: "S3cret!pw"}}), clk)
	opt.Log = logFunc(func(f string, a ...interface{}) {
		logMu.Lock()
		defer logMu.Unlock()
		logb.WriteString(fmt.Sprintf(f, a...) + "\n")
	})
	m := New(opt)
	m.reloadConfig(true)
	var gotOpt sqlcheck.ServerCheckOptions
	var gotServer string
	block := make(chan struct{})
	started := make(chan struct{}, 1)
	m.explorer.CheckServer = func(ctx context.Context, server string, o sqlcheck.ServerCheckOptions) (model.SQLServerCheckResult, error) {
		if server == "slow" {
			started <- struct{}{}
			<-block
		}
		gotOpt, gotServer = o, server
		st := model.SQLCheckStep{ID: model.SQLStepLogin, OK: false, Ms: 12, Code: "login_failed",
			Detail: "Login failed for user 'exch_login'", ErrorKey: "sql.diag_login", AdviceKey: model.MsgSQLCheckAdviceLogin}
		o.Step(model.SQLCheckStep{ID: model.SQLStepParse, OK: true})
		o.Step(st)
		return model.SQLServerCheckResult{Host: "srv", Port: 1433, Steps: []model.SQLCheckStep{{ID: model.SQLStepParse, OK: true}, st}}, nil
	}
	ctx := context.Background()
	var steps []model.SQLCheckStep
	step := func(s model.SQLCheckStep) { steps = append(steps, s) }
	_, err := m.SQLTestSteps(ctx, model.IPCRequest{}, step)
	wizBadRequest(t, "no server", err, "server", model.ValRequired)
	_, err = m.SQLTestSteps(ctx, model.IPCRequest{Server: "np:srv"}, step)
	wizBadRequest(t, "pipe", err, "server", model.ValSQLServerForbidden)
	if _, err = m.SQLTestSteps(ctx, model.IPCRequest{Server: "srv", ProfileID: "nope"}, step); !errors.Is(err, ErrUnknownProfile) {
		t.Errorf("unknown profile %v", err)
	}
	res, err := m.SQLTestSteps(ctx, model.IPCRequest{Server: `srv\SQL2008,1444`, ProfileID: "db1"}, step)
	if err != nil || len(res.Steps) != 2 || len(steps) != 2 || steps[1].Code != "login_failed" {
		t.Fatalf("%+v %v %+v", res, err, steps)
	}
	if gotServer != `srv\SQL2008,1444` || gotOpt.Profile == nil || gotOpt.Profile.ID != "db1" || gotOpt.Password != "S3cret!pw" {
		t.Errorf("opt %q %+v", gotServer, gotOpt.Profile)
	}
	if _, err = m.SQLTestSteps(ctx, model.IPCRequest{Server: "srv"}, nil2(step)); err != nil || gotOpt.Profile != nil || gotOpt.Password != "" {
		t.Errorf("without profile %v %+v", err, gotOpt)
	}
	done := make(chan error, 1)
	go func() { _, err := m.SQLTestSteps(ctx, model.IPCRequest{Server: "slow"}, step); done <- err }()
	<-started
	_, err = m.SQLTestSteps(ctx, model.IPCRequest{Server: "srv"}, step)
	var ie *model.IPCError
	if !errors.As(err, &ie) || ie.Code != model.ErrCodeBusy || ie.RetryAfterMs <= 0 {
		t.Errorf("busy %#v", err)
	}
	close(block)
	<-done
	logMu.Lock()
	l := logb.String()
	logMu.Unlock()
	for _, bad := range []string{"S3cret!pw", "exch_login", "Login failed"} {
		if strings.Contains(l, bad) {
			t.Errorf("log contains %q:\n%s", bad, l)
		}
	}
	if !strings.Contains(l, "sql_test_steps") || !strings.Contains(l, "login:false:12ms:login_failed") {
		t.Errorf("log %s", l)
	}
}

func nil2(f func(model.SQLCheckStep)) func(model.SQLCheckStep) { return f }

type logFunc func(string, ...interface{})

func (f logFunc) Printf(format string, a ...interface{}) { f(format, a...) }
