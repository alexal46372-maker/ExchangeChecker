package hcwinnet

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Phase 10 (CHK-SVC-06, PLAN §12 п. 131): outgoing HTTPS of the service
// (Viber) with the standard library inside this package (POLICY §4: all
// network I/O through hcwinnet; no helper process). TLS 1.2 minimum, the
// certificate is verified with the system roots (Windows: the machine's
// certificate store), no pinning; proxy from the environment
// (http.ProxyFromEnvironment: HTTPS_PROXY / NO_PROXY of the service).

// Error kinds of PostJSON.
var (
	// ErrTLS: TLS handshake / certificate verification failed (Win7
	// without current root certificates: update the root certificates).
	ErrTLS = errors.New("hcwinnet: TLS error")
	// ErrNetwork: connect / DNS / proxy / timeout / read failure.
	ErrNetwork = errors.New("hcwinnet: network error")
	// ErrResponseTooLarge: the response body exceeds the limit.
	ErrResponseTooLarge = errors.New("hcwinnet: response too large")
)

// Client is an HTTPS client (TLS >= 1.2).
type Client struct {
	h *http.Client
}

// NewClient returns a client with the request timeout; tlsCfg nil = the
// system roots (tests pass the fake server's roots). MinVersion is always
// forced to TLS 1.2 or higher.
func NewClient(timeout time.Duration, tlsCfg *tls.Config) *Client {
	cfg := &tls.Config{}
	if tlsCfg != nil {
		cfg = tlsCfg.Clone()
	}
	if cfg.MinVersion < tls.VersionTLS12 {
		cfg.MinVersion = tls.VersionTLS12
	}
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSClientConfig:       cfg,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          2,
		IdleConnTimeout:       90 * time.Second,
	}
	return &Client{h: &http.Client{Transport: tr, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// MinTLSVersion is the effective minimum TLS version (tests).
func (c *Client) MinTLSVersion() uint16 {
	return c.h.Transport.(*http.Transport).TLSClientConfig.MinVersion
}

// PostJSON posts body (application/json) with the headers and returns the
// HTTP status and the response body (at most maxResp bytes). Errors wrap
// ErrTLS or ErrNetwork.
func (c *Client) PostJSON(ctx context.Context, url string, headers map[string]string, body []byte, maxResp int64) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("hcwinnet: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.h.Do(req)
	if err != nil {
		return 0, nil, classify(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResp+1))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("%w: read: %v", ErrNetwork, err)
	}
	if int64(len(b)) > maxResp {
		return resp.StatusCode, nil, ErrResponseTooLarge
	}
	return resp.StatusCode, b, nil
}

// classify maps a transport error to ErrTLS / ErrNetwork (the message
// never contains request headers).
func classify(err error) error {
	var (
		ua  x509.UnknownAuthorityError
		ci  x509.CertificateInvalidError
		he  x509.HostnameError
		rh  tls.RecordHeaderError
		cve *tls.CertificateVerificationError
	)
	// a TLS alert (e.g. protocol version) has no exported type in Go 1.20:
	// recognized by the "tls: " prefix of crypto/tls messages
	if errors.As(err, &ua) || errors.As(err, &ci) || errors.As(err, &he) || errors.As(err, &rh) ||
		errors.As(err, &cve) || strings.Contains(err.Error(), "tls: ") {
		return fmt.Errorf("%w: %v", ErrTLS, err)
	}
	return fmt.Errorf("%w: %v", ErrNetwork, err)
}
