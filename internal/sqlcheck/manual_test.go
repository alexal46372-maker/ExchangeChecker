package sqlcheck

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func TestCHK_SQL_04_ValidateManualSQL(t *testing.T) {
	ok := []string{
		"SELECT n._Description, MAX(t._Fld90), MAX(t._Fld91) FROM _Reference77 n JOIN _InfoRg88 t ON t._Fld89RRef = n._IDRRef GROUP BY n._Description",
		"  select name, s, r from x;  ",
		"SELECT 'a;b', [c;d], \"e;f\" FROM t",
		"WITH q AS (SELECT 1 AS a) SELECT a, NULL, NULL FROM q;",
		"SELECT N'it''s', 1, 2",
	}
	for _, q := range ok {
		if err := ValidateManualSQL(q); err != nil {
			t.Errorf("rejected %q: %v", q, err)
		}
	}
	bad := map[string]error{
		"":                        ErrManualEmpty,
		"  -- only a comment":     ErrManualComment, // phase 6 review: no comments at all
		"UPDATE t SET a = 1":      ErrManualNotSelect,
		"EXEC sp_who":             ErrManualNotSelect,
		"SELECT 1; DELETE FROM t": ErrManualMulti,
		"SELECT 1;SELECT 2":       ErrManualMulti,
		"SELECT 'unterminated":    ErrManualLiteral,
		"SELECT [x FROM t":        ErrManualLiteral,
		"SELECT 1 /* open":        ErrManualComment,
		"SELECTX 1":               ErrManualNotSelect,
		"; SELECT 1":              ErrManualMulti,
	}
	for q, want := range bad {
		if err := ValidateManualSQL(q); !errors.Is(err, want) {
			t.Errorf("%q: err = %v, want %v", q, err, want)
		}
	}
}

func manualProfile(q string, off int) model.Profile {
	p := connProfile()
	p.Mapping = model.Mapping{Mode: model.MappingManual, ManualSQL: q, YearOffset: off}
	return p
}

func TestCHK_SQL_04_QueryReader(t *testing.T) {
	const q = "SELECT name, sent, recv FROM custom_view"
	st := newFake()
	s1, r1 := dbAt(5*time.Minute, 2000), dbAt(time.Minute, 2000)
	st.manual = map[string]fakeManual{q: {rows: []RawPoint{
		{Name: "Точка A", Send: &s1, Recv: &r1},
		{Name: "  ", ID: "", Send: nil, Recv: nil},
	}}}
	p := manualProfile(q, 2000)
	r := ReaderFor(st, p, fixOpt())
	if _, ok := r.(*QueryReader); !ok {
		t.Fatalf("ReaderFor manual SQL = %T", r)
	}
	pts, err := r.ListPoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 || pts[0].Name != "Точка A" || !pts[0].LastRecv.Equal(fixNow.Add(-time.Minute)) || pts[0].Level != model.LevelOK {
		t.Errorf("points %+v", pts)
	}
	if pts[1].Name != "?" || pts[1].Level != model.LevelError || pts[1].Message != model.ReasonNoTimestamps {
		t.Errorf("NULL row %+v", pts[1])
	}
	// errors are explicit
	bad := NewQueryReader(st, manualProfile("DELETE FROM t", 0), fixOpt())
	if _, err := bad.ListPoints(context.Background()); KindOf(err) != KindMapping || !errors.Is(err, ErrManualNotSelect) {
		t.Errorf("bad SQL: %v", err)
	}
	st.manual["SELECT a, b FROM t"] = fakeManual{err: &Error{Kind: KindMapping, Op: "manual sql", Err: ErrManualColumns}}
	if _, err := NewQueryReader(st, manualProfile("SELECT a, b FROM t", 0), fixOpt()).ListPoints(context.Background()); !errors.Is(err, ErrManualColumns) {
		t.Errorf("columns: %v", err)
	}
	if _, err := NewQueryReader(st, connProfile(), fixOpt()).ListPoints(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("empty: %v", err)
	}
	if _, ok := ReaderFor(st, linkedProfile(0), fixOpt()).(*DiscoveringReader); !ok {
		t.Error("ReaderFor table mapping")
	}
	// manual SQL wins over a table mapping (explicit fallback)
	both := linkedProfile(0)
	both.Mapping.ManualSQL = q
	if _, ok := ReaderFor(st, both, fixOpt()).(*QueryReader); !ok {
		t.Error("ReaderFor both")
	}
	if _, err := valueTime("2026-01-01"); !errors.Is(err, errNotTime) {
		t.Errorf("valueTime string: %v", err)
	}
}

func catalogOf(ts ...*fxTable) []Column {
	var out []Column
	for _, t := range ts {
		out = append(out, t.cols...)
	}
	return out
}

func TestCHK_SQL_04_CheckMappingOverride(t *testing.T) {
	cols := catalogOf(append(fx1CNode(0), fxGeneric()...)...)
	good := []model.Mapping{
		linkedProfile(0).Mapping,
		{Mode: model.MappingManual, Schema: "exch", Table: "node]exchange", PointNameColumn: "point name", LastSendColumn: "Send]Date"},
		{Mode: model.MappingManual, Table: "_Node123", PointIDColumn: "_Code", PointKeyColumn: "_IDRRef",
			TimesTable: "_InfoRg456", TimesLinkColumn: "_Fld457RRef", LastRecvColumn: "_Fld459"},
		{Mode: model.MappingCounters, Table: "_Node123", PointNameColumn: "_Description", PointKeyColumn: "_IDRRef"},
		{Mode: model.MappingManual, ManualSQL: "SELECT a, b, c FROM t"},
	}
	for i, m := range good {
		if err := CheckMapping(cols, m); err != nil {
			t.Errorf("good[%d]: %v", i, err)
		}
	}
	base := linkedProfile(0).Mapping
	mut := func(f func(*model.Mapping)) model.Mapping { m := base; f(&m); return m }
	bad := map[string]struct {
		m    model.Mapping
		want error
	}{
		"no table":        {mut(func(m *model.Mapping) { m.Table = "_Node999" }), ErrTableNotFound},
		"no times table":  {mut(func(m *model.Mapping) { m.TimesTable = "_InfoRg1" }), ErrTableNotFound},
		"no name col":     {mut(func(m *model.Mapping) { m.PointNameColumn = "_Name" }), ErrColumnNotFound},
		"no key col":      {mut(func(m *model.Mapping) { m.PointKeyColumn = "_Key" }), ErrColumnNotFound},
		"no link col":     {mut(func(m *model.Mapping) { m.TimesLinkColumn = "_Fld1RRef" }), ErrColumnNotFound},
		"no send col":     {mut(func(m *model.Mapping) { m.LastSendColumn = "_Fld1" }), ErrColumnNotFound},
		"send not date":   {mut(func(m *model.Mapping) { m.LastSendColumn = "_Fld460" }), ErrColumnType},
		"name binary":     {mut(func(m *model.Mapping) { m.PointNameColumn = "_Marked" }), ErrColumnType},
		"link type":       {mut(func(m *model.Mapping) { m.TimesLinkColumn = "_Fld460" }), ErrLinkTypeMismatch},
		"not configured":  {model.Mapping{Mode: model.MappingNone}, ErrNotConfigured},
		"bad manual":      {model.Mapping{Mode: model.MappingManual, ManualSQL: "DROP TABLE x"}, ErrManualNotSelect},
		"counters no col": {model.Mapping{Mode: model.MappingCounters, Table: "_InfoRg456", PointNameColumn: "_Fld460"}, ErrColumnNotFound},
		"counters type": {model.Mapping{Mode: model.MappingCounters, Table: "_Node123", PointNameColumn: "_Description",
			SentNoColumn: "_Description"}, ErrColumnType},
	}
	for name, c := range bad {
		err := CheckMapping(cols, c.m)
		if KindOf(err) != KindMapping || !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	// validation errors from the model (missing columns in linked mode)
	if err := CheckMapping(cols, mut(func(m *model.Mapping) { m.TimesLinkColumn = "" })); KindOf(err) != KindMapping {
		t.Errorf("model validation: %v", err)
	}
	// ambiguous name without schema
	amb := append(cols, Column{Table: TableRef{Schema: "a", Name: "Dup"}, Name: "n", DataType: "nvarchar", MaxLength: 10},
		Column{Table: TableRef{Schema: "b", Name: "Dup"}, Name: "n", DataType: "nvarchar", MaxLength: 10})
	if err := CheckMapping(amb, model.Mapping{Table: "Dup", PointNameColumn: "n", LastSendColumn: "n"}); !errors.Is(err, ErrTableAmbiguous) {
		t.Errorf("ambiguous: %v", err)
	}
	if !strings.Contains(CheckMapping(cols, bad["send not date"].m).Error(), "_InfoRg456._Fld460") {
		t.Error("error must name table.column")
	}
}

func TestCHK_SQL_04_PreviewMapping(t *testing.T) {
	st := newFake(fx1CCustom(2000, "_Reference77")...)
	m := model.Mapping{Mode: model.MappingManual, Table: "_Reference77", PointNameColumn: "_Description", PointKeyColumn: "_IDRRef",
		TimesTable: "_InfoRg88", TimesLinkColumn: "_Fld89RRef", LastSendColumn: "_Fld90", LastRecvColumn: "_Fld91", YearOffset: 2000}
	rows, err := PreviewMapping(context.Background(), st, m, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Point != "Магазин 1" || !rows[0].LastRecv.Equal(fixNow.Add(-15*time.Minute)) {
		t.Errorf("preview %+v", rows)
	}
	m.LastSendColumn = "_Fld92"
	if _, err := PreviewMapping(context.Background(), st, m, 2); !errors.Is(err, ErrColumnType) {
		t.Errorf("preview bad column: %v", err)
	}
	st.manual = map[string]fakeManual{"SELECT a, b, c FROM v": {rows: []RawPoint{{Name: "X"}}}}
	rows, err = PreviewMapping(context.Background(), st, model.Mapping{Mode: model.MappingManual, ManualSQL: "SELECT a, b, c FROM v"}, 0)
	if err != nil || len(rows) != 1 {
		t.Errorf("preview manual: %v %v", rows, err)
	}
	r := NewDiscoveringReader(newFake(fx1CNode(0)...), connProfile(), fixOpt())
	cm := model.Mapping{Mode: model.MappingCounters, Table: "_Node123", PointNameColumn: "_Description"}
	if rows, err := r.PreviewMapping(context.Background(), cm, 10); err != nil || len(rows) != 4 || rows[0].LastSend != nil {
		t.Errorf("preview counters: %v %v", rows, err)
	}
	if err := r.ValidateMapping(context.Background(), linkedProfile(0).Mapping); err != nil {
		t.Errorf("reader ValidateMapping: %v", err)
	}
}
