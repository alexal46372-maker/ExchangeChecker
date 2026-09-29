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

func fixOpt() Options { return Options{Now: func() time.Time { return fixNow }} }

func TestCHK_SQL_03_ConvertDBTime(t *testing.T) {
	naive := time.Date(4026, 9, 29, 11, 50, 0, 0, time.UTC)
	got := ConvertDBTime(&naive, 2000)
	want := time.Date(2026, 9, 29, 8, 50, 0, 0, time.UTC) // 11:50 UTC+3
	if got == nil || !got.Equal(want) || got.Location() != model.DBZone || got.Hour() != 11 {
		t.Fatalf("offset 2000: %v, want %v", got, want)
	}
	plain := time.Date(2026, 9, 29, 11, 50, 0, 0, time.UTC)
	if got := ConvertDBTime(&plain, 0); !got.Equal(want) {
		t.Errorf("offset 0: %v", got)
	}
	leap := time.Date(4024, 2, 29, 23, 59, 59, 0, time.UTC)
	if got := ConvertDBTime(&leap, 2000); got.Month() != time.February || got.Day() != 29 || got.Year() != 2024 {
		t.Errorf("leap day: %v", got)
	}
	for _, e := range []time.Time{emptyDate(2000), emptyDate(0), time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)} {
		off := 0
		if e.Year() == 2001 {
			off = 2000
		}
		if got := ConvertDBTime(&e, off); got != nil {
			t.Errorf("empty date %v -> %v, want nil", e, got)
		}
	}
	if ConvertDBTime(nil, 2000) != nil {
		t.Error("nil")
	}
}

func linkedProfile(off int) model.Profile {
	p := connProfile()
	p.Mapping = model.Mapping{Mode: model.MappingAuto, Table: "_Node123", PointNameColumn: "_Description", PointIDColumn: "_Code",
		PointKeyColumn: "_IDRRef", TimesTable: "_InfoRg456", TimesLinkColumn: "_Fld457RRef",
		LastSendColumn: "_Fld458", LastRecvColumn: "_Fld459", YearOffset: off}
	return p
}

func TestCHK_SQL_03_ListPointsLinked(t *testing.T) {
	for _, off := range []int{0, 2000} {
		r := NewDiscoveringReader(newFake(fx1CNode(off)...), linkedProfile(off), fixOpt())
		pts, err := r.ListPoints(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(pts) != 3 {
			t.Fatalf("off %d: %d points (ЦБ without times must be skipped)", off, len(pts))
		}
		m1, m2, m3 := pts[0], pts[1], pts[2]
		if m1.Name != "Магазин 1" || m1.ProfileID != "p1" || !m1.LastSend.Equal(fixNow.Add(-20*time.Minute)) ||
			!m1.LastRecv.Equal(fixNow.Add(-10*time.Minute)) || m1.Level != model.LevelOK || m1.LastRecv.Location() != model.DBZone {
			t.Errorf("off %d: Магазин 1 = %+v", off, m1)
		}
		if m2.Name != "Магазин 2" || m2.Level != model.LevelError { // 2h > 60m
			t.Errorf("off %d: Магазин 2 = %+v", off, m2)
		}
		if f, _ := m2.FreshAt(); !f.Equal(fixNow.Add(-2 * time.Hour)) {
			t.Errorf("FreshAt = max(send, recv) = %v", f)
		}
		if m3.LastSend != nil || m3.LastRecv != nil || m3.Level != model.LevelError || m3.Message != model.ReasonNoTimestamps {
			t.Errorf("off %d: empty 1C dates must be NULL -> Error: %+v", off, m3)
		}
		if !m1.CheckedAt.Equal(fixNow) {
			t.Error("CheckedAt")
		}
	}
	p := linkedProfile(2000)
	p.Mapping.KeepUnlinkedPoints = true
	pts, _ := NewDiscoveringReader(newFake(fx1CNode(2000)...), p, fixOpt()).ListPoints(context.Background())
	if len(pts) != 4 || pts[3].Name != "ЦБ" || pts[3].Level != model.LevelError {
		t.Errorf("KeepUnlinkedPoints: %+v", pts)
	}
}

func TestCHK_SQL_03_ListPointsSingleAndSettings(t *testing.T) {
	p := connProfile()
	p.Mapping = model.Mapping{Mode: model.MappingAuto, Schema: "exch", Table: "Node]Exchange", PointNameColumn: "Point Name",
		LastSendColumn: "Send]Date", LastRecvColumn: "RecvDate"}
	opt := fixOpt()
	opt.Settings = model.DefaultSettings()
	opt.Settings.WarnMinutes, opt.Settings.ErrorMinutes = 10, 20
	pts, err := NewDiscoveringReader(newFake(fxGeneric()...), p, opt).ListPoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	levels := map[string]model.AlertLevel{}
	for _, x := range pts {
		levels[x.Name] = x.Level
	}
	// North 7m -> OK, South 14m -> Warning, West 21m -> Error
	if levels["North"] != model.LevelOK || levels["South"] != model.LevelWarning || levels["West"] != model.LevelError {
		t.Errorf("levels %v", levels)
	}
}

func TestCHK_SQL_03_ListPointsErrors(t *testing.T) {
	r := NewDiscoveringReader(newFake(fx1CNode(0)...), connProfile(), fixOpt())
	if _, err := r.ListPoints(context.Background()); KindOf(err) != KindMapping || !errors.Is(err, ErrNotConfigured) {
		t.Errorf("unconfigured: %v", err)
	}
	st := newFake(fx1CNode(0)...)
	st.fail = map[string]error{"_InfoRg456": fakeNetErr{}}
	_, err := NewDiscoveringReader(st, linkedProfile(0), fixOpt()).ListPoints(context.Background())
	if KindOf(err) != KindConn {
		t.Errorf("network error -> %v", err)
	}
}

func TestCHK_SQL_03_StatusConnError(t *testing.T) {
	p := linkedProfile(0)
	p.Name = "Главная"
	st := newFake(fx1CNode(0)...)
	st.fail = map[string]error{"_Node123": fakeSQLErr(18456)}
	ps := Status(context.Background(), NewDiscoveringReader(st, p, fixOpt()), p, fixNow)
	if ps.Error == "" || ps.Level() != model.LevelError || ps.Points != nil || ps.Name != "Главная" || !ps.CheckedAt.Equal(fixNow) {
		t.Errorf("conn error status = %+v", ps)
	}
	if !strings.Contains(ps.Error, "(conn)") {
		t.Errorf("error text %q", ps.Error)
	}
	ok := Status(context.Background(), NewDiscoveringReader(newFake(fx1CNode(0)...), p, fixOpt()), p, fixNow)
	if ok.Error != "" || len(ok.Points) != 3 || ok.Level() != model.LevelError { // Магазин 2/3 are Error
		t.Errorf("status = %+v", ok)
	}
	empty := Status(context.Background(), &StubReader{}, p, fixNow)
	if empty.Points == nil || len(empty.Points) != 0 || empty.Level() != model.LevelOK {
		t.Errorf("empty status %+v", empty)
	}
	b, _ := json.Marshal(ok)
	if !strings.Contains(string(b), `"points":[`) {
		t.Errorf("json %s", b)
	}
}

func TestCHK_SQL_03_CountersReader(t *testing.T) {
	now := fixNow
	opt := Options{Now: func() time.Time { return now }, Counters: model.NewMemCounterStore()}
	p := connProfile()
	p.Mapping = model.Mapping{Mode: model.MappingCounters, Table: "_Node123", PointNameColumn: "_Description",
		PointIDColumn: "_Code", PointKeyColumn: "_IDRRef"}
	tables := fx1CNode(0)
	node := tables[0]
	st := newFake(tables...)
	r := NewDiscoveringReader(st, p, opt)
	pts, err := r.ListPoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 4 {
		t.Fatalf("%d points", len(pts))
	}
	for _, x := range pts {
		if x.Source != model.PointSourceCounters || !x.SendBaseline || !x.RecvBaseline || !x.LastSend.Equal(fixNow) || x.Level != model.LevelOK {
			t.Errorf("baseline point %+v", x)
		}
	}
	// 90 minutes later only Магазин 1 (row 1) sent a message
	now = fixNow.Add(90 * time.Minute)
	node.rows[1][node.idx("_SentNo")] = []byte("11")
	pts, err = r.ListPoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range pts {
		switch x.Name {
		case "Магазин 1":
			if x.SendBaseline || !x.RecvBaseline || !x.LastSend.Equal(now) || x.Level != model.LevelOK {
				t.Errorf("changed point %+v", x)
			}
		default:
			if !x.SendBaseline || x.Level != model.LevelError { // 90m since baseline
				t.Errorf("unchanged point %+v", x)
			}
		}
	}
	if ids := opt.Counters.(*model.MemCounterStore).ProfileIDs(); len(ids) != 1 || ids[0] != "p1" {
		t.Errorf("state not saved: %v", ids)
	}
	if got := counterKey(RawCounter{ID: " 001 "}); got != "code:001" {
		t.Error(got)
	}
	if got := counterKey(RawCounter{Name: "X"}); got != "name:X" {
		t.Error(got)
	}
	for _, v := range []interface{}{nil, int64(5), []byte("12"), "7", []byte("3.0"), float64(2)} {
		if _, err := valueInt(v); err != nil {
			t.Errorf("valueInt(%v): %v", v, err)
		}
	}
	if _, err := valueInt([]byte("abc")); err == nil {
		t.Error("valueInt text")
	}
}

func TestCHK_SQL_03_DiscoverOffersCountersFallback(t *testing.T) {
	cands := discoverFx(t, newFake(fx1CNode(2000)...), DiscoverOptions{TopN: 20})
	var cc *model.DiscoveryCandidate
	for i := range cands {
		if cands[i].Mode == model.CandidateCounters {
			cc = &cands[i]
		}
	}
	if cc == nil {
		t.Fatalf("no counters candidate%s", dump(cands))
	}
	if cc.Score >= cands[0].Score || cc.Score != countersScore || !cc.HasHint(model.HintCounters) ||
		cc.Points != 4 || !strings.Contains(cc.Reason, "fallback") {
		t.Errorf("counters candidate %+v", cc)
	}
	m := cc.Mapping()
	if m.Mode != model.MappingCounters || m.SentNoCol() != "_SentNo" || m.ReceivedNoCol() != "_ReceivedNo" || m.Validate() != nil {
		t.Errorf("mapping %+v", m)
	}
	// no _Node table -> no counters candidate
	for _, c := range discoverFx(t, newFake(fx1CCustom(2000, "_Reference77")...), DiscoverOptions{TopN: 20}) {
		if c.Mode == model.CandidateCounters {
			t.Error("counters candidate without _Node table")
		}
	}
}
