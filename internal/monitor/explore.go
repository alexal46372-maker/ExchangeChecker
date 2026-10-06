package monitor

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// Interactive SQL commands of IPC (phase 6, G2/G3; CHK-IPC-08 discovery,
// CHK-IPC-09 check_mapping): discover, discover_by_value,
// list_node_points, check_mapping. Admin only (the IPC server checks the
// client before calling); every request goes through the rate limit, then
// the profile / password rules of test_profile, then the operation limits
// of the Explorer. Each request opens its OWN interactive reader
// (sqlcheck.NewInteractiveReader) and closes it when the driver returns:
// the reader's guard closes the session when the request is canceled or
// its budget runs out, so a canceled request does not leave a query on
// the server. The service composes no SQL: everything goes through
// sqlcheck (read-only, SQL Server 2008 compatible queries).

// InteractiveReader is what the interactive commands need from a reader
// (*sqlcheck.InteractiveReader; tests pass fakes).
type InteractiveReader interface {
	Discover(ctx context.Context) ([]model.DiscoveryCandidate, error)
	DiscoverByValue(ctx context.Context, q model.ValueQuery) ([]model.DiscoveryCandidate, error)
	NodePoints(ctx context.Context) ([]model.NodePoint, error)
	PreviewMapping(ctx context.Context, m model.Mapping, limit int) ([]model.SampleRow, error)
	Close() error
}

// InteractiveFactory opens the interactive reader of one request.
type InteractiveFactory func(p model.Profile, password string, opt sqlcheck.Options) (InteractiveReader, error)

// SQLInteractive is the production InteractiveFactory
// (sqlcheck.NewInteractiveReader: lazy, no network I/O until the first
// call).
func SQLInteractive(p model.Profile, password string, opt sqlcheck.Options) (InteractiveReader, error) {
	r, err := sqlcheck.NewInteractiveReader(p, password, opt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Defaults of the interactive commands.
const (
	// DefaultDiscoverTimeout is the time budget of discover /
	// discover_by_value (sqlcheck.DiscoverOptions.Timeout): when it runs
	// out the partial result is returned with truncated (not an error).
	DefaultDiscoverTimeout = 90 * time.Second
	// DefaultInteractiveLimit is the hard service limit of one
	// interactive request: the reply (failed + monitor.discover_timeout)
	// is sent at the latest then, even if the driver ignores its context.
	DefaultInteractiveLimit = 100 * time.Second
	// DefaultInteractiveHeavy: discover / discover_by_value at a time
	// (one per service).
	DefaultInteractiveHeavy = 1
	// DefaultInteractiveLight: list_node_points / check_mapping at a time
	// (a shared pool of two).
	DefaultInteractiveLight = 2
	// DefaultInteractiveAbandoned: requests whose reply was sent (canceled
	// / service limit) while the driver had not returned yet; while that
	// many are pending, further requests are busy.
	DefaultInteractiveAbandoned = 2
)

// Rate limit keys of the interactive commands (IPCCommandInterval each):
// discover and discover_by_value share "discover" (contract §6);
// list_node_points and check_mapping have their own keys, so the usual
// sequence "points -> search by value -> check" is not refused.
const (
	cmdKeyDiscover     = "discover"
	cmdKeyNodePoints   = "node_points"
	cmdKeyCheckMapping = "check_mapping"
)

var (
	// ErrDiscoverBusy: the heavy slot (1) or the light pool (2) is taken,
	// or too many abandoned requests are pending (IPC code busy).
	ErrDiscoverBusy = &model.IPCError{Code: model.ErrCodeBusy, Key: model.MsgDiscoverBusy, Message: "monitor: interactive operation busy"}
	// ErrDiscoverProfileBusy: an interactive request of the same profile
	// is running (one per profile, Вика 06.10).
	ErrDiscoverProfileBusy = &model.IPCError{Code: model.ErrCodeBusy, Key: model.MsgDiscoverProfileBusy, Message: "monitor: an operation of this profile is running"}
	// errDiscoverTimeout: the service limit ran out.
	errDiscoverTimeout = &model.IPCError{Code: model.ErrCodeFailed, Key: model.MsgDiscoverTimeout, Message: "monitor: interactive operation timed out"}
)

// Explorer runs the interactive requests under the operation limits: one
// heavy (discover / discover_by_value) per service, a pool of MaxLight
// light ones (list_node_points / check_mapping), one per profile (any
// kind), at most MaxAbandoned abandoned ones; each on its own goroutine
// with the hard limit HardTimeout.
type Explorer struct {
	New             InteractiveFactory
	DiscoverTimeout time.Duration
	HardTimeout     time.Duration
	MaxHeavy        int
	MaxLight        int
	MaxAbandoned    int
	// Now is the clock of the discovery budget and of sqlcheck.Options.
	Now func() time.Time
	// WithTimeout derives the context with the hard limit (tests).
	WithTimeout func(parent context.Context, d time.Duration) (context.Context, context.CancelFunc)

	mu        sync.Mutex
	heavy     int
	light     int
	abandoned int
	profiles  map[string]bool
}

func (x *Explorer) defaults() {
	if x.New == nil {
		x.New = SQLInteractive
	}
	if x.DiscoverTimeout <= 0 {
		x.DiscoverTimeout = DefaultDiscoverTimeout
	}
	if x.HardTimeout <= 0 {
		x.HardTimeout = DefaultInteractiveLimit
	}
	if x.MaxHeavy <= 0 {
		x.MaxHeavy = DefaultInteractiveHeavy
	}
	if x.MaxLight <= 0 {
		x.MaxLight = DefaultInteractiveLight
	}
	if x.MaxAbandoned <= 0 {
		x.MaxAbandoned = DefaultInteractiveAbandoned
	}
	if x.Now == nil {
		x.Now = time.Now
	}
	if x.WithTimeout == nil {
		x.WithTimeout = context.WithTimeout
	}
	if x.profiles == nil {
		x.profiles = map[string]bool{}
	}
}

// Busy reports the running heavy / light and the abandoned requests
// (tests).
func (x *Explorer) Busy() (heavy, light, abandoned int) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.heavy, x.light, x.abandoned
}

// exploreRun runs f with a fresh reader of p (closed when f returns) on
// its own goroutine. Busy errors come before anything runs. The reply
// comes when f returns, when ctx ends (canceled: the context error) or at
// the hard limit (errDiscoverTimeout); in the last two cases a goroutine
// still in the driver is counted as abandoned until it returns.
//
// The goroutine sends at most once and never blocks (Вика 06.10): the
// caller may have left on cctx.Done() with the result already in ch and
// a panic in the deferred Close must not send again — a blocked
// goroutine would keep its abandoned slot forever. Whatever the outcome
// (result, error, panic in f or in Close) the abandoned count is
// decremented exactly once; the heavy/light/profile slots are released
// by the caller (end) on every path.
func exploreRun[T any](x *Explorer, ctx context.Context, heavy bool, key string, p model.Profile, password string,
	f func(ctx context.Context, r InteractiveReader) (T, error)) (T, error) {
	var zero T
	x.mu.Lock()
	x.defaults()
	switch {
	case x.profiles[key]:
		x.mu.Unlock()
		return zero, ErrDiscoverProfileBusy
	case x.abandoned >= x.MaxAbandoned, heavy && x.heavy >= x.MaxHeavy, !heavy && x.light >= x.MaxLight:
		x.mu.Unlock()
		return zero, ErrDiscoverBusy
	}
	if heavy {
		x.heavy++
	} else {
		x.light++
	}
	x.profiles[key] = true
	newReader, limit, withTimeout := x.New, x.HardTimeout, x.WithTimeout
	opt := sqlcheck.Options{Discover: sqlcheck.DiscoverOptions{Timeout: x.DiscoverTimeout}, Now: x.Now}
	x.mu.Unlock()

	cctx, cancel := withTimeout(ctx, limit)
	defer cancel()
	type out struct {
		v   T
		err error
	}
	ch := make(chan out, 1)
	run := &testRun{}
	go func() {
		sent := false
		send := func(o out) { // once, non-blocking (the caller reads at most one)
			if sent {
				return
			}
			sent = true
			select {
			case ch <- o:
			default:
			}
		}
		defer func() {
			if r := recover(); r != nil {
				send(out{err: fmt.Errorf("internal error: %v", r)})
			}
			x.mu.Lock()
			run.finished = true
			if run.abandoned {
				run.abandoned = false
				x.abandoned--
			}
			x.mu.Unlock()
		}()
		r, err := newReader(p, password, opt)
		if err != nil {
			send(out{err: err})
			return
		}
		defer r.Close()
		v, err := f(cctx, r)
		send(out{v, err})
	}()
	end := func(abandon bool) {
		x.mu.Lock()
		if heavy {
			x.heavy--
		} else {
			x.light--
		}
		delete(x.profiles, key)
		if abandon && !run.finished {
			run.abandoned = true
			x.abandoned++
		}
		x.mu.Unlock()
	}
	select {
	case o := <-ch:
		end(false)
		if o.err != nil && ctx.Err() == nil && cctx.Err() != nil {
			return o.v, errDiscoverTimeout // the reader saw the hard limit
		}
		return o.v, o.err
	case <-cctx.Done():
		end(true)
	}
	if err := ctx.Err(); err != nil {
		return zero, err // canceled / client gone / service stop
	}
	return zero, errDiscoverTimeout
}

// interactiveKey is the per-profile key of a request: the saved profile's
// ID, else the unsaved profile's ID when it has one (the tray edits an
// existing profile), else server (host\instance, ASCII lower case) +
// port + database: two unsaved profiles of the same database share it.
func interactiveKey(req model.IPCRequest, p model.Profile) string {
	if req.Profile == nil {
		return "id:" + req.ProfileID
	}
	if id := strings.TrimSpace(p.ID); id != "" {
		return "id:" + id
	}
	return "conn:" + strings.ToLower(strings.TrimSpace(p.Server)) + "," + strconv.Itoa(p.Port) + "/" + strings.ToLower(strings.TrimSpace(p.Database))
}

// interactiveBegin applies the rate limit (FIRST, as test_profile: also a
// request answered without a connection uses the slot) and resolves the
// profile and password with the test_profile rules.
func (m *Monitor) interactiveBegin(rateKey string, req model.IPCRequest) (model.Profile, string, string, error) {
	m.cmdMu.Lock()
	ok := m.allowLocked(rateKey)
	m.cmdMu.Unlock()
	if !ok {
		return model.Profile{}, "", "", ErrRateLimited
	}
	p, pw, res, err := m.resolveTest(req)
	if err != nil {
		return p, "", "", err
	}
	if res != nil { // Windows authentication of an unsaved profile
		return p, "", "", &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: res.ErrorKey, Message: res.Error}
	}
	return p, pw, interactiveKey(req, p), nil
}

// interactiveErrorKey maps a reader error to (code, key): the sql.* key
// of the error when model knows it, the test_profile category of a
// connection error, monitor.query_error, monitor.discover_timeout ...
func interactiveErrorKey(err error) (code, key string) {
	var se *sqlcheck.Error
	if errors.As(err, &se) && KnownErrorKey(se.Key) {
		return model.ErrCodeFailed, se.Key
	}
	switch sqlcheck.KindOf(err) {
	case sqlcheck.KindCanceled:
		return model.ErrCodeCanceled, model.MsgMonitorCanceled
	case sqlcheck.KindTimeout:
		return model.ErrCodeFailed, model.MsgDiscoverTimeout
	case sqlcheck.KindConfig:
		return model.ErrCodeInvalidRequest, model.MsgTestConfig
	case sqlcheck.KindConn:
		if cat := sqlcheck.ConnCategory(err); cat != "" && cat != model.TestOther && cat != model.TestOK {
			return model.ErrCodeFailed, model.TestCategoryKey(cat)
		}
		return model.ErrCodeFailed, model.MsgMonitorConnError
	case sqlcheck.KindQuery:
		return model.ErrCodeFailed, model.MsgMonitorQueryError
	case sqlcheck.KindMapping:
		return model.ErrCodeFailed, model.MsgMonitorMapping
	}
	switch {
	case errors.Is(err, context.Canceled):
		return model.ErrCodeCanceled, model.MsgMonitorCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return model.ErrCodeFailed, model.MsgDiscoverTimeout
	}
	return model.ErrCodeFailed, model.MsgDiscoverFailed
}

// interactiveError turns err into the IPC error of the reply (an
// *model.IPCError is kept); the password is scrubbed from the text.
func interactiveError(err error, password string) error {
	var ie *model.IPCError
	if errors.As(err, &ie) {
		return err
	}
	code, key := interactiveErrorKey(err)
	return &model.IPCError{Code: code, Key: key, Message: scrub(err.Error(), password)}
}

// logInteractive logs the outcome (never the password, the manual SQL or
// point names).
func (m *Monitor) logInteractive(cmd string, p model.Profile, start time.Time, err error, password, format string, args ...interface{}) {
	ms := m.explorer.Now().Sub(start).Milliseconds()
	if err != nil {
		m.opt.Log.Printf("%s %s (%s/%s): %s in %d ms", cmd, p.ID, p.Server, p.Database, scrub(err.Error(), password), ms)
		return
	}
	m.opt.Log.Printf("%s %s (%s/%s): "+format+" in %d ms", append(append([]interface{}{cmd, p.ID, p.Server, p.Database}, args...), ms)...)
}

// discoveryTruncated: model.TruncatedBudget when the budget ran out (a
// candidate carries HintTruncated, or the call took the whole budget —
// also with no candidate at all), else "".
func discoveryTruncated(cands []model.DiscoveryCandidate, elapsed, budget time.Duration) string {
	if elapsed >= budget {
		return model.TruncatedBudget
	}
	for _, c := range cands {
		if c.HasHint(model.HintTruncated) {
			return model.TruncatedBudget
		}
	}
	return ""
}

// Discover runs IPC "discover" (CHK-IPC-08): candidates (best first) and
// the truncated reason model.TruncatedBudget when the 90 s budget ran out
// (partial result, not an error); "" = complete.
func (m *Monitor) Discover(ctx context.Context, req model.IPCRequest) ([]model.DiscoveryCandidate, string, error) {
	return m.discover(ctx, model.CmdDiscover, req, nil)
}

// DiscoverByValue runs IPC "discover_by_value" (CHK-IPC-08); an empty
// point name (and no point ref) is invalid_request +
// monitor.discover_point_name_empty, errorArgs ["query.pointName",
// "validation.required"].
func (m *Monitor) DiscoverByValue(ctx context.Context, req model.IPCRequest) ([]model.DiscoveryCandidate, string, error) {
	var q model.ValueQuery
	if req.Query != nil {
		q = *req.Query
	}
	return m.discover(ctx, model.CmdDiscoverByValue, req, &q)
}

func (m *Monitor) discover(ctx context.Context, cmd string, req model.IPCRequest, q *model.ValueQuery) ([]model.DiscoveryCandidate, string, error) {
	p, pw, key, err := m.interactiveBegin(cmdKeyDiscover, req)
	if err != nil {
		return nil, "", err
	}
	if q != nil && strings.TrimSpace(q.PointName) == "" && strings.TrimSpace(q.PointRef) == "" {
		return nil, "", &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgDiscoverPointNameEmpty,
			Message: "discover_by_value: point name is empty", Args: []string{"query.pointName", model.ValRequired}}
	}
	x := m.explorer
	start := x.Now()
	cands, err := exploreRun(x, ctx, true, key, p, pw, func(ctx context.Context, r InteractiveReader) ([]model.DiscoveryCandidate, error) {
		if q != nil {
			return r.DiscoverByValue(ctx, *q)
		}
		return r.Discover(ctx)
	})
	if err != nil {
		err = interactiveError(err, pw)
		m.logInteractive(cmd, p, start, err, pw, "")
		return nil, "", err
	}
	model.TruncateCandidates(cands) // cells <= MaxSampleCellRunes (sqlcheck does it too)
	reason := discoveryTruncated(cands, x.Now().Sub(start), x.DiscoverTimeout)
	m.logInteractive(cmd, p, start, nil, pw, "%d candidates, truncated %q", len(cands), reason)
	return cands, reason, nil
}

// ListNodePoints runs IPC "list_node_points" (CHK-IPC-08): the
// exchange-plan points for the point picker; req.Limit (0 = all read,
// 1..MaxNodePointsLimit) caps them (truncated, reason
// model.TruncatedLimit). Names and codes are cut
// to MaxSampleCellRunes characters (display only; the point is picked by
// its ref).
func (m *Monitor) ListNodePoints(ctx context.Context, req model.IPCRequest) ([]model.NodePoint, string, error) {
	p, pw, key, err := m.interactiveBegin(cmdKeyNodePoints, req)
	if err != nil {
		return nil, "", err
	}
	if req.Limit < 0 || req.Limit > model.MaxNodePointsLimit {
		return nil, "", limitError(req.Limit, model.MaxNodePointsLimit)
	}
	x := m.explorer
	start := x.Now()
	pts, err := exploreRun(x, ctx, false, key, p, pw, func(ctx context.Context, r InteractiveReader) ([]model.NodePoint, error) {
		return r.NodePoints(ctx)
	})
	if err != nil {
		err = interactiveError(err, pw)
		m.logInteractive(model.CmdListNodePoints, p, start, err, pw, "")
		return nil, "", err
	}
	reason := ""
	if req.Limit > 0 && len(pts) > req.Limit {
		pts, reason = pts[:req.Limit], model.TruncatedLimit
	}
	for i := range pts {
		pts[i].Name, _ = model.TruncateSampleCell(pts[i].Name)
		pts[i].Code, _ = model.TruncateSampleCell(pts[i].Code)
	}
	m.logInteractive(model.CmdListNodePoints, p, start, nil, pw, "%d points, truncated %q", len(pts), reason)
	return pts, reason, nil
}

// limitError: invalid_request + monitor.bad_request, errorArgs ["limit",
// "validation.range", "1", max, value].
func limitError(v, max int) error {
	return &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorBadRequest,
		Message: fmt.Sprintf("limit %d out of range 1..%d", v, max),
		Args:    []string{"limit", model.ValRange, "1", strconv.Itoa(max), strconv.Itoa(v)}}
}

// mappingFailure is the ok:false result of a check error: a field error
// gives MsgDiscoverMappingInvalid + [mapping.<field>, reason, params...];
// anything else the key of interactiveErrorKey.
func mappingFailure(err error, password string) model.MappingCheck {
	res := model.MappingCheck{Error: scrub(err.Error(), password)}
	if args := model.FieldErrorArgs(err, "mapping."); args != nil {
		res.ErrorKey, res.ErrorArgs = model.MsgDiscoverMappingInvalid, args
		return res
	}
	_, res.ErrorKey = interactiveErrorKey(err)
	return res
}

// CheckMapping runs IPC "check_mapping" (CHK-IPC-09). Request errors
// (rate limit, profile, password, missing mapping, limit outside 1..20)
// are protocol errors; a failed check is MappingCheck.OK == false. Order:
// Mapping.Validate, a table or manual SQL is set, for manual SQL
// sqlcheck.ValidateManualSQL and sqlcheck.Compat2008Violations (SQL
// Server 2012+ constructs; nothing runs on the server when any is found),
// then — with a connection — sqlcheck PreviewMapping (ValidateMapping
// against the catalog + up to limit rows).
func (m *Monitor) CheckMapping(ctx context.Context, req model.IPCRequest) (model.MappingCheck, error) {
	p, pw, key, err := m.interactiveBegin(cmdKeyCheckMapping, req)
	if err != nil {
		return model.MappingCheck{}, err
	}
	if req.Mapping == nil {
		return model.MappingCheck{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorBadRequest,
			Message: "check_mapping without mapping", Args: []string{"mapping", model.ValRequired}}
	}
	limit := req.Limit
	if limit == 0 {
		limit = model.DefaultCheckMappingLimit
	}
	if limit < 1 || limit > model.MaxCheckMappingLimit {
		return model.MappingCheck{}, limitError(req.Limit, model.MaxCheckMappingLimit)
	}
	mp := *req.Mapping
	x := m.explorer
	start := x.Now()
	done := func(res model.MappingCheck) (model.MappingCheck, error) {
		if res.OK {
			m.logInteractive(model.CmdCheckMapping, p, start, nil, pw, "ok, %d rows", len(res.SampleRows))
		} else {
			m.logInteractive(model.CmdCheckMapping, p, start, nil, pw, "failed: %s %v", res.ErrorKey, res.ErrorArgs)
		}
		return res, nil
	}
	if err := mp.Validate(); err != nil {
		return done(mappingFailure(err, pw))
	}
	if !mp.Configured() {
		return done(model.MappingCheck{ErrorKey: model.MsgDiscoverMappingInvalid, Error: "mapping: a table or manual SQL is required",
			ErrorArgs: []string{"mapping.table", model.ValRequired}})
	}
	if strings.TrimSpace(mp.ManualSQL) != "" {
		verr := sqlcheck.ValidateManualSQL(mp.ManualSQL)
		v := sqlcheck.Compat2008Violations(mp.ManualSQL)
		if verr == nil && len(v) > 0 { // ValidateManualSQL checks it too; never run 2012+ SQL
			list := strings.Join(v, ", ")
			res := model.MappingCheck{ErrorKey: model.MsgDiscoverMappingInvalid, Error: "manual SQL: SQL Server 2012+ constructs: " + list,
				ErrorArgs: []string{"mapping.manualSql", model.ValSQLManualCompat2008, list}}
			res.Violations = v
			return done(res)
		}
		if verr != nil {
			res := mappingFailure(verr, pw)
			res.Violations = v
			return done(res)
		}
	}
	rows, err := exploreRun(x, ctx, false, key, p, pw, func(ctx context.Context, r InteractiveReader) ([]model.SampleRow, error) {
		return r.PreviewMapping(ctx, mp, limit)
	})
	if err != nil {
		var ie *model.IPCError
		if errors.As(err, &ie) && ie != errDiscoverTimeout {
			m.logInteractive(model.CmdCheckMapping, p, start, err, pw, "")
			return model.MappingCheck{}, err // busy
		}
		if code, _ := interactiveErrorKey(err); code == model.ErrCodeCanceled && ie == nil {
			m.logInteractive(model.CmdCheckMapping, p, start, err, pw, "")
			return model.MappingCheck{}, interactiveError(err, pw)
		}
		res := mappingFailure(err, pw)
		if ie != nil { // the service limit
			res.ErrorKey = ie.Key
		}
		return done(res)
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	model.TruncateSampleRows(rows)
	return done(model.MappingCheck{OK: true, SampleRows: rows})
}
