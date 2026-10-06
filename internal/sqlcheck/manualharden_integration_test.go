package sqlcheck

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// manyRowsManualSQL returns far more than 2 rows on any SQL Server.
const manyRowsManualSQL = "SELECT name, CAST(NULL AS datetime), CAST(NULL AS datetime) FROM sys.all_objects"

func countRows(t *testing.T, ctx context.Context, conn *sql.Conn, q string) int {
	t.Helper()
	rows, err := conn.QueryContext(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return n
}

func sessionID(t *testing.T, ctx context.Context, conn *sql.Conn) int {
	t.Helper()
	var id int
	if err := conn.QueryRowContext(ctx, "SELECT @@SPID").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestCHK_SQL_04_IntegrationManualRowCount (phase 6 review, PLAN §12
// p. 80): the manual query is limited on the server (SET ROWCOUNT on the
// session: a following query on the same session without the restore
// returns only limit rows), and after ManualPoints the pooled session is
// back to ROWCOUNT 0 and READ COMMITTED.
func TestCHK_SQL_04_IntegrationManualRowCount(t *testing.T) {
	st := itStore(t, "ec_1c_custom")
	ctx := itCtx(t)
	if err := ValidateManualSQL(manyRowsManualSQL); err != nil {
		t.Fatal(err)
	}
	conn, err := st.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	all := countRows(t, ctx, conn, "SELECT name FROM sys.all_objects")
	if all <= 2 {
		t.Fatalf("sys.all_objects: %d rows", all)
	}
	pts, err := manualOnConn(ctx, conn, manyRowsManualSQL, 2)
	if err != nil || len(pts) != 2 {
		t.Fatalf("manual: %v %d", err, len(pts))
	}
	// without the restore the session keeps the server-side limit
	if n := countRows(t, ctx, conn, "SELECT name FROM sys.all_objects"); n != 2 {
		t.Errorf("server-side ROWCOUNT not applied: %d rows after SET ROWCOUNT 2", n)
	}
	restoreIsolation(conn, 10*time.Second)
	if n := countRows(t, ctx, conn, "SELECT name FROM sys.all_objects"); n != all {
		t.Errorf("after restore: %d rows, want %d", n, all)
	}
	if lvl := sessionIsolation(t, ctx, conn); lvl != 2 {
		t.Errorf("after restore: isolation %d", lvl)
	}
	conn.Close()
	// through the store on a one-connection pool: same session afterwards
	st.DB().SetMaxOpenConns(1)
	c1, err := st.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spid := sessionID(t, ctx, c1)
	c1.Close()
	pts, err = st.ManualPoints(ctx, manyRowsManualSQL, 3)
	if err != nil || len(pts) != 3 {
		t.Fatalf("ManualPoints: %v %d", err, len(pts))
	}
	c2, err := st.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if id := sessionID(t, ctx, c2); id != spid {
		t.Logf("session changed %d -> %d (pool reconnected)", spid, id)
	}
	if n := countRows(t, ctx, c2, "SELECT name FROM sys.all_objects"); n != all {
		t.Errorf("pooled session after ManualPoints: %d rows, want %d (ROWCOUNT sticky)", n, all)
	}
	if lvl := sessionIsolation(t, ctx, c2); lvl != 2 {
		t.Errorf("pooled session after ManualPoints: isolation %d", lvl)
	}
}

// TestCHK_SQL_04_IntegrationNestedCommentBypass: the review example is
// rejected before anything runs; control: the same text sent to SQL
// Server in the rolled-back transaction really commits the DELETE (the
// server nests /* */).
func TestCHK_SQL_04_IntegrationNestedCommentBypass(t *testing.T) {
	st := itStore(t, "ec_1c_custom")
	ctx := itCtx(t)
	for _, q := range []string{
		"IF OBJECT_ID(N'dbo.ec_bypass3') IS NOT NULL DROP TABLE dbo.ec_bypass3",
		"CREATE TABLE dbo.ec_bypass3 (a int NULL)",
		"INSERT INTO dbo.ec_bypass3 (a) VALUES (1)",
	} {
		if _, err := st.DB().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { _, _ = st.DB().ExecContext(context.Background(), "DROP TABLE dbo.ec_bypass3") }()
	count := func() int {
		var n int
		if err := st.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM dbo.ec_bypass3").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	const q = `SELECT 1 AS a,2 AS b,3 AS c /* /* */ ' */ COMMIT DELETE FROM dbo.ec_bypass3 --'`
	if _, err := st.ManualPoints(ctx, q, 10); KindOf(err) != KindMapping || !errors.Is(err, ErrManualComment) {
		t.Errorf("ManualPoints: %v", err)
	}
	if n := count(); n != 1 {
		t.Fatalf("data changed: %d rows", n)
	}
	for _, hq := range []string{
		"SELECT a, NULL, NULL FROM dbo.ec_bypass3 WITH (TABLOCKX, HOLDLOCK)",
		"SELECT a, NULL, NULL FROM dbo.ec_bypass3 DISABLE TRIGGER ALL ON dbo.ec_bypass3",
	} {
		if _, err := st.ManualPoints(ctx, hq, 10); KindOf(err) != KindMapping {
			t.Errorf("%q: %v", hq, err)
		}
	}
	// control: unvalidated, in the rolled-back READ UNCOMMITTED transaction
	conn, err := st.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadUncommitted})
	if err != nil {
		t.Fatal(err)
	}
	_, execErr := tx.ExecContext(ctx, q)
	_ = tx.Rollback()
	restoreIsolation(conn, 10*time.Second)
	t.Logf("unvalidated nested-comment bypass: exec err %v, rows after rollback %d", execErr, count())
	if n := count(); n != 0 {
		t.Errorf("control: SQL Server was expected to run the COMMIT / DELETE after the nested comment, %d rows left", n)
	}
}
