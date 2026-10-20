package monitor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

type stallReader struct {
	wizReader
	mu   *sync.Mutex
	last *time.Time
}

func (stallReader) Test(context.Context) error { return nil }
func (r stallReader) ListPoints(context.Context) ([]model.ExchangePoint, error) {
	r.mu.Lock()
	t := *r.last
	r.mu.Unlock()
	n := int64(7)
	return []model.ExchangePoint{{Name: "Магазин", LastSend: &t, LastRecv: &t, Source: model.PointSourceCounters, SentNo: &n, ReceivedNo: &n}}, nil
}

// TestCHK_MON_18_StalledAlert — a counters point unchanged for longer than
// profile.stalledMinutes is stalled in the status (point and profile),
// problems carry {kind:"stalled", pointKey, since, stalledMinutes}; enter
// and leave are alert events (warning, then recovered) like other alerts.
func TestCHK_MON_18_StalledAlert(t *testing.T) {
	clk := &fakeClock{t: t0}
	p := model.NewProfile("db1")
	p.Name, p.Server, p.Database, p.AuthMode = "Склад", "srv", "db", model.AuthWindows
	p.Mapping = model.Mapping{Mode: model.MappingCounters, Table: "_Node1", PointNameColumn: "_Description"}
	p.StalledMinutes, p.WarnMinutes, p.ErrorMinutes = 10, 600, 1200
	var mu sync.Mutex
	last := t0.Add(-5 * time.Minute)
	var evMu sync.Mutex
	var events []model.AlertEvent
	opt := baseOpt(newFactory(), NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{p}}), clk)
	opt.NewReader = func(model.Profile, string, sqlcheck.Options) (sqlcheck.ReadCloser, error) {
		return stallReader{mu: &mu, last: &last}, nil
	}
	opt.Sink = alert.SinkFunc(func(e model.AlertEvent) { evMu.Lock(); events = append(events, e); evMu.Unlock() })
	m := New(opt)
	m.reloadConfig(true)
	poll := func() model.StatusSnapshot {
		m.pollAll(context.Background())
		return m.Snapshot()
	}
	s := poll()
	if ps := profileByID(s, "db1"); ps.Stalled || len(ps.Points) != 1 || ps.Points[0].Stalled || *ps.Points[0].SentNo != 7 {
		t.Fatalf("fresh %+v", ps)
	}
	var stalled *model.Problem
	for i := 0; i < 5 && stalled == nil; i++ {
		clk.Add(10 * time.Minute)
		s = poll()
		for j := range s.Problems {
			if s.Problems[j].Kind == model.ProblemStalled {
				stalled = &s.Problems[j]
			}
		}
	}
	ps := profileByID(s, "db1")
	if stalled == nil || !ps.Stalled || !ps.Points[0].Stalled || stalled.StalledMinutes != 10 || stalled.PointKey != "Магазин" ||
		stalled.Since == nil || ps.Level != model.LevelOK {
		t.Fatalf("stalled %+v %+v", stalled, ps)
	}
	mu.Lock()
	last = clk.Now()
	mu.Unlock()
	for i := 0; i < 5; i++ {
		clk.Add(time.Minute)
		s = poll()
	}
	for _, pr := range s.Problems {
		if pr.Kind == model.ProblemStalled {
			t.Fatalf("still stalled %+v", s.Problems)
		}
	}
	evMu.Lock()
	defer evMu.Unlock()
	var enter, leave bool
	for _, e := range events {
		if e.Stalled && e.To == model.LevelWarning && e.ErrorKey == model.MsgMonitorStalled {
			enter = true
		}
		if e.Stalled && e.To == model.LevelOK && enter {
			leave = true
		}
	}
	if !enter || !leave {
		t.Errorf("events %+v", events)
	}
}

// TestCHK_MON_18_NodeStatePoints — node states become the counters points
// of ListPoints (times, baselines, plan tags) plus the message numbers;
// splitCounters keeps the time-based mappings only.
func TestCHK_MON_18_NodeStatePoints(t *testing.T) {
	ls := t0.Add(-time.Hour)
	pts := NodeStatePoints("db1", []model.SQLNodeState{{Key: "k", Name: "Магазин", PlanTable: "_Node1", PlanNo: 1, SentNo: 5, ReceivedNo: 9,
		LastSend: ls, LastRecv: ls, SendBaseline: true, RecvBaseline: true}}, model.DefaultSettings(), t0)
	if len(pts) != 1 || pts[0].Source != model.PointSourceCounters || *pts[0].SentNo != 5 || *pts[0].ReceivedNo != 9 ||
		!pts[0].SendBaseline || pts[0].PlanTable != "_Node1" || !pts[0].LastSend.Equal(ls) {
		t.Fatalf("%+v", pts)
	}
	p := model.NewProfile("x")
	p.Mapping = model.Mapping{Mode: model.MappingCounters, Table: "_Node1"}
	if rest, c := splitCounters(p); rest != nil || !c {
		t.Error("only counters")
	}
	p.Mappings = []model.Mapping{{Table: "T", LastSendColumn: "s"}, {Mode: model.MappingCounters, Table: "_Node2"}}
	if rest, c := splitCounters(p); !c || rest == nil || rest.Mapping.Table != "T" || len(rest.Mappings) != 0 {
		t.Errorf("mixed %+v", rest)
	}
	p.Mapping, p.Mappings = model.Mapping{Table: "T", LastSendColumn: "s"}, nil
	if rest, c := splitCounters(p); c || rest != nil {
		t.Error("no counters")
	}
}
