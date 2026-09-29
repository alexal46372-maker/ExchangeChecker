package sqlcheck

import (
	"context"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// CHK-SQL-05 "compat2008": the target server is SQL Server 2008
// (compatibility level 100). Every query the service can issue is built
// with representative mappings and fixtures and checked against the
// forbidden-construct list.

// pingSQL is what go-mssqldb v1.8.2 sends for PingContext (mssql.go).
const pingSQL = "select 1;"

// sqlRecorder wraps the fake store and renders, for every call, the exact
// T-SQL that SQLStore would send for it (same builders, same arguments).
type sqlRecorder struct {
	*fakeStore
	t    *testing.T
	sqls map[string]string // sql -> builder kind
}

func newRecorder(t *testing.T, fx []*fxTable) *sqlRecorder {
	return &sqlRecorder{fakeStore: newFake(fx...), t: t, sqls: map[string]string{}}
}

func (r *sqlRecorder) rec(kind, q string, err error) {
	r.t.Helper()
	if err != nil {
		r.t.Errorf("%s builder: %v", kind, err)
		return
	}
	r.sqls[q] = kind
}

func (r *sqlRecorder) Ping(ctx context.Context) error {
	r.rec("ping", pingSQL, nil)
	return r.fakeStore.Ping(ctx)
}

func (r *sqlRecorder) Columns(ctx context.Context) ([]Column, error) {
	r.rec("catalog", catalogSQL, nil)
	return r.fakeStore.Columns(ctx)
}

func (r *sqlRecorder) RowCounts(ctx context.Context) (map[TableRef]int64, error) {
	r.rec("rowcounts", rowCountsSQL, nil)
	r.rec("rowcounts_legacy", rowCountsLegacySQL, nil)
	return r.fakeStore.RowCounts(ctx)
}

func (r *sqlRecorder) SampleColumns(ctx context.Context, t TableRef, cols []string, limit int) ([][]interface{}, error) {
	q, err := buildSampleSQL(t, cols)
	r.rec("sample", q, err)
	return r.fakeStore.SampleColumns(ctx, t, cols, limit)
}

func (r *sqlRecorder) LinkStats(ctx context.Context, q LinkQuery) (LinkStats, error) {
	s, err := buildLinkStatsSQL(q)
	kind := "linkstats"
	if q.TypeCol != "" {
		kind = "linkstats_composite"
	}
	r.rec(kind, s, err)
	return r.fakeStore.LinkStats(ctx, q)
}

func (r *sqlRecorder) SingleStats(ctx context.Context, q SingleQuery) (SingleStats, error) {
	s, err := buildSingleStatsSQL(q)
	r.rec("singlestats", s, err)
	return r.fakeStore.SingleStats(ctx, q)
}

func (r *sqlRecorder) Points(ctx context.Context, m model.Mapping, limit int) ([]RawPoint, error) {
	s, err := buildPointsSQL(m)
	kind := "points_single"
	switch {
	case m.Composite():
		kind = "points_composite"
	case m.Linked() && m.KeepUnlinkedPoints:
		kind = "points_linked_keep"
	case m.Linked():
		kind = "points_linked"
	}
	r.rec(kind, s, err)
	return r.fakeStore.Points(ctx, m, limit)
}

func (r *sqlRecorder) SearchText(ctx context.Context, t TableRef, sel, texts []string, pattern string, limit int) ([][]interface{}, error) {
	s, err := buildSearchSQL(t, sel, texts, "LIKE")
	r.rec("search_like", s, err)
	return r.fakeStore.SearchText(ctx, t, sel, texts, pattern, limit)
}

func (r *sqlRecorder) SearchRef(ctx context.Context, t TableRef, sel, refs []string, key []byte, limit int) ([][]interface{}, error) {
	s, err := buildSearchSQL(t, sel, refs, "=")
	r.rec("search_ref", s, err)
	return r.fakeStore.SearchRef(ctx, t, sel, refs, key, limit)
}

func (r *sqlRecorder) SearchComposite(ctx context.Context, t TableRef, sel []string, ref CompositeRef, key []byte, limit int) ([][]interface{}, error) {
	s, err := buildCompositeSearchSQL(t, sel, ref)
	r.rec("search_composite", s, err)
	return r.fakeStore.SearchComposite(ctx, t, sel, ref, key, limit)
}

func (r *sqlRecorder) Counters(ctx context.Context, m model.Mapping, limit int) ([]RawCounter, error) {
	s, err := buildCountersSQL(m)
	r.rec("counters", s, err)
	return r.fakeStore.Counters(ctx, m, limit)
}

func (r *sqlRecorder) ManualPoints(ctx context.Context, query string, limit int) ([]RawPoint, error) {
	r.rec("manual", query, nil)
	return r.fakeStore.ManualPoints(ctx, query, limit)
}

// allRecordedSQL runs Discovery, DiscoverByValue (name, name + time, ref +
// time, code + plan), ListNodePoints, CheckMapping/PreviewMapping and
// ListPoints in every mapping mode on the fixtures and returns sql -> kind.
func allRecordedSQL(t *testing.T) map[string]string {
	all := map[string]string{}
	merge := func(r *sqlRecorder) {
		for q, k := range r.sqls {
			all[q] = k
		}
	}
	ctx := context.Background()
	at := func(d time.Duration) *time.Time { x := fixNow.Add(-d); return &x }
	fixtures := map[string][]*fxTable{
		"node0": fx1CNode(0), "node2000": fx1CNode(2000), "plans0": fx1CPlans(0), "plans2000": fx1CPlans(2000),
		"custom": fx1CCustom(2000, "_Reference77"), "dirdates": fx1CDirDates(2000), "generic": fxGeneric(),
	}
	for name, fx := range fixtures {
		r := newRecorder(t, fx)
		if err := r.Ping(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := Discover(ctx, r, DiscoverOptions{TopN: 20}, fixNow); err != nil {
			t.Fatalf("%s: discover: %v", name, err)
		}
		for _, q := range []model.ValueQuery{
			{PointName: "Магазин 2"}, {PointName: "Склад Б", ApproxTime: at(36 * time.Minute), ToleranceMin: 30},
			{PointRef: hex.EncodeToString(ref16(123, 2)), ApproxTime: at(12 * time.Minute)},
			{PointName: "001", PlanTable: "_Node123", ApproxTime: at(12 * time.Minute)},
			{PointName: "North", ApproxTime: at(7 * time.Minute)},
		} {
			if _, err := DiscoverByValue(ctx, r, q, DiscoverOptions{}, fixNow); err != nil && !errors.Is(err, ErrEmptyPointName) {
				t.Fatalf("%s: by value %+v: %v", name, q, err)
			}
		}
		if _, err := ListNodePoints(ctx, r, DiscoverOptions{}, 0); err != nil {
			t.Fatalf("%s: node points: %v", name, err)
		}
		merge(r)
	}
	// ListPoints / Preview in every mode
	counters := func(table string) model.Mapping {
		return model.Mapping{Mode: model.MappingCounters, Table: table, PointNameColumn: "_Description",
			PointIDColumn: "_Code", PointKeyColumn: "_IDRRef"}
	}
	keep := linkedProfile(0).Mapping
	keep.KeepUnlinkedPoints = true
	single := model.Mapping{Mode: model.MappingAuto, Schema: "exch", Table: "Node]Exchange", PointNameColumn: "Point Name",
		LastSendColumn: "Send]Date", LastRecvColumn: "RecvDate"}
	idOnly := model.Mapping{Mode: model.MappingManual, Table: "_Reference55", PointIDColumn: "_Code", LastRecvColumn: "_Fld57"}
	manualSQL := map[string]fakeManual{}
	for _, tpl := range ManualSQLTemplates {
		manualSQL[tpl.SQL] = fakeManual{}
	}
	for _, c := range []struct {
		fx       []*fxTable
		primary  model.Mapping
		extras   []model.Mapping
		isManual bool
	}{
		{fx1CPlans(2000), linkedProfile(2000).Mapping, []model.Mapping{compositeMapping(2000)}, false},
		{fx1CPlans(0), keep, nil, false},
		{fxGeneric(), single, nil, false},
		{fx1CDirDates(0), idOnly, nil, false},
		{fx1CPlans(0), counters("_Node123"), []model.Mapping{counters("_Node124")}, false},
		{fx1CPlans(0), model.Mapping{Mode: model.MappingManual, ManualSQL: ManualSQLTemplates[0].SQL},
			[]model.Mapping{{Mode: model.MappingManual, ManualSQL: ManualSQLTemplates[1].SQL}}, true},
	} {
		r := newRecorder(t, c.fx)
		r.manual = manualSQL
		p := connProfile()
		p.Mapping, p.Mappings = c.primary, c.extras
		p.ServerPreset = model.ServerPresetMSSQL2008
		opt := fixOpt()
		opt.Counters = model.NewMemCounterStore()
		rd := ReaderFor(r, p, opt)
		if _, err := rd.ListPoints(ctx); err != nil {
			t.Fatalf("list points %+v: %v", c.primary, err)
		}
		for _, m := range p.AllMappings() {
			if m.ManualSQL != "" {
				continue
			}
			if _, err := PreviewMapping(ctx, r, m, 5); err != nil {
				t.Fatalf("preview %+v: %v", m, err)
			}
		}
		merge(r)
	}
	all[restoreIsolationSQL] = "restore_isolation" // SQLStore.ManualPoints, after the rollback
	all[serverInfoSQL] = "server_info"             // TestConnectionInfo / SQLStore.ServerInfo (phase 4)
	all[serverPropsSQL] = "server_info_props"      // its fallback without sys.databases
	for _, tpl := range ManualSQLTemplates {
		if _, run := all[tpl.SQL]; !run {
			all[tpl.SQL] = "template:" + tpl.Name
		}
	}
	return all
}

func TestCHK_SQL_05_Compat2008AllQueries(t *testing.T) {
	all := allRecordedSQL(t)
	kinds := map[string]int{}
	var qs []string
	for q, k := range all {
		kinds[strings.SplitN(k, ":", 2)[0]]++
		qs = append(qs, q)
	}
	sort.Strings(qs)
	for _, q := range qs {
		if v := Compat2008Violations(q); len(v) > 0 {
			t.Errorf("%s: SQL Server 2012+ constructs %v in:\n%s", all[q], v, q)
		}
	}
	// every builder / query kind was exercised
	for _, k := range []string{"ping", "catalog", "rowcounts", "rowcounts_legacy", "sample", "linkstats", "linkstats_composite",
		"singlestats", "points_single", "points_linked", "points_linked_keep", "points_composite", "search_like",
		"search_ref", "search_composite", "counters", "manual", "template", "server_info", "server_info_props"} {
		if kinds[k] == 0 {
			t.Errorf("query kind %q was not exercised (kinds: %v)", k, kinds)
		}
	}
	if len(ManualSQLTemplates) < 4 || kinds["template"]+kinds["manual"] < len(ManualSQLTemplates) {
		t.Errorf("templates: %v", kinds)
	}
	for _, tpl := range ManualSQLTemplates {
		if _, ok := all[tpl.SQL]; !ok {
			t.Errorf("template %s not checked", tpl.Name)
		}
	}
	t.Logf("%d distinct queries checked: %v", len(all), kinds)
}

func TestCHK_SQL_05_Compat2008ManualTemplates(t *testing.T) {
	for _, tpl := range ManualSQLTemplates {
		if err := ValidateManualSQL(tpl.SQL); err != nil {
			t.Errorf("%s: %v", tpl.Name, err)
		}
		if tpl.Description == "" || tpl.Name == "" {
			t.Errorf("template without name/description: %+v", tpl)
		}
	}
	// the hints in the manual SQL errors suggest 2008 constructs only
	for _, e := range []error{ErrManualEmpty, ErrManualNotSelect, ErrManualMulti, ErrManualLiteral, ErrManualColumns, ErrManualCompat2008} {
		if v := Compat2008Violations(e.Error()); len(v) > 0 {
			t.Errorf("error text %q mentions %v", e, v)
		}
	}
	err := ValidateManualSQL("SELECT IIF(a > 0, a, 0), b, c FROM t ORDER BY a OFFSET 0 ROWS FETCH NEXT 10 ROWS ONLY")
	if !errors.Is(err, ErrManualCompat2008) || !strings.Contains(err.Error(), "IIF") || !strings.Contains(err.Error(), "OFFSET") ||
		!strings.Contains(err.Error(), "ROW_NUMBER() OVER") {
		t.Errorf("forbidden manual SQL: %v", err)
	}
	// through the reader: KindMapping like other manual SQL errors
	p := manualProfile("SELECT n, s, TRY_CONVERT(datetime, r) FROM t", 0)
	if _, err := NewQueryReader(newFake(), p, fixOpt()).ListPoints(context.Background()); KindOf(err) != KindMapping || !errors.Is(err, ErrManualCompat2008) {
		t.Errorf("reader: %v", err)
	}
}

func TestCHK_SQL_05_Compat2008CheckerFlagsEachToken(t *testing.T) {
	bad := map[string]string{
		"OFFSET ... ROWS":         "SELECT a FROM t ORDER BY a OFFSET 10 ROWS",
		"FETCH NEXT/FIRST":        "SELECT a FROM t ORDER BY a OFFSET 0 ROWS FETCH NEXT 5 ROWS ONLY",
		"TRY_CONVERT":             "SELECT try_convert(int, a) FROM t",
		"TRY_CAST":                "SELECT TRY_CAST(a AS int) FROM t",
		"TRY_PARSE":               "SELECT TRY_PARSE(a AS datetime USING 'ru-RU') FROM t",
		"PARSE":                   "SELECT PARSE(a AS datetime) FROM t",
		"IIF":                     "SELECT IIF(a > 1, 1, 0) FROM t",
		"CHOOSE":                  "SELECT CHOOSE(a, 'x', 'y') FROM t",
		"CONCAT":                  "SELECT Concat(a, b) FROM t",
		"CONCAT_WS":               "SELECT CONCAT_WS(',', a, b) FROM t",
		"FORMAT":                  "SELECT FORMAT(d, 'yyyy') FROM t",
		"STRING_AGG":              "SELECT STRING_AGG(a, ',') FROM t",
		"STRING_SPLIT":            "SELECT value FROM STRING_SPLIT('a,b', ',')",
		"STRING_ESCAPE":           "SELECT STRING_ESCAPE(a, 'json') FROM t",
		"TRIM":                    "SELECT TRIM(a) FROM t",
		"TRANSLATE":               "SELECT TRANSLATE(a, 'ab', 'cd') FROM t",
		"EOMONTH":                 "SELECT EOMONTH(d) FROM t",
		"*FROMPARTS":              "SELECT DATEFROMPARTS(2026, 9, 29), DATETIME2FROMPARTS(1,2,3,4,5,6,7,0)",
		"THROW":                   "BEGIN TRY SELECT 1 END TRY BEGIN CATCH THROW; END CATCH",
		"LAG":                     "SELECT LAG(a) OVER (ORDER BY b) FROM t",
		"LEAD":                    "SELECT LEAD(a, 1) OVER (ORDER BY b) FROM t",
		"FIRST_VALUE":             "SELECT FIRST_VALUE(a) OVER (ORDER BY b) FROM t",
		"LAST_VALUE":              "SELECT LAST_VALUE(a) OVER (ORDER BY b) FROM t",
		"PERCENT_RANK":            "SELECT PERCENT_RANK() OVER (ORDER BY b) FROM t",
		"CUME_DIST":               "SELECT CUME_DIST() OVER (ORDER BY b) FROM t",
		"PERCENTILE_CONT":         "SELECT PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY b) OVER () FROM t",
		"PERCENTILE_DISC":         "SELECT PERCENTILE_DISC(0.5) WITHIN GROUP (ORDER BY b) OVER () FROM t",
		"window frame ROWS/RANGE": "SELECT MAX(a) OVER (PARTITION BY c ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) FROM t",
		"WITHIN GROUP":            "SELECT STRING_AGG(a, ',') WITHIN GROUP (ORDER BY a) FROM t",
		"sys.dm_*":                "SELECT * FROM [sys].[dm_os_host_info]",
		"JSON_VALUE":              "SELECT JSON_VALUE(j, '$.a') FROM t",
		"JSON_QUERY":              "SELECT JSON_QUERY(j) FROM t",
		"JSON_MODIFY":             "SELECT JSON_MODIFY(j, '$.a', 1) FROM t",
		"JSON_OBJECT":             "SELECT JSON_OBJECT('a': 1)",
		"JSON_ARRAY":              "SELECT JSON_ARRAY(1, 2)",
		"ISJSON":                  "SELECT ISJSON(j) FROM t",
		"OPENJSON":                "SELECT * FROM OPENJSON(@j)",
		"FOR JSON":                "SELECT a FROM t FOR JSON PATH",
		"GREATEST":                "SELECT GREATEST(a, b) FROM t",
		"LEAST":                   "SELECT LEAST(a, b) FROM t",
		"DATETRUNC":               "SELECT DATETRUNC(day, d) FROM t",
		"DATE_BUCKET":             "SELECT DATE_BUCKET(day, 1, d) FROM t",
		"GENERATE_SERIES":         "SELECT value FROM GENERATE_SERIES(1, 10)",
		"DATEDIFF_BIG":            "SELECT DATEDIFF_BIG(ms, a, b) FROM t",
		"APPROX_COUNT_DISTINCT":   "SELECT APPROX_COUNT_DISTINCT(a) FROM t",
		"NEXT VALUE FOR":          "SELECT NEXT VALUE FOR dbo.seq",
		"AT TIME ZONE":            "SELECT d AT TIME ZONE 'UTC' FROM t",
		"CREATE OR ALTER":         "CREATE OR ALTER VIEW v AS SELECT 1 AS a",
		"DROP ... IF EXISTS":      "DROP TABLE IF EXISTS t",
		ruleWindowedAggregate:     "SELECT SUM(x) OVER (PARTITION BY y ORDER BY z) FROM t",
	}
	for _, rule := range Compat2008Rules() {
		q, ok := bad[rule]
		if !ok {
			t.Errorf("rule %q has no positive example", rule)
			continue
		}
		found := false
		for _, v := range Compat2008Violations(q) {
			found = found || v == rule
		}
		if !found {
			t.Errorf("%q not flagged in %q (got %v)", rule, q, Compat2008Violations(q))
		}
	}
	for _, q := range []string{
		"select count_big(*) over (partition by a order by b) from t", // lower case, windowed aggregate
		"SELECT COUNT(DISTINCT a) OVER (ORDER BY b) FROM t",
		"SELECT MAX(CASE WHEN x > 0 THEN y END) OVER (PARTITION BY a ORDER BY b) FROM t",
		"SELECT a\nFROM t\nORDER BY a\nOFFSET @skip ROWS",
	} {
		if len(Compat2008Violations(q)) == 0 {
			t.Errorf("not flagged: %q", q)
		}
	}
	// allowed on SQL 2005/2008 (compatibility level 100)
	for _, q := range []string{
		"SELECT TOP (@p1) a FROM t WITH (NOLOCK)",
		"SELECT ROW_NUMBER() OVER (PARTITION BY a ORDER BY b DESC) AS rn FROM t",
		"SELECT RANK() OVER (ORDER BY b), DENSE_RANK() OVER (ORDER BY b), NTILE(4) OVER (ORDER BY b) FROM t",
		"SELECT COUNT(*) OVER (PARTITION BY a), SUM(x) OVER (PARTITION BY a), MAX(y) OVER () FROM t",
		"SELECT CAST(d AS datetime2), CAST(d AS date), CONVERT(datetime, s, 120), SYSDATETIME(), SYSDATETIMEOFFSET() FROM t",
		"SELECT s.name, SUM(p.rows) FROM sys.partitions AS p JOIN sys.objects AS o ON o.object_id = p.object_id JOIN sys.schemas AS s ON s.schema_id = o.schema_id GROUP BY s.name",
		"SELECT c.TABLE_NAME FROM INFORMATION_SCHEMA.COLUMNS AS c",
		"MERGE INTO t USING s ON t.a = s.a WHEN MATCHED THEN UPDATE SET b = s.b;",
		"SELECT [Format], [IIF(], [Concat], [Offset Rows], 'TRY_CONVERT(' , N'LAG(x)', \"lead(\" FROM [dbo].[Log]",
		"-- OFFSET 1 ROWS; LAG(x)\nSELECT 1 /* FORMAT(x) THROW */",
		"SELECT _Flag, lead_time, _Fld1Offset, choose_id FROM t",
		"SELECT a FROM t WHERE b = 'x' ORDER BY a",
		"SELECT MAX(CASE WHEN n.k IS NOT NULL THEN t.c0 END) FROM t",
	} {
		if v := Compat2008Violations(q); len(v) > 0 {
			t.Errorf("false positive %v in %q", v, q)
		}
	}
}
