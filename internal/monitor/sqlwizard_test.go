package monitor

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

type wizReader struct{ n int }

func (wizReader) Discover(context.Context) ([]model.DiscoveryCandidate, error) { return nil, nil }
func (wizReader) DiscoverByValue(context.Context, model.ValueQuery) ([]model.DiscoveryCandidate, error) {
	return nil, nil
}
func (wizReader) NodePoints(context.Context) ([]model.NodePoint, error) { return nil, nil }
func (wizReader) PreviewMapping(context.Context, model.Mapping, int) ([]model.SampleRow, error) {
	return nil, nil
}
func (wizReader) Close() error { return nil }
func (r wizReader) Databases(ctx context.Context, opt sqlcheck.DatabasesOptions) (model.SQLDatabasesResult, error) {
	var res model.SQLDatabasesResult
	for i := 0; i < r.n; i++ {
		res.Databases = append(res.Databases, model.SQLDatabaseInfo{Name: "db" + strconv.Itoa(i), State: "ONLINE"})
	}
	return res, nil
}

func wizBadRequest(t *testing.T, what string, err error, field, key string) {
	t.Helper()
	var ie *model.IPCError
	if !errors.As(err, &ie) || ie.Code != model.ErrCodeInvalidRequest || len(ie.Args) < 2 || ie.Args[0] != field || ie.Args[1] != key {
		t.Errorf("%s: %#v", what, err)
	}
}

// TestCHK_IPC_10_SQLBrowse — host validation (ValidateSQLServerAddr),
// rate limits (broadcast 15 s, host 3 s), cap / truncation, cancel.
func TestCHK_IPC_10_SQLBrowse(t *testing.T) {
	clk := &fakeClock{t: t0}
	m := New(baseOpt(newFactory(), NewMemConfig(Config{Settings: model.DefaultSettings()}), clk))
	var mu sync.Mutex
	var got []model.SQLBrowseRequest
	m.explorer.Browse = func(ctx context.Context, req model.SQLBrowseRequest) (model.SQLBrowseResult, error) {
		mu.Lock()
		got = append(got, req)
		mu.Unlock()
		if req.Host == "slow" {
			<-ctx.Done()
			return model.SQLBrowseResult{}, ctx.Err()
		}
		var res model.SQLBrowseResult
		for i := 0; i < 300; i++ {
			res.Instances = append(res.Instances, model.SQLServerInstance{Host: "h" + strconv.Itoa(i), Instance: "I"})
		}
		return res, nil
	}
	ctx := context.Background()
	for _, h := range []string{"np:srv", "tcp:srv", `\\srv\pipe\sql\query`, "/srv", "admin:srv"} {
		_, err := m.SQLBrowse(ctx, model.IPCRequest{Host: h})
		wizBadRequest(t, h, err, "host", model.ValSQLServerForbidden)
	}
	_, err := m.SQLBrowse(ctx, model.IPCRequest{Host: "bad host!"})
	wizBadRequest(t, "format", err, "host", model.ValSQLBrowseHost)
	res, err := m.SQLBrowse(ctx, model.IPCRequest{Limit: 10})
	if err != nil || len(res.Instances) != 10 || !res.Truncated {
		t.Fatalf("broadcast: %d %v %v", len(res.Instances), res.Truncated, err)
	}
	if got[0].TimeoutMs != model.DefaultSQLBrowseTimeoutMs {
		t.Errorf("default timeout %d", got[0].TimeoutMs)
	}
	limited := func(what string, err error, ms int64) {
		t.Helper()
		var ie *model.IPCError
		if !errors.As(err, &ie) || ie.Code != model.ErrCodeRateLimited || ie.Key != model.MsgBrowseRateLimited || ie.RetryAfterMs != ms {
			t.Errorf("%s: %#v", what, err)
		}
	}
	_, err = m.SQLBrowse(ctx, model.IPCRequest{})
	limited("broadcast again", err, 15000)
	if _, err = m.SQLBrowse(ctx, model.IPCRequest{Host: "srv1", TimeoutMs: 60000}); err != nil {
		t.Fatal(err)
	}
	if got[1].TimeoutMs != model.IPCSQLBrowseMaxTimeoutMs {
		t.Errorf("clamped timeout %d", got[1].TimeoutMs)
	}
	_, err = m.SQLBrowse(ctx, model.IPCRequest{Host: "SRV1"})
	limited("host again", err, 3000)
	if _, err = m.SQLBrowse(ctx, model.IPCRequest{Host: "srv2", TimeoutMs: 1}); err != nil {
		t.Fatal(err)
	}
	if got[2].TimeoutMs != model.IPCSQLBrowseMinTimeoutMs {
		t.Errorf("clamped timeout %d", got[2].TimeoutMs)
	}
	clk.Add(3 * time.Second)
	if _, err = m.SQLBrowse(ctx, model.IPCRequest{Host: "srv1"}); err != nil {
		t.Errorf("host after 3 s: %v", err)
	}
	clk.Add(12 * time.Second)
	if _, err = m.SQLBrowse(ctx, model.IPCRequest{}); err != nil {
		t.Errorf("broadcast after 15 s: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	if _, err = m.SQLBrowse(cctx, model.IPCRequest{Host: "slow"}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
	if model.SQLBrowseBroadcastInterval != 15*time.Second || model.SQLBrowseHostInterval != 3*time.Second {
		t.Error("intervals")
	}
}

// TestCHK_IPC_10_SQLDatabases — server validation, the saved password is
// used only for the same profile and target (never after a server / port
// change), a new password is used as given, never logged; cap /
// truncation.
func TestCHK_IPC_10_SQLDatabases(t *testing.T) {
	e := newCfgEnv(t)
	if _, err := e.m.SaveProfile(sqlProf6("s1"), "Pw-Saved!", false, e.rev(), "a"); err != nil {
		t.Fatal(err)
	}
	e.m.opt.CommandInterval = -1
	var mu sync.Mutex
	var pws []string
	e.m.explorer.New = func(p model.Profile, pw string, opt sqlcheck.Options) (InteractiveReader, error) {
		mu.Lock()
		pws = append(pws, pw)
		mu.Unlock()
		return wizReader{n: 50}, nil
	}
	ctx := context.Background()
	p := sqlProf6("s1")
	p.Database = ""
	res, err := e.m.SQLDatabases(ctx, model.IPCRequest{Profile: &p, Limit: 5})
	if err != nil || len(res.Databases) != 5 || !res.Truncated || len(pws) != 1 || pws[0] != "Pw-Saved!" {
		t.Fatalf("same target: %+v %v %v", res, err, pws)
	}
	q := p
	q.Server = `other\INST`
	_, err = e.m.SQLDatabases(ctx, model.IPCRequest{Profile: &q})
	var ie *model.IPCError
	if !errors.As(err, &ie) || ie.Key != model.MsgPasswordTargetChange || len(pws) != 1 {
		t.Fatalf("server change reused the password: %v %v", err, pws)
	}
	q = p
	q.Port = 1500
	if _, err = e.m.SQLDatabases(ctx, model.IPCRequest{Profile: &q}); !errors.As(err, &ie) || ie.Key != model.MsgPasswordTargetChange || len(pws) != 1 {
		t.Fatalf("port change reused the password: %v %v", err, pws)
	}
	q.Server = `other\INST`
	if _, err = e.m.SQLDatabases(ctx, model.IPCRequest{Profile: &q, Password: "Pw-New!"}); err != nil || pws[len(pws)-1] != "Pw-New!" {
		t.Fatalf("new password: %v %v", err, pws)
	}
	n := len(pws)
	for _, s := range []string{"np:srv", "tcp:srv,1433", `\\srv\pipe\x`, "lpc:."} {
		b := p
		b.Server = s
		_, err = e.m.SQLDatabases(ctx, model.IPCRequest{Profile: &b, Password: "x"})
		wizBadRequest(t, s, err, "server", model.ValSQLServerForbidden)
	}
	if _, err = e.m.SQLDatabases(ctx, model.IPCRequest{}); err == nil {
		t.Error("no profile accepted")
	}
	if len(pws) != n {
		t.Error("a rejected request connected")
	}
	for _, pw := range []string{"Pw-Saved!", "Pw-New!"} {
		if e.lg.has(pw) {
			t.Errorf("password %q in the log", pw)
		}
	}
}
