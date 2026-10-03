package sqlcheck

import (
	"bytes"
	"context"
	"net"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// silentProxy forwards TCP to the SQL server until the client sends a
// packet containing marker (UCS-2, readable with encrypt=disable); from
// then on it forwards nothing in either direction but keeps the sockets
// open: the server "goes silent" after login.
type silentProxy struct {
	ln       net.Listener
	target   string
	marker   []byte
	except   []byte // a packet with marker that also contains except is forwarded
	drop     bool   // on marker: close both sockets at once instead of going silent
	dropped  int
	mu       sync.Mutex
	conns    []net.Conn
	silenced int
	closed   chan struct{} // one value per client connection closed by the client
	wg       sync.WaitGroup
}

func newSilentProxy(t *testing.T, target, marker string) *silentProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	px := &silentProxy{ln: ln, target: target, closed: make(chan struct{}, 16)}
	if marker != "" {
		px.marker = ucs2(marker)
	}
	px.wg.Add(1)
	go px.serve()
	t.Cleanup(func() { px.ln.Close(); px.closeAll(); px.wg.Wait() })
	return px
}

func (px *silentProxy) port() int { return px.ln.Addr().(*net.TCPAddr).Port }

func (px *silentProxy) closeAll() {
	px.mu.Lock()
	defer px.mu.Unlock()
	for _, c := range px.conns {
		c.Close()
	}
}

func (px *silentProxy) serve() {
	defer px.wg.Done()
	for {
		c, err := px.ln.Accept()
		if err != nil {
			return
		}
		s, err := net.Dial("tcp", px.target)
		if err != nil {
			c.Close()
			continue
		}
		px.mu.Lock()
		px.conns = append(px.conns, c, s)
		px.mu.Unlock()
		var silent sync.Once
		quiet := make(chan struct{})
		px.wg.Add(2)
		go func() { // client -> server
			defer px.wg.Done()
			buf := make([]byte, 32<<10)
			for {
				n, err := c.Read(buf)
				if err != nil {
					px.closed <- struct{}{}
					s.Close()
					return
				}
				select {
				case <-quiet:
					continue // silent: swallow
				default:
				}
				hit := px.marker != nil && bytes.Contains(buf[:n], px.marker) &&
					(px.except == nil || !bytes.Contains(buf[:n], px.except))
				if hit && px.drop {
					px.mu.Lock()
					px.dropped++
					px.mu.Unlock()
					c.Close()
					s.Close()
					continue // the next Read fails: counted as closed
				}
				if hit {
					silent.Do(func() {
						px.mu.Lock()
						px.silenced++
						px.mu.Unlock()
						close(quiet)
					})
					continue
				}
				if _, err := s.Write(buf[:n]); err != nil {
					return
				}
			}
		}()
		go func() { // server -> client
			defer px.wg.Done()
			buf := make([]byte, 32<<10)
			for {
				n, err := s.Read(buf)
				if err != nil {
					return
				}
				select {
				case <-quiet:
					continue
				default:
				}
				if _, err := c.Write(buf[:n]); err != nil {
					return
				}
			}
		}()
	}
}

func (px *silentProxy) waitClosed(t *testing.T, what string) {
	t.Helper()
	select {
	case <-px.closed:
	case <-time.After(3 * time.Second):
		t.Errorf("%s: the client socket is still open", what)
	}
}

func silentProfile(t *testing.T, px *silentProxy, p model.Profile) model.Profile {
	t.Helper()
	q := p
	q.Server, q.Port = "127.0.0.1", px.port()
	q.Encrypt = model.EncryptDefault // disable (phase 2d): the marker is readable
	return q
}

// TestCHK_SQL_01_IntegrationSilentAfterLogin: the server completes the
// login and then stops answering (at Ping or at the server-info query).
// TestConnectionInfo returns within its budget (2 x connect timeout for
// Ping, query + connect timeout for the info query, or the caller's
// cancel) with a timeout / cancel error, the socket is closed and no
// goroutine is left behind.
func TestCHK_SQL_01_IntegrationSilentAfterLogin(t *testing.T) {
	p, pw := itProfile(t, "ec_1c_plans_off")
	target := net.JoinHostPort(p.Server, strconv.Itoa(p.Port))
	// control: a transparent proxy works
	px := newSilentProxy(t, target, "")
	if info, err := TestConnectionInfo(itCtx(t), silentProfile(t, px, p), pw); err != nil || info.CompatLevel != itCompatLevel {
		t.Fatalf("control: %+v %v", info, err)
	}
	px.waitClosed(t, "control")

	base := runtime.NumGoroutine()
	for _, c := range []struct {
		name       string
		marker     string
		connectSec int
		querySec   int
		cancel     time.Duration // caller cancel after this delay (0: none)
		max        time.Duration
		category   string
	}{
		{"silent at ping", "select 1", 1, 1, 0, 2*time.Second + 1500*time.Millisecond, ConnCategoryTimeout},
		{"silent at server info", "SERVERPROPERTY", 1, 1, 0, 2*time.Second + 1500*time.Millisecond, ConnCategoryTimeout},
		{"caller cancel", "select 1", 30, 30, 700 * time.Millisecond, 700*time.Millisecond + 1500*time.Millisecond, ConnCategoryOther},
	} {
		px := newSilentProxy(t, target, c.marker)
		q := silentProfile(t, px, p)
		q.ConnectTimeoutSec, q.QueryTimeoutSec = c.connectSec, c.querySec
		ctx, cancel := context.WithCancel(context.Background())
		if c.cancel > 0 {
			time.AfterFunc(c.cancel, cancel)
		}
		start := time.Now()
		_, err := TestConnectionInfo(ctx, q, pw)
		d := time.Since(start)
		cancel()
		if err == nil || d > c.max || ConnCategory(err) != c.category {
			t.Errorf("%s: %v after %v (max %v) -> %q", c.name, err, d, c.max, ConnCategory(err))
		}
		px.mu.Lock()
		silenced := px.silenced
		px.mu.Unlock()
		if silenced != 1 {
			t.Errorf("%s: the proxy went silent %d times", c.name, silenced)
		}
		px.waitClosed(t, c.name)
		px.ln.Close()
		px.closeAll()
		px.wg.Wait()
		t.Logf("%s: returned after %v: %v", c.name, d.Round(10*time.Millisecond), err)
	}
	settleGoroutines(t, base)
}

// TestCHK_SQL_01_IntegrationDriverIgnoresCtxAfterLogin documents the
// go-mssqldb v1.8.2 behaviour behind sessionHold: on a poll store (Open,
// no hold) a query whose context expires on a silent server does not
// return (ATTENTION is sent, then the driver waits for the confirmation
// with a blocking read). Polling relies on the query timeout of the
// server plus the monitor's hung-reader handling (CHK-MON-13); the call
// is released here by closing the proxy sockets.
func TestCHK_SQL_01_IntegrationDriverIgnoresCtxAfterLogin(t *testing.T) {
	p, pw := itProfile(t, "ec_1c_plans_off")
	px := newSilentProxy(t, net.JoinHostPort(p.Server, strconv.Itoa(p.Port)), "ec_silent_marker")
	q := silentProfile(t, px, p)
	st, err := Open(q, pw)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Ping(itCtx(t)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		var s string
		done <- st.DB().QueryRowContext(ctx, "SELECT N'ec_silent_marker'").Scan(&s)
	}()
	select {
	case err := <-done:
		t.Logf("the driver honoured the context after login (go-mssqldb changed?): %v", err)
		return
	case <-time.After(3 * time.Second):
		t.Logf("confirmed: the query is still blocked 2 s after its context expired")
	}
	px.closeAll()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the query did not return after the sockets were closed")
	}
}

// TestCHK_SQL_01_IntegrationServerInfoFallbackDropped: a login denied
// SELECT on sys.databases gets a query error for the info query, then the
// SERVERPROPERTY fallback runs. Through a transparent proxy the fallback
// succeeds (version, CompatLevel 0, no error). When the proxy drops the
// session at the fallback, TestConnectionInfo returns an error with
// ConnCategory "network" (or "timeout"), not a success with CompatLevel 0.
func TestCHK_SQL_01_IntegrationServerInfoFallbackDropped(t *testing.T) {
	p, _ := itProfile(t, "ec_1c_plans_off")
	ctx := itCtx(t)
	const login, lpw = "ec_reader_fb", "Ec_Reader-fbPassw0rd!"
	admin := itStore(t, "master")
	exec := func(q string) {
		t.Helper()
		if _, err := admin.DB().ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	drop := []string{
		"USE [ec_1c_plans_off]; IF USER_ID(N'" + login + "') IS NOT NULL DROP USER [" + login + "]",
		"USE [master]; IF USER_ID(N'" + login + "') IS NOT NULL DROP USER [" + login + "]",
		"USE [master]; IF SUSER_ID(N'" + login + "') IS NOT NULL DROP LOGIN [" + login + "]",
	}
	for _, q := range drop {
		exec(q)
	}
	t.Cleanup(func() {
		for _, q := range drop {
			_, _ = admin.DB().ExecContext(context.Background(), q)
		}
	})
	exec("USE [master]; CREATE LOGIN [" + login + "] WITH PASSWORD = N'" + lpw + "', CHECK_POLICY = OFF")
	exec("USE [ec_1c_plans_off]; CREATE USER [" + login + "] FOR LOGIN [" + login + "]; EXEC sp_addrolemember N'db_datareader', N'" + login + "'")
	exec("USE [master]; CREATE USER [" + login + "] FOR LOGIN [" + login + "]; DENY SELECT ON sys.databases TO [" + login + "]")
	r := p
	r.Login = login
	target := net.JoinHostPort(p.Server, strconv.Itoa(p.Port))

	// fallback succeeds: connection OK, version, CompatLevel 0, no error
	px := newSilentProxy(t, target, "")
	info, err := TestConnectionInfo(ctx, silentProfile(t, px, r), lpw)
	if err != nil || info.Version == "" || info.Edition == "" || info.CompatLevel != 0 {
		t.Fatalf("fallback ok: %+v %v", info, err)
	}
	px.waitClosed(t, "fallback ok")

	// the session is dropped at the fallback (SERVERPROPERTY without sys.databases)
	px = newSilentProxy(t, target, "SERVERPROPERTY")
	px.except, px.drop = ucs2("sys.databases"), true
	q := silentProfile(t, px, r)
	q.ConnectTimeoutSec, q.QueryTimeoutSec = 2, 2
	start := time.Now()
	info, err = TestConnectionInfo(ctx, q, lpw)
	d := time.Since(start)
	px.mu.Lock()
	dropped := px.dropped
	px.mu.Unlock()
	cat := ConnCategory(err)
	if err == nil || info != (ServerInfo{}) || (cat != ConnCategoryNetwork && cat != ConnCategoryTimeout) || KindOf(err) == KindQuery {
		t.Errorf("fallback dropped: %+v %v kind %q -> %q", info, err, KindOf(err), cat)
	}
	if dropped < 1 {
		t.Errorf("fallback dropped: the proxy saw no fallback query")
	}
	if d > 6*time.Second {
		t.Errorf("fallback dropped: returned after %v", d)
	}
	t.Logf("fallback dropped (%d session(s)): %v after %v -> %q", dropped, err, d.Round(10*time.Millisecond), cat)
}
