package sqlcheck

import (
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_SQL_05_IntegrationServerInfo: TestConnectionInfo on a database at
// compatibility level 100 with the default encrypt ("disable", phase 2d),
// the "mssql2008" preset (TLS 1.0 allowed) and full TLS; a login with only
// db_datareader sees its compatibility level; a login denied SELECT on
// sys.databases still passes the connection test, with the version and
// CompatLevel 0.
func TestCHK_SQL_05_IntegrationServerInfo(t *testing.T) {
	p, pw := itProfile(t, "ec_1c_plans_off")
	ctx := itCtx(t)
	for _, c := range []struct {
		name  string
		setup func(*model.Profile)
	}{
		{"default (disable)", func(q *model.Profile) {}},
		{"preset", func(q *model.Profile) { _ = q.ApplyServerPreset(model.ServerPresetMSSQL2008) }},
		{"preset optional TLS 1.0", func(q *model.Profile) {
			_ = q.ApplyServerPreset(model.ServerPresetMSSQL2008)
			q.Encrypt = model.EncryptOptional
		}},
		{"on", func(q *model.Profile) { q.Encrypt, q.TrustServerCert = model.EncryptOn, true }},
	} {
		q := p
		q.Encrypt = model.EncryptDefault
		c.setup(&q)
		info, err := TestConnectionInfo(ctx, q, pw)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if info.Version == "" || !strings.Contains(info.Version, ".") || info.ProductLevel == "" || info.Edition == "" || info.CompatLevel != itCompatLevel {
			t.Errorf("%s: %+v", c.name, info)
		}
		if c.name == "on" {
			t.Logf("server: %+v", info)
		}
	}
	if err := TestConnection(ctx, p, pw); err != nil {
		t.Errorf("TestConnection wrapper: %v", err)
	}

	// a monitoring login with db_datareader only (PLAN.md §12 п. 30)
	const login, lpw = "ec_reader_p4", "Ec_Reader-p4Passw0rd!"
	admin := itStore(t, "master")
	exec := func(db, q string) {
		t.Helper()
		if _, err := admin.DB().ExecContext(ctx, "USE ["+db+"]; "+q); err != nil {
			t.Fatalf("%s: %s: %v", db, q, err)
		}
	}
	exec("ec_1c_plans_off", "IF USER_ID(N'"+login+"') IS NOT NULL DROP USER ["+login+"]")
	exec("master", "IF USER_ID(N'"+login+"') IS NOT NULL DROP USER ["+login+"]")
	exec("master", "IF SUSER_ID(N'"+login+"') IS NOT NULL DROP LOGIN ["+login+"]")
	exec("master", "CREATE LOGIN ["+login+"] WITH PASSWORD = N'"+lpw+"', CHECK_POLICY = OFF")
	exec("ec_1c_plans_off", "CREATE USER ["+login+"] FOR LOGIN ["+login+"]; EXEC sp_addrolemember N'db_datareader', N'"+login+"'")
	t.Cleanup(func() {
		for _, q := range []string{
			"USE [ec_1c_plans_off]; IF USER_ID(N'" + login + "') IS NOT NULL DROP USER [" + login + "]",
			"USE [master]; IF USER_ID(N'" + login + "') IS NOT NULL DROP USER [" + login + "]",
			"USE [master]; IF SUSER_ID(N'" + login + "') IS NOT NULL DROP LOGIN [" + login + "]",
		} {
			_, _ = admin.DB().ExecContext(ctx, q)
		}
	})
	r := p
	r.Login = login
	info, err := TestConnectionInfo(ctx, r, lpw)
	if err != nil || info.CompatLevel != itCompatLevel || info.Version == "" {
		t.Errorf("db_datareader login: %+v %v", info, err)
	}
	// no access to sys.databases: connection OK, CompatLevel 0, no error
	exec("master", "CREATE USER ["+login+"] FOR LOGIN ["+login+"]; DENY SELECT ON sys.databases TO ["+login+"]")
	info, err = TestConnectionInfo(ctx, r, lpw)
	if err != nil || info.CompatLevel != 0 || info.Version == "" || info.Edition == "" {
		t.Errorf("denied sys.databases: %+v %v", info, err)
	}
	t.Logf("denied sys.databases: %+v", info)
}

// TestCHK_SQL_01_IntegrationConnCategory: real server errors are sorted
// into "login" (wrong password) and "database" (unknown database).
func TestCHK_SQL_01_IntegrationConnCategory(t *testing.T) {
	p, pw := itProfile(t, "ec_1c_custom")
	ctx := itCtx(t)
	_, err := TestConnectionInfo(ctx, p, "definitely-Wrong-9")
	if ConnCategory(err) != ConnCategoryLogin {
		t.Errorf("wrong password: %v -> %q", err, ConnCategory(err))
	}
	q := p
	q.Database = "ec_no_such_db"
	_, err = TestConnectionInfo(ctx, q, pw)
	if ConnCategory(err) != ConnCategoryDatabase {
		t.Errorf("unknown database: %v -> %q", err, ConnCategory(err))
	}
	q = p
	q.Login = "ec_no_such_login"
	if err := TestConnection(ctx, q, pw); ConnCategory(err) != ConnCategoryLogin {
		t.Errorf("unknown login: %v -> %q", err, ConnCategory(err))
	}
	if _, err := TestConnectionInfo(ctx, p, pw); err != nil || ConnCategory(err) != "" {
		t.Errorf("good profile: %v", err)
	}
}
