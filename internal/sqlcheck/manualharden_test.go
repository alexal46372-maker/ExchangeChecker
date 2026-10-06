package sqlcheck

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 6 review of the Discovery contour (PLAN §12 p. 80): manual SQL
// hardening — no comments (nested /* */), lock hints whitelist, no
// OPTION, whitelist of top-level words, server-side SET ROWCOUNT.

// vikaExample is the blocker from the review: a validator closing /* at
// the first */ sees "SELECT 1, 2, 3 ' */ COMMIT ... --'" with the tail in
// a string; SQL Server nests the comment and runs COMMIT and DELETE.
const vikaExample = `SELECT 1 AS a,2 AS b,3 AS c /* /* */ ' */ COMMIT DELETE FROM dbo.t --'`

func expectManual(t *testing.T, q string, want error, key string, args ...string) {
	t.Helper()
	err := ValidateManualSQL(q)
	if !errors.Is(err, want) {
		t.Errorf("%q: err = %v, want %v", q, err, want)
		return
	}
	fe := FieldErrorOf(err)
	if fe == nil || fe.Field != "manualSql" || fe.Key != key || (args != nil && !reflect.DeepEqual(fe.Args, args)) {
		t.Errorf("%q: field error %+v, want %s %v", q, fe, key, args)
	}
}

// TestCHK_SQL_04_ManualSQLNoComments: any comment outside literals and
// quoted identifiers is rejected (validation.sql_comment), including the
// review example and nested depth 2/3; comment characters inside
// '...', N'...', [...] (with ]] escapes) and "..." do not trigger.
func TestCHK_SQL_04_ManualSQLNoComments(t *testing.T) {
	for _, q := range []string{
		vikaExample,
		"SELECT a, b, c FROM t /* x */",
		"SELECT a, b, c FROM t -- x",
		"SELECT a, b, c FROM t--x",
		"SELECT a, b, c FROM t/**/",
		"SELECT a, b, c /* /* */ */ FROM t",                  // depth 2
		"SELECT a, b, c /* 1 /* 2 /* 3 */ */ */ FROM t",      // depth 3
		"SELECT a, b, c /* /* */ FROM t",                     // unterminated nested
		"SELECT [a]]--], b, c FROM t -- tail",                // ]] escape, then a real comment
		"SELECT 'it''s', b, c FROM t /* ' */",                // '' escape, then a real comment
		"SELECT N'x', \"y\"\"--\", c FROM t --",              // "" escape
		"SELECT a, b, c FROM t WHERE x = 1 -- DELETE FROM t", // the old test: comment hid nothing, still rejected
		"-- header\nSELECT a, b, c FROM t",
		"/* header */ SELECT a, b, c FROM t",
		"SELECT a, b, c FROM t; -- trailing",
	} {
		expectManual(t, q, ErrManualComment, model.ValSQLComment, nil...)
	}
	for _, q := range []string{
		"SELECT '--', '/*', N'*/' FROM t",
		"SELECT [a--b], [c/*d], [e]]--f] FROM t",
		"SELECT \"x--y\", \"a\"\"/*b\", 'it''s -- ok' FROM t",
		"SELECT N'Магазин ''А'' -- /* x */', b, c FROM t",
		"SELECT [a]]], b, c FROM t WHERE d = N'/* */ --'",
		"SELECT a / 2, b * 3, c - 1 FROM t",
		"SELECT a, b, c FROM t WHERE x = '*/'",
	} {
		if err := ValidateManualSQL(q); err != nil {
			t.Errorf("rejected %q: %v", q, err)
		}
	}
	for _, q := range []string{
		"SELECT 'abc, b, c FROM t",
		"SELECT N'abc, b, c FROM t",
		"SELECT 'it'', b, c FROM t",
		"SELECT [a]], b, c FROM t",
		"SELECT \"a, b, c FROM t",
		"SELECT [a, b, c FROM t",
	} {
		expectManual(t, q, ErrManualLiteral, model.ValSQLManualLiteral, nil...)
	}
	// defense in depth: blankSQL (compat2008, token scans) nests like SQL Server
	toks := strings.Join(sqlTokens(blankSQL(vikaExample, false)), " ")
	if !strings.Contains(toks, "COMMIT DELETE FROM DBO . T") {
		t.Errorf("blankSQL must see the statements after the nested comment: %s", toks)
	}
	if got := strings.Join(sqlTokens(blankSQL("SELECT /* 1 /* 2 /* 3 */ DROP */ EXEC */ x", false)), " "); got != "SELECT X" {
		t.Errorf("depth 3: %s", got)
	}
	if v := Compat2008Violations("SELECT /* /* */ IIF(a, 1, 0) */ 1"); len(v) != 0 {
		t.Errorf("IIF inside a nested comment: %v", v)
	}
	if v := Compat2008Violations("SELECT /* /* */ */ IIF(a = 1, 1, 0)"); len(v) != 1 {
		t.Errorf("IIF after a nested comment: %v", v)
	}
}

// TestCHK_SQL_04_ManualSQLLockHints: table hints only NOLOCK,
// READUNCOMMITTED, NOWAIT (WITH (...) or the legacy "t (NOLOCK)"); any
// other table / join hint and OPTION (...) are rejected; CTE heads and
// TOP ... WITH TIES / GROUP BY ... WITH ROLLUP are not hints.
func TestCHK_SQL_04_ManualSQLLockHints(t *testing.T) {
	for q, hint := range map[string]string{
		"SELECT a,b,c FROM dbo._Reference10 WITH (TABLOCKX, HOLDLOCK)":             "TABLOCKX",
		"SELECT a,b,c FROM t WITH (NOLOCK, HOLDLOCK)":                              "HOLDLOCK",
		"SELECT a,b,c FROM t (TABLOCKX)":                                           "TABLOCKX",
		"SELECT a,b,c FROM t HOLDLOCK":                                             "HOLDLOCK",
		"SELECT a,b,c FROM t WITH (INDEX(1))":                                      "INDEX",
		"SELECT a,b,c FROM t WITH (INDEX = ix_a)":                                  "INDEX",
		"SELECT a,b,c FROM t WITH (FORCESEEK)":                                     "FORCESEEK",
		"SELECT a,b,c FROM t WITH (UPDLOCK)":                                       "UPDLOCK",
		"SELECT a,b,c FROM t WITH (xlock)":                                         "XLOCK",
		"SELECT a,b,c FROM t WITH (PAGLOCK)":                                       "PAGLOCK",
		"SELECT a,b,c FROM t WITH (ROWLOCK)":                                       "ROWLOCK",
		"SELECT a,b,c FROM t WITH (SERIALIZABLE)":                                  "SERIALIZABLE",
		"SELECT a,b,c FROM t WITH (REPEATABLEREAD)":                                "REPEATABLEREAD",
		"SELECT a,b,c FROM t WITH (READCOMMITTEDLOCK)":                             "READCOMMITTEDLOCK",
		"SELECT a,b,c FROM t WITH (READPAST)":                                      "READPAST",
		"SELECT a,b,c FROM v WITH (NOEXPAND)":                                      "NOEXPAND",
		"SELECT a,b,c FROM t WITH (SNAPSHOT)":                                      "SNAPSHOT",
		"SELECT a,b,c FROM t WITH ([NOLOCK])":                                      "[NOLOCK]",
		"SELECT a,(SELECT MAX(x) FROM u WITH (TABLOCKX)),c FROM t":                 "TABLOCKX",
		"WITH q AS (SELECT a FROM t WITH (XLOCK)) SELECT a, 1, 2 FROM q":           "XLOCK",
		"SELECT t.a, u.b, u.c FROM t INNER LOOP JOIN u ON u.k = t.k":               "LOOP",
		"SELECT t.a, u.b, u.c FROM t INNER HASH JOIN u ON u.k = t.k":               "HASH",
		"SELECT t.a, u.b, u.c FROM t LEFT REMOTE JOIN u ON u.k = t.k":              "REMOTE",
		"SELECT a,b,c FROM t WITH (NOLOCK) JOIN u WITH (TABLOCK) ON u.k = t.k":     "TABLOCK",
		"SELECT a,b,c FROM t WITH (NOLOCK) WHERE x IN (SELECT y FROM u (UPDLOCK))": "UPDLOCK",
	} {
		expectManual(t, q, ErrManualHint, model.ValSQLManualHint, hint)
	}
	for _, q := range []string{
		"SELECT a,b,c FROM t OPTION (MAXDOP 1)",
		"SELECT a,b,c FROM t WITH (NOLOCK) OPTION (RECOMPILE)",
		"SELECT a,b,c FROM t option (fast 1)",
	} {
		expectManual(t, q, ErrManualOption, model.ValSQLManualOption, nil...)
	}
	for _, q := range []string{
		"SELECT a,b,c FROM t WITH (NOLOCK)",
		"SELECT a,b,c FROM t with(nolock)",
		"SELECT a,b,c FROM t (NOLOCK)",
		"SELECT a,b,c FROM t WITH (READUNCOMMITTED)",
		"SELECT a,b,c FROM t WITH (NOLOCK, NOWAIT)",
		"SELECT a,b,c FROM t WITH (NOLOCK) JOIN u WITH (READUNCOMMITTED, NOWAIT) ON u.k = t.k",
		"SELECT a,b,c FROM t WITH (NOLOCK) WHERE x IN (SELECT y FROM u (NOLOCK))",
		"SELECT TOP (5) WITH TIES a, b, c FROM t ORDER BY a",
		"SELECT a, COUNT(*), MAX(c) FROM t GROUP BY a WITH ROLLUP",
		"SELECT a, COUNT(*), MAX(c) FROM t GROUP BY a WITH CUBE",
		"WITH q (a, b) AS (SELECT 1, 2) SELECT a, b, NULL FROM q",
		"WITH [q] AS (SELECT 1 AS a) SELECT a, 1, 2 FROM [q] WITH (NOLOCK)",
		"WITH a AS (SELECT x FROM t WITH (NOLOCK)), b AS (SELECT x FROM a) SELECT x, NULL, NULL FROM b",
		"SELECT t.Snapshot, x AS rowlock, t.[Index] FROM t",
		"SELECT a AS hash, b AS loop, c FROM t",
	} {
		if err := ValidateManualSQL(q); err != nil {
			t.Errorf("rejected %q: %v", q, err)
		}
	}
	for _, tpl := range model.SQLManualTemplates() {
		if err := ValidateManualSQL(tpl.SQL); err != nil {
			t.Errorf("template %s: %v", tpl.ID, err)
		}
	}
}

// TestCHK_SQL_04_ManualSQLWhitelist: a second statement is rejected even
// when its verb is not a reserved word and not in the blacklist (review:
// DISABLE TRIGGER, RECEIVE; also ENABLE, SEND, MOVE, GET, END
// CONVERSATION, ADD SIGNATURE, ...); reserved words outside the SELECT
// whitelist and non-printing characters too; no false positives on
// quoted names, aliases, dotted names and CASE ... END.
func TestCHK_SQL_04_ManualSQLWhitelist(t *testing.T) {
	for q, word := range map[string]string{
		"SELECT a,b,c FROM t DISABLE TRIGGER ALL ON t":                      "DISABLE",
		"SELECT a,b,c FROM t ENABLE TRIGGER ALL ON t":                       "ENABLE",
		"SELECT a,b,c FROM t RECEIVE * FROM q":                              "RECEIVE",
		"SELECT a,b,c FROM t RECEIVE TOP (1) * FROM dbo.q":                  "RECEIVE",
		"SELECT a,b,c FROM t SEND ON CONVERSATION @h (N'x')":                "SEND",
		"SELECT a,b,c FROM t MOVE CONVERSATION @h TO @g":                    "MOVE",
		"SELECT a,b,c FROM t GET CONVERSATION GROUP @g FROM q":              "GET",
		"SELECT a,b,c FROM t END CONVERSATION @h":                           "END",
		"SELECT a,b,c FROM t ADD SIGNATURE TO p BY CERTIFICATE c":           "ADD",
		"SELECT a,b,c FROM t BEGIN DIALOG @h FROM SERVICE s TO SERVICE 'x'": "BEGIN",
		"SELECT a,b,c FROM t CHECKPOINT":                                    "CHECKPOINT",
		"SELECT a,b,c FROM t DUMP TRANSACTION d WITH NO_LOG":                "DUMP",
		"SELECT a,b,c FROM t LOAD DATABASE d FROM x":                        "LOAD",
		"SELECT a,b,c FROM t SETUSER":                                       "SETUSER",
		"SELECT a,b,c FROM t FOR UPDATE OF a":                               "UPDATE",
		"SELECT a,b,c FROM t WHERE CURRENT OF c":                            "CURRENT",
		"SELECT a,b,c FROM t LINENO 5":                                      "LINENO",
		"SELECT a,b,c FROM t TEXTSIZE":                                      "TEXTSIZE",
		"SELECT a,b,c FROM OPENXML(@h, '/r') WITH (a int)":                  "OPENXML",
		"SELECT IDENTITY(int, 1, 1) AS id, b, c FROM t":                     "IDENTITY",
		"SELECT a,b,c FROM t ALTER DATABASE d SET OFFLINE":                  "ALTER",
		"SELECT a,b,c FROM t WHERE a = 1\u00a0DELETE FROM t":                "U+00A0",
		"SELECT a,b,c FROM t DEL\u200bETE FROM t":                           "U+200B",
		"SELECT a,b,c FROM t\x00":                                           "U+0000",
		"SELECT a, b, c FROM t \xff":                                        "invalid UTF-8",
	} {
		expectManual(t, q, ErrManualForbidden, model.ValSQLManualForbidden, word)
	}
	for q, want := range map[string]error{
		"SELECT a,b,c FROM t WITH x AS (SELECT 1 AS a) SELECT a, 1, 2 FROM x": ErrManualMulti,
		"SELECT a,b,c FROM t SELECT 1, 2, 3":                                  ErrManualMulti,
		"SELECT a,b,c FROM t; RECEIVE * FROM q":                               ErrManualMulti,
		"SELECT a,b,c FROM t THROW 50000, 'x', 1":                             ErrManualCompat2008,
		"WITH (NOLOCK) SELECT 1, 2, 3":                                        ErrManualNotSelect,
		"WITH SELECT AS (SELECT 1) SELECT 1, 2, 3":                            ErrManualNotSelect,
		"RECEIVE * FROM q":                                                    ErrManualNotSelect,
		"(SELECT a, b, c FROM t)":                                             ErrManualNotSelect,
		"SELECT (a, b, c FROM t":                                              ErrManualNotSelect,
	} {
		if err := ValidateManualSQL(q); !errors.Is(err, want) {
			t.Errorf("%q: err = %v, want %v", q, err, want)
		}
	}
	for _, q := range []string{
		"SELECT CASE WHEN a > 0 THEN 'x' ELSE 'y' END AS [End], b, c FROM t",
		"SELECT CASE a WHEN 1 THEN CASE b WHEN 2 THEN 'x' END END, b, c FROM t",
		"SELECT [Update], [Send], [Receive] FROM t",
		"SELECT \"Disable\", \"Get\", \"Move\" FROM t",
		"SELECT a AS send, b AS receive, c AS move FROM t",
		"SELECT t.send, t.get, t.disable FROM dbo.t AS t",
		"SELECT x.[Disable], y, z FROM dbo.t AS x",
		"SELECT CONVERT(varchar(10), a, 104), COALESCE(b, c), NULLIF(c, 0) FROM t",
		"SELECT LEFT(a, 3), RIGHT(b, 2), c FROM t WHERE a LIKE 'x!%' ESCAPE '!'",
		"SELECT a, b, c FROM t WHERE EXISTS (SELECT 1 FROM u WHERE u.a = t.a) AND b IS NOT NULL AND c BETWEEN 1 AND 2",
		"SELECT DISTINCT TOP 10 PERCENT a, b, c FROM t",
		"SELECT a, b, c FROM t FULL OUTER JOIN u ON t.k = u.k LEFT JOIN v ON 1 = 1 RIGHT OUTER JOIN w ON 1 = 1 CROSS JOIN z",
		"SELECT a, ROW_NUMBER() OVER (PARTITION BY b ORDER BY c DESC), c FROM t",
		"SELECT t.a, x.b, x.c FROM t CROSS APPLY (SELECT TOP 1 b, c FROM u WHERE u.k = t.k ORDER BY c DESC) AS x",
		"SELECT t.a, x.b, x.c FROM t OUTER APPLY (SELECT TOP 1 b, c FROM u) x",
		"SELECT CAST(a AS DOUBLE PRECISION), CAST(b AS CHAR VARYING(10)), c FROM t",
		"SELECT CURRENT_TIMESTAMP, SYSTEM_USER, USER",
		"SELECT SESSION_USER, CURRENT_USER, 1",
		"SELECT a, b, c FROM (VALUES (1, 2, 3)) AS v (a, b, c)",
		"SELECT a, b, c FROM t TABLESAMPLE (10 PERCENT)",
		"SELECT p.a, p.[1], p.[2] FROM t PIVOT (MAX(v) FOR k IN ([1], [2])) AS p",
		"SELECT a, b, c FROM t WHERE a = ANY (SELECT x FROM u) OR b = SOME (SELECT y FROM u) OR c > ALL (SELECT z FROM u)",
		"SELECT a, b, c FROM t FOR XML PATH('r')",
		"SELECT $1, 0x0A, 1e3 FROM t",
		"SELECT a COLLATE Cyrillic_General_CI_AS, b, c FROM t ORDER BY a ASC, b DESC",
		"SELECT Имя, Отправка, Приём FROM Точки WHERE Имя = N'Магазин'",
		"SELECT a, b, c FROM t;",
		"SELECT a, b, c FROM t ;; ",
		"SELECT a, b, c FROM t INTERSECT SELECT a, b, c FROM u",
		"SELECT a, b, c FROM t WHERE CONTAINS(a, 'x') OR FREETEXT(b, 'y')",
		"select a, b, c from t with (nolock) where a in (select x from u) group by a, b, c having count(*) > 1",
	} {
		if err := ValidateManualSQL(q); err != nil {
			t.Errorf("rejected %q: %v", q, err)
		}
	}
	// every reserved word is either allowed in a SELECT or rejected
	for w := range tsqlReserved {
		if manualAllowedReserved[w] || w == "WITH" || w == "OPTION" || w == "HOLDLOCK" || w == "INDEX" {
			continue
		}
		q := fmt.Sprintf("SELECT a, b, c FROM t %s x", w)
		if err := ValidateManualSQL(q); err == nil {
			t.Errorf("%q accepted", q)
		}
	}
}

// ---- server-side row limit (SET ROWCOUNT) ----

// rcDriver records every statement of a dedicated manual connection.
type rcDriver struct {
	mu      sync.Mutex
	log     []string
	rows    int
	fail    map[string]error // statement prefix -> error
	closed  int
	rcLimit int // emulated SET ROWCOUNT of the session
}

type rcConn struct{ d *rcDriver }
type rcTx struct{ d *rcDriver }
type rcRows struct {
	n, i int
}

func (d *rcDriver) Open(string) (driver.Conn, error) { return &rcConn{d}, nil }
func (d *rcDriver) rec(s string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.log = append(d.log, s)
	for p, e := range d.fail {
		if strings.HasPrefix(s, p) {
			return e
		}
	}
	return nil
}
func (c *rcConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("no prepare") }
func (c *rcConn) Close() error {
	c.d.mu.Lock()
	c.d.closed++
	c.d.mu.Unlock()
	return nil
}
func (c *rcConn) Begin() (driver.Tx, error) { return nil, errors.New("use BeginTx") }
func (c *rcConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	return &rcTx{c.d}, c.d.rec(fmt.Sprintf("BEGIN TRAN %d", o.Isolation))
}
func (c *rcConn) ExecContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if err := c.d.rec(q); err != nil {
		return nil, err
	}
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	var n int
	if strings.Contains(q, restoreRowCountSQL) {
		c.d.rcLimit = 0
	} else if _, err := fmt.Sscanf(q, "SET ROWCOUNT %d", &n); err == nil {
		c.d.rcLimit = n
	}
	return driver.RowsAffected(0), nil
}
func (c *rcConn) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := c.d.rec(q); err != nil {
		return nil, err
	}
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	n := c.d.rows
	if c.d.rcLimit > 0 && c.d.rcLimit < n {
		n = c.d.rcLimit
	}
	return &rcRows{n: n}, nil
}
func (t *rcTx) Commit() error   { return t.d.rec("COMMIT") }
func (t *rcTx) Rollback() error { return t.d.rec("ROLLBACK") }
func (r *rcRows) Columns() []string {
	return []string{"p", "s", "r"}
}
func (r *rcRows) Close() error { return nil }
func (r *rcRows) Next(dest []driver.Value) error {
	if r.i >= r.n {
		return io.EOF
	}
	r.i++
	dest[0], dest[1], dest[2] = fmt.Sprintf("P%d", r.i), nil, nil
	return nil
}

var rcDriverN int

func rcStore(t *testing.T, d *rcDriver) *SQLStore {
	t.Helper()
	rcDriverN++
	name := fmt.Sprintf("ecrowcount%d", rcDriverN)
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return NewSQLStore(db, time.Second, time.Second)
}

// TestCHK_SQL_04_ManualRowCount: ManualPoints sends SET ROWCOUNT limit
// before the rolled-back transaction and restores READ COMMITTED +
// ROWCOUNT 0 in one batch afterwards; a failed restore discards the
// connection; SET ROWCOUNT failing stops the query but still restores.
func TestCHK_SQL_04_ManualRowCount(t *testing.T) {
	const q = "SELECT a, b, c FROM t"
	d := &rcDriver{rows: 50}
	st := rcStore(t, d)
	pts, err := st.ManualPoints(context.Background(), q, 7)
	if err != nil || len(pts) != 7 {
		t.Fatalf("manual: %d %v", len(pts), err)
	}
	want := []string{"SET ROWCOUNT 7", fmt.Sprintf("BEGIN TRAN %d", sql.LevelReadUncommitted), q, "ROLLBACK", restoreSessionSQL}
	if !reflect.DeepEqual(d.log, want) {
		t.Errorf("statements %q\nwant %q", d.log, want)
	}
	if d.rcLimit != 0 || d.closed != 0 {
		t.Errorf("session ROWCOUNT %d after restore, closed %d", d.rcLimit, d.closed)
	}
	// a failed restore discards the connection (never back to the pool)
	d2 := &rcDriver{rows: 3, fail: map[string]error{restoreIsolationSQL: errors.New("restore failed")}}
	st2 := rcStore(t, d2)
	if _, err := st2.ManualPoints(context.Background(), q, 7); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	d2.mu.Lock()
	closed := d2.closed
	d2.mu.Unlock()
	if closed != 1 {
		t.Errorf("connection not discarded after a failed restore: closed %d", closed)
	}
	// SET ROWCOUNT fails: no query, restore still sent
	d3 := &rcDriver{rows: 3, fail: map[string]error{"SET ROWCOUNT 7": errors.New("boom")}}
	st3 := rcStore(t, d3)
	if _, err := st3.ManualPoints(context.Background(), q, 7); err == nil {
		t.Error("SET ROWCOUNT error not returned")
	}
	if want := []string{"SET ROWCOUNT 7", restoreSessionSQL}; !reflect.DeepEqual(d3.log, want) {
		t.Errorf("statements %q, want %q", d3.log, want)
	}
	// texts: SQL Server 2008, no user text in the SET
	for _, s := range []string{manualRowCountSQL(100), restoreSessionSQL, restoreRowCountSQL} {
		if v := Compat2008Violations(s); len(v) != 0 {
			t.Errorf("%q: %v", s, v)
		}
	}
	if manualRowCountSQL(100) != "SET ROWCOUNT 100" || restoreSessionSQL != "SET TRANSACTION ISOLATION LEVEL READ COMMITTED;\nSET ROWCOUNT 0" {
		t.Errorf("%q / %q", manualRowCountSQL(100), restoreSessionSQL)
	}
}
