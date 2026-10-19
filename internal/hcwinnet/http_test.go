package hcwinnet

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func rootsOf(s *httptest.Server) *tls.Config {
	p := x509.NewCertPool()
	p.AddCert(s.Certificate())
	return &tls.Config{RootCAs: p}
}

// TestCHK_SVC_06_HTTPSClient — TLS 1.2 minimum is forced, a server
// limited to TLS 1.1 and an untrusted certificate fail with ErrTLS, the
// response limit holds, headers and body are sent.
func TestCHK_SVC_06_HTTPSClient(t *testing.T) {
	var gotTok, gotBody string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTok = r.Header.Get("X-Test-Token")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		if strings.Contains(gotBody, "big") {
			_, _ = w.Write([]byte(strings.Repeat("x", 100)))
			return
		}
		_, _ = w.Write([]byte(`{"status":0}`))
	}))
	defer srv.Close()
	c := NewClient(5*time.Second, &tls.Config{MinVersion: tls.VersionTLS10, RootCAs: rootsOf(srv).RootCAs})
	if c.MinTLSVersion() != tls.VersionTLS12 {
		t.Fatalf("min TLS %x", c.MinTLSVersion())
	}
	st, b, err := c.PostJSON(context.Background(), srv.URL, map[string]string{"X-Test-Token": "t1"}, []byte(`{"a":1}`), 1024)
	if err != nil || st != 200 || string(b) != `{"status":0}` || gotTok != "t1" || gotBody != `{"a":1}` {
		t.Fatalf("post %d %q %v", st, b, err)
	}
	if _, _, err := c.PostJSON(context.Background(), srv.URL, nil, []byte(`"big"`), 10); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("limit %v", err)
	}
	// untrusted certificate
	if _, _, err := NewClient(5*time.Second, nil).PostJSON(context.Background(), srv.URL, nil, nil, 10); !errors.Is(err, ErrTLS) {
		t.Fatalf("untrusted %v", err)
	}
	// TLS 1.1-only server
	old := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	old.TLS = &tls.Config{MaxVersion: tls.VersionTLS11}
	old.StartTLS()
	defer old.Close()
	if _, _, err := NewClient(5*time.Second, rootsOf(old)).PostJSON(context.Background(), old.URL, nil, nil, 10); !errors.Is(err, ErrTLS) {
		t.Fatalf("TLS 1.1 server %v", err)
	}
	// connection refused
	if _, _, err := c.PostJSON(context.Background(), "https://127.0.0.1:1/", nil, nil, 10); !errors.Is(err, ErrNetwork) {
		t.Fatalf("refused %v", err)
	}
}
