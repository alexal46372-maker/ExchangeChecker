package sqlcheck

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 2b (customer clarification): exchange plans _NodeNNN are the
// primary source, a profile may watch several plans, 1C 8.1 composite
// references, time-based DiscoverByValue.

func findCand(cands []model.DiscoveryCandidate, f func(c model.DiscoveryCandidate) bool) (model.DiscoveryCandidate, bool) {
	for _, c := range cands {
		if f(c) {
			return c, true
		}
	}
	return model.DiscoveryCandidate{}, false
}

func TestCHK_SQL_02_DiscoverPlansNodePrimary(t *testing.T) {
	for _, off := range []int{0, 2000} {
		cands := discoverFx(t, newFake(fx1CPlans(off)...), DiscoverOptions{TopN: 20})
		if len(cands) < 3 {
			t.Fatalf("off %d: %s", off, dump(cands))
		}
		c1, c2 := cands[0], cands[1]
		if c1.Table != "_Node124" || c2.Table != "_Node123" {
			t.Fatalf("off %d: top two must be the exchange plans%s", off, dump(cands))
		}
		// composite candidate of _Node124
		if c1.TimesTable != "_InfoRg600" || c1.LinkCol != "_Fld601_RRRef" || c1.LinkTypeCol != "_Fld601_TYPE" ||
			c1.LinkRTRefCol != "_Fld601_RTRef" || c1.LinkRTRef != 124 || c1.SendCol != "_Fld602" || c1.RecvCol != "_Fld603" ||
			!c1.HasHint(model.HintCompositeRef) || !c1.HasHint(model.HintNodeTable) || c1.YearOffset != off ||
			c1.PlanTable != "_Node124" || c1.PlanNo != 124 {
			t.Errorf("off %d: composite candidate %+v", off, c1)
		}
		wantLinked(t, c2, "_Node123", "_InfoRg456", "_Fld457RRef", "_Fld458", "_Fld459", off)
		if c2.LinkTypeCol != "" || c2.PlanNo != 123 {
			t.Errorf("off %d: simple-ref candidate %+v", off, c2)
		}
		// no composite candidate for _Node123 (the RTRef=123 collision row
		// references a _Node124 key, the owner table number must match)
		if c, ok := findCand(cands, func(c model.DiscoveryCandidate) bool { return c.Table == "_Node123" && c.LinkTypeCol != "" }); ok {
			t.Errorf("off %d: collision produced a composite candidate %+v", off, c)
		}
		// _Node candidates outrank every non-_Node distractor by >= 30
		for _, c := range cands {
			if model.PlanTableOf(c.Table) != "" {
				continue
			}
			if c2.Score-c.Score < 30 {
				t.Errorf("off %d: distractor %s|%s score %d too close to %d", off, c.Table, c.TimesTable, c.Score, c2.Score)
			}
		}
		// the composite candidate's mapping is valid and reproduces the filter
		m := c1.Mapping()
		if err := m.Validate(); err != nil || !m.Composite() || m.TimesLinkRTRef != 124 || m.PlanNo() != 124 {
			t.Errorf("off %d: mapping %+v: %v", off, m, err)
		}
		if err := CheckMapping(catalogOf(fx1CPlans(off)...), m); err != nil {
			t.Errorf("off %d: CheckMapping: %v", off, err)
		}
	}
}

func compositeMapping(off int) model.Mapping {
	return model.Mapping{Mode: model.MappingAuto, Table: "_Node124", PointNameColumn: "_Description", PointIDColumn: "_Code",
		PointKeyColumn: "_IDRRef", TimesTable: "_InfoRg600", TimesLinkColumn: "_Fld601_RRRef",
		TimesLinkTypeColumn: "_Fld601_TYPE", TimesLinkRTRefColumn: "_Fld601_RTRef", TimesLinkRTRef: 124,
		LastSendColumn: "_Fld602", LastRecvColumn: "_Fld603", YearOffset: off, PlanLabel: "Магазин"}
}

func TestCHK_SQL_03_ListPointsComposite(t *testing.T) {
	for _, off := range []int{0, 2000} {
		p := connProfile()
		p.Mapping = compositeMapping(off)
		pts, err := NewDiscoveringReader(newFake(fx1CPlans(off)...), p, fixOpt()).ListPoints(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		byName := pointsByName(pts)
		if len(pts) != 2 {
			t.Fatalf("off %d: %+v (ЦБ has no rows and must be skipped)", off, pts)
		}
		a, b := byName["Склад А"], byName["Склад Б"]
		if !a.LastSend.Equal(fixNow.Add(-25*time.Minute)) || !a.LastRecv.Equal(fixNow.Add(-12*time.Minute)) {
			t.Errorf("off %d: Склад А %+v", off, a)
		}
		// the RTRef=123 collision row (-1m) must not be counted
		if !b.LastSend.Equal(fixNow.Add(-40*time.Minute)) || !b.LastRecv.Equal(fixNow.Add(-35*time.Minute)) {
			t.Errorf("off %d: Склад Б %v / %v, want -40m / -35m", off, b.LastSend, b.LastRecv)
		}
		if b.PlanTable != "_Node124" || b.PlanNo != 124 || b.PlanLabel != "Магазин" {
			t.Errorf("off %d: plan tags %+v", off, b)
		}
	}
	// SQL: the composite filter is parameterized inside the times subquery
	q, err := buildPointsSQL(compositeMapping(0))
	if err != nil {
		t.Fatal(err)
	}
	checkSQL(t, "composite points", q, "WHERE [_Fld601_TYPE] = @p2 AND [_Fld601_RTRef] = @p3) AS t ON t.l = p.k", "INNER JOIN")
	args := pointsArgs(compositeMapping(0), 50)
	if len(args) != 3 || args[0] != 50 || string(args[1].([]byte)) != "\x08" || hex.EncodeToString(args[2].([]byte)) != "0000007c" {
		t.Errorf("points args %#v", args)
	}
	if args := pointsArgs(linkedProfile(0).Mapping, 50); len(args) != 1 {
		t.Errorf("simple linked args %#v", args)
	}
}

func TestCHK_SQL_03_MultiPlanProfile(t *testing.T) {
	cands := discoverFx(t, newFake(fx1CPlans(2000)...), DiscoverOptions{TopN: 20})
	ms := model.SuggestPlanMappings(cands)
	if len(ms) != 2 || ms[0].Table != "_Node124" || ms[1].Table != "_Node123" || !ms[0].Composite() || ms[1].Composite() {
		t.Fatalf("suggested %+v", ms)
	}
	p := connProfile()
	p.Mapping = ms[1]
	p.Mapping.PlanLabel = "Основной"
	extra := ms[0]
	extra.PlanLabel = "Магазин"
	p.Mappings = []model.Mapping{extra}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	pts, err := NewDiscoveringReader(newFake(fx1CPlans(2000)...), p, fixOpt()).ListPoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 5 {
		t.Fatalf("%d points: %+v", len(pts), pts)
	}
	plans := map[string]string{}
	for _, x := range pts {
		plans[x.Name] = x.PlanTable + "/" + x.PlanLabel
		if x.ProfileID != "p1" {
			t.Errorf("profile id %+v", x)
		}
	}
	for name, want := range map[string]string{"Магазин 1": "_Node123/Основной", "Магазин 3": "_Node123/Основной",
		"Склад А": "_Node124/Магазин", "Склад Б": "_Node124/Магазин"} {
		if plans[name] != want {
			t.Errorf("%s: %q, want %q", name, plans[name], want)
		}
	}
	// ReaderFor: extra mapping errors propagate
	st := newFake(fx1CPlans(2000)...)
	st.fail = map[string]error{"_InfoRg600": fakeNetErr{}}
	if _, err := ReaderFor(st, p, fixOpt()).ListPoints(context.Background()); KindOf(err) != KindConn {
		t.Errorf("extra mapping failure: %v", err)
	}
	// JSON: old single-mapping profiles are unchanged
	old := connProfile()
	old.Mapping = linkedProfile(0).Mapping
	if old.AllMappings() == nil || len(old.AllMappings()) != 1 {
		t.Errorf("AllMappings %+v", old.AllMappings())
	}
}

func TestCHK_SQL_03_MultiPlanCounters(t *testing.T) {
	now := fixNow
	opt := Options{Now: func() time.Time { return now }, Counters: model.NewMemCounterStore()}
	p := connProfile()
	p.Mapping = model.Mapping{Mode: model.MappingCounters, Table: "_Node123", PointNameColumn: "_Description",
		PointIDColumn: "_Code", PointKeyColumn: "_IDRRef", PlanLabel: "Основной"}
	p.Mappings = []model.Mapping{{Mode: model.MappingCounters, Table: "_Node124", PointNameColumn: "_Description",
		PointIDColumn: "_Code", PointKeyColumn: "_IDRRef", PlanLabel: "Магазин"}}
	tables := fx1CPlans(0)
	var n124 *fxTable
	for _, x := range tables {
		if x.ref.Name == "_Node124" {
			n124 = x
		}
	}
	r := NewDiscoveringReader(newFake(tables...), p, opt)
	pts, err := r.ListPoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 7 {
		t.Fatalf("%d points: %+v", len(pts), pts)
	}
	for _, x := range pts {
		if !x.SendBaseline || x.PlanTable == "" || (x.PlanTable == "_Node124") != (x.PlanLabel == "Магазин") {
			t.Errorf("baseline %+v", x)
		}
	}
	// ЦБ exists in both plans with different codes; the keys never mix.
	now = fixNow.Add(90 * time.Minute)
	n124.rows[2][n124.idx("_SentNo")] = []byte("6") // Склад Б
	pts, err = r.ListPoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	changed := 0
	for _, x := range pts {
		if !x.SendBaseline {
			changed++
			if x.Name != "Склад Б" || x.PlanTable != "_Node124" || !x.LastSend.Equal(now) || x.Level != model.LevelOK {
				t.Errorf("changed %+v", x)
			}
		}
	}
	if changed != 1 {
		t.Errorf("%d changed points", changed)
	}
}

func TestCHK_SQL_02_DiscoverByValueByTime(t *testing.T) {
	at := func(d time.Duration) *time.Time { x := fixNow.Add(-d); return &x }
	for _, off := range []int{0, 2000} {
		st := newFake(fx1CPlans(off)...)
		// by name + approximate time: composite register of _Node124
		c := byValue(t, st, model.ValueQuery{PointName: "Склад Б", ApproxTime: at(36 * time.Minute), ToleranceMin: 30}, DiscoverOptions{})[0]
		if c.Table != "_Node124" || c.TimesTable != "_InfoRg600" || c.LinkRTRef != 124 || c.SendCol != "_Fld602" ||
			c.RecvCol != "_Fld603" || !c.HasHint(model.HintByTime) || !c.HasHint(model.HintNearApproxTime) ||
			!c.HasHint(model.HintCompositeRef) || c.YearOffset != off {
			t.Errorf("off %d: by time %+v", off, c)
		}
		if c.Matched == nil || c.Matched.LastRecv == nil || !c.Matched.LastRecv.Equal(fixNow.Add(-35*time.Minute)) {
			t.Errorf("off %d: matched %+v", off, c.Matched)
		}
		// by _IDRRef (picked from ListNodePoints) without a name
		c = byValue(t, st, model.ValueQuery{PointRef: hex.EncodeToString(ref16(123, 2)), ApproxTime: at(12 * time.Minute)}, DiscoverOptions{})[0]
		wantLinked(t, c, "_Node123", "_InfoRg456", "_Fld457RRef", "_Fld458", "_Fld459", off)
		if !c.HasHint(model.HintByTime) || c.Matched == nil || c.Matched.Point != "Магазин 1" {
			t.Errorf("off %d: by ref %+v", off, c)
		}
		// by code within one plan
		c = byValue(t, st, model.ValueQuery{PointName: "001", PlanTable: "_Node123", ApproxTime: at(12 * time.Minute)}, DiscoverOptions{})[0]
		if c.Table != "_Node123" || c.TimesTable != "_InfoRg456" || !c.HasHint(model.HintByTime) {
			t.Errorf("off %d: by code %+v", off, c)
		}
		// -1m only matches the RTRef=123 collision row: no time match for
		// Склад Б, the name-based search is the fallback (composite-filtered)
		cands := byValue(t, st, model.ValueQuery{PointName: "Склад Б", ApproxTime: at(time.Minute), ToleranceMin: 5}, DiscoverOptions{})
		for _, x := range cands {
			if x.HasHint(model.HintByTime) || x.HasHint(model.HintNearApproxTime) {
				t.Errorf("off %d: collision row matched: %+v", off, x)
			}
		}
		c = cands[0]
		if c.Table != "_Node124" || c.LinkRTRef != 124 || !c.HasHint(model.HintExactMatch) || !c.FreshestAt.Equal(fixNow.Add(-35*time.Minute)) {
			t.Errorf("off %d: fallback %+v", off, c)
		}
	}
	st := newFake(fx1CPlans(0)...)
	if _, err := DiscoverByValue(context.Background(), st, model.ValueQuery{PointRef: "xyz", ApproxTime: at(time.Minute)}, DiscoverOptions{}, fixNow); KindOf(err) != KindConfig || !errors.Is(err, ErrBadPointRef) {
		t.Errorf("bad ref: %v", err)
	}
	if _, err := DiscoverByValue(context.Background(), st, model.ValueQuery{ApproxTime: at(time.Minute)}, DiscoverOptions{}, fixNow); KindOf(err) != KindConfig || !errors.Is(err, ErrEmptyPointName) {
		t.Errorf("empty: %v", err)
	}
	if got := closenessPoints(10 * time.Minute); got != 35 {
		t.Errorf("closeness %d", got)
	}
}

func TestCHK_SQL_02_ListNodePoints(t *testing.T) {
	st := newFake(fx1CPlans(2000)...)
	np, err := NewDiscoveringReader(st, connProfile(), fixOpt()).NodePoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(np) != 7 {
		t.Fatalf("%d node points %+v", len(np), np)
	}
	var n123, n124 int
	for _, x := range np {
		switch x.PlanTable {
		case "_Node123":
			n123++
		case "_Node124":
			n124++
		}
		if len(x.Ref) != 32 || x.Name == "" || x.PlanNo != model.PlanNoOf(x.PlanTable) {
			t.Errorf("node point %+v", x)
		}
	}
	if n123 != 4 || n124 != 3 || np[0].PlanTable != "_Node123" || np[4].Name != "Склад А" || np[4].Code != "S01" {
		t.Errorf("node points %+v", np)
	}
	var lister PlanPointLister = &StubReader{Nodes: np[:1]}
	if got, _ := lister.NodePoints(context.Background()); len(got) != 1 {
		t.Error("stub")
	}
	// no exchange plans -> empty list
	if np, err := ListNodePoints(context.Background(), newFake(fx1CCustom(0, "_Reference77")...), DiscoverOptions{}, 0); err != nil || len(np) != 0 {
		t.Errorf("custom: %v %v", np, err)
	}
}

func TestCHK_SQL_04_CheckMappingComposite(t *testing.T) {
	cols := catalogOf(fx1CPlans(0)...)
	good := compositeMapping(0)
	if err := CheckMapping(cols, good); err != nil {
		t.Fatal(err)
	}
	mut := func(f func(*model.Mapping)) model.Mapping { m := good; f(&m); return m }
	for name, c := range map[string]struct {
		m    model.Mapping
		want error
	}{
		"type not binary(1)":  {mut(func(m *model.Mapping) { m.TimesLinkTypeColumn = "_Fld604" }), ErrColumnType},
		"rtref not binary(4)": {mut(func(m *model.Mapping) { m.TimesLinkRTRefColumn = "_Fld601_TYPE" }), ErrColumnType},
		"no type col":         {mut(func(m *model.Mapping) { m.TimesLinkTypeColumn = "_Fld999_TYPE" }), ErrColumnNotFound},
	} {
		if err := CheckMapping(cols, c.m); KindOf(err) != KindMapping || !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	for name, m := range map[string]model.Mapping{
		"rtref 0":         mut(func(m *model.Mapping) { m.TimesLinkRTRef = 0 }),
		"rtref no cols":   mut(func(m *model.Mapping) { m.TimesLinkTypeColumn, m.TimesLinkRTRefColumn = "", "" }),
		"half composite":  mut(func(m *model.Mapping) { m.TimesLinkRTRefColumn = "" }),
		"single+composit": {Table: "_InfoRg600", PointNameColumn: "_Fld604", LastSendColumn: "_Fld602", TimesLinkTypeColumn: "_Fld601_TYPE", TimesLinkRTRefColumn: "_Fld601_RTRef", TimesLinkRTRef: 1},
	} {
		if err := CheckMapping(cols, m); KindOf(err) != KindMapping || !strings.Contains(err.Error(), "composite") && !strings.Contains(err.Error(), "RTRef") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
