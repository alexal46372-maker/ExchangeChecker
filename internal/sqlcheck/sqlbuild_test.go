package sqlcheck

import (
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func TestCHK_SQL_02_QuoteIdent(t *testing.T) {
	cases := map[string]string{
		"_Node123":          "[_Node123]",
		"Point Name":        "[Point Name]",
		"a]b":               "[a]]b]",
		"]]":                "[]]]]]",
		"x];DROP TABLE t--": "[x]];DROP TABLE t--]",
		"[x]":               "[[x]]]",
		"Узел'обмена":       "[Узел'обмена]",
	}
	for in, want := range cases {
		if got := QuoteIdent(in); got != want {
			t.Errorf("QuoteIdent(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "a\x00b", string([]byte{0xff}), strings.Repeat("x", MaxIdentLen+1)} {
		if _, err := quoteChecked(bad); err == nil {
			t.Errorf("quoteChecked(%q) accepted", bad)
		}
	}
	if _, err := quoteChecked(strings.Repeat("я", MaxIdentLen)); err != nil {
		t.Errorf("128 runes rejected: %v", err)
	}
	if s, _ := (TableRef{Schema: "ex]ch", Name: "Node]Exchange"}).SQL(); s != "[ex]]ch].[Node]]Exchange]" {
		t.Errorf("TableRef.SQL = %s", s)
	}
	if s, _ := (TableRef{Name: "_InfoRg88"}).SQL(); s != "[_InfoRg88]" {
		t.Errorf("unqualified = %s", s)
	}
	if (TableRef{Schema: "dbo", Name: "t"}).String() != "dbo.t" {
		t.Error("TableRef.String")
	}
}

func TestCHK_SQL_02_LikeContains(t *testing.T) {
	cases := map[string]string{
		"Магазин 1": "%Магазин 1%",
		"50%":       "%50[%]%",
		"a_b":       "%a[_]b%",
		"[x]":       "%[[]x]%",
		"it's":      "%it's%",
	}
	for in, want := range cases {
		if got := LikeContains(in); got != want {
			t.Errorf("LikeContains(%q) = %q, want %q", in, got, want)
		}
	}
}

// Every generated query: quoted identifiers, NOLOCK on user tables, values
// only as @p1/@p2, no statement separators.
func checkSQL(t *testing.T, name, q string, wantParts ...string) {
	t.Helper()
	for _, w := range wantParts {
		if !strings.Contains(q, w) {
			t.Errorf("%s: missing %q in\n%s", name, w, q)
		}
	}
	if strings.Contains(q, ";") {
		t.Errorf("%s: statement separator in\n%s", name, q)
	}
	if !strings.Contains(q, "TOP (@p1)") {
		t.Errorf("%s: no parameterized TOP", name)
	}
}

func TestCHK_SQL_02_SQLBuilders(t *testing.T) {
	tr := TableRef{Schema: "exch", Name: "Node]Exchange"}
	q, err := buildSampleSQL(tr, []string{"Point Name", "Send]Date"})
	if err != nil {
		t.Fatal(err)
	}
	checkSQL(t, "sample", q, "[Point Name], [Send]]Date] FROM [exch].[Node]]Exchange] WITH (NOLOCK)")

	q, err = buildSearchSQL(tr, []string{"Point Name", "Id"}, []string{"Point Name", "Alias]"}, "LIKE")
	if err != nil {
		t.Fatal(err)
	}
	checkSQL(t, "search text", q, "WITH (NOLOCK) WHERE [Point Name] LIKE @p2 OR [Alias]]] LIKE @p2")
	q, _ = buildSearchSQL(tr, []string{"_Fld1RRef"}, []string{"_Fld1RRef"}, "=")
	checkSQL(t, "search ref", q, "WHERE [_Fld1RRef] = @p2")
	if _, err := buildSearchSQL(tr, []string{"a"}, []string{"a"}, "; DROP"); err == nil {
		t.Error("bad operator accepted")
	}

	q, err = buildLinkStatsSQL(LinkQuery{Points: TableRef{Schema: "dbo", Name: "_Reference77"}, KeyCol: "_IDRRef",
		Times: TableRef{Schema: "dbo", Name: "_InfoRg88"}, LinkCol: "_Fld89RRef", DateCols: []string{"_Fld90", "_Fld91"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	checkSQL(t, "link", q, "FROM [dbo].[_InfoRg88] WITH (NOLOCK)", "FROM [dbo].[_Reference77] WITH (NOLOCK)",
		"[_Fld89RRef] AS l", "MAX(CASE WHEN n.k IS NOT NULL THEN t.c1 END)", "COUNT_BIG(DISTINCT n.k)")

	q, _ = buildSingleStatsSQL(SingleQuery{Table: tr, PointCol: "Point Name", DateCols: []string{"Send]Date"}, Limit: 5})
	checkSQL(t, "single", q, "[Point Name] AS p", "[Send]]Date] AS c0", "WITH (NOLOCK)")

	single := model.Mapping{Mode: model.MappingManual, Schema: "exch", Table: "Node]Exchange", PointNameColumn: "Point Name", LastSendColumn: "Send]Date"}
	q, err = buildPointsSQL(single)
	if err != nil {
		t.Fatal(err)
	}
	checkSQL(t, "points single", q, "[Point Name] AS n", "CAST(NULL AS nvarchar(1)) AS i", "[Send]]Date] AS s",
		"CAST(NULL AS datetime) AS r", "FROM [exch].[Node]]Exchange] WITH (NOLOCK)", "GROUP BY t.n, t.i")

	linked := model.Mapping{Mode: model.MappingAuto, Table: "_Reference77", PointNameColumn: "_Description", PointIDColumn: "_Code",
		PointKeyColumn: "_IDRRef", TimesTable: "_InfoRg88", TimesLinkColumn: "_Fld89RRef", LastSendColumn: "_Fld90", LastRecvColumn: "_Fld91"}
	q, _ = buildPointsSQL(linked)
	checkSQL(t, "points linked", q, "INNER JOIN", "FROM [_Reference77] WITH (NOLOCK)", "FROM [_InfoRg88] WITH (NOLOCK)",
		"ON t.l = p.k", "GROUP BY p.k, p.n, p.i")
	linked.KeepUnlinkedPoints = true
	if q, _ = buildPointsSQL(linked); !strings.Contains(q, "LEFT JOIN") {
		t.Error("KeepUnlinkedPoints must use LEFT JOIN")
	}

	counters := model.Mapping{Mode: model.MappingCounters, Table: "_Node123", PointNameColumn: "_Description", PointKeyColumn: "_IDRRef"}
	q, err = buildCountersSQL(counters)
	if err != nil {
		t.Fatal(err)
	}
	checkSQL(t, "counters", q, "[_IDRRef] AS k", "[_SentNo] AS s", "[_ReceivedNo] AS r", "FROM [_Node123] WITH (NOLOCK)")
	if _, err := buildCountersSQL(linked); err == nil {
		t.Error("counters SQL for a non-counters mapping")
	}

	for name, m := range map[string]model.Mapping{
		"no table":    {PointNameColumn: "a", LastSendColumn: "b"},
		"bad ident":   {Table: "t", PointNameColumn: "a\x00", LastSendColumn: "b"},
		"linked, key": {Table: "t", PointNameColumn: "a", LastSendColumn: "b", TimesTable: "x", TimesLinkColumn: "l"},
	} {
		if _, err := buildPointsSQL(m); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, q := range []string{catalogSQL, rowCountsSQL, rowCountsLegacySQL} {
		if !strings.HasPrefix(q, "SET TRANSACTION ISOLATION LEVEL READ UNCOMMITTED;") || !strings.Contains(q, "@p1") {
			t.Errorf("catalog query not read-uncommitted/parameterized:\n%s", q)
		}
	}
}
