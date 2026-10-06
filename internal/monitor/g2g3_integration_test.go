package monitor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_IPC_08_IntegrationInteractive — the interactive commands
// against the real SQL Server of the linux CI job (EC_SQL_TEST_*, skipped
// otherwise; database at compatibility level 100) through the production
// path (SQLInteractive = sqlcheck.NewInteractiveReader, one reader per
// request): list_node_points lists the _Node rows, discover and
// discover_by_value answer, check_mapping previews a manual SQL, refuses
// 2012+ SQL before running it and reports a missing table as ok:false; a
// canceled request answers canceled; the password is in no error.
func TestCHK_IPC_08_IntegrationInteractive(t *testing.T) {
	p, pw := itMonSetup(t)
	m := New(Options{Source: NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{p},
		Passwords: map[string]string{p.ID: pw}}), CommandInterval: -1, PollInterval: time.Hour, ConfigCheck: time.Hour})
	m.reloadConfig(true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	req := model.IPCRequest{ProfileID: p.ID}

	pts, tr, err := m.ListNodePoints(ctx, req)
	if err != nil || tr || len(pts) != 3 {
		t.Fatalf("node points: %+v %v %v", pts, tr, err)
	}
	cands, tr, err := m.Discover(ctx, req)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	t.Logf("discover: %d candidates, truncated %v", len(cands), tr)
	q := req
	q.Query = &model.ValueQuery{PointName: "Магазин 1"}
	if _, _, err := m.DiscoverByValue(ctx, q); err != nil {
		t.Fatalf("discover_by_value: %v", err)
	}

	check := func(sql string) model.MappingCheck {
		t.Helper()
		r := req
		r.Mapping, r.Limit = &model.Mapping{Mode: model.MappingManual, ManualSQL: sql}, 20
		res, err := m.CheckMapping(ctx, r)
		if err != nil {
			t.Fatalf("check_mapping %q: %v", sql, err)
		}
		if strings.Contains(res.Error, pw) && pw != "" {
			t.Fatal("password in the error")
		}
		return res
	}
	res := check("SELECT _Description, CONVERT(datetime, '2026-10-06T09:00:00', 126), CAST(NULL AS datetime) FROM _Node300")
	if !res.OK || len(res.SampleRows) != 2 {
		t.Fatalf("manual SQL: %+v", res)
	}
	res = check("SELECT _Description, TRY_CONVERT(datetime, _Code), CAST(NULL AS datetime) FROM _Node300")
	if res.OK || len(res.Violations) == 0 || res.ErrorKey != model.MsgDiscoverMappingInvalid {
		t.Fatalf("2012+ SQL: %+v", res)
	}
	res = check("SELECT a, b, c FROM ec_no_such_table")
	if res.OK || res.ErrorKey == "" || res.Error == "" {
		t.Fatalf("missing table: %+v", res)
	}
	t.Logf("missing table: %s %v", res.ErrorKey, res.ErrorArgs)

	cctx, ccancel := context.WithCancel(ctx)
	ccancel()
	_, _, err = m.ListNodePoints(cctx, req)
	var ie *model.IPCError
	if !errors.As(err, &ie) || ie.Code != model.ErrCodeCanceled || ie.Key != model.MsgMonitorCanceled {
		t.Fatalf("canceled: %v", err)
	}
}
