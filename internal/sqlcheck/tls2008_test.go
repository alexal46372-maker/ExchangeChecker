package sqlcheck

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// CHK-SQL-05: SQL Server 2008 preset, TLS 1.0 for servers without the
// TLS 1.2 update, localizable TLS error keys.

func TestCHK_SQL_05_Preset2008(t *testing.T) {
	p := connProfile()
	if err := p.ApplyServerPreset(model.ServerPresetMSSQL2008); err != nil {
		t.Fatal(err)
	}
	if p.ServerPreset != "mssql2008" || p.TLSMinVersion != model.TLSMin10 || p.Encrypt != model.EncryptDefault || p.Validate() != nil {
		t.Fatalf("preset: %+v", p)
	}
	cfg, err := driverConfig(p, "")
	if err != nil {
		t.Fatal(err)
	}
	// driver default encryption (login-only TLS), cert not validated, TLS 1.0 allowed
	if cfg.Encryption != msdsn.EncryptionOff || cfg.TLSConfig == nil || cfg.TLSConfig.MinVersion != tls.VersionTLS10 || !cfg.TLSConfig.InsecureSkipVerify {
		t.Errorf("driver config %v %+v", cfg.Encryption, cfg.TLSConfig)
	}
	// preset without an explicit TLSMinVersion still means TLS 1.0
	q := connProfile()
	q.ServerPreset = model.ServerPresetMSSQL2008
	if cfg, _ := driverConfig(q, ""); q.EffectiveTLSMinVersion() != "1.0" || cfg.TLSConfig.MinVersion != tls.VersionTLS10 {
		t.Errorf("implicit preset TLS: %q", q.EffectiveTLSMinVersion())
	}
	// an explicit choice wins; strict is replaced; unknown presets fail
	q = connProfile()
	q.TLSMinVersion, q.Encrypt = model.TLSMin12, model.EncryptStrict
	if err := q.ApplyServerPreset(model.ServerPresetMSSQL2008); err != nil || q.TLSMinVersion != "1.2" || q.Encrypt != model.EncryptDefault {
		t.Errorf("explicit TLS / strict: %+v %v", q, err)
	}
	if err := q.ApplyServerPreset("mssql2000"); err == nil {
		t.Error("unknown preset accepted")
	}
	q.Encrypt = model.EncryptStrict
	if err := q.Validate(); err == nil {
		t.Error("strict + mssql2008 must not validate")
	}
	q.ServerPreset, q.Encrypt = "sql7", ""
	if err := q.Validate(); err == nil {
		t.Error("unknown serverPreset must not validate")
	}
	// encrypt=disable: no TLS config at all (prelogin ENCRYPT_NOT_SUP)
	d := connProfile()
	d.ServerPreset, d.Encrypt = model.ServerPresetMSSQL2008, model.EncryptDisable
	if cfg, _ := driverConfig(d, ""); cfg.Encryption != msdsn.EncryptionDisabled || cfg.TLSConfig != nil {
		t.Errorf("disable: %v %+v", cfg.Encryption, cfg.TLSConfig)
	}
	// JSON: omitted when empty, kept when set
	b, _ := json.Marshal(connProfile())
	if strings.Contains(string(b), "serverPreset") {
		t.Errorf("omitempty: %s", b)
	}
	b, _ = json.Marshal(p)
	if !strings.Contains(string(b), `"serverPreset":"mssql2008"`) || !strings.Contains(string(b), `"tlsMinVersion":"1.0"`) {
		t.Errorf("json: %s", b)
	}
	if Message(MsgPresetMSSQL2008, "ru") == "" || !strings.Contains(Message(MsgPresetMSSQL2008, "en"), "TLS 1.0") {
		t.Error("preset hint")
	}
}

func TestCHK_SQL_05_TLSErrorKeys(t *testing.T) {
	cases := map[string]string{
		"TLS Handshake failed: remote error: tls: protocol version not supported":     MsgTLSEnableTLS10,
		"TLS Handshake failed: tls: server selected unsupported protocol version 301": MsgTLSEnableTLS10,
		"TLS Handshake failed: remote error: tls: handshake failure":                  MsgTLSEnableTLS10,
		"TLS Handshake failed: EOF":                                                    MsgTLSEnableTLS10,
		"cannot read handshake packet: EOF":                                            MsgTLSEnableTLS10,
		"TLS Handshake failed: x509: certificate signed by unknown authority":          MsgTLSCertificate,
		"TLS Handshake failed: tls: failed to verify certificate: x509: name mismatch": MsgTLSCertificate,
		"mssql: login error: Login failed for user 'sa'.":                              "",
		"dial tcp 10.0.0.5:1433: connect: connection refused":                          "",
		"mssql: Invalid object name 'dbo.x'.":                                          "",
	}
	for msg, want := range cases {
		err := classify("connect", errors.New(msg), true)
		if got := MessageKeyOf(err); got != want {
			t.Errorf("%q: key %q, want %q", msg, got, want)
		}
		if want != "" && (KindOf(err) != KindConn || !strings.Contains(err.Error(), "["+want+"]")) {
			t.Errorf("%q: %v", msg, err)
		}
	}
	// typed crypto/tls error, also outside the connect phase
	if err := classify("points", fmt.Errorf("read: %w", tls.RecordHeaderError{Msg: "bad"}), false); KindOf(err) != KindConn || MessageKeyOf(err) != MsgTLSEnableTLS10 {
		t.Errorf("record header: %v", err)
	}
	// TLS 1.0 already allowed -> "legacy failed" (encrypt=disable / update the server)
	p := connProfile()
	p.ServerPreset = model.ServerPresetMSSQL2008
	tlsErr := func() error {
		return classify("connect", errors.New("TLS Handshake failed: remote error: tls: handshake failure"), true)
	}
	if got := MessageKeyOf(refineTLS(tlsErr(), p)); got != MsgTLSLegacyFailed {
		t.Errorf("refined: %q", got)
	}
	if got := MessageKeyOf(refineTLS(tlsErr(), connProfile())); got != MsgTLSEnableTLS10 {
		t.Errorf("not refined: %q", got)
	}
	// Test() and Status() carry the key
	st := newFake(fx1CNode(0)...)
	st.pingErr = errors.New("TLS Handshake failed: EOF")
	st.fail = map[string]error{"_Node123": errors.New("TLS Handshake failed: EOF")}
	lp := linkedProfile(0)
	if err := NewDiscoveringReader(st, lp, fixOpt()).Test(context.Background()); MessageKeyOf(err) != MsgTLSEnableTLS10 {
		t.Errorf("Test: %v", err)
	}
	ps := Status(context.Background(), NewDiscoveringReader(st, lp, fixOpt()), lp, fixNow)
	if ps.ErrorKey != MsgTLSEnableTLS10 || ps.Level() != model.LevelError {
		t.Errorf("status %+v", ps)
	}
	// RU and EN texts for every key; EN is used for "en", RU otherwise
	for _, k := range MessageKeys() {
		ru, en := Message(k, model.LangRU), Message(k, model.LangEN)
		if ru == "" || en == "" || ru == en || Message(k, "de") != ru {
			t.Errorf("%s: ru %q en %q", k, ru, en)
		}
	}
	if !strings.Contains(Message(MsgTLSEnableTLS10, "ru"), "TLS 1.0") || !strings.Contains(Message(MsgTLSEnableTLS10, "en"), "enable TLS 1.0") {
		t.Error("tls hint text")
	}
	if Message("sql.unknown", "ru") != "" {
		t.Error("unknown key")
	}
}

// legacyCert is a self-signed RSA-1024 certificate like the one SQL Server
// 2008 generates when no certificate is configured.
func legacyCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "SSL_Self_Signed_Fallback"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// handshake runs a client handshake with cfg against an in-memory TLS 1.0
// server offering only suites (nil = Go defaults) and returns the state.
func handshake(t *testing.T, cert tls.Certificate, suites []uint16, cfg *tls.Config) (tls.ConnectionState, error) {
	t.Helper()
	cs, ss := net.Pipe()
	defer cs.Close()
	defer ss.Close()
	srv := tls.Server(ss, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS10,
		MaxVersion: tls.VersionTLS10, CipherSuites: suites})
	done := make(chan struct{})
	go func() { _ = srv.Handshake(); ss.Close(); close(done) }()
	cli := tls.Client(cs, cfg)
	_ = cs.SetDeadline(time.Now().Add(10 * time.Second))
	err := cli.Handshake()
	state := cli.ConnectionState()
	cs.Close()
	<-done
	return state, err
}

func TestCHK_SQL_05_GoTLS10CipherSuites(t *testing.T) {
	cert := legacyCert(t)
	legacy := connProfile()
	legacy.ServerPreset = model.ServerPresetMSSQL2008
	lc, err := driverConfig(legacy, "")
	if err != nil {
		t.Fatal(err)
	}
	dc, _ := driverConfig(connProfile(), "") // no preset: Go 1.20 default MinVersion (TLS 1.2)

	// 1. without the preset a TLS 1.0-only server fails, with a classified key
	if _, err := handshake(t, cert, nil, dc.TLSConfig.Clone()); err == nil {
		t.Fatal("TLS 1.0 server accepted with the Go default MinVersion")
	} else if k := MessageKeyOf(classify("connect", fmt.Errorf("TLS Handshake failed: %v", err), true)); k != MsgTLSEnableTLS10 {
		t.Errorf("handshake error %v -> key %q", err, k)
	}
	// 2. Windows Server 2008 Schannel (AES-CBC-SHA, 3DES, RC4; also ECDHE)
	st, err := handshake(t, cert, nil, lc.TLSConfig.Clone())
	if err != nil || st.Version != tls.VersionTLS10 {
		t.Fatalf("TLS 1.0 with preset: %v %x", err, st.Version)
	}
	t.Logf("default server suites: %s", tls.CipherSuiteName(st.CipherSuite))
	for name, c := range map[string]struct {
		suites []uint16
		want   uint16
	}{
		"rsa aes-cbc-sha (2008 w/o ECDHE)":   {[]uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA, tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA, tls.TLS_RSA_WITH_RC4_128_SHA}, tls.TLS_RSA_WITH_AES_128_CBC_SHA},
		"aes256":                             {[]uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA}, tls.TLS_RSA_WITH_AES_256_CBC_SHA},
		"3des + rc4 (Windows 2003 Schannel)": {[]uint16{tls.TLS_RSA_WITH_RC4_128_SHA, tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA}, tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA},
	} {
		st, err := handshake(t, cert, c.suites, lc.TLSConfig.Clone())
		if err != nil || st.CipherSuite != c.want {
			t.Errorf("%s: %v %s", name, err, tls.CipherSuiteName(st.CipherSuite))
		}
	}
	// 3. RC4-only servers: Go 1.20 does not offer RC4 -> handshake failure
	if _, err := handshake(t, cert, []uint16{tls.TLS_RSA_WITH_RC4_128_SHA}, lc.TLSConfig.Clone()); err == nil {
		t.Error("RC4-only server accepted")
	} else if k := MessageKeyOf(refineTLS(classify("connect", fmt.Errorf("TLS Handshake failed: %v", err), true), legacy)); k != MsgTLSLegacyFailed {
		t.Errorf("rc4 only: %v -> %q", err, k)
	}
}
