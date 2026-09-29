package sqlcheck

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Integration tests against a real SQL Server (CHK-SQL-01..04). They run
// only when EC_SQL_TEST_HOST is set (the linux CI job starts an
// mcr.microsoft.com/mssql/server container); otherwise they are skipped.
//
//	EC_SQL_TEST_HOST, EC_SQL_TEST_PORT (default 1433),
//	EC_SQL_TEST_USER (default sa), EC_SQL_TEST_PASSWORD
//
// The fixture databases are created from the same fxTable fixtures as the
// unit tests (fake_test.go), so the expectations are identical; "now" is
// injected as fixNow.

var itDatabases = map[string]func() []*fxTable{
	"ec_1c_node_off":   func() []*fxTable { return fx1CNode(2000) },
	"ec_1c_node_plain": func() []*fxTable { return fx1CNode(0) },
	"ec_1c_custom":     func() []*fxTable { return fx1CCustom(2000, "_Reference77") },
	"ec_1c_dirdates":   func() []*fxTable { return fx1CDirDates(2000) },
	"ec_generic":       fxGeneric,
	// phase 2b: two exchange plans, composite reference (customer clarification)
	"ec_1c_plans_off":   func() []*fxTable { return fx1CPlans(2000) },
	"ec_1c_plans_plain": func() []*fxTable { return fx1CPlans(0) },
}

// itCompatLevel: the target server is SQL Server 2008, so every fixture
// database runs at compatibility level 100 (CHK-SQL-05); the CI container
// is SQL Server 2019, which still supports level 100.
const itCompatLevel = 100

var (
	itOnce sync.Once
	itErr  error
)

func itBase(t *testing.T) (model.Profile, string) {
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
	p := model.Profile{ID: "it", Name: "integration", Server: host, Port: port, Database: "master",
		AuthMode: model.AuthSQL, Login: user, Enabled: true, ConnectTimeoutSec: 10, QueryTimeoutSec: 30}
	return p, os.Getenv("EC_SQL_TEST_PASSWORD")
}

// itProfile returns a profile for fixture database db (created once).
func itProfile(t *testing.T, db string) (model.Profile, string) {
	t.Helper()
	p, pw := itBase(t)
	itOnce.Do(func() { itErr = itSetup(p, pw) })
	if itErr != nil {
		t.Fatalf("integration setup: %v", itErr)
	}
	p.Database = db
	return p, pw
}

func itStore(t *testing.T, db string) *SQLStore {
	t.Helper()
	p, pw := itProfile(t, db)
	st, err := Open(p, pw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func itSetup(p model.Profile, pw string) error {
	st, err := Open(p, pw)
	if err != nil {
		return err
	}
	defer st.Close()
	// the container may still be starting: wait up to 120 s for a login
	deadline := time.Now().Add(120 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err = st.Ping(ctx)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("server not ready: %w", err)
		}
		time.Sleep(2 * time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for name, fx := range itDatabases {
		coll := " COLLATE Cyrillic_General_CI_AS" // as 1C creates its databases
		if name == "ec_generic" {
			coll = ""
		}
		for _, q := range []string{
			"IF DB_ID(N'" + name + "') IS NOT NULL BEGIN ALTER DATABASE " + QuoteIdent(name) +
				" SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE " + QuoteIdent(name) + " END",
			"CREATE DATABASE " + QuoteIdent(name) + coll,
			"ALTER DATABASE " + QuoteIdent(name) + " SET COMPATIBILITY_LEVEL = " + strconv.Itoa(itCompatLevel),
		} {
			if _, err := st.DB().ExecContext(ctx, q); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		var level int
		if err := st.DB().QueryRowContext(ctx, "SELECT compatibility_level FROM sys.databases WHERE name = @p1", name).Scan(&level); err != nil {
			return fmt.Errorf("%s: compatibility level: %w", name, err)
		}
		if level != itCompatLevel {
			return fmt.Errorf("%s: compatibility level %d, want %d", name, level, itCompatLevel)
		}
		dp := p
		dp.Database = name
		ds, err := Open(dp, pw)
		if err != nil {
			return err
		}
		for _, q := range fxDDL(fx()) {
			if _, err := ds.DB().ExecContext(ctx, q); err != nil {
				ds.Close()
				return fmt.Errorf("%s: %s: %w", name, q, err)
			}
		}
		ds.Close()
	}
	return nil
}

// fxDDL renders fixtures as T-SQL batches (CREATE SCHEMA / TABLE, INSERT).
func fxDDL(ts []*fxTable) []string {
	var out []string
	schemas := map[string]bool{"dbo": true}
	for _, t := range ts {
		if !schemas[t.ref.Schema] {
			schemas[t.ref.Schema] = true
			out = append(out, "CREATE SCHEMA "+QuoteIdent(t.ref.Schema))
		}
		var defs, names []string
		var keep []int
		for i, c := range t.cols {
			typ := c.DataType
			switch {
			case typ == "numeric" && strings.HasSuffix(c.Name, "No"):
				typ = "numeric(10,0)"
			case typ == "numeric":
				typ = "numeric(15,2)"
			case c.MaxLength == -1:
				typ += "(max)"
			case c.MaxLength > 0:
				typ += "(" + strconv.Itoa(c.MaxLength) + ")"
			}
			defs = append(defs, QuoteIdent(c.Name)+" "+typ+" NULL")
			if c.DataType != "timestamp" {
				names = append(names, QuoteIdent(c.Name))
				keep = append(keep, i)
			}
		}
		ref, err := t.ref.SQL()
		if err != nil {
			panic(err)
		}
		out = append(out, "CREATE TABLE "+ref+" ("+strings.Join(defs, ", ")+")")
		for _, r := range t.rows {
			var vals []string
			for _, i := range keep {
				vals = append(vals, sqlLiteral(t.cols[i], r[i]))
			}
			out = append(out, "INSERT INTO "+ref+" ("+strings.Join(names, ", ")+") VALUES ("+strings.Join(vals, ", ")+")")
		}
	}
	return out
}

func sqlLiteral(c Column, v interface{}) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		if c.DataType == "numeric" {
			return string(x)
		}
		return "0x" + hex.EncodeToString(x)
	case string:
		return "N'" + strings.ReplaceAll(x, "'", "''") + "'"
	case time.Time:
		return "'" + x.Format("2006-01-02T15:04:05") + "'"
	case int64:
		return strconv.FormatInt(x, 10)
	}
	panic(fmt.Sprintf("sqlLiteral %T", v))
}

func itCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func TestCHK_SQL_01_IntegrationConnect(t *testing.T) {
	p, pw := itProfile(t, "ec_1c_custom")
	ctx := itCtx(t)
	for _, e := range []model.EncryptMode{model.EncryptDefault, model.EncryptOptional, model.EncryptDisable, model.EncryptOn} {
		q := p
		q.Encrypt, q.TrustServerCert = e, e == model.EncryptOn
		if err := TestConnection(ctx, q, pw); err != nil {
			t.Errorf("encrypt %q: %v", e, err)
		}
	}
	q := p
	q.Encrypt, q.TLSMinVersion = model.EncryptOn, model.TLSMin12
	if err := TestConnection(ctx, q, pw); KindOf(err) != KindConn {
		t.Errorf("self-signed certificate must fail without trustServerCert: %v", err)
	}
	const wrong = "definitely-Wrong-9"
	err := TestConnection(ctx, p, wrong)
	if KindOf(err) != KindConn || strings.Contains(err.Error(), wrong) {
		t.Errorf("wrong password: %v", err)
	}
	q = p
	q.Database = "ec_no_such_db"
	if err := TestConnection(ctx, q, pw); KindOf(err) != KindConn {
		t.Errorf("unknown database: %v", err)
	}
	q = p
	q.Port, q.ConnectTimeoutSec = 1, 3
	start := time.Now()
	if err := TestConnection(ctx, q, pw); err == nil || (KindOf(err) != KindConn && KindOf(err) != KindTimeout) {
		t.Errorf("closed port: %v", err)
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("closed port took %v", time.Since(start))
	}
	r, err := NewReader(p, pw, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Test(ctx); err != nil {
		t.Errorf("reader Test: %v", err)
	}
}

func TestCHK_SQL_02_IntegrationDiscover(t *testing.T) {
	for _, off := range []int{2000, 0} {
		db := map[int]string{2000: "ec_1c_node_off", 0: "ec_1c_node_plain"}[off]
		cands, err := Discover(itCtx(t), itStore(t, db), DiscoverOptions{TopN: 20}, fixNow)
		if err != nil || len(cands) == 0 {
			t.Fatalf("%s: %v", db, err)
		}
		wantLinked(t, cands[0], "_Node123", "_InfoRg456", "_Fld457RRef", "_Fld458", "_Fld459", off)
		var counters bool
		for _, c := range cands {
			if c.Mode == model.CandidateCounters && c.Score < cands[0].Score && c.Points == 4 {
				counters = true
			}
		}
		if !counters {
			t.Errorf("%s: no low-score counters fallback%s", db, dump(cands))
		}
	}
	cands, err := Discover(itCtx(t), itStore(t, "ec_1c_custom"), DiscoverOptions{}, fixNow)
	if err != nil || len(cands) == 0 {
		t.Fatalf("custom: %v", err)
	}
	wantLinked(t, cands[0], "_Reference77", "_InfoRg88", "_Fld89RRef", "_Fld90", "_Fld91", 2000)
	for _, c := range cands {
		if c.Mode == model.CandidateCounters {
			t.Errorf("counters candidate without _Node: %+v", c)
		}
	}
	cands, err = Discover(itCtx(t), itStore(t, "ec_1c_dirdates"), DiscoverOptions{}, fixNow)
	if err != nil || len(cands) == 0 {
		t.Fatalf("dirdates: %v", err)
	}
	if c := cands[0]; c.Mode != model.CandidateSingle || c.Table != "_Reference55" || c.SendCol != "_Fld56" || c.RecvCol != "_Fld57" || c.YearOffset != 2000 {
		t.Errorf("dirdates top %+v%s", c, dump(cands))
	}
	cands, err = Discover(itCtx(t), itStore(t, "ec_generic"), DiscoverOptions{}, fixNow)
	if err != nil || len(cands) == 0 {
		t.Fatalf("generic: %v", err)
	}
	if c := cands[0]; c.Schema != "exch" || c.Table != "Node]Exchange" || c.PointCol != "Point Name" ||
		c.SendCol != "Send]Date" || c.RecvCol != "RecvDate" || c.YearOffset != 0 || c.Points != 3 {
		t.Errorf("generic top %+v%s", c, dump(cands))
	}
}

func TestCHK_SQL_02_IntegrationDiscoverByValue(t *testing.T) {
	st := itStore(t, "ec_1c_custom")
	approx := fixNow.Add(-45 * time.Minute)
	cands, err := DiscoverByValue(itCtx(t), st, model.ValueQuery{PointName: "магазин 2", ApproxTime: &approx}, DiscoverOptions{}, fixNow)
	if err != nil || len(cands) == 0 {
		t.Fatalf("by value: %v", err)
	}
	top := cands[0]
	wantLinked(t, top, "_Reference77", "_InfoRg88", "_Fld89RRef", "_Fld90", "_Fld91", 2000)
	if !top.HasHint(model.HintExactMatch) || !top.HasHint(model.HintNearApproxTime) || top.Matched == nil ||
		!top.Matched.LastRecv.Equal(fixNow.Add(-50*time.Minute)) {
		t.Errorf("by value top %+v", top)
	}
	cands, err = DiscoverByValue(itCtx(t), st, model.ValueQuery{PointName: "Склад 50%"}, DiscoverOptions{}, fixNow)
	if err != nil || len(cands) == 0 || cands[0].Matched.Point != "Склад 50%" {
		t.Errorf("literal %%: %v%s", err, dump(cands))
	}
	// '_' must be a literal too: "Магазин_1" must not match "Магазин 1"
	cands, err = DiscoverByValue(itCtx(t), st, model.ValueQuery{PointName: "Магазин_1"}, DiscoverOptions{}, fixNow)
	if err != nil || len(cands) != 0 {
		t.Errorf("LIKE escaping: %v%s", err, dump(cands))
	}
	cands, err = DiscoverByValue(itCtx(t), itStore(t, "ec_1c_node_off"), model.ValueQuery{PointName: "Магазин 1"}, DiscoverOptions{}, fixNow)
	if err != nil || len(cands) == 0 {
		t.Fatalf("node by value: %v", err)
	}
	wantLinked(t, cands[0], "_Node123", "_InfoRg456", "_Fld457RRef", "_Fld458", "_Fld459", 2000)
}

func pointsByName(pts []model.ExchangePoint) map[string]model.ExchangePoint {
	m := map[string]model.ExchangePoint{}
	for _, p := range pts {
		m[p.Name] = p
	}
	return m
}

func TestCHK_SQL_03_IntegrationListPoints(t *testing.T) {
	for db, off := range map[string]int{"ec_1c_node_off": 2000, "ec_1c_node_plain": 0} {
		p, pw := itProfile(t, db)
		p.Mapping = linkedProfile(off).Mapping
		r, err := NewReader(p, pw, fixOpt())
		if err != nil {
			t.Fatal(err)
		}
		pts, err := r.ListPoints(itCtx(t))
		r.Close()
		if err != nil {
			t.Fatalf("%s: %v", db, err)
		}
		m := pointsByName(pts)
		m1, m2, m3 := m["Магазин 1"], m["Магазин 2"], m["Магазин 3"]
		if len(pts) != 3 || m1.LastSend == nil || !m1.LastSend.Equal(fixNow.Add(-20*time.Minute)) ||
			!m1.LastRecv.Equal(fixNow.Add(-10*time.Minute)) || m1.Level != model.LevelOK {
			t.Errorf("%s: %+v", db, pts)
		}
		if m2.Level != model.LevelError || m3.LastSend != nil || m3.Message != model.ReasonNoTimestamps {
			t.Errorf("%s: Магазин 2/3 %+v %+v", db, m2, m3)
		}
	}
	p, pw := itProfile(t, "ec_generic")
	p.Mapping = model.Mapping{Mode: model.MappingAuto, Schema: "exch", Table: "Node]Exchange", PointNameColumn: "Point Name",
		PointIDColumn: "Id", LastSendColumn: "Send]Date", LastRecvColumn: "RecvDate"}
	r, err := NewReader(p, pw, fixOpt())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ps := Status(itCtx(t), r, p, fixNow)
	if ps.Error != "" || len(ps.Points) != 3 || !pointsByName(ps.Points)["West"].LastRecv.Equal(fixNow.Add(-21*time.Minute)) {
		t.Errorf("generic status %+v", ps)
	}
	bad := p
	bad.Port, bad.ConnectTimeoutSec = 1, 3
	br, _ := NewReader(bad, pw, fixOpt())
	defer br.Close()
	if ps := Status(itCtx(t), br, bad, fixNow); ps.Error == "" || ps.Level() != model.LevelError {
		t.Errorf("unreachable server status %+v", ps)
	}
}

func TestCHK_SQL_03_IntegrationCounters(t *testing.T) {
	p, pw := itProfile(t, "ec_1c_node_off")
	p.Mapping = model.Mapping{Mode: model.MappingCounters, Table: "_Node123", PointNameColumn: "_Description",
		PointIDColumn: "_Code", PointKeyColumn: "_IDRRef"}
	now := fixNow
	opt := Options{Now: func() time.Time { return now }, Counters: model.NewMemCounterStore()}
	r, err := NewReader(p, pw, opt)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pts, err := r.ListPoints(itCtx(t))
	if err != nil || len(pts) != 4 {
		t.Fatalf("first poll: %v %+v", err, pts)
	}
	for _, x := range pts {
		if !x.SendBaseline || !x.RecvBaseline || x.Source != model.PointSourceCounters {
			t.Errorf("baseline %+v", x)
		}
	}
	st := itStore(t, "ec_1c_node_off")
	if _, err := st.DB().ExecContext(itCtx(t), "UPDATE _Node123 SET _SentNo = _SentNo + 1 WHERE _Description = N'Магазин 1'"); err != nil {
		t.Fatal(err)
	}
	now = fixNow.Add(90 * time.Minute)
	pts, err = r.ListPoints(itCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	m := pointsByName(pts)
	if x := m["Магазин 1"]; x.SendBaseline || !x.RecvBaseline || !x.LastSend.Equal(now) || x.Level != model.LevelOK {
		t.Errorf("changed %+v", x)
	}
	if x := m["Магазин 2"]; !x.SendBaseline || !x.LastSend.Equal(fixNow) || x.Level != model.LevelError {
		t.Errorf("unchanged %+v", x)
	}
}

func TestCHK_SQL_04_IntegrationManualMapping(t *testing.T) {
	p, pw := itProfile(t, "ec_1c_custom")
	const q = "-- manual; mapping\nSELECT d._Description, t._Fld90, t._Fld91 FROM _Reference77 d JOIN _InfoRg88 t ON t._Fld89RRef = d._IDRRef;"
	p.Mapping = model.Mapping{Mode: model.MappingManual, ManualSQL: q, YearOffset: 2000}
	r, err := NewReader(p, pw, fixOpt())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pts, err := r.ListPoints(itCtx(t))
	if err != nil || len(pts) != 3 || !pointsByName(pts)["Магазин 1"].LastRecv.Equal(fixNow.Add(-15*time.Minute)) {
		t.Fatalf("manual: %v %+v", err, pts)
	}
	st := itStore(t, "ec_1c_custom")
	ctx := itCtx(t)
	if _, err := st.ManualPoints(ctx, "SELECT _Description, _Code FROM _Reference77", 10); !errors.Is(err, ErrManualColumns) {
		t.Errorf("2 columns: %v", err)
	}
	if _, err := st.ManualPoints(ctx, "SELECT a, b, c FROM no_such_table", 10); KindOf(err) != KindQuery {
		t.Errorf("bad table: %v", err)
	}
	if _, err := st.ManualPoints(ctx, "UPDATE _Reference77 SET _Code = N'x'", 10); !errors.Is(err, ErrManualNotSelect) {
		t.Errorf("update: %v", err)
	}
	m := model.Mapping{Mode: model.MappingManual, Table: "_Reference77", PointNameColumn: "_Description", PointKeyColumn: "_IDRRef",
		TimesTable: "_InfoRg88", TimesLinkColumn: "_Fld89RRef", LastSendColumn: "_Fld90", LastRecvColumn: "_Fld91", YearOffset: 2000}
	rows, err := PreviewMapping(ctx, st, m, 2)
	if err != nil || len(rows) != 2 {
		t.Errorf("preview: %v %+v", err, rows)
	}
	m.LastRecvColumn = "_Fld999"
	if err := ValidateMapping(ctx, st, m); !errors.Is(err, ErrColumnNotFound) {
		t.Errorf("validate: %v", err)
	}
}

// ---- phase 2b / SQL Server 2008 target -------------------------------

func TestCHK_SQL_05_IntegrationCompat100(t *testing.T) {
	ctx := itCtx(t)
	for db := range itDatabases {
		st := itStore(t, db)
		var level int
		if err := st.DB().QueryRowContext(ctx, "SELECT compatibility_level FROM sys.databases WHERE name = DB_NAME()").Scan(&level); err != nil || level != itCompatLevel {
			t.Errorf("%s: compatibility level %d (%v), want %d", db, level, err, itCompatLevel)
		}
	}
	// the SQL Server 2008 preset connects (TLS 1.0 allowed, the 2019 server negotiates 1.2)
	pp, ppw := itProfile(t, "ec_1c_plans_off")
	if err := pp.ApplyServerPreset(model.ServerPresetMSSQL2008); err != nil {
		t.Fatal(err)
	}
	for _, e := range []model.EncryptMode{model.EncryptDefault, model.EncryptOptional, model.EncryptDisable} {
		pp.Encrypt = e
		if err := TestConnection(ctx, pp, ppw); err != nil {
			t.Errorf("preset, encrypt %q: %v", e, err)
		}
	}
	// the level is effective: STRING_SPLIT needs level 130
	st := itStore(t, "ec_1c_plans_off")
	rows, err := st.DB().QueryContext(ctx, "SELECT value FROM STRING_SPLIT(N'a,b', N',')")
	if err == nil {
		rows.Close()
		t.Error("STRING_SPLIT works: the database is not at compatibility level 100")
	}
	// the manual SQL templates run at level 100 and return the fixture data
	for _, c := range []struct {
		db   string
		tpl  int
		off  int
		n    int
		name string
		recv time.Duration
	}{
		{"ec_1c_plans_off", 0, 2000, 3, "Магазин 1", 10 * time.Minute},
		{"ec_1c_plans_plain", 1, 0, 2, "Склад Б", 35 * time.Minute},
		{"ec_1c_plans_off", 1, 2000, 2, "Склад Б", 35 * time.Minute},
		{"ec_1c_plans_off", 2, 2000, 3, "Магазин 1", 10 * time.Minute},
		{"ec_1c_dirdates", 3, 2000, 3, "Магазин 2", 20 * time.Minute},
	} {
		p, pw := itProfile(t, c.db)
		p.ServerPreset = model.ServerPresetMSSQL2008
		p.Mapping = model.Mapping{Mode: model.MappingManual, ManualSQL: ManualSQLTemplates[c.tpl].SQL, YearOffset: c.off}
		r, err := NewReader(p, pw, fixOpt())
		if err != nil {
			t.Fatal(err)
		}
		pts, err := r.ListPoints(ctx)
		r.Close()
		x := pointsByName(pts)[c.name]
		if err != nil || len(pts) != c.n || x.LastRecv == nil || !x.LastRecv.Equal(fixNow.Add(-c.recv)) {
			t.Errorf("template %s on %s: %v %+v", ManualSQLTemplates[c.tpl].Name, c.db, err, pts)
		}
	}
}

func TestCHK_SQL_02_IntegrationPlans(t *testing.T) {
	for db, off := range map[string]int{"ec_1c_plans_off": 2000, "ec_1c_plans_plain": 0} {
		st := itStore(t, db)
		cands, err := Discover(itCtx(t), st, DiscoverOptions{TopN: 20}, fixNow)
		if err != nil || len(cands) < 2 {
			t.Fatalf("%s: %v%s", db, err, dump(cands))
		}
		c1, c2 := cands[0], cands[1]
		if c1.Table != "_Node124" || c1.TimesTable != "_InfoRg600" || c1.LinkRTRef != 124 || c1.LinkTypeCol != "_Fld601_TYPE" ||
			!c1.HasHint(model.HintCompositeRef) || c1.YearOffset != off {
			t.Errorf("%s: composite top %+v", db, c1)
		}
		wantLinked(t, c2, "_Node123", "_InfoRg456", "_Fld457RRef", "_Fld458", "_Fld459", off)
		for _, c := range cands {
			if model.PlanTableOf(c.Table) == "" && c2.Score-c.Score < 30 {
				t.Errorf("%s: distractor %s|%s %d vs %d", db, c.Table, c.TimesTable, c.Score, c2.Score)
			}
			if c.Table == "_Node123" && c.LinkTypeCol != "" {
				t.Errorf("%s: collision candidate %+v", db, c)
			}
		}
		// one profile, both plans
		ms := model.SuggestPlanMappings(cands)
		if len(ms) != 2 {
			t.Fatalf("%s: suggested %+v", db, ms)
		}
		p, pw := itProfile(t, db)
		p.ServerPreset = model.ServerPresetMSSQL2008
		p.Mapping, p.Mappings = ms[1], ms[:1]
		p.Mapping.PlanLabel, p.Mappings[0].PlanLabel = "Основной", "Магазин"
		r, err := NewReader(p, pw, fixOpt())
		if err != nil {
			t.Fatal(err)
		}
		ps := Status(itCtx(t), r, p, fixNow)
		r.Close()
		m := pointsByName(ps.Points)
		if ps.Error != "" || len(ps.Points) != 5 || m["Склад Б"].PlanLabel != "Магазин" || m["Магазин 1"].PlanTable != "_Node123" ||
			!m["Склад Б"].LastRecv.Equal(fixNow.Add(-35*time.Minute)) || !m["Склад Б"].LastSend.Equal(fixNow.Add(-40*time.Minute)) {
			t.Errorf("%s: multi-plan status %+v", db, ps)
		}
		// time-based DiscoverByValue
		approx := fixNow.Add(-36 * time.Minute)
		bt, err := DiscoverByValue(itCtx(t), st, model.ValueQuery{PointName: "Склад Б", ApproxTime: &approx, ToleranceMin: 30}, DiscoverOptions{}, fixNow)
		if err != nil || len(bt) == 0 || bt[0].Table != "_Node124" || bt[0].LinkRTRef != 124 || !bt[0].HasHint(model.HintByTime) {
			t.Errorf("%s: by time %v%s", db, err, dump(bt))
		}
		approx = fixNow.Add(-12 * time.Minute)
		bt, err = DiscoverByValue(itCtx(t), st, model.ValueQuery{PointRef: hex.EncodeToString(ref16(123, 2)), ApproxTime: &approx}, DiscoverOptions{}, fixNow)
		if err != nil || len(bt) == 0 {
			t.Fatalf("%s: by ref %v", db, err)
		}
		wantLinked(t, bt[0], "_Node123", "_InfoRg456", "_Fld457RRef", "_Fld458", "_Fld459", off)
		np, err := ListNodePoints(itCtx(t), st, DiscoverOptions{}, 0)
		if err != nil || len(np) != 7 {
			t.Errorf("%s: node points %v %+v", db, err, np)
		}
	}
}

// ---- phase 2c: read-only manual SQL, isolation level on pooled sessions ----

// sessionIsolation returns sys.dm_exec_sessions.transaction_isolation_level
// of conn's session (1 = READ UNCOMMITTED, 2 = READ COMMITTED). Test-only:
// product SQL uses no DMV (CHK-SQL-05).
func sessionIsolation(t *testing.T, ctx context.Context, conn *sql.Conn) int {
	t.Helper()
	var lvl int
	if err := conn.QueryRowContext(ctx, "SELECT transaction_isolation_level FROM sys.dm_exec_sessions WHERE session_id = @@SPID").Scan(&lvl); err != nil {
		t.Fatal(err)
	}
	return lvl
}

func drain(t *testing.T, ctx context.Context, conn *sql.Conn, q string, args ...interface{}) {
	t.Helper()
	rows, err := conn.QueryContext(ctx, q, args...)
	if err != nil {
		t.Fatalf("%v\n%s", err, q)
	}
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
}

func TestCHK_SQL_04_IntegrationIsolationNotSticky(t *testing.T) {
	st := itStore(t, "ec_1c_plans_off")
	ctx := itCtx(t)
	conn, err := st.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if lvl := sessionIsolation(t, ctx, conn); lvl != 2 {
		t.Fatalf("initial isolation %d", lvl)
	}
	// catalog queries on the same session (no pool reset in between)
	drain(t, ctx, conn, catalogSQL, catalogTableType)
	drain(t, ctx, conn, rowCountsSQL, rowCountsObjectType)
	drain(t, ctx, conn, rowCountsLegacySQL, rowCountsObjectType)
	if lvl := sessionIsolation(t, ctx, conn); lvl != 2 {
		t.Errorf("after catalog queries: isolation %d, want 2 (READ COMMITTED)", lvl)
	}
	// the method detects the phase-2 behaviour (session-level SET sticks)
	drain(t, ctx, conn, "SET TRANSACTION ISOLATION LEVEL READ UNCOMMITTED; SELECT 1")
	if lvl := sessionIsolation(t, ctx, conn); lvl != 1 {
		t.Errorf("control: isolation %d after SET, want 1", lvl)
	}
	restoreIsolation(conn, 10*time.Second)
	if lvl := sessionIsolation(t, ctx, conn); lvl != 2 {
		t.Errorf("restoreIsolation: %d", lvl)
	}
	// the manual query's READ UNCOMMITTED transaction leaves the level on
	// the session; ManualPoints restores it before the connection is reused
	pts, err := manualOnConn(ctx, conn, ManualSQLTemplates[0].SQL, 100)
	if err != nil || len(pts) != 3 {
		t.Fatalf("manual on conn: %v %+v", err, pts)
	}
	after := sessionIsolation(t, ctx, conn)
	restoreIsolation(conn, 10*time.Second)
	if lvl := sessionIsolation(t, ctx, conn); lvl != 2 {
		t.Errorf("after manual query + restore: isolation %d (before restore %d)", lvl, after)
	}
	t.Logf("isolation right after the rolled-back READ UNCOMMITTED manual tx: %d", after)
	// the pool: one connection only, ManualPoints then catalog, then check
	st.DB().SetMaxOpenConns(1)
	conn.Close()
	if _, err := st.ManualPoints(ctx, ManualSQLTemplates[0].SQL, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Columns(ctx); err != nil {
		t.Fatal(err)
	}
	c2, err := st.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if lvl := sessionIsolation(t, ctx, c2); lvl != 2 {
		t.Errorf("pooled connection after ManualPoints + Columns: isolation %d", lvl)
	}
}

func TestCHK_SQL_04_IntegrationManualBypassBlocked(t *testing.T) {
	st := itStore(t, "ec_1c_custom")
	ctx := itCtx(t)
	for _, q := range []string{
		"IF OBJECT_ID(N'dbo.ec_bypass') IS NOT NULL DROP TABLE dbo.ec_bypass",
		"CREATE TABLE dbo.ec_bypass (a int NULL)",
		"INSERT INTO dbo.ec_bypass (a) VALUES (1)",
	} {
		if _, err := st.DB().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int {
		var n int
		if err := st.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM dbo.ec_bypass").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, q := range []string{
		"SELECT 1, NULL, NULL COMMIT DELETE FROM dbo.ec_bypass",
		"SELECT 1, NULL, NULL DELETE FROM dbo.ec_bypass",
		"SELECT a, NULL, NULL INTO dbo.ec_bypass2 FROM dbo.ec_bypass",
		"SELECT 1, NULL, NULL\nCOMMIT\nTRUNCATE TABLE dbo.ec_bypass",
	} {
		_, err := st.ManualPoints(ctx, q, 10)
		if KindOf(err) != KindMapping || !errors.Is(err, ErrManualForbidden) {
			t.Errorf("%q: %v", q, err)
		}
		if n := count(); n != 1 {
			t.Fatalf("%q changed data: %d rows", q, n)
		}
	}
	var obj interface{}
	if err := st.DB().QueryRowContext(ctx, "SELECT OBJECT_ID(N'dbo.ec_bypass2')").Scan(&obj); err != nil || obj != nil {
		t.Errorf("SELECT INTO created a table: %v %v", obj, err)
	}
	// why: without the check the same text in the rolled-back transaction
	// really commits (phase-2 behaviour, run here directly, not via ManualPoints)
	conn, err := st.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadUncommitted})
	if err != nil {
		t.Fatal(err)
	}
	_, execErr := tx.ExecContext(ctx, "SELECT 1, NULL, NULL COMMIT DELETE FROM dbo.ec_bypass")
	_ = tx.Rollback()
	restoreIsolation(conn, 10*time.Second)
	t.Logf("unvalidated bypass: exec err %v, rows after rollback %d", execErr, count())
	if n := count(); n != 0 {
		t.Errorf("control: expected the unvalidated bypass to commit the DELETE, %d rows left", n)
	}
	_, _ = st.DB().ExecContext(ctx, "DROP TABLE dbo.ec_bypass")
}
