package sqlcheck

import (
	"context"
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
}

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
		} {
			if _, err := st.DB().ExecContext(ctx, q); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
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
	for _, e := range []model.EncryptMode{model.EncryptDefault, model.EncryptDisable, model.EncryptOn} {
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
