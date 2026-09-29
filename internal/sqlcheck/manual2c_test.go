package sqlcheck

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Phase 2c (CHK-SQL-04): manual SQL must be exactly one read-only SELECT;
// T-SQL needs no ';' between statements, so keywords are checked outside
// literals, comments and quoted identifiers.

func TestCHK_SQL_04_ManualSQLBypassWithoutSemicolon(t *testing.T) {
	bad := map[string]error{
		"SELECT 1,2,3 DELETE FROM t":                                    ErrManualForbidden,
		"SELECT a, b, c INTO dbo.x FROM t":                              ErrManualForbidden,
		"SELECT 1 COMMIT DELETE FROM t":                                 ErrManualForbidden,
		"SELECT 1, 2, 3 COMMIT TRAN":                                    ErrManualForbidden,
		"SELECT 1,2,3 ROLLBACK":                                         ErrManualForbidden,
		"SELECT 1,2,3 INSERT INTO t VALUES (1)":                         ErrManualForbidden,
		"SELECT 1,2,3 UPDATE t SET a = 1":                               ErrManualForbidden,
		"SELECT 1,2,3\nMERGE t USING s ON 1=1 WHEN MATCHED THEN DELETE": ErrManualForbidden,
		"SELECT 1,2,3 EXEC sp_who":                                      ErrManualForbidden,
		"SELECT 1,2,3 EXECUTE('DELETE FROM t')":                         ErrManualForbidden,
		"SELECT 1,2,3 SAVE TRANSACTION s":                               ErrManualForbidden,
		"SELECT 1,2,3 BEGIN TRAN":                                       ErrManualForbidden,
		"SELECT 1,2,3 DROP TABLE t":                                     ErrManualForbidden,
		"SELECT 1,2,3 ALTER LOGIN sa ENABLE":                            ErrManualForbidden,
		"SELECT 1,2,3 CREATE TABLE x (a int)":                           ErrManualForbidden,
		"SELECT 1,2,3 TRUNCATE TABLE t":                                 ErrManualForbidden,
		"SELECT 1,2,3 GRANT CONTROL TO public":                          ErrManualForbidden,
		"SELECT 1,2,3 REVOKE SELECT ON t FROM u":                        ErrManualForbidden,
		"SELECT 1,2,3 DENY SELECT ON t TO u":                            ErrManualForbidden,
		"SELECT 1,2,3 DBCC CHECKDB":                                     ErrManualForbidden,
		"SELECT 1,2,3 BACKUP DATABASE d TO DISK = 'x'":                  ErrManualForbidden,
		"SELECT 1,2,3 RESTORE DATABASE d FROM DISK = 'x'":               ErrManualForbidden,
		"SELECT 1,2,3 SHUTDOWN":                                         ErrManualForbidden,
		"SELECT 1,2,3 KILL 55":                                          ErrManualForbidden,
		"SELECT * FROM OPENROWSET('SQLNCLI', 'x', 'SELECT 1')":          ErrManualForbidden,
		"SELECT * FROM OPENQUERY(srv, 'SELECT 1')":                      ErrManualForbidden,
		"SELECT * FROM OPENDATASOURCE('SQLNCLI', 'x').d.dbo.t":          ErrManualForbidden,
		"SELECT 1,2,3 BULK INSERT t FROM 'x'":                           ErrManualForbidden,
		"SELECT 1,2,3 WAITFOR DELAY '00:01'":                            ErrManualForbidden,
		"SELECT 1,2,3 DECLARE @a int":                                   ErrManualForbidden,
		"SELECT 1,2,3 SET NOCOUNT ON":                                   ErrManualForbidden,
		"SELECT 1,2,3 USE master":                                       ErrManualForbidden,
		"select 1,2,3 dElEtE from t":                                    ErrManualForbidden,
		"WITH q AS (SELECT 1 AS a) DELETE FROM t":                       ErrManualForbidden,
		"SELECT 1,2,3 /* c */ DELETE /* c */ FROM t":                    ErrManualForbidden,
		"SELECT 1,2,3 -- c\nDELETE FROM t":                              ErrManualForbidden,
		"SELECT 1,2,3 IF 1=1 SELECT 4":                                  ErrManualForbidden,
		"SELECT 1,2,3 RECONFIGURE":                                      ErrManualForbidden,
		"SELECT 1,2,3 SELECT 4,5,6":                                     ErrManualMulti,
		"SELECT 1,2,3 WITH q AS (SELECT 1 AS a) SELECT a, 1, 2 FROM q":  ErrManualMulti,
		"SELECT 1,2,3)":                                                 ErrManualNotSelect,
	}
	for q, want := range bad {
		err := ValidateManualSQL(q)
		if !errors.Is(err, want) {
			t.Errorf("%q: err = %v, want %v", q, err, want)
		}
	}
	err := ValidateManualSQL("SELECT 1 COMMIT DELETE FROM t")
	if err == nil || !strings.Contains(err.Error(), "COMMIT") {
		t.Errorf("error must name the keyword: %v", err)
	}
	// through the reader: KindMapping, nothing is executed
	st := newFake()
	p := manualProfile("SELECT 1,2,3 DELETE FROM t", 0)
	if _, err := NewQueryReader(st, p, fixOpt()).ListPoints(context.Background()); KindOf(err) != KindMapping || !errors.Is(err, ErrManualForbidden) {
		t.Errorf("reader: %v", err)
	}
	if len(st.called("manual")) != 0 {
		t.Error("forbidden manual SQL reached the store")
	}
}

func TestCHK_SQL_04_ManualSQLNoFalsePositives(t *testing.T) {
	ok := []string{
		"SELECT 'DELETE FROM t; COMMIT', N'INSERT INTO', 1 FROM t",
		"SELECT [Update], [Delete], [Set] FROM [dbo].[Insert] AS [Into]",
		"SELECT \"Update\", \"Begin\", 1 FROM \"Exec\"",
		"SELECT a, b, c FROM t -- DELETE FROM t\n",
		"SELECT a, b, c /* COMMIT; DROP TABLE t; EXEC x */ FROM t",
		"SELECT a AS updated_at, b AS set_id, c AS begin_date FROM t WHERE deleted = 0 AND is_open = 1",
		"SELECT @@SERVERNAME, 1, 2",
		"SELECT _Description, _Fld1, _Fld2 FROM _Reference1 WITH (NOLOCK)",
		"SELECT a, b, c FROM t UNION SELECT a, b, c FROM u UNION ALL SELECT a, b, c FROM v",
		"SELECT a, b, c FROM t EXCEPT SELECT a, b, c FROM u",
		"SELECT a, (SELECT MAX(x) FROM u), (SELECT MIN(y) FROM v) FROM t",
		"WITH a AS (SELECT 1 AS x), b AS (SELECT x FROM a) SELECT x, NULL, NULL FROM b",
		"SELECT CASE WHEN a > 0 THEN 'x' END, b, c FROM t",
		"SELECT a, b, c FROM t WHERE a IN (SELECT a FROM u) ORDER BY a",
	}
	for _, q := range ok {
		if err := ValidateManualSQL(q); err != nil {
			t.Errorf("rejected %q: %v", q, err)
		}
	}
	for _, tpl := range ManualSQLTemplates {
		if err := ValidateManualSQL(tpl.SQL); err != nil {
			t.Errorf("template %s: %v", tpl.Name, err)
		}
	}
	if got := strings.Join(sqlTokens("SELECT @set, #tmp, a.b(1)"), " "); got != "SELECT @SET , #TMP , A . B ( 1 )" {
		t.Errorf("tokens: %q", got)
	}
}

func TestCHK_SQL_04_NoSessionSetInProductSQL(t *testing.T) {
	// every query the service sends (recorded like CHK-SQL-05) is free of
	// session-level SET / isolation changes, except the explicit restore
	// after a manual query
	for q, kind := range allRecordedSQL(t) {
		if strings.HasPrefix(kind, "manual") || strings.HasPrefix(kind, "template") || kind == "restore_isolation" {
			continue
		}
		for _, tk := range sqlTokens(blankSQL(q, false)) {
			if tk == "ISOLATION" || manualForbidden[tk] {
				t.Errorf("%s: %s in\n%s", kind, tk, q)
			}
		}
	}
	if restoreIsolationSQL != "SET TRANSACTION ISOLATION LEVEL READ COMMITTED" || len(Compat2008Violations(restoreIsolationSQL)) != 0 {
		t.Error(restoreIsolationSQL)
	}
}
