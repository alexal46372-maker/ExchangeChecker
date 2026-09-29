package sqlcheck

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func connProfile() model.Profile {
	return model.Profile{ID: "p1", Server: "10.0.0.5", Database: "buh", AuthMode: model.AuthSQL, Login: "sa", Enabled: true}
}

const secretPw = "S3cr#t;pa ss'wd"

func TestCHK_SQL_01_DriverConfig(t *testing.T) {
	cfg, err := driverConfig(connProfile(), secretPw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "10.0.0.5" || cfg.Port != 1433 || cfg.Database != "buh" || cfg.User != "sa" || cfg.Password != secretPw {
		t.Errorf("basic cfg: host=%s port=%d db=%s user=%s", cfg.Host, cfg.Port, cfg.Database, cfg.User)
	}
	if cfg.AppName != AppName || cfg.DialTimeout != 15*time.Second {
		t.Errorf("app=%s dial=%v", cfg.AppName, cfg.DialTimeout)
	}
	// phase 2d (customer decision): unset encrypt = disable, no TLS at all
	if cfg.Encryption != msdsn.EncryptionDisabled || cfg.TLSConfig != nil {
		t.Errorf("default encryption = %v (disable expected)", cfg.Encryption)
	}
	for k, v := range cfg.Parameters {
		if strings.Contains(v, secretPw) || strings.Contains(k, secretPw) {
			t.Fatal("password leaked into DSN parameters")
		}
	}
	if strings.Contains(fmt.Sprint(cfg.Parameters), secretPw) {
		t.Fatal("password leaked")
	}

	p := connProfile()
	p.Server, p.Port = `sql01\SQL2005`, 0
	cfg, _ = driverConfig(p, "")
	if cfg.Host != "sql01" || cfg.Instance != "SQL2005" || cfg.Port != 0 {
		t.Errorf("instance: host=%s inst=%s port=%d (SQL Browser lookup expected)", cfg.Host, cfg.Instance, cfg.Port)
	}
	p.Port = 1500
	if cfg, _ = driverConfig(p, ""); cfg.Port != 1500 || cfg.Instance != "SQL2005" {
		t.Errorf("instance+port: %d %s", cfg.Port, cfg.Instance)
	}

	enc := []struct {
		mode  model.EncryptMode
		trust bool
		want  msdsn.Encryption
		skip  bool
	}{
		{model.EncryptOptional, false, msdsn.EncryptionOff, true},
		{model.EncryptDisable, false, msdsn.EncryptionDisabled, false},
		{model.EncryptOn, false, msdsn.EncryptionRequired, false},
		{model.EncryptOn, true, msdsn.EncryptionRequired, true},
		{model.EncryptStrict, true, msdsn.EncryptionStrict, false},
	}
	for _, c := range enc {
		p := connProfile()
		p.Encrypt, p.TrustServerCert = c.mode, c.trust
		cfg, err := driverConfig(p, "x")
		if err != nil {
			t.Fatalf("%s: %v", c.mode, err)
		}
		if cfg.Encryption != c.want {
			t.Errorf("%s: encryption %v, want %v", c.mode, cfg.Encryption, c.want)
		}
		if c.mode == model.EncryptDisable {
			if cfg.TLSConfig != nil {
				t.Error("disable: TLS config present")
			}
			continue
		}
		if cfg.TLSConfig.InsecureSkipVerify != c.skip {
			t.Errorf("%s trust=%v: InsecureSkipVerify=%v", c.mode, c.trust, cfg.TLSConfig.InsecureSkipVerify)
		}
	}

	p = connProfile()
	p.TLSMinVersion, p.Encrypt = model.TLSMin10, model.EncryptOn
	if cfg, _ = driverConfig(p, ""); cfg.TLSConfig.MinVersion != tls.VersionTLS10 {
		t.Errorf("legacy TLS 1.0: MinVersion=%x", cfg.TLSConfig.MinVersion)
	}
	p.TLSMinVersion, p.Encrypt = "", model.EncryptOptional
	if cfg, _ = driverConfig(p, ""); cfg.TLSConfig.MinVersion != 0 {
		t.Errorf("default MinVersion=%x (Go default expected)", cfg.TLSConfig.MinVersion)
	}
	p.ConnectTimeoutSec = 7
	if cfg, _ = driverConfig(p, ""); cfg.DialTimeout != 7*time.Second {
		t.Errorf("dial timeout %v", cfg.DialTimeout)
	}
}

func TestCHK_SQL_01_DriverConfigAuth(t *testing.T) {
	p := connProfile()
	p.AuthMode, p.Login = model.AuthWindows, ""
	cfg, err := driverConfig(p, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.User != "" || cfg.Password != "" || cfg.Parameters["authenticator"] != integratedAuthProvider {
		t.Errorf("windows (process identity): user=%q auth=%q", cfg.User, cfg.Parameters["authenticator"])
	}
	p.Login = `CORP\svc1c`
	cfg, _ = driverConfig(p, "pw")
	if cfg.User != `CORP\svc1c` || cfg.Password != "pw" {
		t.Errorf("windows explicit: %q", cfg.User)
	}
	p = connProfile()
	p.Login = `CORP\x`
	if _, err := driverConfig(p, ""); err == nil {
		t.Error("SQL login with backslash accepted")
	}
	p = connProfile()
	p.AuthMode = "kerberos"
	if _, err := driverConfig(p, ""); err == nil {
		t.Error("unknown auth accepted")
	}
	p = connProfile()
	p.Server = "  "
	if _, err := Open(p, ""); KindOf(err) != KindConfig {
		t.Errorf("empty server: %v", err)
	}
	p = connProfile()
	p.Encrypt = "maybe"
	if err := TestConnection(context.Background(), p, ""); KindOf(err) != KindConfig {
		t.Errorf("bad encrypt: %v", err)
	}
	if integratedAuthProvider != "winsspi" && integratedAuthProvider != "ntlm" {
		t.Error("provider")
	}
}

type fakeNetErr struct{}

func (fakeNetErr) Error() string   { return "dial tcp: connection refused" }
func (fakeNetErr) Timeout() bool   { return false }
func (fakeNetErr) Temporary() bool { return false }

type fakeSQLErr int32

func (e fakeSQLErr) Error() string         { return fmt.Sprintf("mssql: error %d", int32(e)) }
func (e fakeSQLErr) SQLErrorNumber() int32 { return int32(e) }

func TestCHK_SQL_01_ErrorClassify(t *testing.T) {
	var _ net.Error = fakeNetErr{}
	cases := []struct {
		err       error
		connPhase bool
		want      ErrorKind
	}{
		{context.DeadlineExceeded, false, KindTimeout},
		{fmt.Errorf("x: %w", context.Canceled), false, KindCanceled},
		{fakeNetErr{}, false, KindConn},
		{driver.ErrBadConn, false, KindConn},
		{fakeSQLErr(18456), false, KindConn},
		{fakeSQLErr(4060), false, KindConn},
		{fakeSQLErr(208), false, KindQuery},
		{errors.New("whatever"), true, KindConn},
		{errors.New("whatever"), false, KindQuery},
	}
	for _, c := range cases {
		err := classify("op", c.err, c.connPhase)
		if KindOf(err) != c.want || !errors.Is(err, c.err) {
			t.Errorf("classify(%v, %v) = %v (%s), want %s", c.err, c.connPhase, err, KindOf(err), c.want)
		}
	}
	if classify("op", nil, true) != nil || KindOf(nil) != "" || KindOf(errors.New("x")) != "" {
		t.Error("nil / foreign")
	}
	e := classify("connect", fakeSQLErr(18456), true)
	if !strings.Contains(e.Error(), "sqlcheck: connect (conn)") {
		t.Errorf("message %q", e.Error())
	}
	if classify("again", e, false) != e {
		t.Error("double wrap")
	}
}

func TestCHK_SQL_01_ReaderTest(t *testing.T) {
	st := newFake()
	r := NewDiscoveringReader(st, connProfile(), Options{})
	if err := r.Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	st.pingErr = fakeSQLErr(18456)
	if err := r.Test(context.Background()); KindOf(err) != KindConn {
		t.Errorf("login failed: %v", err)
	}
	st.pingErr = nil
	st.delay = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := r.Test(ctx); KindOf(err) != KindTimeout {
		t.Errorf("timeout: %v", err)
	}
	if err := (&StubReader{TestErr: errors.New("x")}).Test(context.Background()); err == nil {
		t.Error("stub")
	}
	if err := r.Close(); err != nil || !st.closed {
		t.Error("close")
	}
}
