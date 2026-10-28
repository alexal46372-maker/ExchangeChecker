package monitor

import (
	"context"
	"errors"
	"fmt"
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
	return m.SQLBrowseProgress(ctx, req, nil)
}

// SQLBrowseProgress is SQLBrowse with the phase 11 request fields (mode,
// port, cidr; CHK-IPC-12) and sqlcheck.Browse progress events. Limits per
// mode (rate_limited + retryAfterMs): broadcast / auto 15 s, local 3 s,
// subnet 15 s between requests, host 3 s (+ any host 1 s); a running
// browse: busy + retryAfterMs. Subnet: the whole request ends after
// timeoutMs (model.ClampSQLBrowseTimeoutMsFor: subnet default 30 s / max
// 45 s, CHK-SQL-15) plus model.IPCSQLBrowseGrace.
func (m *Monitor) SQLBrowseProgress(ctx context.Context, req model.IPCRequest, progress func(model.SQLBrowseProgress)) (model.SQLBrowseResult, error) {
	var res model.SQLBrowseResult
	br := model.SQLBrowseRequest{Host: strings.TrimSpace(req.Host), Instance: strings.TrimSpace(req.Instance),
		TimeoutMs: model.ClampSQLBrowseTimeoutMsFor(req.Mode, req.TimeoutMs), Limit: req.Limit,
		Mode: req.Mode, Port: req.Port, CIDR: strings.TrimSpace(req.CIDR)}
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
	mode := br.EffectiveMode()
	switch mode {
	case model.SQLBrowseLocal:
		key, every = "l", model.SQLBrowseLocalInterval
	case model.SQLBrowseSubnet:
		key, every = "s", model.SQLBrowseSubnetInterval
	case model.SQLBrowseHost:
		key, every = "h:"+strings.ToLower(br.Host), model.SQLBrowseHostInterval
	}
	targeted := mode == model.SQLBrowseHost
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
	if targeted { // aliases (FQDN / IP) of one host: service-wide limit
		hold(browseKeyAnyHost, model.SQLBrowseAnyHostInterval)
	}
	if wait > 0 {
		x.mu.Unlock()
		ms := int64((wait + time.Millisecond - 1) / time.Millisecond)
		return res, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgBrowseRateLimited,
			Message: "monitor: sql_browse rate limited, retry after " + strconv.FormatInt(ms, 10) + " ms", RetryAfterMs: ms}
	}
	// subnet: timeoutMs default 30 s / max 45 s (ClampSQLBrowseTimeoutMsFor,
	// CHK-SQL-15; only on an explicit mode "subnet"; "" = host or broadcast)
	total := time.Duration(br.TimeoutMs)*time.Millisecond + model.IPCSQLBrowseGrace
	if x.browsing {
		ms := int64(x.browseEnd.Sub(now) / time.Millisecond)
		if ms < 1 {
			ms = 1
		}
		x.mu.Unlock()
		return res, &model.IPCError{Code: model.ErrCodeBusy, Key: model.MsgDiscoverBusy, RetryAfterMs: ms,
			Message: "monitor: a sql_browse is running, retry after " + strconv.FormatInt(ms, 10) + " ms"}
	}
	x.browsing = true
	x.browseEnd = now.Add(total)
	x.browseAt[key] = now
	if targeted {
		x.browseAt[browseKeyAnyHost] = now
	}
	for k, t := range x.browseAt { // forget old hosts
		if d := now.Sub(t); d > model.SQLBrowseBroadcastInterval || d < 0 {
			delete(x.browseAt, k)
		}
	}
	browse := x.Browse
	if browse == nil {
		with := x.BrowseWith
		browse = func(ctx context.Context, r model.SQLBrowseRequest) (model.SQLBrowseResult, error) {
			return with(ctx, r, sqlcheck.BrowseOptions{Progress: progress})
		}
	}
	x.mu.Unlock()
	defer func() {
		x.mu.Lock()
		x.browsing = false
		x.mu.Unlock()
	}()
	cctx, cancel := context.WithTimeout(ctx, total)
	defer cancel()
	start := time.Now()
	res, err := browse(cctx, br)
	if err != nil {
		if ctx.Err() != nil {
			return model.SQLBrowseResult{}, ctx.Err()
		}
		err = interactiveError(err, "")
		m.opt.Log.Printf("sql_browse %s %q: %v in %d ms", mode, br.Host, err, time.Since(start).Milliseconds())
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
	if len(res.SendErrors) > model.MaxSQLBrowseSendErrors {
		res.SendErrors = res.SendErrors[:model.MaxSQLBrowseSendErrors]
	}
	// diagnostics: addresses / ports / counts only (no credentials exist here)
	m.opt.Log.Printf("sql_browse %s %q cidr %q: %d instances, %d responders, sent %d, send errors %d, responded %d, advice %v, truncated %v in %d ms",
		mode, br.Host, br.CIDR, len(res.Instances), res.Responders, res.Sent, res.SendErrorCount, res.Responded, res.Advice,
		res.Truncated, time.Since(start).Milliseconds())
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

// SQLTestSteps runs IPC sql_test_steps (phase 11, CHK-IPC-13): the
// step-by-step check of req.Server (sqlcheck.CheckServer), with the saved
// profile req.ProfileID (auth, database, encryption and its saved
// password, loaded here — a password never travels over IPC) when given.
// One check per service (busy + retryAfterMs), the whole request ends
// after model.IPCSQLTestStepsTimeout. step is called after every stage.
// The log gets the server, the profile id and per stage id / ok / ms /
// code only (never the login, the password, a connection string or the
// stage detail, which may carry server error text).
func (m *Monitor) SQLTestSteps(ctx context.Context, req model.IPCRequest, step func(model.SQLCheckStep)) (model.SQLServerCheckResult, error) {
	server := strings.TrimSpace(req.Server)
	if server == "" {
		return model.SQLServerCheckResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorBadRequest,
			Message: "sql_test_steps: server is required", Args: []string{"server", model.ValRequired}}
	}
	if err := model.ValidateSQLServerAddr(server); err != nil {
		return model.SQLServerCheckResult{}, fieldRequestError(err, "server")
	}
	opt := sqlcheck.ServerCheckOptions{Step: step}
	// pw: the saved password as bytes, zeroed when the call ends; the
	// string handed to sqlcheck (its API) is the only other copy and is
	// not kept after the call (opt.Password cleared).
	var pw []byte
	defer func() {
		for i := range pw {
			pw[i] = 0
		}
	}()
	if req.ProfileID != "" {
		m.mu.Lock()
		e := m.entries[req.ProfileID]
		var p model.Profile
		if e != nil {
			p = e.profile
			pw = append(pw, e.password...)
		}
		m.mu.Unlock()
		if e == nil {
			return model.SQLServerCheckResult{}, ErrUnknownProfile
		}
		// the saved password only for the profile's own server
		host, inst, port, err := sqlcheck.ParseSQLServerTarget(server)
		if err != nil {
			return model.SQLServerCheckResult{}, fieldRequestError(err, "server")
		}
		target := p
		target.Server, target.Port = host, port
		if inst != "" {
			target.Server = host + `\` + inst
		}
		if model.TargetChanged(target, p) {
			m.opt.Log.Printf("sql_test_steps %q profile %q: server does not match the profile, refused", server, req.ProfileID)
			return model.SQLServerCheckResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgTestStepsServerMismatch,
				Message: "sql_test_steps: server does not match the profile's server", Args: []string{"server"}}
		}
		opt.Profile = &p
	}
	x := m.explorer
	now := m.opt.Now()
	x.mu.Lock()
	x.defaults()
	if x.checking {
		ms := int64(x.checkEnd.Sub(now) / time.Millisecond)
		if ms < 1 {
			ms = 1
		}
		x.mu.Unlock()
		return model.SQLServerCheckResult{}, &model.IPCError{Code: model.ErrCodeBusy, Key: model.MsgDiscoverBusy, RetryAfterMs: ms,
			Message: "monitor: a sql_test_steps is running, retry after " + strconv.FormatInt(ms, 10) + " ms"}
	}
	x.checking, x.checkEnd = true, now.Add(model.IPCSQLTestStepsTimeout)
	check := x.CheckServer
	x.mu.Unlock()
	defer func() {
		x.mu.Lock()
		x.checking = false
		x.mu.Unlock()
	}()
	cctx, cancel := context.WithTimeout(ctx, model.IPCSQLTestStepsTimeout)
	defer cancel()
	start := time.Now()
	opt.Password = string(pw)
	res, err := check(cctx, server, opt)
	scrub := opt.Password
	opt.Password = ""
	if err != nil {
		if ctx.Err() != nil {
			return model.SQLServerCheckResult{}, ctx.Err()
		}
		err = interactiveError(err, scrub)
		m.opt.Log.Printf("sql_test_steps %q profile %q: %v in %d ms", server, req.ProfileID, err, time.Since(start).Milliseconds())
		return model.SQLServerCheckResult{}, err
	}
	if res.Steps == nil {
		res.Steps = []model.SQLCheckStep{}
	}
	var b strings.Builder
	for _, s := range res.Steps {
		fmt.Fprintf(&b, " %s:%v:%dms", s.ID, s.OK, s.Ms)
		if s.Code != "" {
			b.WriteString(":" + s.Code)
		}
	}
	m.opt.Log.Printf("sql_test_steps %q profile %q: ok %v, port %d, version %q, encryption %q, steps%s in %d ms", server, req.ProfileID,
		res.OK, res.Port, res.Version, res.Encryption, b.String(), time.Since(start).Milliseconds())
	return res, nil
}
