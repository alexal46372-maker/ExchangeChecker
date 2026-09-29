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

	mssql "github.com/microsoft/go-mssqldb"
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

// driverConfig builds the go-mssqldb configuration for p. The login and the
// password are set on the returned struct only: they never appear in a DSN
// string, a log line or an error message.
//
// Port: Profile.Port if set; otherwise 1433, except for "host\instance",
// where the port is resolved by the SQL Browser (UDP 1434).
// Networking: the driver's own net.Dialer (TCP) is used; there is no
// project network package convention yet (PLAN.md §12).
func driverConfig(p model.Profile, password string) (msdsn.Config, error) {
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
	cfg, err := driverConfig(p, password)
	if err != nil {
		return nil, &Error{Kind: KindConfig, Op: "open", Err: err}
	}
	c := mssql.NewConnectorConfig(cfg)
	c.Dialer = handshakeDialer{nd: net.Dialer{KeepAlive: cfg.KeepAlive}}
	db := sql.OpenDB(c)
	db.SetMaxOpenConns(2)
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

// handshakeDialer bounds the whole session setup (TCP, prelogin, TLS,
// login) by the driver's dial context. go-mssqldb v1.8.2 applies the dial
// timeout (Profile.ConnectTimeout) to the TCP dial only and reads the
// prelogin / login responses without a deadline, so a host that accepts
// the connection and never answers hung TestConnection forever. The dial
// context of go-mssqldb ends when its connect() returns (deferred cancel,
// context.Canceled) or when the dial timeout / the caller's deadline
// expires first (context.DeadlineExceeded); only in the latter case the
// socket gets a past deadline, so blocked reads fail with a timeout
// (ConnCategory "timeout"). Established sessions are not affected.
type handshakeDialer struct{ nd net.Dialer }

func (d handshakeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := d.nd.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	if ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				_ = conn.SetDeadline(time.Unix(1, 0))
			}
		}()
	}
	return conn, nil
}
