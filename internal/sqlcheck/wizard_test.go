package sqlcheck

import (
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
		{"plans0", fx1CPlans(0), model.MsgSQLAutoPickMultiplePlans},
		{"plans2000", fx1CPlans(2000), model.MsgSQLAutoPickMultiplePlans},
		{"custom", fx1CCustom(2000, "_Reference77"), model.MsgSQLAutoPickLowScore},
		{"dirdates", fx1CDirDates(2000), model.MsgSQLAutoPickLowScore},
		{"generic", fxGeneric(), model.MsgSQLAutoPickLowScore},
	}
	for _, c := range cases {
		cands := discoverFx(t, newFake(c.fx...), DiscoverOptions{TopN: 20})
		p := AutoPick(cands)
		if p.ReasonKey != c.key || p.Apply != (c.key == model.MsgSQLAutoPickOK) {
			t.Errorf("%s: %+v", c.name, p)
			continue
		}
		if p.Apply {
			if cands[p.Index].Key() != p.CandidateKey || p.Score < model.SQLAutoPickMinScore || p.Margin < model.SQLAutoPickMinMargin {
				t.Errorf("%s: %+v", c.name, p)
			}
		}
	}
	if p := AutoPick(nil); p.Apply || p.Index != -1 {
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
