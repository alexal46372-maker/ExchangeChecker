package sqlcheck

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func discoverFx(t *testing.T, st Store, opt DiscoverOptions) []model.DiscoveryCandidate {
	t.Helper()
	cands, err := Discover(context.Background(), st, opt, fixNow)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(cands) == 0 {
		t.Fatal("Discover: no candidates")
	}
	return cands
}

func dump(cands []model.DiscoveryCandidate) string {
	var b strings.Builder
	for _, c := range cands {
		b.WriteString("\n  " + c.Table + "|" + c.TimesTable + "." + c.LinkCol + " send=" + c.SendCol + " recv=" + c.RecvCol +
			" score=" + itoa(c.Score) + " hints=" + strings.Join(c.Hints, ",") + " :: " + c.Reason)
	}
	return b.String()
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func wantLinked(t *testing.T, c model.DiscoveryCandidate, table, times, link, send, recv string, off int) {
	t.Helper()
	if c.Mode != model.CandidateLinked || c.Table != table || c.TimesTable != times || c.LinkCol != link ||
		c.SendCol != send || c.RecvCol != recv || c.YearOffset != off || c.PointKeyCol != "_IDRRef" {
		t.Fatalf("unexpected top candidate %+v", c)
	}
}

func TestCHK_SQL_02_Discover1CNodeOffset2000(t *testing.T) {
	st := newFake(fx1CNode(2000)...)
	cands := discoverFx(t, st, DiscoverOptions{})
	top := cands[0]
	wantLinked(t, top, "_Node123", "_InfoRg456", "_Fld457RRef", "_Fld458", "_Fld459", 2000)
	if top.PointCol != "_Description" || top.PointIDCol != "_Code" {
		t.Errorf("point columns = %q/%q", top.PointCol, top.PointIDCol)
	}
	for _, h := range []string{model.HintLinked, model.HintNodeTable, model.HintYearOffset2000, model.HintSendRecvByOrder} {
		if !top.HasHint(h) {
			t.Errorf("missing hint %s: %v", h, top.Hints)
		}
	}
	if top.Points != 3 || len(top.SampleRows) != 3 {
		t.Fatalf("points=%d samples=%d (ЦБ without times rows must be excluded)%s", top.Points, len(top.SampleRows), dump(cands))
	}
	s := top.SampleRows[0]
	want := fixNow.Add(-10 * time.Minute)
	if s.Point != "Магазин 1" || s.LastRecv == nil || !s.LastRecv.Equal(want) || s.LastRecv.Location() != model.DBZone {
		t.Errorf("sample row = %+v, want recv %v", s, want)
	}
	if top.FreshestAt == nil || !top.FreshestAt.Equal(want) {
		t.Errorf("FreshestAt = %v", top.FreshestAt)
	}
	for _, c := range cands {
		if c.TimesTable == "_InfoRg900" {
			t.Errorf("unlinked register proposed: %+v", c)
		}
	}
	// the old linked register ranks below the fresh one
	for i, c := range cands {
		if c.TimesTable == "_InfoRg500" && i == 0 {
			t.Error("stale register ranked first")
		}
	}
}

func TestCHK_SQL_02_Discover1CNodeNoOffset(t *testing.T) {
	cands := discoverFx(t, newFake(fx1CNode(0)...), DiscoverOptions{})
	wantLinked(t, cands[0], "_Node123", "_InfoRg456", "_Fld457RRef", "_Fld458", "_Fld459", 0)
	if cands[0].HasHint(model.HintYearOffset2000) {
		t.Error("offset hint without offset")
	}
}

func TestCHK_SQL_02_DiscoverCustomWithoutNode(t *testing.T) {
	cands := discoverFx(t, newFake(fx1CCustom(2000, "_Reference77")...), DiscoverOptions{})
	top := cands[0]
	wantLinked(t, top, "_Reference77", "_InfoRg88", "_Fld89RRef", "_Fld90", "_Fld91", 2000)
	if top.HasHint(model.HintNodeTable) || !top.HasHint(model.HintAllPointsFresh) {
		t.Errorf("hints = %v", top.Hints)
	}
	if top.Points != 3 {
		t.Errorf("points = %d", top.Points)
	}
	var cur *model.DiscoveryCandidate
	for i := range cands {
		if cands[i].TimesTable == "_InfoRg11" {
			cur = &cands[i]
		}
	}
	if cur == nil || cur.Score >= top.Score || !cur.HasHint(model.HintSingleSeries) {
		t.Errorf("currency register: %+v%s", cur, dump(cands))
	}
}

func TestCHK_SQL_02_DiscoverDatesInDirectory(t *testing.T) {
	cands := discoverFx(t, newFake(fx1CDirDates(2000)...), DiscoverOptions{})
	top := cands[0]
	if top.Mode != model.CandidateSingle || top.Table != "_Reference55" || top.SendCol != "_Fld56" ||
		top.RecvCol != "_Fld57" || top.YearOffset != 2000 || !top.HasHint(model.HintSameTable) {
		t.Fatalf("top = %+v%s", top, dump(cands))
	}
	if top.Points != 3 || top.SampleRows[1].Point != "Магазин 2" {
		t.Errorf("samples = %+v", top.SampleRows)
	}
}

func TestCHK_SQL_02_DiscoverGenericSchema(t *testing.T) {
	cands := discoverFx(t, newFake(fxGeneric()...), DiscoverOptions{})
	top := cands[0]
	if top.Mode != model.CandidateSingle || top.Schema != "exch" || top.Table != "Node]Exchange" ||
		top.PointCol != "Point Name" || top.SendCol != "Send]Date" || top.RecvCol != "RecvDate" || top.YearOffset != 0 {
		t.Fatalf("top = %+v%s", top, dump(cands))
	}
	if !top.HasHint(model.HintGeneric) || !top.HasHint(model.HintSendRecvByName) {
		t.Errorf("hints = %v", top.Hints)
	}
	for _, c := range cands {
		if c.Table == "Customers" {
			t.Error("table without markers proposed")
		}
	}
}

func TestCHK_SQL_02_ScoringNodeBonusAndFreshness(t *testing.T) {
	custom := discoverFx(t, newFake(fx1CCustom(2000, "_Reference77")...), DiscoverOptions{})[0]
	node := discoverFx(t, newFake(fx1CCustom(2000, "_Node77")...), DiscoverOptions{})[0]
	if node.Table != "_Node77" || node.Score != custom.Score+nodeBonus || !node.HasHint(model.HintNodeTable) {
		t.Errorf("node bonus: custom %d, node %d (%v)", custom.Score, node.Score, node.Hints)
	}
	// Same data 40 days older: freshness and per-point signals drop.
	old := fx1CCustom(2000, "_Reference77")
	for _, r := range old[1].rows {
		r[1] = r[1].(time.Time).AddDate(0, 0, -40)
		r[2] = r[2].(time.Time).AddDate(0, 0, -40)
	}
	stale := discoverFx(t, newFake(old[:2]...), DiscoverOptions{})[0]
	if stale.TimesTable != "_InfoRg88" || stale.Score >= custom.Score {
		t.Errorf("stale %d >= fresh %d", stale.Score, custom.Score)
	}
	// One point never updated: "values updated across all points" drops.
	partial := fx1CCustom(2000, "_Reference77")
	partial[1].rows[2][1], partial[1].rows[2][2] = dbAt(200*day, 2000), dbAt(200*day, 2000)
	p := discoverFx(t, newFake(partial[:2]...), DiscoverOptions{})[0]
	if p.Score >= custom.Score || p.HasHint(model.HintAllPointsFresh) {
		t.Errorf("partial %d (%v) vs full %d", p.Score, p.Hints, custom.Score)
	}
}

func TestCHK_SQL_02_DiscoverDirectoryRowLimit(t *testing.T) {
	st := newFake(fx1CCustom(2000, "_Reference77")...)
	cands, err := Discover(context.Background(), st, DiscoverOptions{MaxDirRows: 2}, fixNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		if c.Table == "_Reference77" || c.Table == "_Reference10" {
			t.Errorf("directory above MaxDirRows used: %+v", c)
		}
	}
}

func TestCHK_SQL_02_DiscoverWithoutRowCounts(t *testing.T) {
	st := newFake(fx1CCustom(2000, "_Reference77")...)
	st.noCounts = true
	cands := discoverFx(t, st, DiscoverOptions{})
	wantLinked(t, cands[0], "_Reference77", "_InfoRg88", "_Fld89RRef", "_Fld90", "_Fld91", 2000)
}

func TestCHK_SQL_02_DiscoverSkipsFailingTable(t *testing.T) {
	st := newFake(fx1CNode(2000)...)
	st.fail = map[string]error{"_InfoRg456": errors.New("permission denied")}
	cands := discoverFx(t, st, DiscoverOptions{})
	for _, c := range cands {
		if c.TimesTable == "_InfoRg456" {
			t.Fatal("failing table proposed")
		}
	}
}

func TestCHK_SQL_02_DiscoverTimeBudget(t *testing.T) {
	st := newFake(fx1CNode(2000)...)
	st.delay = 20 * time.Millisecond
	start := time.Now()
	cands, err := Discover(context.Background(), st, DiscoverOptions{Timeout: 120 * time.Millisecond}, fixNow)
	if err != nil {
		t.Fatalf("budget exhaustion must return a partial result, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("budget not enforced: %v", time.Since(start))
	}
	for _, c := range cands {
		if !c.HasHint(model.HintTruncated) {
			t.Errorf("partial candidate without truncated hint: %v", c.Hints)
		}
	}
	// A caller cancel is an error, not a partial result.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Discover(ctx, st, DiscoverOptions{}, fixNow); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: err = %v", err)
	}
	// Per-query timeout: every query too slow -> catalog error.
	st.delay = 50 * time.Millisecond
	if _, err := Discover(context.Background(), st, DiscoverOptions{QueryTimeout: 5 * time.Millisecond}, fixNow); err == nil {
		t.Error("catalog timeout must fail")
	}
}

func TestCHK_SQL_02_DetectYearOffset(t *testing.T) {
	tp := func(y int, mo time.Month, d, h int) *time.Time {
		x := time.Date(y, mo, d, h, 0, 0, 0, time.UTC)
		return &x
	}
	cases := []struct {
		name string
		v    []*time.Time
		want int
	}{
		{"none", nil, 0},
		{"plain 2026", []*time.Time{tp(2026, 9, 29, 10)}, 0},
		{"offset 4026", []*time.Time{nil, tp(4026, 9, 29, 10)}, 2000},
		{"offset 4025", []*time.Time{tp(4025, 1, 1, 10)}, 2000},
		{"far future 9999 sentinel", []*time.Time{tp(9999, 12, 31, 0)}, 0},
		{"only empty 1C dates", []*time.Time{tp(2001, 1, 1, 0), tp(2001, 1, 1, 0)}, 2000},
		{"empty + real 2025", []*time.Time{tp(2001, 1, 1, 0), tp(2025, 5, 1, 3)}, 0},
	}
	for _, c := range cases {
		if got := DetectYearOffset(c.v, fixNow); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

func TestCHK_SQL_02_SendRecvSelection(t *testing.T) {
	cols := func(names ...string) []Column {
		var out []Column
		for i, n := range names {
			out = append(out, Column{Name: n, DataType: "datetime", Ordinal: i + 1})
		}
		return out
	}
	at := func(ago time.Duration) *time.Time { x := dbAt(ago, 0); return &x }
	// by names, regardless of freshness
	ev := evaluateSeries(cols("CreatedAt", "LastRecvDate", "LastSendDate"), []*time.Time{at(time.Minute), at(time.Hour), at(2 * time.Hour)}, fixNow)
	if ev.send != "LastSendDate" || ev.recv != "LastRecvDate" {
		t.Errorf("by name: %q/%q", ev.send, ev.recv)
	}
	// Russian names
	ev = evaluateSeries(cols("ДатаВыгрузки", "ДатаЗагрузки"), []*time.Time{at(time.Hour), at(time.Minute)}, fixNow)
	if ev.send != "ДатаВыгрузки" || ev.recv != "ДатаЗагрузки" {
		t.Errorf("russian: %q/%q", ev.send, ev.recv)
	}
	// opaque names: two freshest, lower ordinal = send; stale/NULL ignored
	ev = evaluateSeries(cols("_Fld1", "_Fld2", "_Fld3", "_Fld4"), []*time.Time{at(300 * day), at(time.Hour), nil, at(time.Minute)}, fixNow)
	if ev.send != "_Fld2" || ev.recv != "_Fld4" || ev.score != 10 {
		t.Errorf("opaque: %q/%q score %d", ev.send, ev.recv, ev.score)
	}
	// one series
	ev = evaluateSeries(cols("_Period"), []*time.Time{at(time.Hour)}, fixNow)
	if ev.send != "_Period" || ev.recv != "" {
		t.Errorf("single: %+v", ev)
	}
	// tokens: "in" as a word only
	if nameHint("DateIn") != hintRecv || nameHint("Binary") != hintNone || nameHint("Send]Date") != hintSend ||
		nameHint("sent_out") != hintSend || nameHint("SendRecv") != hintNone {
		t.Error("nameHint tokens")
	}
	if got := strings.Join(splitTokens("LastRecvDate2x"), " "); got != "last recv date 2 x" {
		t.Errorf("splitTokens = %q", got)
	}
}

func TestCHK_SQL_02_CandidateJSONAndMapping(t *testing.T) {
	cands := discoverFx(t, newFake(fx1CCustom(2000, "_Reference77")...), DiscoverOptions{TopN: 3})
	b, err := json.Marshal(cands)
	if err != nil {
		t.Fatal(err)
	}
	var back []model.DiscoveryCandidate
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if len(back) != len(cands) || back[0].Key() != cands[0].Key() || back[0].Score != cands[0].Score ||
		!back[0].SampleRows[0].LastRecv.Equal(*cands[0].SampleRows[0].LastRecv) {
		t.Fatalf("JSON roundtrip mismatch: %s", b)
	}
	m := back[0].Mapping()
	if err := m.Validate(); err != nil || !m.Linked() || m.Mode != model.MappingAuto || m.YearOffset != 2000 ||
		m.TimesLinkColumn != "_Fld89RRef" || m.PointKeyColumn != "_IDRRef" {
		t.Fatalf("mapping %+v: %v", m, err)
	}
	st := newFake(fx1CCustom(2000, "_Reference77")...)
	cols, _ := st.Columns(context.Background())
	if err := CheckMapping(cols, m); err != nil {
		t.Errorf("candidate mapping does not pass CheckMapping: %v", err)
	}
}
