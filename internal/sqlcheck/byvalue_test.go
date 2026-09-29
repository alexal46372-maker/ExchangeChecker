package sqlcheck

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func byValue(t *testing.T, st Store, q model.ValueQuery, opt DiscoverOptions) []model.DiscoveryCandidate {
	t.Helper()
	cands, err := DiscoverByValue(context.Background(), st, q, opt, fixNow)
	if err != nil {
		t.Fatalf("DiscoverByValue: %v", err)
	}
	return cands
}

func TestCHK_SQL_02_DiscoverByValueLinked(t *testing.T) {
	approx := fixNow.Add(-45 * time.Minute) // Магазин 2: recv 50m ago, send 55m ago
	st := newFake(fx1CCustom(2000, "_Reference77")...)
	cands := byValue(t, st, model.ValueQuery{PointName: "Магазин 2", ApproxTime: &approx}, DiscoverOptions{})
	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	top := cands[0]
	wantLinked(t, top, "_Reference77", "_InfoRg88", "_Fld89RRef", "_Fld90", "_Fld91", 2000)
	for _, h := range []string{model.HintByValue, model.HintLinked, model.HintExactMatch, model.HintNearApproxTime} {
		if !top.HasHint(h) {
			t.Errorf("missing hint %s: %v", h, top.Hints)
		}
	}
	if top.PointCol != "_Description" || top.PointIDCol != "_Code" {
		t.Errorf("point cols %q/%q", top.PointCol, top.PointIDCol)
	}
	if top.Matched == nil || top.Matched.Point != "Магазин 2" || top.Matched.LastRecv == nil ||
		!top.Matched.LastRecv.Equal(fixNow.Add(-50*time.Minute)) {
		t.Fatalf("matched = %+v", top.Matched)
	}
	if len(top.SampleRows) != 3 {
		t.Errorf("sample rows %d", len(top.SampleRows))
	}
	// only small tables were searched, all with NOLOCK builders (fake checks SQL building)
	for _, c := range st.called("text:") {
		if !strings.HasPrefix(c, "text:_") {
			t.Errorf("unexpected search %s", c)
		}
	}
	if len(st.called("ref:")) == 0 {
		t.Error("linked rows not searched")
	}
	b, err := json.Marshal(model.ValueQuery{PointName: "Магазин 2", ApproxTime: &approx})
	if err != nil || !strings.Contains(string(b), `"pointName":"Магазин 2"`) {
		t.Errorf("ValueQuery JSON %s %v", b, err)
	}
}

func TestCHK_SQL_02_DiscoverByValueScoring(t *testing.T) {
	st := newFake(fx1CCustom(2000, "_Reference77")...)
	exact := byValue(t, st, model.ValueQuery{PointName: "магазин 2"}, DiscoverOptions{})[0]
	sub := byValue(t, st, model.ValueQuery{PointName: "азин 2"}, DiscoverOptions{})[0]
	if !exact.HasHint(model.HintExactMatch) || sub.HasHint(model.HintExactMatch) || sub.Score >= exact.Score {
		t.Errorf("exact %d %v vs substring %d %v", exact.Score, exact.Hints, sub.Score, sub.Hints)
	}
	near := fixNow.Add(-50 * time.Minute)
	far := fixNow.Add(-20 * day)
	n := byValue(t, st, model.ValueQuery{PointName: "Магазин 2", ApproxTime: &near}, DiscoverOptions{})[0]
	f := byValue(t, st, model.ValueQuery{PointName: "Магазин 2", ApproxTime: &far}, DiscoverOptions{})[0]
	if f.HasHint(model.HintNearApproxTime) || f.Score >= n.Score {
		t.Errorf("near %d vs far %d (%v)", n.Score, f.Score, f.Hints)
	}
	tol := model.ValueQuery{PointName: "x", ToleranceMin: 30}
	if tol.Tolerance() != 30*time.Minute || (model.ValueQuery{}).Tolerance() != 24*time.Hour {
		t.Error("Tolerance")
	}
	node := byValue(t, newFake(fx1CCustom(2000, "_Node77")...), model.ValueQuery{PointName: "Магазин 2"}, DiscoverOptions{})[0]
	if node.Score != exact.Score+10 || !node.HasHint(model.HintNodeTable) {
		t.Errorf("node bonus %d vs %d", node.Score, exact.Score)
	}
}

func TestCHK_SQL_02_DiscoverByValueSameRow(t *testing.T) {
	cands := byValue(t, newFake(fx1CDirDates(2000)...), model.ValueQuery{PointName: "Магазин 3"}, DiscoverOptions{})
	top := cands[0]
	if top.Mode != model.CandidateSingle || top.Table != "_Reference55" || top.SendCol != "_Fld56" ||
		top.RecvCol != "_Fld57" || top.YearOffset != 2000 || !top.HasHint(model.HintSameTable) {
		t.Fatalf("top = %+v%s", top, dump(cands))
	}
	if top.Matched == nil || !top.Matched.LastRecv.Equal(fixNow.Add(-30*time.Minute)) {
		t.Errorf("matched %+v", top.Matched)
	}
}

func TestCHK_SQL_02_DiscoverByValueLimits(t *testing.T) {
	st := newFake(fx1CCustom(2000, "_Reference77")...)
	if _, err := DiscoverByValue(context.Background(), st, model.ValueQuery{PointName: "  "}, DiscoverOptions{}, fixNow); KindOf(err) != KindConfig {
		t.Errorf("empty name: %v", err)
	}
	// tables above MaxValueRows are not searched at all
	cands := byValue(t, st, model.ValueQuery{PointName: "Магазин 2"}, DiscoverOptions{MaxValueRows: 2})
	if len(cands) != 0 {
		t.Errorf("found in a table above the row limit: %s", dump(cands))
	}
	for _, c := range st.called("text:") {
		if c == "text:_Reference77" {
			t.Error("big table searched")
		}
	}
	// '%' is a literal
	cands = byValue(t, newFake(fx1CCustom(2000, "_Reference77")...), model.ValueQuery{PointName: "Склад 50%"}, DiscoverOptions{})
	if len(cands) == 0 || cands[0].Matched.Point != "Склад 50%" {
		t.Errorf("literal %% search: %s", dump(cands))
	}
	// unknown name
	if cands := byValue(t, newFake(fx1CCustom(2000, "_Reference77")...), model.ValueQuery{PointName: "Нет такой"}, DiscoverOptions{}); len(cands) != 0 {
		t.Errorf("unexpected: %s", dump(cands))
	}
	// time budget: partial, no error
	slow := newFake(fx1CCustom(2000, "_Reference77")...)
	slow.delay = 20 * time.Millisecond
	if _, err := DiscoverByValue(context.Background(), slow, model.ValueQuery{PointName: "Магазин 2"},
		DiscoverOptions{Timeout: 100 * time.Millisecond}, fixNow); err != nil {
		t.Errorf("budget: %v", err)
	}
	// reader + stub wiring
	r := NewDiscoveringReader(newFake(fx1CCustom(2000, "_Reference77")...), connProfile(), Options{Now: func() time.Time { return fixNow }})
	if c, err := r.DiscoverByValue(context.Background(), model.ValueQuery{PointName: "Магазин 1"}); err != nil || len(c) == 0 {
		t.Errorf("reader DiscoverByValue: %v", err)
	}
	stub := &StubReader{Candidates: []model.DiscoveryCandidate{{Table: "t"}}}
	if c, _ := stub.DiscoverByValue(context.Background(), model.ValueQuery{}); len(c) != 1 {
		t.Error("stub DiscoverByValue")
	}
}
