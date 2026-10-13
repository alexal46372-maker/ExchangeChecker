package sqlcheck

import (
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// CHK-SQL-07 / CHK-SQL-09 (SQL Server, compat 100): the generated grant
// script (SQL login) runs, is idempotent and refuses the placeholder; the
// granted login (db_datareader only) lists exactly the granted database.
func TestCHK_SQL_07_DatabasesGrantIntegration(t *testing.T) {
	sa := itStore(t, "master")
	ctx := itCtx(t)
	pSA, pwSA := itProfile(t, "")
	r, err := NewInteractiveReader(pSA, pwSA, Options{})
	if err != nil {
		t.Fatal(err)
	}
	all, err := r.Databases(ctx, DatabasesOptions{})
	r.Close()
	if err != nil || len(all.Databases) < 2 || all.Truncated {
		t.Fatalf("sa: %+v %v", all, err)
	}
	byName := map[string]model.SQLDatabaseInfo{}
	for _, d := range all.Databases {
		byName[d.Name] = d
	}
	if n, g := byName["ec_1c_node_off"], byName["ec_generic"]; !n.Probed || !n.HasNodeTables || n.Is1C || n.SizeMB <= 0 || !g.Probed || g.HasNodeTables || all.ConnectMs < 0 {
		t.Errorf("probe: %+v %+v", n, g)
	}
	for _, d := range all.Databases {
		if d.State != "ONLINE" || d.Name == "master" || d.Name == "tempdb" || d.Name == "model" || d.Name == "msdb" {
			t.Errorf("sa: %+v", d)
		}
	}
	if lim, err := (func() (model.SQLDatabasesResult, error) {
		r, _ := NewInteractiveReader(pSA, pwSA, Options{})
		defer r.Close()
		return r.Databases(ctx, DatabasesOptions{Limit: 1})
	})(); err != nil || len(lim.Databases) != 1 || !lim.Truncated {
		t.Errorf("limit: %+v %v", lim, err)
	}

	const pw = "Ec_Reader-Passw0rd!"
	for _, login := range []string{"ec_wizard_reader", "ec_wiz]'; DROP LOGIN sa; --"} {
		drop := "DECLARE @s nvarchar(max); " +
			"IF EXISTS (SELECT 1 FROM ec_1c_custom.sys.database_principals WHERE name = @p1) BEGIN SET @s = N'USE ec_1c_custom; DROP USER ' + QUOTENAME(@p1) + N';'; EXEC (@s); END; " +
			"IF EXISTS (SELECT 1 FROM sys.server_principals WHERE name = @p1) BEGIN SET @s = N'DROP LOGIN ' + QUOTENAME(@p1) + N';'; EXEC (@s); END;"
		if _, err := sa.DB().ExecContext(ctx, drop, login); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { sa.DB().ExecContext(itCtx(t), drop, login) })
		gs, err := model.SQLGrantScriptFor(model.SQLGrantRequest{Principal: login, Database: "ec_1c_custom"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sa.DB().ExecContext(ctx, gs.SQL); err == nil || !strings.Contains(err.Error(), "replace") {
			t.Fatalf("placeholder accepted: %v", err)
		}
		long := strings.Replace(gs.SQL, "N'"+gs.PasswordPlaceholder+"'", "N'"+strings.Repeat("x", 129)+"'", 1)
		if _, err := sa.DB().ExecContext(ctx, long); err == nil || !strings.Contains(err.Error(), "128") {
			t.Fatalf("long password accepted: %v", err)
		}
		filled, err := model.SQLGrantFillPassword(gs, pw)
		if err != nil {
			t.Fatal(err)
		}
		script := strings.Replace(gs.SQL, "N'"+gs.PasswordPlaceholder+"'", "N'"+pw+"'", 1)
		for i, sc := range []string{script, filled} { // idempotent
			if _, err := sa.DB().ExecContext(ctx, sc); err != nil {
				t.Fatalf("%q run %d: %v", login, i, err)
			}
		}
		var n int
		if err := sa.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM sys.server_principals WHERE name IN (N'sa')").Scan(&n); err != nil || n != 1 {
			t.Fatalf("sa login: %d %v", n, err)
		}
		p := pSA
		p.Login, p.Database = login, ""
		r, err := NewInteractiveReader(p, pw, Options{})
		if err != nil {
			t.Fatal(err)
		}
		res, err := r.Databases(ctx, DatabasesOptions{})
		r.Close()
		if err != nil || len(res.Databases) != 1 || res.Databases[0].Name != "ec_1c_custom" || !res.Databases[0].Probed || res.Databases[0].CompatLevel != itCompatLevel || res.Truncated {
			t.Errorf("%q: %+v %v", login, res, err)
		}
		// db_datareader only: no write.
		p.Database = "ec_1c_custom"
		st, err := Open(p, pw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.DB().ExecContext(ctx, "CREATE TABLE dbo.ec_wiz_probe (x int)"); err == nil {
			t.Errorf("%q can write", login)
		}
		st.Close()
	}
	if _, err := model.SQLGrantScriptFor(model.SQLGrantRequest{Principal: `CORP\EC-HOST$`, Windows: true, Database: "ec_1c_custom"}); err != nil {
		t.Fatal(err)
	}
}
