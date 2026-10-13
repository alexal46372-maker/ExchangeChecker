package monitor

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// Phase 9 (EC Service, CHK-IPC-10): the connection wizard commands
// sql_browse and sql_databases (admin-only, checked by the IPC server).
// SQL I/O stays in sqlcheck (EC SQL); the service validates, limits,
// resolves the password and caps the reply.

const cmdKeySQLDatabases = "sql_databases"

// browseKeyAnyHost: limiter key of all targeted browses (Explorer.browseAt).
const browseKeyAnyHost = "hosts"

// databaseLister is implemented by *sqlcheck.InteractiveReader.
type databaseLister interface {
	Databases(ctx context.Context, opt sqlcheck.DatabasesOptions) (model.SQLDatabasesResult, error)
}

// fieldRequestError turns a validation error into invalid_request with
// errorKey monitor.bad_request and errorArgs [field, validation key,
// args...] (the profile_invalid convention); field "" = the error's own.
func fieldRequestError(err error, field string) error {
	if fe := model.AsFieldError(err); fe != nil {
		if field == "" {
			field = fe.Field
		}
		return &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorBadRequest, Message: err.Error(),
			Args: append([]string{field, fe.Key}, fe.Args...)}
	}
	return &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorBadRequest, Message: err.Error(),
		Args: []string{field}}
}

// SQLBrowse runs IPC sql_browse: validation (model.ValidateSQLServerAddr
// and SQLBrowseRequest.Validate), rate limits (broadcast once per
// model.SQLBrowseBroadcastInterval, a host once per
// model.SQLBrowseHostInterval), one browse per service (busy), timeout
// clamped to model.IPCSQLBrowseMin/MaxTimeoutMs, instances capped to the
// limit and model.MaxSQLWizardJSONBytes (Truncated).
func (m *Monitor) SQLBrowse(ctx context.Context, req model.IPCRequest) (model.SQLBrowseResult, error) {
	var res model.SQLBrowseResult
	br := model.SQLBrowseRequest{Host: strings.TrimSpace(req.Host), Instance: strings.TrimSpace(req.Instance),
		TimeoutMs: model.ClampSQLBrowseTimeoutMs(req.TimeoutMs), Limit: req.Limit}
	if br.Host != "" {
		if err := model.ValidateSQLServerAddr(br.Host); err != nil {
			return res, fieldRequestError(err, "host")
		}
	}
	if err := br.Validate(); err != nil {
		return res, fieldRequestError(err, "")
	}
	x := m.explorer
	now := m.opt.Now()
	key, every := "", model.SQLBrowseBroadcastInterval
	if br.Host != "" {
		key, every = "h:"+strings.ToLower(br.Host), model.SQLBrowseHostInterval
	}
	x.mu.Lock()
	x.defaults()
	var wait time.Duration
	hold := func(k string, d time.Duration) {
		if t, ok := x.browseAt[k]; ok {
			if el := now.Sub(t); el >= 0 && el < d && d-el > wait {
				wait = d - el
			}
		}
	}
	hold(key, every)
	if br.Host != "" { // aliases (FQDN / IP) of one host: service-wide limit
		hold(browseKeyAnyHost, model.SQLBrowseAnyHostInterval)
	}
	if wait > 0 {
		x.mu.Unlock()
		ms := int64((wait + time.Millisecond - 1) / time.Millisecond)
		return res, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgBrowseRateLimited,
			Message: "monitor: sql_browse rate limited, retry after " + strconv.FormatInt(ms, 10) + " ms", RetryAfterMs: ms}
	}
	if x.browsing {
		x.mu.Unlock()
		return res, ErrDiscoverBusy
	}
	x.browsing = true
	x.browseAt[key] = now
	if br.Host != "" {
		x.browseAt[browseKeyAnyHost] = now
	}
	for k, t := range x.browseAt { // forget old hosts
		if d := now.Sub(t); d > model.SQLBrowseBroadcastInterval || d < 0 {
			delete(x.browseAt, k)
		}
	}
	browse := x.Browse
	x.mu.Unlock()
	defer func() {
		x.mu.Lock()
		x.browsing = false
		x.mu.Unlock()
	}()
	cctx, cancel := context.WithTimeout(ctx, time.Duration(br.TimeoutMs)*time.Millisecond+model.IPCSQLBrowseGrace)
	defer cancel()
	start := time.Now()
	res, err := browse(cctx, br)
	if err != nil {
		if ctx.Err() != nil {
			return model.SQLBrowseResult{}, ctx.Err()
		}
		err = interactiveError(err, "")
		m.opt.Log.Printf("sql_browse %q: %v in %d ms", br.Host, err, time.Since(start).Milliseconds())
		return model.SQLBrowseResult{}, err
	}
	limit := br.Limit
	if limit == 0 {
		limit = model.MaxSQLBrowseInstances
	}
	var cut bool
	res.Instances, cut = model.CapSQLList(res.Instances, limit)
	res.Truncated = res.Truncated || cut
	if res.Instances == nil {
		res.Instances = []model.SQLServerInstance{}
	}
	m.opt.Log.Printf("sql_browse %q: %d instances, %d responders, truncated %v in %d ms", br.Host, len(res.Instances),
		res.Responders, res.Truncated, time.Since(start).Milliseconds())
	return res, nil
}

// SQLDatabases runs IPC sql_databases: the user databases of the
// (unsaved) req.Profile. The server address is checked with
// model.ValidateSQLServerAddr; without a password the saved secret of the
// same profile is used only for the same target (server / instance /
// port; else config.password_required_on_target_change) and the
// test_profile rules (model.CanReuseSavedPassword). An empty database is
// master. Runs as a light interactive operation (limits, cancel, hard
// timeout); the password is never logged or returned.
func (m *Monitor) SQLDatabases(ctx context.Context, req model.IPCRequest) (model.SQLDatabasesResult, error) {
	var res model.SQLDatabasesResult
	if req.Profile == nil {
		return res, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgTestInvalidProfile,
			Message: "sql_databases: profile is required", Args: []string{"profile", model.ValRequired}}
	}
	p := *req.Profile
	if err := model.ValidateSQLServerAddr(p.Server); err != nil {
		return res, fieldRequestError(err, "server")
	}
	if req.Limit < 0 || req.Limit > model.MaxSQLDatabases {
		return res, limitError(req.Limit, model.MaxSQLDatabases)
	}
	if strings.TrimSpace(p.Database) == "" {
		p.Database = "master"
	}
	if p.ID == "" {
		p.ID = req.ProfileID
	}
	if req.Password == "" && p.AuthMode != model.AuthWindows && p.ID != "" {
		m.mu.Lock()
		saved := m.entries[p.ID]
		m.mu.Unlock()
		if saved != nil && model.TargetChanged(p, saved.profile) {
			return res, errPasswordTargetChange
		}
	}
	r := req
	r.Profile = &p
	p, pw, key, err := m.interactiveBegin(cmdKeySQLDatabases, r)
	if err != nil {
		return res, err
	}
	x := m.explorer
	start := x.Now()
	limit := req.Limit
	if limit == 0 {
		limit = model.DefaultSQLDatabasesLimit
	}
	res, err = exploreRun(x, ctx, false, key, p, pw, func(ctx context.Context, rd InteractiveReader) (model.SQLDatabasesResult, error) {
		dl, ok := rd.(databaseLister)
		if !ok {
			return model.SQLDatabasesResult{}, errors.New("sql_databases: the reader cannot list databases")
		}
		return dl.Databases(ctx, sqlcheck.DatabasesOptions{Limit: limit})
	})
	if err != nil {
		err = interactiveError(err, pw)
		m.logInteractive(model.CmdSQLDatabases, p, start, err, pw, "")
		return model.SQLDatabasesResult{}, err
	}
	var cut bool
	res.Databases, cut = model.CapSQLList(res.Databases, limit)
	res.Truncated = res.Truncated || cut
	if res.Databases == nil {
		res.Databases = []model.SQLDatabaseInfo{}
	}
	m.logInteractive(model.CmdSQLDatabases, p, start, nil, pw, "%d databases, truncated %v", len(res.Databases), res.Truncated)
	return res, nil
}
