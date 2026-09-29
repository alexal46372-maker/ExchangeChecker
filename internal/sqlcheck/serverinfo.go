package sqlcheck

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"

	mssql "github.com/microsoft/go-mssqldb"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// ServerInfo describes the SQL Server behind a profile (phase 4 IPC
// test_profile, PLAN.md §12). Fields the server does not report (NULL) or
// the login cannot see are left empty / 0.
type ServerInfo struct {
	// Version is SERVERPROPERTY('ProductVersion'), e.g. "10.50.1600.1"
	// for SQL Server 2008 R2 RTM.
	Version string `json:"version,omitempty"`
	// ProductLevel is SERVERPROPERTY('ProductLevel'): "RTM", "SP1", ...
	ProductLevel string `json:"productLevel,omitempty"`
	// Edition is SERVERPROPERTY('Edition'), e.g. "Standard Edition (64-bit)".
	Edition string `json:"edition,omitempty"`
	// CompatLevel is sys.databases.compatibility_level of the profile
	// database (100 = SQL Server 2008), 0 when not visible to the login.
	CompatLevel int `json:"compatLevel,omitempty"`
}

// serverInfoSQL is T-SQL 2005/2008 (CHK-SQL-05): SERVERPROPERTY and
// sys.databases exist since 2005; it is a single autocommit SELECT without
// any session-level SET (phase 2c). SERVERPROPERTY needs no permission;
// the sys.databases row of the current database is visible to every user
// of it, otherwise the scalar subquery yields NULL.
const serverInfoSQL = `SELECT CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128)),
 CAST(SERVERPROPERTY('ProductLevel') AS nvarchar(128)),
 CAST(SERVERPROPERTY('Edition') AS nvarchar(128)),
 (SELECT compatibility_level FROM sys.databases WHERE database_id = DB_ID())`

// serverPropsSQL is the fallback when serverInfoSQL fails with a query
// error (e.g. DENY SELECT ON sys.databases): SERVERPROPERTY only.
const serverPropsSQL = `SELECT CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128)),
 CAST(SERVERPROPERTY('ProductLevel') AS nvarchar(128)),
 CAST(SERVERPROPERTY('Edition') AS nvarchar(128))`

// ServerInfo reads the server version, product level, edition and the
// compatibility level of the current database with serverInfoSQL. NULL
// columns become "" / 0. If that query is rejected (query error, e.g. no
// SELECT on sys.databases), SERVERPROPERTY alone is read (serverPropsSQL)
// and CompatLevel stays 0 without an error. Errors are classified like
// other queries.
func (s *SQLStore) ServerInfo(ctx context.Context) (ServerInfo, error) {
	ctx, cancel := s.qctx(ctx)
	defer cancel()
	var ver, lvl, ed sql.NullString
	var compat sql.NullInt64
	if err := s.db.QueryRowContext(ctx, serverInfoSQL).Scan(&ver, &lvl, &ed, &compat); err != nil {
		err = classify("server_info", err, false)
		if KindOf(err) != KindQuery {
			return ServerInfo{}, err
		}
		// no access to sys.databases: keep the version, CompatLevel 0
		ver, lvl, ed = sql.NullString{}, sql.NullString{}, sql.NullString{}
		if err2 := s.db.QueryRowContext(ctx, serverPropsSQL).Scan(&ver, &lvl, &ed); err2 != nil {
			return ServerInfo{}, err
		}
		return ServerInfo{Version: ver.String, ProductLevel: lvl.String, Edition: ed.String}, nil
	}
	return ServerInfo{Version: ver.String, ProductLevel: lvl.String, Edition: ed.String, CompatLevel: int(compat.Int64)}, nil
}

// TestConnectionInfo is TestConnection plus one read-only query for the
// server description (phase 4 IPC test_profile): it opens a session to p
// (TCP, TLS as configured — works with encrypt "disable" and the
// "mssql2008" TLS 1.0 preset —, login, database), pings, then runs
// serverInfoSQL. Connection errors are *Error with KindConn / KindTimeout
// / KindConfig (TLS failures carry the message key, see ConnCategory).
// If only the info query fails with a query error (e.g. DENY on
// sys.databases), the connection test still succeeds and ServerInfo is
// returned empty; context/connection errors of that query are returned.
func TestConnectionInfo(ctx context.Context, p model.Profile, password string) (ServerInfo, error) {
	st, err := Open(p, password)
	if err != nil {
		return ServerInfo{}, err
	}
	defer st.Close()
	if err := refineTLS(st.Ping(ctx), p); err != nil {
		return ServerInfo{}, err
	}
	info, err := st.ServerInfo(ctx)
	if err != nil {
		if KindOf(err) == KindQuery {
			return ServerInfo{}, nil
		}
		return ServerInfo{}, refineTLS(err, p)
	}
	return info, nil
}

// Connection error categories returned by ConnCategory (phase 4 IPC
// test_profile, stable strings for the GUI).
const (
	ConnCategoryLogin    = "login"
	ConnCategoryDatabase = "database"
	ConnCategoryTLS      = "tls"
	ConnCategoryTimeout  = "timeout"
	ConnCategoryNetwork  = "network"
	ConnCategoryConfig   = "config"
	ConnCategoryOther    = "other"
)

var (
	loginErrorNumbers = map[int32]bool{18456: true, 18452: true, 18486: true, 18487: true, 18488: true}
	// 4063 is what SQL Server 2019/2022 report for an unknown database
	// requested at login ("... Using the user default database instead").
	databaseErrorNumbers = map[int32]bool{4060: true, 4063: true, 4064: true}
	networkErrorNumbers  = map[int32]bool{233: true, 10054: true}
)

// ConnCategory sorts a connection / TestConnection(Info) error into one of
// "login" (SQL errors 18456, 18452, 18486-18488), "database" (4060, 4063,
// 4064; every error token of the response is checked), "tls" (TLS handshake / certificate failures, the errors with a TLS
// message key), "timeout" (context deadline, net timeouts), "network"
// (net.Error, connection refused/reset, DNS, SQL Browser / instance
// resolution), "config" (the profile cannot be turned into a connection:
// *Error with KindConfig, invalid DSN) or "other" (including a canceled
// context and query errors). It looks through wrapped errors (errors.As).
// A nil err returns "".
func ConnCategory(err error) string {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		switch {
		case e.Kind == KindConfig:
			return ConnCategoryConfig
		case isTLSKey(e.Key):
			return ConnCategoryTLS
		}
	}
	var nums []int32
	var num sqlErrorNumber
	if errors.As(err, &num) {
		nums = append(nums, num.SQLErrorNumber())
	}
	var me mssql.Error
	if errors.As(err, &me) {
		for _, x := range me.All { // every error token of the response
			nums = append(nums, x.Number)
		}
	}
	for _, set := range []struct {
		m   map[int32]bool
		cat string
	}{{loginErrorNumbers, ConnCategoryLogin}, {databaseErrorNumbers, ConnCategoryDatabase}, {networkErrorNumbers, ConnCategoryNetwork}} {
		for _, n := range nums {
			if set.m[n] {
				return set.cat
			}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) || (e != nil && e.Kind == KindTimeout) {
		return ConnCategoryTimeout
	}
	if errors.Is(err, context.Canceled) {
		return ConnCategoryOther
	}
	if tlsKey(err) != "" {
		return ConnCategoryTLS
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ConnCategoryTimeout
	}
	var dns *net.DNSError
	var op *net.OpError
	var addr *net.AddrError
	if errors.As(err, &dns) || errors.As(err, &op) || errors.As(err, &addr) || errors.As(err, &ne) ||
		errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, driver.ErrBadConn) {
		return ConnCategoryNetwork
	}
	// go-mssqldb flattens some causes into text (fmt.Errorf("%v")).
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"i/o timeout", "deadline exceeded"} {
		if strings.Contains(msg, s) {
			return ConnCategoryTimeout
		}
	}
	for _, s := range []string{"connection refused", "connection reset", "no such host", "no route to host",
		"network is unreachable", "no instance matching", "sql server browser", "unable to open tcp connection",
		"actively refused", "forcibly closed"} {
		if strings.Contains(msg, s) {
			return ConnCategoryNetwork
		}
	}
	for _, s := range []string{"invalid dsn", "unknown encrypt mode", "server is empty", "unknown auth mode"} {
		if strings.Contains(msg, s) {
			return ConnCategoryConfig
		}
	}
	return ConnCategoryOther
}

func isTLSKey(k string) bool {
	return k == MsgTLSEnableTLS10 || k == MsgTLSLegacyFailed || k == MsgTLSCertificate
}
