package monitor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// fakeIR is an injectable InteractiveFactory recording its calls.
type fakeIR struct {
	mu      sync.Mutex
	opens   []irOpen
	closed  int
	cands   []model.DiscoveryCandidate
	pts     []model.NodePoint
	rows    []model.SampleRow
	err     func(pw string) error
	block   chan struct{} // blocks until closed (or ctx done unless ignoreCtx)
	ignore  bool
	started chan string
	sleep   bool // sleeps opt.Discover.Timeout (the discovery budget)
	query   model.ValueQuery
	limit   int
	mapping model.Mapping
}

type irOpen struct {
	p   model.Profile
	pw  string
	opt sqlcheck.Options
}

type fakeIRReader struct {
	f   *fakeIR
	opt sqlcheck.Options
}

func (f *fakeIR) New(p model.Profile, pw string, opt sqlcheck.Options) (InteractiveReader, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens = append(f.opens, irOpen{p, pw, opt})
	return &fakeIRReader{f: f, opt: opt}, nil
}

func (f *fakeIR) set(fn func(f *fakeIR)) { f.mu.Lock(); fn(f); f.mu.Unlock() }
func (f *fakeIR) n() int                 { f.mu.Lock(); defer f.mu.Unlock(); return len(f.opens) }
func (f *fakeIR) last() irOpen {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.opens) == 0 {
		return irOpen{}
	}
	return f.opens[len(f.opens)-1]
}
func (f *fakeIR) closes() int { f.mu.Lock(); defer f.mu.Unlock(); return f.closed }
func (f *fakeIR) seen() (model.ValueQuery, int, model.Mapping) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.query, f.limit, f.mapping
}

func (r *fakeIRReader) op(ctx context.Context, name string) error {
	f := r.f
	f.mu.Lock()
	block, ignore, started, sleep, errf, pw := f.block, f.ignore, f.started, f.sleep, f.err, f.opens[len(f.opens)-1].pw
	f.mu.Unlock()
	if started != nil {
		started <- name
	}
	if block != nil {
		if ignore {
			<-block
		} else {
			select {
			case <-block:
			case <-ctx.Done():
				return &sqlcheck.Error{Kind: sqlcheck.KindCanceled, Op: name, Err: ctx.Err()}
			}
		}
	}
	if sleep {
		time.Sleep(r.opt.Discover.Timeout)
	}
	if errf != nil {
		return errf(pw)
	}
	return nil
}

func (r *fakeIRReader) Discover(ctx context.Context) ([]model.DiscoveryCandidate, error) {
	if err := r.op(ctx, "discover"); err != nil {
		return nil, err
	}
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	return append([]model.DiscoveryCandidate(nil), r.f.cands...), nil
}

func (r *fakeIRReader) DiscoverByValue(ctx context.Context, q model.ValueQuery) ([]model.DiscoveryCandidate, error) {
	r.f.set(func(f *fakeIR) { f.query = q })
	return r.Discover(ctx)
}

func (r *fakeIRReader) NodePoints(ctx context.Context) ([]model.NodePoint, error) {
	if err := r.op(ctx, "node_points"); err != nil {
		return nil, err
	}
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	return append([]model.NodePoint(nil), r.f.pts...), nil
}

func (r *fakeIRReader) PreviewMapping(ctx context.Context, m model.Mapping, limit int) ([]model.SampleRow, error) {
	r.f.set(func(f *fakeIR) { f.mapping, f.limit = m, limit })
	if err := r.op(ctx, "preview"); err != nil {
		return nil, err
	}
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	return append([]model.SampleRow(nil), r.f.rows...), nil
}

func (r *fakeIRReader) Close() error { r.f.set(func(f *fakeIR) { f.closed++ }); return nil }

type iEnv struct {
	m   *Monitor
	fi  *fakeIR
	clk *fakeClock
	lg  *memLog
}

// newIEnv: saved SQL profiles (password savedPW) and the fake factory;
// interval < 0 disables the rate limit.
func newIEnv(t *testing.T, saved []model.Profile, interval time.Duration, tune func(*Options)) *iEnv {
	t.Helper()
	e := &iEnv{fi: &fakeIR{}, clk: &fakeClock{t: t0}, lg: &memLog{}}
	pws := map[string]string{}
	for _, p := range saved {
		if p.AuthMode == model.AuthSQL {
			pws[p.ID] = savedPW
		}
	}
	opt := baseOpt(newFactory(), NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: saved, Passwords: pws}), e.clk)
	opt.Interactive, opt.Log, opt.CommandInterval = e.fi.New, e.lg, interval
	if tune != nil {
		tune(&opt)
	}
	e.m = New(opt)
	e.m.reloadConfig(true)
	return e
}

func ipcErr(t *testing.T, err error, code, key string) *model.IPCError {
	t.Helper()
	var ie *model.IPCError
	if !errors.As(err, &ie) || ie.Code != code || ie.Key != key {
		t.Fatalf("want %s/%s, got %#v", code, key, err)
	}
	return ie
}

// TestCHK_IPC_08_DiscoverProfileRules — discover uses the test_profile
// rules: the saved profile with its saved password, an unsaved one with
// its own password, the saved password only for the same server / login
// / auth mode, Windows authentication only for the saved profile; unknown
// ID -> not_found. Each request opens its own reader (closed after it)
// with the 90 s discovery budget; the password never reaches the log or
// the error text.
func TestCHK_IPC_08_DiscoverProfileRules(t *testing.T) {
	win := prof("win")
	win.AuthMode = model.AuthWindows
	e := newIEnv(t, []model.Profile{sqlProf("p1"), win}, -1, nil)
	e.fi.set(func(f *fakeIR) {
		f.cands = []model.DiscoveryCandidate{{Mode: model.CandidateSingle, Table: "_Node1", PointCol: "_Description", SendCol: "s"}}
	})
	ctx := context.Background()

	cands, tr, err := e.m.Discover(ctx, model.IPCRequest{ProfileID: "p1"})
	if err != nil || len(cands) != 1 || tr {
		t.Fatalf("saved: %v %v %v", cands, tr, err)
	}
	if o := e.fi.last(); o.p.ID != "p1" || o.pw != savedPW || o.opt.Discover.Timeout != DefaultDiscoverTimeout || e.fi.closes() != 1 {
		t.Fatalf("saved open %+v closes %d", o, e.fi.closes())
	}
	// unsaved with its own password
	other := sqlProf("new")
	other.Server = "other"
	if _, _, err := e.m.Discover(ctx, model.IPCRequest{Profile: &other, Password: reqPW}); err != nil || e.fi.last().pw != reqPW {
		t.Fatalf("unsaved: %v %+v", err, e.fi.last())
	}
	// the saved password is not reused for another server
	moved := sqlProf("p1")
	moved.Server = "evil"
	n := e.fi.n()
	_, _, err = e.m.Discover(ctx, model.IPCRequest{Profile: &moved})
	ipcErr(t, err, model.ErrCodeInvalidRequest, model.MsgTestPasswordRequired)
	// ... but for the same connection it is
	same := sqlProf("p1")
	if _, _, err := e.m.Discover(ctx, model.IPCRequest{Profile: &same}); err != nil || e.fi.last().pw != savedPW {
		t.Fatalf("same connection: %v", err)
	}
	// Windows authentication: saved ok, unsaved refused without a connection
	if _, _, err := e.m.Discover(ctx, model.IPCRequest{ProfileID: "win"}); err != nil {
		t.Fatal(err)
	}
	wu := win
	wu.ID, wu.Server = "", "elsewhere"
	n = e.fi.n()
	_, _, err = e.m.Discover(ctx, model.IPCRequest{Profile: &wu})
	ipcErr(t, err, model.ErrCodeInvalidRequest, model.MsgTestWindowsUnsaved)
	if e.fi.n() != n {
		t.Fatal("Windows authentication of an unsaved profile connected")
	}
	_, _, err = e.m.Discover(ctx, model.IPCRequest{ProfileID: "nope"})
	ipcErr(t, err, model.ErrCodeNotFound, model.MsgMonitorUnknownProfile)

	// a driver error quoting the password: scrubbed in the reply and log
	e.fi.set(func(f *fakeIR) {
		f.err = func(pw string) error {
			return &sqlcheck.Error{Kind: sqlcheck.KindQuery, Op: "catalog", Err: fmt.Errorf("bad %s", pw)}
		}
	})
	_, _, err = e.m.Discover(ctx, model.IPCRequest{ProfileID: "p1"})
	ie := ipcErr(t, err, model.ErrCodeFailed, model.MsgMonitorQueryError)
	if strings.Contains(ie.Message, savedPW) {
		t.Fatalf("password in the error: %q", ie.Message)
	}
	e.lg.mu.Lock()
	defer e.lg.mu.Unlock()
	for _, l := range e.lg.lines {
		if strings.Contains(l, savedPW) || strings.Contains(l, reqPW) {
			t.Fatalf("password in the log: %q", l)
		}
	}
	if len(e.lg.lines) == 0 || !strings.Contains(strings.Join(e.lg.lines, "\n"), "discover p1") {
		t.Fatalf("not logged: %v", e.lg.lines)
	}
}

// TestCHK_IPC_08_RateLimit — the rate limit comes before the profile
// checks (an unknown profile is rate_limited, not not_found);
// discover and discover_by_value share the key "discover" (10 s);
// list_node_points and check_mapping have their own keys.
func TestCHK_IPC_08_RateLimit(t *testing.T) {
	e := newIEnv(t, []model.Profile{sqlProf("p1")}, 10*time.Second, nil)
	ctx := context.Background()
	if _, _, err := e.m.Discover(ctx, model.IPCRequest{ProfileID: "p1"}); err != nil {
		t.Fatal(err)
	}
	_, _, err := e.m.DiscoverByValue(ctx, model.IPCRequest{ProfileID: "nope", Query: &model.ValueQuery{PointName: "A"}})
	ipcErr(t, err, model.ErrCodeRateLimited, model.MsgMonitorRateLimited)
	if _, _, err := e.m.ListNodePoints(ctx, model.IPCRequest{ProfileID: "p1"}); err != nil {
		t.Fatalf("node points after discover: %v", err)
	}
	mp := model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	if _, err := e.m.CheckMapping(ctx, model.IPCRequest{ProfileID: "p1", Mapping: &mp}); err != nil {
		t.Fatalf("check_mapping after discover: %v", err)
	}
	_, err = e.m.CheckMapping(ctx, model.IPCRequest{ProfileID: "p1", Mapping: &mp})
	ipcErr(t, err, model.ErrCodeRateLimited, model.MsgMonitorRateLimited)
	e.clk.Add(10 * time.Second)
	if _, _, err := e.m.DiscoverByValue(ctx, model.IPCRequest{ProfileID: "p1", Query: &model.ValueQuery{PointName: "A"}}); err != nil {
		t.Fatalf("after 10 s: %v", err)
	}
}

// waitStarted waits for n reader operations to start.
func waitStarted(t *testing.T, ch chan string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("operation did not start")
		}
	}
}

// TestCHK_IPC_08_Busy — one heavy operation (discover /
// discover_by_value) per service, a pool of two light ones
// (list_node_points / check_mapping), one operation per profile (Вика
// 06.10): monitor.discover_busy / monitor.discover_profile_busy (code
// busy); the slots free when the operations end.
func TestCHK_IPC_08_Busy(t *testing.T) {
	var saved []model.Profile
	for _, id := range []string{"p1", "p2", "p3", "p4"} {
		saved = append(saved, sqlProf(id))
	}
	e := newIEnv(t, saved, -1, nil)
	block := make(chan struct{})
	started := make(chan string, 8)
	e.fi.set(func(f *fakeIR) { f.block, f.started = block, started })
	ctx := context.Background()
	mp := model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	goRun := func(f func() error) {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- f() }()
	}
	goRun(func() error { _, _, err := e.m.Discover(ctx, model.IPCRequest{ProfileID: "p1"}); return err })
	waitStarted(t, started, 1)
	// heavy slot taken
	_, _, err := e.m.DiscoverByValue(ctx, model.IPCRequest{ProfileID: "p2", Query: &model.ValueQuery{PointName: "A"}})
	ipcErr(t, err, model.ErrCodeBusy, model.MsgDiscoverBusy)
	// same profile, other kind: one per profile
	_, err = e.m.CheckMapping(ctx, model.IPCRequest{ProfileID: "p1", Mapping: &mp})
	ipcErr(t, err, model.ErrCodeBusy, model.MsgDiscoverProfileBusy)
	// the unsaved edit of p1 (same id) counts as p1
	p1 := sqlProf("p1")
	_, _, err = e.m.ListNodePoints(ctx, model.IPCRequest{Profile: &p1})
	ipcErr(t, err, model.ErrCodeBusy, model.MsgDiscoverProfileBusy)
	// light pool of two
	goRun(func() error { _, _, err := e.m.ListNodePoints(ctx, model.IPCRequest{ProfileID: "p2"}); return err })
	goRun(func() error {
		_, err := e.m.CheckMapping(ctx, model.IPCRequest{ProfileID: "p3", Mapping: &mp})
		return err
	})
	waitStarted(t, started, 2)
	if h, l, a := e.m.explorer.Busy(); h != 1 || l != 2 || a != 0 {
		t.Fatalf("busy %d %d %d", h, l, a)
	}
	_, _, err = e.m.ListNodePoints(ctx, model.IPCRequest{ProfileID: "p4"})
	ipcErr(t, err, model.ErrCodeBusy, model.MsgDiscoverBusy)
	close(block)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if h, l, a := e.m.explorer.Busy(); h != 0 || l != 0 || a != 0 {
		t.Fatalf("slots not freed: %d %d %d", h, l, a)
	}
	e.fi.set(func(f *fakeIR) { f.started = nil })
	if _, _, err := e.m.ListNodePoints(ctx, model.IPCRequest{ProfileID: "p4"}); err != nil {
		t.Fatal(err)
	}
	if e.fi.closes() != e.fi.n() {
		t.Fatalf("readers %d closed %d", e.fi.n(), e.fi.closes())
	}
}

// TestCHK_IPC_08_InteractiveKey — the per-profile key: saved ID, the
// unsaved profile's ID, else server (ASCII case-insensitive) + port +
// database.
func TestCHK_IPC_08_InteractiveKey(t *testing.T) {
	a := model.Profile{Server: ` SRV\Inst `, Port: 0, Database: "Base"}
	b := model.Profile{Server: `srv\inst`, Database: "base "}
	c := model.Profile{Server: `srv\inst`, Port: 1500, Database: "base"}
	ka, kb, kc := interactiveKey(model.IPCRequest{Profile: &a}, a), interactiveKey(model.IPCRequest{Profile: &b}, b), interactiveKey(model.IPCRequest{Profile: &c}, c)
	if ka != kb || ka == kc {
		t.Fatalf("keys %q %q %q", ka, kb, kc)
	}
	d := model.Profile{ID: "p1", Server: "x"}
	if interactiveKey(model.IPCRequest{Profile: &d}, d) != "id:p1" || interactiveKey(model.IPCRequest{ProfileID: "p1"}, model.Profile{ID: "p1"}) != "id:p1" {
		t.Fatal("id keys")
	}
}

// TestCHK_IPC_08_CancelAndAbandoned — a canceled request (cancel {ref} /
// client gone: ctx) returns at once with a canceled error and its reader
// is closed; a driver ignoring its context is abandoned at the hard limit
// (failed + monitor.discover_timeout): at most two abandoned, then busy,
// until the driver returns.
func TestCHK_IPC_08_CancelAndAbandoned(t *testing.T) {
	e := newIEnv(t, []model.Profile{sqlProf("p1"), sqlProf("p2"), sqlProf("p3")}, -1, func(o *Options) {
		o.InteractiveLimit = 50 * time.Millisecond
	})
	block := make(chan struct{})
	started := make(chan string, 8)
	e.fi.set(func(f *fakeIR) { f.block, f.started = block, started })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, _, err := e.m.Discover(ctx, model.IPCRequest{ProfileID: "p1"}); done <- err }()
	waitStarted(t, started, 1)
	cancel()
	select {
	case err := <-done:
		ipcErr(t, err, model.ErrCodeCanceled, model.MsgMonitorCanceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not end the request")
	}
	deadline := time.Now().Add(5 * time.Second)
	for e.fi.closes() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if e.fi.closes() != 1 {
		t.Fatal("reader not closed after cancel")
	}
	// a driver that ignores its context: abandoned at the hard limit
	e.fi.set(func(f *fakeIR) { f.ignore = true })
	_, _, err := e.m.Discover(context.Background(), model.IPCRequest{ProfileID: "p1"})
	ipcErr(t, err, model.ErrCodeFailed, model.MsgDiscoverTimeout)
	mp := model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	res, err := e.m.CheckMapping(context.Background(), model.IPCRequest{ProfileID: "p2", Mapping: &mp})
	if err != nil || res.OK || res.ErrorKey != model.MsgDiscoverTimeout {
		t.Fatalf("check_mapping at the hard limit: %+v %v", res, err)
	}
	waitStarted(t, started, 2)
	if h, l, a := e.m.explorer.Busy(); h != 0 || l != 0 || a != 2 {
		t.Fatalf("busy %d %d %d", h, l, a)
	}
	_, _, err = e.m.ListNodePoints(context.Background(), model.IPCRequest{ProfileID: "p3"})
	ipcErr(t, err, model.ErrCodeBusy, model.MsgDiscoverBusy)
	close(block) // the drivers return
	for {
		if _, _, a := e.m.explorer.Busy(); a == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("abandoned not released")
		}
		time.Sleep(time.Millisecond)
	}
	e.fi.set(func(f *fakeIR) { f.started = nil })
	if _, _, err := e.m.ListNodePoints(context.Background(), model.IPCRequest{ProfileID: "p3"}); err != nil {
		t.Fatal(err)
	}
}

// TestCHK_IPC_08_TruncatedByBudget — when the discovery budget runs out
// the partial result is a result with truncated (not an error): also with
// no candidate at all, and when a candidate carries HintTruncated.
func TestCHK_IPC_08_TruncatedByBudget(t *testing.T) {
	e := newIEnv(t, []model.Profile{sqlProf("p1")}, -1, func(o *Options) { o.DiscoverTimeout = 40 * time.Millisecond })
	ctx := context.Background()
	e.fi.set(func(f *fakeIR) { f.sleep = true })
	cands, tr, err := e.m.Discover(ctx, model.IPCRequest{ProfileID: "p1"})
	if err != nil || !tr || len(cands) != 0 || e.fi.last().opt.Discover.Timeout != 40*time.Millisecond {
		t.Fatalf("budget: %v %v %v", cands, tr, err)
	}
	e.fi.set(func(f *fakeIR) {
		f.sleep = false
		f.cands = []model.DiscoveryCandidate{{Table: "_Node1", Hints: []string{model.HintTruncated}}}
	})
	if _, tr, err := e.m.DiscoverByValue(ctx, model.IPCRequest{ProfileID: "p1", Query: &model.ValueQuery{PointName: "A"}}); err != nil || !tr {
		t.Fatalf("hint: %v %v", tr, err)
	}
	e.fi.set(func(f *fakeIR) { f.cands[0].Hints = nil })
	if _, tr, err := e.m.Discover(ctx, model.IPCRequest{ProfileID: "p1"}); err != nil || tr {
		t.Fatalf("complete: %v %v", tr, err)
	}
}

// TestCHK_IPC_08_ByValueAndNodePoints — discover_by_value without a point
// name (and ref) is invalid_request + monitor.discover_point_name_empty
// (errorArgs [query.pointName, validation.required]) without a
// connection; the query reaches the reader; list_node_points: limit cuts
// (truncated), long names are cut to 256 characters, a bad limit is
// invalid_request + validation.range.
func TestCHK_IPC_08_ByValueAndNodePoints(t *testing.T) {
	e := newIEnv(t, []model.Profile{sqlProf("p1")}, -1, nil)
	ctx := context.Background()
	for _, q := range []*model.ValueQuery{nil, {PointName: "  "}} {
		_, _, err := e.m.DiscoverByValue(ctx, model.IPCRequest{ProfileID: "p1", Query: q})
		ie := ipcErr(t, err, model.ErrCodeInvalidRequest, model.MsgDiscoverPointNameEmpty)
		if strings.Join(ie.Args, ",") != "query.pointName,validation.required" {
			t.Fatalf("args %v", ie.Args)
		}
	}
	if e.fi.n() != 0 {
		t.Fatal("connected for an empty point name")
	}
	at := t0
	q := model.ValueQuery{PointName: "Склад", ApproxTime: &at, ToleranceMin: 30}
	if _, _, err := e.m.DiscoverByValue(ctx, model.IPCRequest{ProfileID: "p1", Query: &q}); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := e.fi.seen(); got.PointName != "Склад" || got.ToleranceMin != 30 {
		t.Fatalf("query: %+v", got)
	}
	if _, _, err := e.m.DiscoverByValue(ctx, model.IPCRequest{ProfileID: "p1", Query: &model.ValueQuery{PointRef: "0x0102"}}); err != nil {
		t.Fatalf("ref only: %v", err)
	}
	long := strings.Repeat("я", model.MaxSampleCellRunes+50)
	e.fi.set(func(f *fakeIR) {
		f.pts = []model.NodePoint{{PlanTable: "_Node1", Name: long, Code: "1"}, {PlanTable: "_Node1", Name: "B"}, {PlanTable: "_Node2", Name: "C"}}
	})
	pts, tr, err := e.m.ListNodePoints(ctx, model.IPCRequest{ProfileID: "p1"})
	if err != nil || tr || len(pts) != 3 || utf8.RuneCountInString(pts[0].Name) != model.MaxSampleCellRunes {
		t.Fatalf("points: %d %v %v", len(pts), tr, err)
	}
	if pts, tr, err = e.m.ListNodePoints(ctx, model.IPCRequest{ProfileID: "p1", Limit: 2}); err != nil || !tr || len(pts) != 2 {
		t.Fatalf("limit 2: %d %v %v", len(pts), tr, err)
	}
	for _, l := range []int{-1, model.MaxNodePointsLimit + 1} {
		_, _, err := e.m.ListNodePoints(ctx, model.IPCRequest{ProfileID: "p1", Limit: l})
		if ie := ipcErr(t, err, model.ErrCodeInvalidRequest, model.MsgMonitorBadRequest); ie.Args[0] != "limit" || ie.Args[1] != model.ValRange {
			t.Fatalf("limit %d args %v", l, ie.Args)
		}
	}
}

// TestCHK_IPC_09_CheckMapping — check_mapping: request errors are
// protocol errors (no mapping, limit outside 1..20); limit 0 = 5; a
// failed check is ok:false with errorKey monitor.discover_mapping_invalid
// and errorArgs [mapping.<field>, validation.* reason, params...]
// (Mapping.Validate, no table / SQL, ValidateManualSQL,
// Compat2008Violations — the last two without a connection; violations
// listed), database errors from PreviewMapping (field errors with the
// mapping. prefix, query errors, the profile budget); ok:true with the
// preview rows (at most limit).
func TestCHK_IPC_09_CheckMapping(t *testing.T) {
	e := newIEnv(t, []model.Profile{sqlProf("p1")}, -1, nil)
	ctx := context.Background()
	check := func(m *model.Mapping, limit int) (model.MappingCheck, error) {
		return e.m.CheckMapping(ctx, model.IPCRequest{ProfileID: "p1", Mapping: m, Limit: limit})
	}
	_, err := check(nil, 0)
	ipcErr(t, err, model.ErrCodeInvalidRequest, model.MsgMonitorBadRequest)
	good := model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	for _, l := range []int{-1, 21, 100} {
		_, err := check(&good, l)
		ie := ipcErr(t, err, model.ErrCodeInvalidRequest, model.MsgMonitorBadRequest)
		if strings.Join(ie.Args, ",") != fmt.Sprintf("limit,validation.range,1,20,%d", l) {
			t.Fatalf("limit %d: %v", l, ie.Args)
		}
	}
	rows := make([]model.SampleRow, 30)
	for i := range rows {
		rows[i] = model.SampleRow{Point: fmt.Sprintf("P%d", i)}
	}
	e.fi.set(func(f *fakeIR) { f.rows = rows[:5] })
	res, err := check(&good, 0)
	_, gotLimit, gotMapping := e.fi.seen()
	if err != nil || !res.OK || len(res.SampleRows) != 5 || gotLimit != model.DefaultCheckMappingLimit || gotMapping.Table != "_Node1" {
		t.Fatalf("default limit: %+v %v (limit %d)", res, err, gotLimit)
	}
	e.fi.set(func(f *fakeIR) { f.rows = rows })
	res, err = check(&good, 20)
	if _, gotLimit, _ = e.fi.seen(); err != nil || !res.OK || len(res.SampleRows) != 20 || gotLimit != 20 {
		t.Fatalf("limit 20: %d %v", len(res.SampleRows), err)
	}
	if res, _ = check(&good, 1); len(res.SampleRows) != 1 {
		t.Fatalf("limit 1: %d", len(res.SampleRows))
	}

	n := e.fi.n()
	failed := func(name string, m model.Mapping, args string) model.MappingCheck {
		t.Helper()
		res, err := check(&m, 0)
		if err != nil || res.OK || res.ErrorKey != model.MsgDiscoverMappingInvalid || !strings.HasPrefix(strings.Join(res.ErrorArgs, ","), args) || res.Error == "" {
			t.Fatalf("%s: %+v %v", name, res, err)
		}
		return res
	}
	failed("mode", model.Mapping{Mode: "bogus"}, "mapping.mode,validation.enum,bogus")
	failed("empty", model.Mapping{}, "mapping.table,validation.required")
	failed("forbidden", model.Mapping{ManualSQL: "SELECT a, b, c FROM t; DELETE FROM t"}, "mapping.manualSql,validation.sql_manual")
	failed("delete", model.Mapping{ManualSQL: "SELECT a, b, c FROM t WHERE 1 = 1 DELETE FROM t"}, "mapping.manualSql,validation.sql_manual_forbidden")
	res = failed("2012+", model.Mapping{ManualSQL: "SELECT name, TRY_CONVERT(datetime, s), IIF(r > 0, r, NULL) FROM t"},
		"mapping.manualSql,validation.sql_manual_compat2008")
	if len(res.Violations) < 2 {
		t.Fatalf("violations %v", res.Violations)
	}
	if e.fi.n() != n {
		t.Fatal("connected although the mapping failed the static checks")
	}
	// manual SQL that passes: previewed
	e.fi.set(func(f *fakeIR) { f.rows = rows[:3] })
	if res, err = check(&model.Mapping{Mode: model.MappingManual, ManualSQL: "SELECT name, s, r FROM t"}, 3); err != nil || !res.OK || len(res.SampleRows) != 3 {
		t.Fatalf("manual ok: %+v %v", res, err)
	}
	// database errors
	setErr := func(err error) { e.fi.set(func(f *fakeIR) { f.err = func(string) error { return err } }) }
	setErr(&model.FieldError{Field: "lastSendColumn", Key: model.ValSQLColumnNotFound, Args: []string{"_Node1", "s"}, Msg: "column not found"})
	if res, _ = check(&good, 0); res.OK || res.ErrorKey != model.MsgDiscoverMappingInvalid ||
		strings.Join(res.ErrorArgs, ",") != "mapping.lastSendColumn,validation.sql_column_not_found,_Node1,s" {
		t.Fatalf("field error: %+v", res)
	}
	setErr(&sqlcheck.Error{Kind: sqlcheck.KindQuery, Op: "manual sql", Err: errors.New("Invalid column name 'x'")})
	if res, _ = check(&good, 0); res.OK || res.ErrorKey != model.MsgMonitorQueryError || res.ErrorArgs != nil || !strings.Contains(res.Error, "Invalid column") {
		t.Fatalf("query error: %+v", res)
	}
	setErr(&sqlcheck.Error{Kind: sqlcheck.KindTimeout, Op: "check mapping", Err: context.DeadlineExceeded})
	if res, _ = check(&good, 0); res.OK || res.ErrorKey != model.MsgDiscoverTimeout {
		t.Fatalf("budget: %+v", res)
	}
	setErr(&sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "ping", Err: errors.New("TLS"), Key: model.MsgSQLTLSEnableTLS10})
	if res, _ = check(&good, 0); res.OK || res.ErrorKey != model.MsgSQLTLSEnableTLS10 {
		t.Fatalf("sql key: %+v", res)
	}
}

// TestCHK_IPC_08_ErrorKeys — the reply code / key of reader errors.
func TestCHK_IPC_08_ErrorKeys(t *testing.T) {
	for _, c := range []struct {
		err       error
		code, key string
	}{
		{&sqlcheck.Error{Kind: sqlcheck.KindCanceled, Err: context.Canceled}, model.ErrCodeCanceled, model.MsgMonitorCanceled},
		{&sqlcheck.Error{Kind: sqlcheck.KindTimeout, Err: context.DeadlineExceeded}, model.ErrCodeFailed, model.MsgDiscoverTimeout},
		{&sqlcheck.Error{Kind: sqlcheck.KindConfig, Err: errors.New("x")}, model.ErrCodeInvalidRequest, model.MsgTestConfig},
		{&sqlcheck.Error{Kind: sqlcheck.KindConn, Err: errors.New("x")}, model.ErrCodeFailed, model.MsgMonitorConnError},
		{&sqlcheck.Error{Kind: sqlcheck.KindConn, Err: sqlNumErr{18456}}, model.ErrCodeFailed, model.MsgTestLogin},
		{&sqlcheck.Error{Kind: sqlcheck.KindQuery, Err: errors.New("x")}, model.ErrCodeFailed, model.MsgMonitorQueryError},
		{&sqlcheck.Error{Kind: sqlcheck.KindConn, Key: model.MsgSQLTLSCertificate, Err: errors.New("x")}, model.ErrCodeFailed, model.MsgSQLTLSCertificate},
		{&sqlcheck.Error{Kind: sqlcheck.KindConn, Key: "sql.unknown_key", Err: errors.New("x")}, model.ErrCodeFailed, model.MsgMonitorConnError},
		{context.Canceled, model.ErrCodeCanceled, model.MsgMonitorCanceled},
		{errors.New("boom"), model.ErrCodeFailed, model.MsgDiscoverFailed},
	} {
		code, key := interactiveErrorKey(c.err)
		if code != c.code || key != c.key {
			t.Errorf("%v: %s %s, want %s %s", c.err, code, key, c.code, c.key)
		}
		if !KnownErrorKey(key) {
			t.Errorf("%s has no text in model", key)
		}
	}
	for _, k := range []string{model.MsgDiscoverBusy, model.MsgDiscoverProfileBusy, model.MsgDiscoverTimeout, model.MsgDiscoverFailed,
		model.MsgDiscoverPointNameEmpty, model.MsgDiscoverMappingInvalid} {
		if model.MonitorMessage(k, model.LangRU) == "" || model.MonitorMessage(k, model.LangEN) == "" {
			t.Errorf("%s: no RU/EN text", k)
		}
	}
}
