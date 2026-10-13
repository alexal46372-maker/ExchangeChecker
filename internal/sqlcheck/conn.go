package sqlcheck

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// AppName is sent to SQL Server as the client application name.
const AppName = "ExchangeChecker"

// splitServer splits "host\instance" (instance optional).
func splitServer(server string) (host, instance string) {
	server = strings.TrimSpace(server)
	if i := strings.IndexByte(server, '\\'); i >= 0 {
		return strings.TrimSpace(server[:i]), strings.TrimSpace(server[i+1:])
	}
	return server, ""
}

// forbiddenServerForm is the pre-dial guard of phase 9 (CHK-SQL-09, PLAN
// §12 p. 109): a thin wrapper over model.ValidateSQLServerAddr, the single
// source of the rule (np:, lpc:, admin:, UNC, leading '\', pipe paths;
// owned by EC Service). The service (LocalSystem) never opens SMB / named
// pipes to arbitrary hosts. go-mssqldb v1.8.2 with the URL DSN built here
// does not interpret such prefixes (np / lpc dialers are not linked), but
// the guard does not rely on that.
func forbiddenServerForm(server string) error { return model.ValidateSQLServerAddr(server) }

// driverConfig builds the go-mssqldb configuration for p. The login and the
// password are set on the returned struct only: they never appear in a DSN
// string, a log line or an error message.
//
// Port: Profile.Port if set; otherwise 1433, except for "host\instance",
// where the port is resolved by the SQL Browser (UDP 1434).
// Networking: the driver's own net.Dialer (TCP) is used; there is no
// project network package convention yet (PLAN.md §12).
func driverConfig(p model.Profile, password string) (msdsn.Config, error) {
	if err := forbiddenServerForm(p.Server); err != nil {
		return msdsn.Config{}, err
	}
	host, inst := splitServer(p.Server)
	if host == "" {
		return msdsn.Config{}, errors.New("server is empty")
	}
	if !p.Encrypt.Valid() {
		return msdsn.Config{}, fmt.Errorf("unknown encrypt mode %q", string(p.Encrypt))
	}
	q := url.Values{}
	q.Set("database", p.Database)
	q.Set("app name", AppName)
	q.Set("dial timeout", strconv.Itoa(int(p.ConnectTimeout()/time.Second)))
	switch p.EffectiveEncrypt() { // unset = disable (phase 2d, customer decision)
	case model.EncryptDisable:
		q.Set("encrypt", "disable")
	case model.EncryptOn:
		q.Set("encrypt", "true")
		q.Set("TrustServerCertificate", strconv.FormatBool(p.TrustServerCert))
	case model.EncryptStrict:
		q.Set("encrypt", "strict")
	default: // EncryptOptional: driver default (login-only TLS, cert not validated)
	}
	if v := p.EffectiveTLSMinVersion(); v != "" {
		// go-mssqldb: tlsmin "1.0" -> tls.Config.MinVersion = tls.VersionTLS10
		// (Go 1.20 clients default to TLS 1.2 minimum).
		q.Set("tlsmin", v)
	}
	var user, pass string
	switch p.AuthMode {
	case model.AuthSQL:
		if strings.ContainsRune(p.Login, '\\') {
			return msdsn.Config{}, errors.New(`SQL login must not contain '\' (use Windows authentication for DOMAIN\user)`)
		}
		user, pass = p.Login, password
	case model.AuthWindows:
		// SSPI (secur32.dll): the current process identity (the service
		// account), or explicit DOMAIN\user + password when Login is set.
		q.Set("authenticator", integratedAuthProvider)
		if strings.ContainsRune(p.Login, '\\') {
			user, pass = p.Login, password
		}
	default:
		return msdsn.Config{}, fmt.Errorf("unknown auth mode %q", string(p.AuthMode))
	}
	u := url.URL{Scheme: "sqlserver", Host: host, RawQuery: q.Encode()}
	switch {
	case p.Port != 0:
		u.Host = net.JoinHostPort(host, strconv.Itoa(p.Port))
	case inst == "":
		u.Host = net.JoinHostPort(host, strconv.Itoa(model.DefaultSQLPort))
	}
	if inst != "" {
		u.Path = "/" + inst
	}
	cfg, err := msdsn.Parse(u.String())
	if err != nil {
		return msdsn.Config{}, err
	}
	cfg.User, cfg.Password = user, pass
	return cfg, nil
}

// Open creates a lazily-connecting SQLStore for p. It does not touch the
// network; use Ping / TestConnection for that.
func Open(p model.Profile, password string) (*SQLStore, error) {
	return openWith(p, password, nil)
}

// openWith is Open with an optional session keeper: the sockets of
// sessions established through the store stay under it after login
// (TestConnectionInfo: sessionHold, interactive stores: queryGuard; poll
// stores pass nil).
func openWith(p model.Profile, password string, hold sessionKeeper) (*SQLStore, error) {
	cfg, err := driverConfig(p, password)
	if err != nil {
		return nil, &Error{Kind: KindConfig, Op: "open", Err: err}
	}
	gc := newGuardedConnector(cfg, p.ConnectTimeout())
	gc.hold = hold
	db := sql.OpenDB(gc)
	db.SetMaxOpenConns(2)
	if _, ok := hold.(*queryGuard); ok {
		// one session: a guard kill closes exactly the session of the
		// request, the next query dials a new one
		db.SetMaxOpenConns(1)
	}
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return NewSQLStore(db, p.ConnectTimeout(), p.QueryTimeout()), nil
}

// TestConnection opens a session to p (TCP, TLS as configured, login,
// database) and runs "select 1" (CHK-SQL-01). Errors are *Error with
// KindConn / KindTimeout / KindConfig; ConnCategory sorts them for the UI.
// It is a wrapper of TestConnectionInfo that drops the ServerInfo.
func TestConnection(ctx context.Context, p model.Profile, password string) error {
	_, err := TestConnectionInfo(ctx, p, password)
	return err
}
