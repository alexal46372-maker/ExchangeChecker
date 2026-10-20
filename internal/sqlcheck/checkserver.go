package sqlcheck

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 11 (PLAN §12 p. 163, CHK-SQL-13): step-by-step server check of
// "host[\instance][,port]": parse (model.ValidateSQLServerAddr) → dns →
// browser (host\instance without a port: SSRP CLNT_UCAST_INST) → tcp →
// prelogin (TDS PRELOGIN, version) → tls (encryption negotiated in
// PRELOGIN vs the profile) → login → db → tables (1C _Node* visible). Each
// stage reports its duration and, on failure, a model.SQLCode* code, the
// sql.diag_* error key and the sql.check_advice_<stage> key; the check
// stops at the first failure. login / db / tables run only with a
// Profile; db / tables only with Profile.Database.

// ServerCheckOptions configures CheckServer.
type ServerCheckOptions struct {
	// Profile (optional): auth, database and encryption of the login
	// stages; its Server / Port are replaced by the checked server.
	Profile *model.Profile
	// Password of Profile (SQL auth). Never logged nor returned.
	Password string
	// Step, if set, is called synchronously after every stage (push).
	Step func(model.SQLCheckStep)
	// BrowserPort of the SSRP query (0 = 1434; tests).
	BrowserPort int
	// Timeout per network stage (0 = Profile connect timeout, max 15 s;
	// without a profile 5 s).
	Timeout time.Duration

	dial func(ctx context.Context, addr string) (net.Conn, error) // tests
}

// ParseSQLServerTarget splits "host[\instance][,port]" after
// model.ValidateSQLServerAddr (port 0: not given).
func ParseSQLServerTarget(server string) (host, instance string, port int, err error) {
	s := strings.TrimSpace(server)
	if err = model.ValidateSQLServerAddr(s); err != nil {
		return "", "", 0, err
	}
	if i := strings.LastIndexByte(s, ','); i >= 0 {
		p, perr := strconv.Atoi(strings.TrimSpace(s[i+1:]))
		if perr != nil || p <= 0 || p > 65535 {
			return "", "", 0, &model.FieldError{Field: "server", Key: model.ValSQLServerForbidden, Args: []string{"port"}, Msg: "server: bad port after ','"}
		}
		port, s = p, s[:i]
	}
	host, instance = splitServer(s)
	if host == "" || strings.ContainsAny(host, " ,;/\\") || len(host) > 255 || len(instance) > 64 {
		return "", "", 0, &model.FieldError{Field: "server", Key: model.ValSQLServerForbidden, Args: []string{"host"}, Msg: "server: bad host name"}
	}
	return host, instance, port, nil
}

type checkRun struct {
	opt ServerCheckOptions
	res model.SQLServerCheckResult
}

// stage runs f as stage id; false stops the check.
func (c *checkRun) stage(ctx context.Context, id string, f func() (detail string, err error, info model.SQLErrorInfo)) bool {
	start := time.Now()
	detail, err, info := f()
	st := model.SQLCheckStep{ID: id, OK: err == nil, Ms: time.Since(start).Milliseconds(), Detail: detail}
	if err != nil {
		if info.Code == "" {
			info = ErrorInfo(err)
		}
		if ctx.Err() != nil {
			info = ErrorInfo(ctxErr(id, ctx.Err()))
		}
		st.Code, st.ErrorKey, st.AdviceKey = info.Code, info.ErrorKey, model.SQLCheckAdviceKey(id)
		if st.Detail == "" {
			st.Detail = safeDetail(err)
		}
		cat := info.Category
		if cat == "" {
			cat = ConnCategoryNetwork
		}
		d := model.SQLDiagnose(cat, info.ErrorKey)
		c.res.Diagnosis = &d
	}
	c.res.Steps = append(c.res.Steps, st)
	if c.opt.Step != nil {
		c.opt.Step(st)
	}
	return err == nil
}

// safeDetail: the error text without anything after "password" (driver
// messages never carry it, the cut is defensive).
func safeDetail(err error) string {
	s := err.Error()
	if i := strings.Index(strings.ToLower(s), "password"); i >= 0 {
		s = s[:i] + "…"
	}
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

func netInfo(code string) model.SQLErrorInfo {
	cat := ConnCategoryNetwork
	if code == model.SQLCodeTimeout {
		cat = ConnCategoryTimeout
	}
	if code == model.SQLCodeConfig {
		cat = ConnCategoryConfig
	}
	if code == model.SQLCodeTLS {
		cat = ConnCategoryTLS
	}
	return model.SQLErrorInfo{Code: code, Category: cat, ErrorKey: model.SQLDiagKeyForCode(code, "")}
}

func dialCode(err error) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() || errors.Is(err, context.DeadlineExceeded) {
		return model.SQLCodeTimeout
	}
	return model.SQLCodeNetwork
}

// CheckServer checks server stage by stage (see above). The result is
// always filled; the error is only ctx's (*Error KindCanceled /
// KindTimeout) — stage failures are in Steps and Diagnosis.
func CheckServer(ctx context.Context, server string, opt ServerCheckOptions) (model.SQLServerCheckResult, error) {
	c := &checkRun{opt: opt, res: model.SQLServerCheckResult{Steps: []model.SQLCheckStep{}}}
	if c.opt.BrowserPort <= 0 || c.opt.BrowserPort > 65535 {
		c.opt.BrowserPort = SQLBrowserPort
	}
	if c.opt.Timeout <= 0 {
		c.opt.Timeout = 5 * time.Second
		if opt.Profile != nil {
			c.opt.Timeout = opt.Profile.ConnectTimeout()
		}
	}
	if c.opt.Timeout > 15*time.Second {
		c.opt.Timeout = 15 * time.Second
	}
	if c.opt.dial == nil {
		c.opt.dial = func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp4", addr)
		}
	}
	done := func() (model.SQLServerCheckResult, error) {
		c.res.OK = c.res.Diagnosis == nil && len(c.res.Steps) > 0
		if err := ctx.Err(); err != nil {
			c.res.OK = false
			return c.res, ctxErr("check_server", err)
		}
		return c.res, nil
	}
	var host, inst string
	var port int
	if !c.stage(ctx, model.SQLStepParse, func() (string, error, model.SQLErrorInfo) {
		var err error
		host, inst, port, err = ParseSQLServerTarget(server)
		if err != nil {
			return "", err, netInfo(model.SQLCodeConfig)
		}
		if port == 0 && opt.Profile != nil && opt.Profile.Port > 0 {
			port = opt.Profile.Port
		}
		return "", nil, model.SQLErrorInfo{}
	}) {
		return done()
	}
	c.res.Host, c.res.Instance = host, inst
	var ip string
	if !c.stage(ctx, model.SQLStepDNS, func() (string, error, model.SQLErrorInfo) {
		dctx, cancel := context.WithTimeout(ctx, c.opt.Timeout)
		defer cancel()
		ips, err := lookupIPv4(dctx, host)
		if err != nil {
			return "", err, netInfo(model.SQLCodeNetwork)
		}
		ip = ips[0].String()
		var ss []string
		for _, x := range ips {
			ss = append(ss, x.String())
		}
		return strings.Join(ss, ", "), nil, model.SQLErrorInfo{}
	}) {
		return done()
	}
	c.res.Address = ip
	if inst != "" && port == 0 {
		if !c.stage(ctx, model.SQLStepBrowser, func() (string, error, model.SQLErrorInfo) {
			bctx, cancel := context.WithTimeout(ctx, c.opt.Timeout)
			defer cancel()
			b := &browseRun{opt: BrowseOptions{Port: c.opt.BrowserPort, Sends: model.SQLBrowseSends, SendInterval: model.SQLBrowseSendIntervalMs * time.Millisecond}, seen: map[string]model.SQLServerInstance{}}
			msg := append(append([]byte{ssrpClntUcastIns}, inst...), 0)
			_ = b.ssrpRound(bctx, msg, []*net.UDPAddr{{IP: net.ParseIP(ip).To4(), Port: c.opt.BrowserPort}}, true, true)
			if b.res.Responded == 0 {
				return "", errors.New("no answer from " + net.JoinHostPort(ip, strconv.Itoa(c.opt.BrowserPort)) + " (UDP)"), netInfo(model.SQLCodeBrowserUnreachable)
			}
			for _, in := range b.seen {
				if strings.EqualFold(in.Instance, inst) {
					if in.TCPPort == 0 {
						return "", errors.New("the instance has no TCP port"), netInfo(model.SQLCodeInstanceNotFound)
					}
					port = in.TCPPort
					return "tcp " + strconv.Itoa(port), nil, model.SQLErrorInfo{}
				}
			}
			return "", errors.New("instance not announced"), netInfo(model.SQLCodeInstanceNotFound)
		}) {
			return done()
		}
	}
	if port == 0 {
		port = model.DefaultSQLPort
	}
	c.res.Port = port
	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	if !c.stage(ctx, model.SQLStepTCP, func() (string, error, model.SQLErrorInfo) {
		tctx, cancel := context.WithTimeout(ctx, c.opt.Timeout)
		defer cancel()
		conn, err := c.opt.dial(tctx, addr)
		if err != nil {
			return addr, err, netInfo(dialCode(err))
		}
		conn.Close()
		return addr, nil, model.SQLErrorInfo{}
	}) {
		return done()
	}
	enc := byte(0x02)
	mode := model.EncryptDisable
	if opt.Profile != nil {
		mode = opt.Profile.Encrypt.Effective()
	}
	switch mode {
	case model.EncryptOptional:
		enc = 0x00
	case model.EncryptOn, model.EncryptStrict:
		enc = 0x01
	}
	var pl preloginInfo
	if !c.stage(ctx, model.SQLStepPrelogin, func() (string, error, model.SQLErrorInfo) {
		var err error
		pl, _, err = prelogin(ctx, c.opt.dial, addr, enc, c.opt.Timeout)
		if err != nil {
			return "", err, netInfo(dialCode(err))
		}
		return pl.Version, nil, model.SQLErrorInfo{}
	}) {
		return done()
	}
	c.res.Version, c.res.Encryption = pl.Version, pl.Encryption
	if !c.stage(ctx, model.SQLStepTLS, func() (string, error, model.SQLErrorInfo) {
		d := string(mode) + "/" + pl.Encryption
		switch {
		case mode == model.EncryptDisable && pl.Encryption == SQLEncRequired:
			return d, errors.New("the server requires encryption"), netInfo(model.SQLCodeTLS)
		case (mode == model.EncryptOn || mode == model.EncryptStrict) && pl.Encryption == SQLEncNotSupported:
			return d, errors.New("the server does not support encryption"), netInfo(model.SQLCodeTLS)
		}
		return d, nil, model.SQLErrorInfo{}
	}) {
		return done()
	}
	if opt.Profile == nil {
		return done()
	}
	p := *opt.Profile
	p.Server, p.Port = host, port
	if inst != "" {
		p.Server = host + `\` + inst
	}
	db := p.Database
	p.Database = "master"
	if !c.stage(ctx, model.SQLStepLogin, func() (string, error, model.SQLErrorInfo) {
		_, err := TestConnectionInfo(ctx, p, opt.Password)
		return "", err, model.SQLErrorInfo{}
	}) || strings.TrimSpace(db) == "" {
		return done()
	}
	p.Database = db
	var st *SQLStore
	if !c.stage(ctx, model.SQLStepDB, func() (string, error, model.SQLErrorInfo) {
		var err error
		if st, err = Open(p, opt.Password); err != nil {
			return "", err, model.SQLErrorInfo{}
		}
		pctx, cancel := context.WithTimeout(ctx, 2*st.connectTimeout)
		defer cancel()
		return "", classify("db", st.Ping(pctx), true), model.SQLErrorInfo{}
	}) {
		if st != nil {
			st.Close()
		}
		return done()
	}
	defer st.Close()
	c.stage(ctx, model.SQLStepTables, func() (string, error, model.SQLErrorInfo) {
		qctx, cancel := context.WithTimeout(ctx, st.queryTimeout)
		defer cancel()
		var n int
		err := st.db.QueryRowContext(qctx, `select count(*) from sys.tables where name like '\_Node%' escape '\'`).Scan(&n)
		if err != nil {
			return "", classify("tables", err, false), model.SQLErrorInfo{}
		}
		if n == 0 {
			return "0", ErrNot1CDatabase, model.SQLErrorInfo{}
		}
		return strconv.Itoa(n), nil, model.SQLErrorInfo{}
	})
	return done()
}
