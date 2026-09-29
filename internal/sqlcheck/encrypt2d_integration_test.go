package sqlcheck

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_SQL_01_IntegrationConnectDisable2008: phase 2d — a profile with
// the "mssql2008" preset and no explicit encrypt connects to a database at
// compatibility level 100 without encryption (the session reports
// encrypt_option FALSE; sys.dm_exec_connections is used in this test only,
// CHK-SQL-05 bans DMVs in product SQL); encrypt "on" is still possible.
func TestCHK_SQL_01_IntegrationConnectDisable2008(t *testing.T) {
	p, pw := itProfile(t, "ec_1c_plans_off")
	ctx := itCtx(t)
	for _, c := range []struct {
		name      string
		setup     func(*model.Profile)
		encrypted string
	}{
		{"unset", func(q *model.Profile) { q.ServerPreset = model.ServerPresetMSSQL2008 }, "FALSE"},
		{"preset", func(q *model.Profile) { _ = q.ApplyServerPreset(model.ServerPresetMSSQL2008) }, "FALSE"},
		{"new profile", func(q *model.Profile) { q.ApplySQLDefaults() }, "FALSE"},
		{"on", func(q *model.Profile) {
			_ = q.ApplyServerPreset(model.ServerPresetMSSQL2008)
			q.Encrypt, q.TrustServerCert = model.EncryptOn, true
		}, "TRUE"},
	} {
		q := p
		q.Encrypt = model.EncryptDefault
		c.setup(&q)
		if err := TestConnection(ctx, q, pw); err != nil {
			t.Errorf("%s: connect: %v", c.name, err)
			continue
		}
		st, err := Open(q, pw)
		if err != nil {
			t.Fatal(err)
		}
		var enc string
		var level int
		err = st.DB().QueryRowContext(ctx, `SELECT c.encrypt_option, d.compatibility_level
			FROM sys.dm_exec_connections c CROSS JOIN sys.databases d
			WHERE c.session_id = @@SPID AND d.name = DB_NAME()`).Scan(&enc, &level)
		st.Close()
		if err != nil || enc != c.encrypted || level != itCompatLevel {
			t.Errorf("%s: encrypt_option %q (want %q), level %d (%v)", c.name, enc, c.encrypted, level, err)
		}
		if got := len(Warnings(q)) == 1; got != (c.encrypted == "FALSE") {
			t.Errorf("%s: warnings %v", c.name, Warnings(q))
		}
	}
}
