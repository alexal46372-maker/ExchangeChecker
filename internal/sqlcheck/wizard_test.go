package sqlcheck

import (
	"context"
	"fmt"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// CHK-SQL-08: auto-pick on the calibrated discovery fixtures.
func TestCHK_SQL_08_AutoPickFixtures(t *testing.T) {
	cases := []struct {
		name string
		fx   []*fxTable
		key  string
	}{
		{"node0", fx1CNode(0), model.MsgSQLAutoPickOK},
		{"node2000", fx1CNode(2000), model.MsgSQLAutoPickOK},
		{"plans0", fx1CPlans(0), model.MsgSQLAutoPickOK}, // CHK-SQL-16/17: plans merged
		{"plans2000", fx1CPlans(2000), model.MsgSQLAutoPickOK},
		{"custom", fx1CCustom(2000, "_Reference77"), model.MsgSQLAutoPickLowScore},
		{"dirdates", fx1CDirDates(2000), model.MsgSQLAutoPickLowScore},
		{"generic", fxGeneric(), model.MsgSQLAutoPickLowScore},
	}
	for _, c := range cases {
		cands := discoverFx(t, newFake(c.fx...), DiscoverOptions{TopN: 20})
		p := AutoPick(cands)
		if p.ReasonKey != c.key || p.AutoApply != (c.key == model.MsgSQLAutoPickOK) {
			t.Errorf("%s: %+v", c.name, p)
			continue
		}
		if p.AutoApply && len(p.Plans) < 2 {
			if cands[p.Index].Key() != p.CandidateKey || p.Score < model.SQLAutoPickMinScore || p.Margin < model.SQLAutoPickMinMargin {
				t.Errorf("%s: %+v", c.name, p)
			}
		}
	}
	if p := AutoPick(nil); p.AutoApply || p.Index != -1 {
		t.Errorf("%+v", p)
	}
}

// CHK-SQL-09 / CHK-SQL-05: both grant script variants are SQL Server 2008
// compatible.
func TestCHK_SQL_09_GrantCompat(t *testing.T) {
	for _, r := range []model.SQLGrantRequest{
		{Principal: "ec_reader", Database: "buh"},
		{Principal: `CORP\EC-HOST$`, Windows: true, Database: "buh"},
	} {
		s, err := model.SQLGrantScriptFor(r)
		if err != nil {
			t.Fatal(err)
		}
		if v := Compat2008Violations(s.SQL); len(v) != 0 {
			t.Errorf("%+v: %v", r, v)
		}
	}
	// The sqlcheck connection categories are the model ones (sql_diagnose).
	got := []string{ConnCategoryLogin, ConnCategoryDatabase, ConnCategoryTLS, ConnCategoryTimeout, ConnCategoryNetwork, ConnCategoryConfig, ConnCategoryOther}
	for i, c := range model.SQLConnCategories() {
		if string(got[i]) != c {
			t.Errorf("%d: %s != %s", i, got[i], c)
		}
	}
}

// fx81Counters: a 1C 8.1 database without time registers: exchange plans
// _Node123 «Основной» (3 nodes), _Node124 «Магазины» (4 nodes) and
// optionally _Node125 «РИБ» (3 nodes), only _SentNo/_ReceivedNo.
func fx81Counters(three bool) []*fxTable {
	plan := func(no int, names ...string) *fxTable {
		n := fxT("dbo", fmt.Sprintf("_Node%d", no), "_IDRRef:binary(16)", "_Version:timestamp", "_Marked:binary(1)",
			"_Code:nvarchar(9)", "_Description:nvarchar(50)", "_SentNo:numeric", "_ReceivedNo:numeric", "_PredefinedID:binary(16)")
		for i, s := range names {
			n.add(ref16(byte(no), byte(i+1)), []byte{0, 0, 0, 0, 0, 0, byte(no), byte(i)}, []byte{0}, fmt.Sprintf("%03d", i), s,
				[]byte(fmt.Sprint(10+i)), []byte(fmt.Sprint(9+i)), predefined(i == 0))
		}
		return n
	}
	out := []*fxTable{plan(123, "ЦБ", "Филиал 1", "Филиал 2"), plan(124, "ЦБ", "Магазин 1", "Магазин 2", "Магазин 3")}
	if three {
		out = append(out, plan(125, "ЦБ", "РИБ 1", "РИБ 2"))
	}
	return append(out, fxCurrency(0)...)
}

// CHK-SQL-16: counters-only 1C 8.1 (2-3 plans, no time register): applied
// automatically (autopick_counters_ok), main plan = most points
// (_Node124), the others by lower PlanNo; order is deterministic.
func TestCHK_SQL_16_AutoPick81Counters(t *testing.T) {
	for _, three := range []bool{false, true} {
		cands := discoverFx(t, newFake(fx81Counters(three)...), DiscoverOptions{TopN: 20})
		p := AutoPick(cands)
		want := []string{"_Node124", "_Node123"}
		if three {
			want = append(want, "_Node125")
		}
		if !p.AutoApply || p.ReasonKey != model.MsgSQLAutoPickCountersOK || len(p.Plans) != len(want) || p.Plans[0] != p.Index {
			t.Fatalf("three=%v: %+v", three, p)
		}
		for i, ix := range p.Plans {
			if c := cands[ix]; c.Table != want[i] || c.Mode != model.CandidateCounters {
				t.Errorf("three=%v plan %d: %s %s", three, i, c.Table, c.Mode)
			}
		}
		// deterministic: reversed input gives the same plans
		rev := make([]model.DiscoveryCandidate, len(cands))
		for i := range cands {
			rev[len(cands)-1-i] = cands[i]
		}
		q := AutoPick(rev)
		for i, ix := range q.Plans {
			if rev[ix].Table != want[i] {
				t.Errorf("three=%v reversed plan %d: %s", three, i, rev[ix].Table)
			}
		}
	}
	if model.SQLMessage(model.MsgSQLAutoPickCountersOK, "ru") == "" || model.SQLMessage(model.MsgSQLAutoPickCountersOK, "en") == "" {
		t.Error("no RU/EN text")
	}
}

// CHK-SQL-17: a linked _InfoRg register on one plan plus a second plan:
// the linked time candidate wins inside its plan and is the main plan.
func TestCHK_SQL_17_AutoPickLinkedPlusPlan(t *testing.T) {
	fx := append(fx1CNode(2000), fx81Counters(false)[1]) // _Node123+_InfoRg456 linked, _Node124 counters only
	cands := discoverFx(t, newFake(fx...), DiscoverOptions{TopN: 20})
	p := AutoPick(cands)
	if !p.AutoApply || p.ReasonKey != model.MsgSQLAutoPickOK || len(p.Plans) != 2 {
		t.Fatalf("%+v", p)
	}
	m, o := cands[p.Plans[0]], cands[p.Plans[1]]
	if m.Table != "_Node123" || m.Mode != model.CandidateLinked || m.TimesTable != "_InfoRg456" || o.Table != "_Node124" || o.Mode != model.CandidateCounters {
		t.Errorf("main %s/%s/%s other %s/%s", m.Table, m.Mode, m.TimesTable, o.Table, o.Mode)
	}
}

// CHK-SQL-18: real ambiguity (two non-plan candidates of the same table
// within the margin) → autopick_ambiguous; an empty database →
// autopick_none; two different non-plan tables are not ambiguous.
func TestCHK_SQL_18_AutoPickAmbiguousEmpty(t *testing.T) {
	c := func(table, times string, score int) model.DiscoveryCandidate {
		return model.DiscoveryCandidate{Mode: model.CandidateLinked, Schema: "dbo", Table: table, PointCol: "_Description",
			TimesTable: times, LinkCol: "_Fld89RRef", SendCol: "_Fld90", RecvCol: "_Fld91", Points: 4, Score: score}
	}
	p := AutoPick([]model.DiscoveryCandidate{c("_Reference77", "_InfoRg88", 140), c("_Reference77", "_InfoRg95", 125)})
	if p.AutoApply || p.ReasonKey != model.MsgSQLAutoPickAmbiguous || len(p.Plans) != 0 {
		t.Errorf("ambiguous: %+v", p)
	}
	if p := AutoPick([]model.DiscoveryCandidate{c("_Reference77", "_InfoRg88", 140), c("_Reference80", "_InfoRg95", 125)}); !p.AutoApply || p.ReasonKey != model.MsgSQLAutoPickOK {
		t.Errorf("different tables: %+v", p)
	}
	// empty database: no tables at all
	cands, err := Discover(context.Background(), newFake(), DiscoverOptions{TopN: 20}, fixNow)
	if err != nil || len(cands) != 0 {
		t.Fatalf("empty: %v %d", err, len(cands))
	}
	if p := AutoPick(cands); p.AutoApply || p.ReasonKey != model.MsgSQLAutoPickNone || p.Index != -1 || len(p.Plans) != 0 {
		t.Errorf("empty: %+v", p)
	}
}
