package sqlcheck

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	mssql "github.com/microsoft/go-mssqldb"
)

// Phase 4 IPC test_profile helpers: TestConnectionInfo / ServerInfo and
// ConnCategory (PLAN.md §12 п. 32).

// infoDriver is a minimal database/sql driver that answers every query
// with one row of fixed values and records the query texts.
type infoDriver struct {
	row     []driver.Value
	err     error
	queries *[]string
}

type infoConn struct{ d *infoDriver }
type infoStmt struct {
	d *infoDriver
	q string
}
type infoRows struct {
	row  []driver.Value
	done bool
}

func (d *infoDriver) Open(string) (driver.Conn, error) { return &infoConn{d}, nil }
func (c *infoConn) Prepare(q string) (driver.Stmt, error) {
	return &infoStmt{c.d, q}, nil
}
func (c *infoConn) Close() error              { return nil }
func (c *infoConn) Begin() (driver.Tx, error) { return nil, errors.New("no tx") }
func (s *infoStmt) Close() error              { return nil }
func (s *infoStmt) NumInput() int             { return -1 }
func (s *infoStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("no exec")
}
func (s *infoStmt) Query([]driver.Value) (driver.Rows, error) {
	*s.d.queries = append(*s.d.queries, s.q)
	if s.d.err != nil {
		return nil, s.d.err
	}
	return &infoRows{row: s.d.row}, nil
}
func (r *infoRows) Columns() []string { return []string{"v", "l", "e", "c"} }
func (r *infoRows) Close() error      { return nil }
func (r *infoRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, r.row)
	return nil
}

var infoDriverN int

func infoStore(t *testing.T, row []driver.Value, err error) (*SQLStore, *[]string) {
	t.Helper()
	var qs []string
	infoDriverN++
	name := fmt.Sprintf("ecinfo%d", infoDriverN)
	sql.Register(name, &infoDriver{row: row, err: err, queries: &qs})
	db, e := sql.Open(name, "")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return NewSQLStore(db, time.Second, time.Second), &qs
}

func TestCHK_SQL_05_ServerInfoQuery(t *testing.T) {
	if v := Compat2008Violations(serverInfoSQL); len(v) != 0 {
		t.Errorf("server info SQL is not SQL Server 2008 T-SQL: %v", v)
	}
	for _, tk := range sqlTokens(blankSQL(serverInfoSQL, false)) {
		if tk == "ISOLATION" || manualForbidden[tk] {
			t.Errorf("server info SQL contains %s", tk)
		}
	}
	for _, want := range []string{"SERVERPROPERTY('ProductVersion')", "SERVERPROPERTY('ProductLevel')", "SERVERPROPERTY('Edition')",
		"compatibility_level FROM sys.databases WHERE database_id = DB_ID()"} {
		if !strings.Contains(serverInfoSQL, want) {
			t.Errorf("server info SQL lacks %s", want)
		}
	}
	// values are scanned; exactly one query, no session SET before it
	st, qs := infoStore(t, []driver.Value{"10.50.1600.1", "RTM", "Standard Edition (64-bit)", int64(100)}, nil)
	info, err := st.ServerInfo(context.Background())
	want := ServerInfo{Version: "10.50.1600.1", ProductLevel: "RTM", Edition: "Standard Edition (64-bit)", CompatLevel: 100}
	if err != nil || info != want {
		t.Errorf("info %+v %v", info, err)
	}
	if len(*qs) != 1 || (*qs)[0] != serverInfoSQL {
		t.Errorf("queries %q", *qs)
	}
	// NULLs (no access to sys.databases, unknown property) -> "" / 0, no error
	st, _ = infoStore(t, []driver.Value{nil, nil, nil, nil}, nil)
	if info, err := st.ServerInfo(context.Background()); err != nil || info != (ServerInfo{}) {
		t.Errorf("NULL info %+v %v", info, err)
	}
	st, _ = infoStore(t, []driver.Value{"15.0.4000.5", "RTM", "Developer Edition (64-bit)", nil}, nil)
	if info, err := st.ServerInfo(context.Background()); err != nil || info.CompatLevel != 0 || info.Version != "15.0.4000.5" {
		t.Errorf("NULL compat %+v %v", info, err)
	}
	// a query error is classified (KindQuery)
	st, _ = infoStore(t, nil, mssql.Error{Number: 229, Message: "The SELECT permission was denied"})
	if _, err := st.ServerInfo(context.Background()); KindOf(err) != KindQuery {
		t.Errorf("denied: %v", err)
	}
	if v := Compat2008Violations(serverPropsSQL); len(v) != 0 || !strings.HasPrefix(serverInfoSQL, serverPropsSQL) {
		t.Errorf("fallback SQL: %v", v)
	}
}

type ciNetErr struct{ timeout bool }

func (e ciNetErr) Error() string   { return "fake net error" }
func (e ciNetErr) Timeout() bool   { return e.timeout }
func (e ciNetErr) Temporary() bool { return false }

func TestCHK_SQL_01_ConnCategory(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("outer: %w", classify("connect", err, true)) }
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"login 18456", mssql.Error{Number: 18456, Message: "login error: Login failed for user 'x'."}, "login"},
		{"login 18452", mssql.Error{Number: 18452}, "login"},
		{"login 18486", mssql.Error{Number: 18486}, "login"},
		{"login 18487 wrapped", wrap(mssql.Error{Number: 18487}), "login"},
		{"login 18488 pointer", &mssql.Error{Number: 18488}, "login"},
		{"database 4060", mssql.Error{Number: 4060, Message: "Cannot open database"}, "database"},
		{"database 4064 wrapped", wrap(mssql.Error{Number: 4064}), "database"},
		{"database 4063 (2019+)", mssql.Error{Number: 4063}, "database"},
		{"database in All", mssql.Error{Number: 0, All: []mssql.Error{{Number: 5701}, {Number: 4060}}}, "database"},
		{"tls key enable", &Error{Kind: KindConn, Op: "connect", Err: errors.New("x"), Key: MsgTLSEnableTLS10}, "tls"},
		{"tls key legacy", &Error{Kind: KindConn, Op: "connect", Err: errors.New("x"), Key: MsgTLSLegacyFailed}, "tls"},
		{"tls classified", wrap(errors.New("TLS Handshake failed: tls: protocol version not supported")), "tls"},
		{"tls certificate", wrap(errors.New("TLS Handshake failed: x509: certificate signed by unknown authority")), "tls"},
		{"tls raw", errors.New("TLS Handshake failed: EOF"), "tls"},
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"deadline wrapped", wrap(fmt.Errorf("dial: %w", context.DeadlineExceeded)), "timeout"},
		{"kind timeout", &Error{Kind: KindTimeout, Op: "connect", Err: errors.New("x")}, "timeout"},
		{"net timeout", &net.OpError{Op: "dial", Net: "tcp", Err: ciNetErr{timeout: true}}, "timeout"},
		{"os deadline", fmt.Errorf("read: %w", os.ErrDeadlineExceeded), "timeout"},
		{"io timeout text", errors.New("unable to open tcp connection with host '10.0.0.5:1433': dial tcp 10.0.0.5:1433: i/o timeout"), "timeout"},
		{"refused", &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}, "network"},
		{"refused errno", fmt.Errorf("x: %w", syscall.ECONNREFUSED), "network"},
		{"refused wrapped", wrap(&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}), "network"},
		{"dns", &net.DNSError{Err: "no such host", Name: "nosuchhost.invalid", IsNotFound: true}, "network"},
		{"dns wrapped", wrap(&net.DNSError{Err: "no such host", Name: "x"}), "network"},
		{"net error", ciNetErr{}, "network"},
		{"browser", errors.New("no instance matching 'SQL2008' returned from host 'srv'"), "network"},
		{"browser port", errors.New("invalid tcp port returned from Sql Server Browser 'x': y"), "network"},
		{"refused text", errors.New("unable to open tcp connection with host 'srv:1433': dial tcp: connection refused"), "network"},
		{"sql 233", mssql.Error{Number: 233}, "network"},
		{"bad conn", fmt.Errorf("x: %w", driver.ErrBadConn), "network"},
		{"config", &Error{Kind: KindConfig, Op: "open", Err: errors.New("server is empty")}, "config"},
		{"config wrapped", fmt.Errorf("ipc: %w", &Error{Kind: KindConfig, Op: "open", Err: errors.New("x")}), "config"},
		{"config text", errors.New(`unknown encrypt mode "maybe"`), "config"},
		{"canceled", context.Canceled, "other"},
		{"query", mssql.Error{Number: 208, Message: "Invalid object name"}, "other"},
		{"plain", errors.New("something else"), "other"},
	}
	for _, c := range cases {
		if got := ConnCategory(c.err); got != c.want {
			t.Errorf("%s: ConnCategory(%v) = %q, want %q", c.name, c.err, got, c.want)
		}
	}
	// real open errors: an invalid profile is a config error
	bad := connProfile()
	bad.Server = ""
	_, err := TestConnectionInfo(context.Background(), bad, "x")
	if ConnCategory(err) != ConnCategoryConfig {
		t.Errorf("empty server: %v -> %q", err, ConnCategory(err))
	}
	bad = connProfile()
	bad.Encrypt = "maybe"
	if err := TestConnection(context.Background(), bad, "x"); ConnCategory(err) != ConnCategoryConfig {
		t.Errorf("bad encrypt: %v -> %q", err, ConnCategory(err))
	}
	// real dial errors: refused port on loopback -> network
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	p := connProfile()
	// Windows retries a refused loopback connect for about 2 s before
	// reporting WSAECONNREFUSED, so the dial timeout must be longer.
	p.Server, p.Port, p.ConnectTimeoutSec = "127.0.0.1", port, 10
	if _, err := TestConnectionInfo(context.Background(), p, "x"); ConnCategory(err) != ConnCategoryNetwork {
		t.Errorf("refused: %v -> %q", err, ConnCategory(err))
	}
	// a server that accepts and never answers -> timeout
	ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	p.Port, p.ConnectTimeoutSec = ln.Addr().(*net.TCPAddr).Port, 1
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := TestConnectionInfo(ctx, p, "x"); ConnCategory(err) != ConnCategoryTimeout {
		t.Errorf("silent server: %v -> %q", err, ConnCategory(err))
	}

}
