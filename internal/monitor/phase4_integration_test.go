package monitor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_IPC_03_IntegrationTestProfile — test_profile against the real
// SQL Server of the linux CI job (EC_SQL_TEST_*, skipped otherwise)
// through the production path (monitor.SQLTest = sqlcheck.
// TestConnectionInfo + sqlcheck.ConnCategory): the saved profile
// (encrypt "disable", mssql2008 preset) connects and reports the server
// version and compatibility level 100; the TLS 1.0 floor with login
// encryption works too; a wrong password is "login", an unknown database
// "database", a closed port "network"; the password is not in any error.
func TestCHK_IPC_03_IntegrationTestProfile(t *testing.T) {
	p, pw := itMonSetup(t) // database at compatibility level 100
	m := New(Options{Source: NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{p},
		Passwords: map[string]string{p.ID: pw}}), CommandInterval: -1, PollInterval: time.Hour, ConfigCheck: time.Hour})
	m.reloadConfig(true)
	ctx := context.Background()
	r, err := m.TestProfile(ctx, model.IPCRequest{ProfileID: p.ID})
	if err != nil || !r.OK || r.Category != model.TestOK || r.ServerVersion == "" || r.CompatLevel != 100 || r.Encrypt != "disable" {
		t.Fatalf("saved profile: %+v %v", r, err)
	}
	t.Logf("server %s %s %s, compat %d, %d ms", r.ServerVersion, r.ProductLevel, r.Edition, r.CompatLevel, r.DurationMs)
	tls := p
	tls.Encrypt, tls.TLSMinVersion = model.EncryptOptional, model.TLSMin10
	if r, err := m.TestProfile(ctx, model.IPCRequest{Profile: &tls}); err != nil || !r.OK || r.TLSMin != "1.0" {
		t.Errorf("TLS 1.0 floor (saved password reused, stronger encryption): %+v %v", r, err)
	}
	const wrong = "Wrong-Passw0rd-XYZ"
	cases := []struct {
		name string
		mut  func(*model.Profile)
		pw   string
		cat  string
	}{
		{"wrong password", func(*model.Profile) {}, wrong, model.TestLogin},
		{"unknown database", func(q *model.Profile) { q.Database = "ec_no_such_db" }, pw, model.TestDatabase},
		{"closed port", func(q *model.Profile) { q.Server, q.Port, q.ConnectTimeoutSec = "127.0.0.1", 1, 3 }, pw, model.TestNetwork},
	}
	for _, c := range cases {
		q := p
		q.ID = "probe"
		c.mut(&q)
		r, err := m.TestProfile(ctx, model.IPCRequest{Profile: &q, Password: c.pw})
		if err != nil || r.OK || r.Category != c.cat {
			t.Errorf("%s: %+v %v", c.name, r, err)
		}
		if strings.Contains(r.Error, c.pw) {
			t.Errorf("%s: password in the error", c.name)
		}
	}
}
