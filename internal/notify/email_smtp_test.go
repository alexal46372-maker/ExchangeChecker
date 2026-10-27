package notify

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// fakeSMTP is a minimal SMTP listener for Dial/SendSMTP mode tests
// (CHK-SVC-08). Records whether AUTH was attempted.
type fakeSMTP struct {
	mode           string // none | starttls | tls
	cert           tls.Certificate
	mu             sync.Mutex
	auth           bool
	mailOK         bool
	gotFrom, gotTo string
}

func (f *fakeSMTP) sawAuth() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.auth
}

func (f *fakeSMTP) ok() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mailOK
}

func testTLSCert(t *testing.T, hosts ...string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "smtp.test.local"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     hosts,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return cert, pool
}

func serveFakeSMTP(t *testing.T, f *fakeSMTP) (addr string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if f.mode == model.EmailTLSImplicit {
			tconn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{f.cert}})
			if err := tconn.Handshake(); err != nil {
				return
			}
			conn = tconn
		}
		f.handle(conn)
	}()
	return ln.Addr().String(), func() { ln.Close(); <-done }
}

func (f *fakeSMTP) handle(conn net.Conn) {
	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)
	write := func(s string) {
		_, _ = bw.WriteString(s + "\r\n")
		_ = bw.Flush()
	}
	write("220 smtp.test.local ESMTP fake")
	tlsUp := f.mode == model.EmailTLSImplicit
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO"):
			write("250-smtp.test.local")
			if f.mode == model.EmailTLSStartTLS && !tlsUp {
				write("250-STARTTLS")
			}
			if tlsUp && f.mode != model.EmailTLSNone {
				write("250-AUTH PLAIN LOGIN")
			}
			write("250 HELP")
		case strings.HasPrefix(upper, "STARTTLS"):
			if f.mode != model.EmailTLSStartTLS || tlsUp {
				write("503 Bad sequence")
				continue
			}
			write("220 Ready to start TLS")
			tconn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{f.cert}})
			if err := tconn.Handshake(); err != nil {
				return
			}
			conn = tconn
			br = bufio.NewReader(conn)
			bw = bufio.NewWriter(conn)
			tlsUp = true
		case strings.HasPrefix(upper, "AUTH"):
			f.mu.Lock()
			f.auth = true
			f.mu.Unlock()
			if f.mode == model.EmailTLSNone || !tlsUp {
				write("503 AUTH not allowed")
				continue
			}
			write("235 Authentication successful")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			f.mu.Lock()
			f.gotFrom = line
			f.mu.Unlock()
			write("250 OK")
		case strings.HasPrefix(upper, "RCPT TO:"):
			f.mu.Lock()
			f.gotTo = line
			f.mu.Unlock()
			write("250 OK")
		case upper == "DATA":
			write("354 End data with <CR><LF>.<CR><LF>")
			for {
				l, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(l, "\r\n") == "." {
					break
				}
			}
			f.mu.Lock()
			f.mailOK = true
			f.mu.Unlock()
			write("250 OK")
		case upper == "QUIT":
			write("221 Bye")
			return
		case upper == "RSET" || upper == "NOOP":
			write("250 OK")
		default:
			write("502 Command not implemented")
		}
	}
}

func dialTo(lnAddr string) SMTPDialer {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", lnAddr)
	}
}

// TestCHK_SVC_08_SendSMTPModes — Dial + fake SMTP for none (no AUTH),
// starttls, and tls. Host is smtp.test.local (not localhost) so Go's
// PlainAuth would refuse AUTH without TLS if we called it on none.
func TestCHK_SVC_08_SendSMTPModes(t *testing.T) {
	cert, pool := testTLSCert(t, "smtp.test.local")
	tlsClient := &tls.Config{ServerName: "smtp.test.local", RootCAs: pool, MinVersion: tls.VersionTLS12}
	const (
		from = "bot@example.com"
		to   = "a@b.co"
		pass = "SECRET-smtp"
	)

	t.Run("none_no_auth", func(t *testing.T) {
		f := &fakeSMTP{mode: model.EmailTLSNone, cert: cert}
		addr, closeFn := serveFakeSMTP(t, f)
		defer closeFn()
		err := SendSMTP(context.Background(), dialTo(addr), nil, "smtp.test.local", 25,
			model.EmailTLSNone, from, pass, to, "subj", "body")
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		if f.sawAuth() {
			t.Fatal("none must not send AUTH (password ignored)")
		}
		if !f.ok() {
			t.Fatal("mail not accepted")
		}
	})

	t.Run("starttls_auth", func(t *testing.T) {
		f := &fakeSMTP{mode: model.EmailTLSStartTLS, cert: cert}
		addr, closeFn := serveFakeSMTP(t, f)
		defer closeFn()
		err := SendSMTP(context.Background(), dialTo(addr), tlsClient, "smtp.test.local", 587,
			model.EmailTLSStartTLS, from, pass, to, "subj", "body")
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		if !f.sawAuth() {
			t.Fatal("starttls must AUTH")
		}
		if !f.ok() {
			t.Fatal("mail not accepted")
		}
	})

	t.Run("tls_auth", func(t *testing.T) {
		f := &fakeSMTP{mode: model.EmailTLSImplicit, cert: cert}
		addr, closeFn := serveFakeSMTP(t, f)
		defer closeFn()
		err := SendSMTP(context.Background(), dialTo(addr), tlsClient, "smtp.test.local", 465,
			model.EmailTLSImplicit, from, pass, to, "subj", "body")
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		if !f.sawAuth() {
			t.Fatal("tls must AUTH")
		}
		if !f.ok() {
			t.Fatal("mail not accepted")
		}
	})

	t.Run("none_with_password_still_no_auth", func(t *testing.T) {
		// password present must still be ignored on none (Go would allow
		// AUTH on 127.0.0.1; we use non-localhost name + assert no AUTH).
		f := &fakeSMTP{mode: model.EmailTLSNone, cert: cert}
		addr, closeFn := serveFakeSMTP(t, f)
		defer closeFn()
		_ = pass
		err := SendSMTP(context.Background(), dialTo(addr), nil, "smtp.test.local", 25,
			model.EmailTLSNone, from, "EVEN-IF-SET", to, "subj", "body")
		if err != nil {
			t.Fatal(err)
		}
		if f.sawAuth() {
			t.Fatal("AUTH despite none")
		}
	})
}

func TestCHK_CFG_09_NeedsSMTPAuth(t *testing.T) {
	if model.NeedsSMTPAuth(model.EmailTLSNone) {
		t.Fatal("none")
	}
	if !model.NeedsSMTPAuth(model.EmailTLSStartTLS) || !model.NeedsSMTPAuth(model.EmailTLSImplicit) {
		t.Fatal("tls modes need auth")
	}
}

func TestCHK_CFG_09_EnabledNoneNoPassword(t *testing.T) {
	e := model.DefaultEmailSettings()
	e.Enabled = true
	e.Host, e.Port, e.From = "smtp.test.local", 25, "bot@example.com"
	e.TLSMode = model.EmailTLSNone
	e.Recipients = []model.EmailRecipient{{ID: "e1", Addr: "a@b.co", Name: "A"}}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
}
