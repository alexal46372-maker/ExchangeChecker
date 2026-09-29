package monitor

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// CHK-MON-09: full counters cycle against a real SQL Server (linux CI job,
// env EC_SQL_TEST_HOST/PORT/USER/PASSWORD as in internal/sqlcheck; skipped
// otherwise). The database runs at compatibility level 100 (SQL Server
// 2008, CHK-SQL-05) and the profile is created with model.NewProfile, i.e.
// with the default "mssql2008" preset (CHK-MON-08).

const itMonDB = "ec_mon_counters"

func itMonBase(t *testing.T) (model.Profile, string) {
	t.Helper()
	host := os.Getenv("EC_SQL_TEST_HOST")
	if host == "" {
		t.Skip("EC_SQL_TEST_HOST is not set: SQL Server integration tests run in the linux CI job")
	}
	port := 1433
	if s := os.Getenv("EC_SQL_TEST_PORT"); s != "" {
		var err error
		if port, err = strconv.Atoi(s); err != nil {
			t.Fatalf("EC_SQL_TEST_PORT: %v", err)
		}
	}
	user := os.Getenv("EC_SQL_TEST_USER")
	if user == "" {
		user = "sa"
	}
	p := model.NewProfile("mon-it")
	p.Name, p.Server, p.Port, p.Database, p.Login = "monitor integration", host, port, "master", user
	p.ConnectTimeoutSec, p.QueryTimeoutSec = 10, 30
	return p, os.Getenv("EC_SQL_TEST_PASSWORD")
}

func itExec(t *testing.T, st *sqlcheck.SQLStore, qs ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, q := range qs {
		if _, err := st.DB().ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func itMonSetup(t *testing.T) (model.Profile, string) {
	t.Helper()
	p, pw := itMonBase(t)
	st, err := sqlcheck.Open(p, pw)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	deadline := time.Now().Add(120 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err = st.Ping(ctx)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server not ready: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
	db := sqlcheck.QuoteIdent(itMonDB)
	itExec(t, st,
		"IF DB_ID(N'"+itMonDB+"') IS NOT NULL BEGIN ALTER DATABASE "+db+" SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE "+db+" END",
		"CREATE DATABASE "+db+" COLLATE Cyrillic_General_CI_AS",
		"ALTER DATABASE "+db+" SET COMPATIBILITY_LEVEL = 100")
	p.Database = itMonDB
	ds, err := sqlcheck.Open(p, pw)
	if err != nil {
		t.Fatal(err)
	}
	defer ds.Close()
	var stmts []string
	for _, tbl := range []string{"_Node300", "_Node301"} {
		stmts = append(stmts, "CREATE TABLE "+tbl+" (_IDRRef binary(16) NOT NULL, _Code nvarchar(9) NULL, _Description nvarchar(50) NULL, _SentNo numeric(10,0) NULL, _ReceivedNo numeric(10,0) NULL)")
	}
	stmts = append(stmts,
		"INSERT INTO _Node300 VALUES (0x00000000000000000000000000000001, N'001', N'Магазин 1', 10, 20)",
		"INSERT INTO _Node300 VALUES (0x00000000000000000000000000000002, N'002', N'Магазин 2', 5, 5)",
		// the same _IDRRef in the second plan: keys must not collide (Table/ prefix)
		"INSERT INTO _Node301 VALUES (0x00000000000000000000000000000001, N'001', N'Магазин 1', 1, 1)")
	itExec(t, ds, stmts...)
	return p, pw
}

func pointsOf(s model.ProfileSnapshot) map[string]model.PointStatus {
	out := map[string]model.PointStatus{}
	for _, x := range s.Points {
		out[x.PlanTable+"/"+x.Name] = x
	}
	return out
}

// TestCHK_MON_09_IntegrationCountersCycle — a changed _SentNo yields a new
// time, unchanged counters stay "counting since observation", and a
// monitor restart (new Monitor + FileCounterStore over the same file)
// keeps the baseline and the observed change times.
func TestCHK_MON_09_IntegrationCountersCycle(t *testing.T) {
	p, pw := itMonSetup(t)
	if p.ServerPreset != model.ServerPresetMSSQL2008 {
		t.Fatalf("new profile preset %q", p.ServerPreset)
	}
	counters := func(tbl string) model.Mapping {
		return model.Mapping{Mode: model.MappingCounters, Table: tbl, PointNameColumn: "_Description",
			PointIDColumn: "_Code", PointKeyColumn: "_IDRRef"}
	}
	p.Mapping = counters("_Node300")
	p.Mappings = []model.Mapping{counters("_Node301")}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	clk := &fakeClock{t: time.Date(2026, 9, 29, 9, 0, 0, 0, model.DBZone)}
	t0 := clk.Now()
	cfg := Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{p}, Passwords: map[string]string{p.ID: pw}}
	newMon := func() *Monitor {
		m := New(Options{Source: NewMemConfig(cfg), Counters: OpenFileCounterStore(dir, nil), Now: clk.Now,
			Jitter: -1, PollInterval: time.Hour, ConfigCheck: time.Hour})
		m.reloadConfig(true)
		return m
	}
	poll := func(m *Monitor) map[string]model.PointStatus {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		m.pollAll(ctx)
		s := m.Snapshot().Profiles[0]
		if s.Error != "" {
			t.Fatalf("poll error: %s (%s)", s.Error, s.ErrorKey)
		}
		return pointsOf(s)
	}

	m := newMon()
	pts := poll(m)
	if len(pts) != 3 {
		t.Fatalf("points %v", pts)
	}
	for k, x := range pts {
		if !x.Baseline || !x.FreshAt.Equal(t0) || x.Source != model.PointSourceCounters || x.Level != model.LevelOK {
			t.Errorf("first poll %s: %+v", k, x)
		}
	}
	if w := m.Snapshot().Profiles[0].Warnings; len(w) != 1 || w[0] != sqlcheck.MsgWarnPlaintextLogin {
		t.Errorf("SQL auth without encryption must carry the plaintext-login warning: %v", w)
	}

	st, err := sqlcheck.Open(p, pw)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	clk.Add(40 * time.Minute)
	t1 := clk.Now()
	itExec(t, st, "UPDATE _Node300 SET _SentNo = _SentNo + 1 WHERE _Code = N'001'")
	pts = poll(m)
	if x := pts["_Node300/Магазин 1"]; x.Baseline || !x.FreshAt.Equal(t1) || !x.LastSend.Equal(t1) || x.Level != model.LevelOK || x.LagMinutes != 0 {
		t.Errorf("changed _SentNo: %+v", x)
	}
	if x := pts["_Node300/Магазин 2"]; !x.Baseline || !x.FreshAt.Equal(t0) || x.Level != model.LevelWarning || x.LagMinutes != 40 {
		t.Errorf("unchanged point: %+v", x)
	}
	if x := pts["_Node301/Магазин 1"]; !x.Baseline || !x.FreshAt.Equal(t0) {
		t.Errorf("second plan must not see the first plan's change: %+v", x)
	}
	if x := pts["_Node300/Магазин 1"]; x.FreshAt.Location() != model.DBZone {
		t.Errorf("zone %v", x.FreshAt.Location())
	}
	m.closeAll()

	// service restart: nothing changes in the database meanwhile
	clk.Add(5 * time.Minute)
	m2 := newMon()
	defer m2.closeAll()
	pts = poll(m2)
	if x := pts["_Node300/Магазин 1"]; x.Baseline || !x.FreshAt.Equal(t1) || x.LagMinutes != 5 {
		t.Errorf("after restart, observed change time lost: %+v", x)
	}
	if x := pts["_Node300/Магазин 2"]; !x.Baseline || !x.FreshAt.Equal(t0) || x.LagMinutes != 45 {
		t.Errorf("after restart, baseline reset: %+v", x)
	}
	// a change after the restart is detected against the restored state
	clk.Add(time.Minute)
	t2 := clk.Now()
	itExec(t, st, "UPDATE _Node300 SET _ReceivedNo = _ReceivedNo + 1 WHERE _Code = N'002'")
	pts = poll(m2)
	if x := pts["_Node300/Магазин 2"]; x.Baseline || !x.FreshAt.Equal(t2) || !x.LastRecv.Equal(t2) || x.Level != model.LevelOK {
		t.Errorf("recv change after restart: %+v", x)
	}
	if b, err := os.ReadFile(OpenFileCounterStore(dir, nil).Path()); err != nil || len(b) == 0 {
		t.Errorf("state file: %v", err)
	}
	fmt.Fprintf(os.Stderr, "CHK-MON-09: counters cycle OK on %s (compat 100, preset %s)\n", itMonDB, p.ServerPreset)
}
