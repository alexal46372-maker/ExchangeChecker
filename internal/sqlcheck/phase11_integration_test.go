package sqlcheck

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// CHK-SQL-13: the full step-by-step check against the real server
// (compatibility level 100): login / db / tables, a wrong password, a
// non-1C database.
func TestCHK_SQL_13_IntegrationCheckServer(t *testing.T) {
	p, pw := itProfile(t, "ec_1c_node_off")
	port := p.Port
	if port == 0 {
		port = model.DefaultSQLPort
	}
	server := p.Server + "," + strconv.Itoa(port)
	var ids []string
	res, err := CheckServer(context.Background(), server, ServerCheckOptions{Profile: &p, Password: pw, Step: func(s model.SQLCheckStep) { ids = append(ids, s.ID) }})
	if err != nil || !res.OK || strings.Join(ids, ",") != "parse,dns,tcp,prelogin,tls,login,db,tables" || res.Version == "" {
		t.Fatalf("%+v %v %v", res, err, ids)
	}
	if last(res).Detail == "0" {
		t.Errorf("tables: %+v", last(res))
	}
	res, _ = CheckServer(context.Background(), server, ServerCheckOptions{Profile: &p, Password: pw + "x"})
	if res.OK || last(res).ID != model.SQLStepLogin || last(res).Code != model.SQLCodeLoginFailed || res.Diagnosis == nil {
		t.Errorf("bad password: %+v", res)
	}
	for _, s := range res.Steps {
		if strings.Contains(s.Detail, pw) || strings.Contains(strings.ToLower(s.Detail), "password=") {
			t.Errorf("secret in detail: %+v", s)
		}
	}
	g, gpw := itProfile(t, "ec_generic")
	res, _ = CheckServer(context.Background(), server, ServerCheckOptions{Profile: &g, Password: gpw})
	if res.OK || last(res).ID != model.SQLStepTables || last(res).Code != model.SQLCodeNot1CDatabase {
		t.Errorf("non-1C: %+v", res)
	}
	m, mpw := itProfile(t, "ec_no_such_db")
	res, _ = CheckServer(context.Background(), server, ServerCheckOptions{Profile: &m, Password: mpw})
	if res.OK || last(res).ID != model.SQLStepDB || last(res).Code != model.SQLCodeDatabaseUnavailable {
		t.Errorf("missing db: %+v", res)
	}
}

// CHK-SQL-14: node states (SentNo / ReceivedNo + change times) on the 1C
// fixture at compatibility level 100.
func TestCHK_SQL_14_IntegrationNodeStates(t *testing.T) {
	st := itStore(t, "ec_1c_node_off")
	p, _ := itProfile(t, "ec_1c_node_off")
	p.ID = "nodes"
	p.Mapping = model.Mapping{Mode: model.MappingCounters, Table: "_Node123", PointNameColumn: "_Description", PointIDColumn: "_Code", PointKeyColumn: "_IDRRef"}
	now := fixNow
	opt := Options{Now: func() time.Time { return now }, Counters: model.NewMemCounterStore()}
	ns, err := ReadNodeStates(context.Background(), st, p, opt)
	if err != nil || len(ns) == 0 {
		t.Fatalf("%+v %v", ns, err)
	}
	for _, n := range ns {
		if n.Key == "" || n.Name == "" || n.PlanTable != "_Node123" || n.PlanNo != 123 || !n.SendBaseline || !n.FreshAt.Equal(now) {
			t.Errorf("%+v", n)
		}
	}
	// second poll, numbers unchanged: times stay (stalled), baseline kept
	now = now.Add(time.Hour)
	ns2, err := ReadNodeStates(context.Background(), st, p, opt)
	if err != nil || len(ns2) != len(ns) || !ns2[0].LastSend.Equal(fixNow) || ns2[0].SentNo != ns[0].SentNo {
		t.Errorf("%+v %v", ns2, err)
	}
	// no counters mapping: empty
	q := p
	q.Mapping = model.Mapping{Table: "x"}
	if out, err := ReadNodeStates(context.Background(), st, q, opt); err != nil || out == nil || len(out) != 0 {
		t.Errorf("%v %v", out, err)
	}
}
