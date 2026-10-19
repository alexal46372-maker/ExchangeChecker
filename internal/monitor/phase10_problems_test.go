package monitor

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// TestCHK_MON_17_AlarmPersists — phase 10: acknowledging does not stop
// the checks; every failing check of an active alert is pushed again
// (stillFailing, repeat, since, lastCheckedAt; acknowledged too) without a
// new event id; hasProblems / problems in every snapshot; recovery clears
// them with the normal recovered event.
func TestCHK_MON_17_AlarmPersists(t *testing.T) {
	e := newAckEnv(t, nil, alert.Policy{RaisePolls: 1, ClearPolls: 2, ProfileRaisePolls: 1})
	var reps []model.AlertEvent
	e.m.opt.OnRepeat = func(ev model.AlertEvent) { e.mu.Lock(); reps = append(reps, ev); e.mu.Unlock() }

	s := e.poll(1)
	if s.HasProblems || len(s.Problems) != 0 {
		t.Fatalf("ok: %+v", s.Problems)
	}
	b, _ := json.Marshal(s)
	if !strings.Contains(string(b), `"hasProblems":false`) {
		t.Fatalf("hasProblems not explicit: %s", b)
	}
	s = e.poll(40) // warning: transition, no repeat
	if !s.HasProblems || len(s.Problems) != 1 || len(reps) != 0 {
		t.Fatalf("warning: %+v reps %d", s.Problems, len(reps))
	}
	p := s.Problems[0]
	if p.Kind != model.ProblemWarning || p.ProfileID != "a" || p.PointKey != "_Node1/A" || p.Since == nil || p.EventID == 0 {
		t.Fatalf("problem %+v", p)
	}
	evID := p.EventID
	if _, err := e.m.AckAlert("a", ""); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		polls := e.f.get(e.f.calls, "a")
		s = e.poll(40 + i)
		if e.f.get(e.f.calls, "a") != polls+1 {
			t.Fatalf("acknowledged alarm stopped the checks")
		}
		if len(reps) != i {
			t.Fatalf("repeat %d: %d pushes", i, len(reps))
		}
		r := reps[i-1]
		if !r.StillFailing || r.Repeat != uint32(i) || !r.Acknowledged || r.ID != evID || r.EventID != evID ||
			r.Since == nil || !r.Since.Equal(*p.Since) || r.LastCheckedAt == nil || r.RunID == "" {
			t.Fatalf("repeat %+v", r)
		}
		if s.Problems[0].Repeat != uint32(i) || !s.Problems[0].Acknowledged {
			t.Fatalf("problem %+v", s.Problems[0])
		}
	}
	if pg := e.m.EventsSince(e.m.RunID(), 0, 0); len(pg.Events) != 1 {
		t.Fatalf("repeats in events_since: %+v", pg.Events)
	}
	s = e.poll(80) // error: transition (no repeat), since kept
	if len(reps) != 3 || s.Problems[0].Kind != model.ProblemError || !s.Problems[0].Since.Equal(*p.Since) {
		t.Fatalf("error: %+v reps %d", s.Problems, len(reps))
	}
	e.poll(1)
	s = e.poll(1) // recovered after ClearPolls = 2
	if s.HasProblems || len(s.Problems) != 0 {
		t.Fatalf("recovered: %+v", s.Problems)
	}
	if len(reps) != 3 { // an ok observation is never a repeat
		t.Fatalf("repeats after recovery %d", len(reps))
	}
}

// TestCHK_MON_17_NoConnectionProblem — a profile connection error is a
// no_connection problem, repeated on every failing poll, sorted first.
func TestCHK_MON_17_NoConnectionProblem(t *testing.T) {
	e := newAckEnv(t, nil, alert.Policy{RaisePolls: 1, ClearPolls: 1, ProfileRaisePolls: 1})
	var reps []model.AlertEvent
	e.m.opt.OnRepeat = func(ev model.AlertEvent) { reps = append(reps, ev) }
	e.poll(40)
	e.f.set("b", behavior{err: &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "ping", Err: errors.New("down")}})
	e.poll(40)
	s := e.poll(41)
	if len(s.Problems) != 2 || s.Problems[0].Kind != model.ProblemNoConnection || s.Problems[0].ProfileID != "b" ||
		s.Problems[0].PointKey != "" || s.Problems[0].ErrorKey != model.MsgMonitorConnError {
		t.Fatalf("problems %+v", s.Problems)
	}
	n := 0
	for _, r := range reps {
		if r.ProfileID == "b" && r.StillFailing {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("profile repeats %d: %+v", n, reps)
	}
}

func TestCHK_MON_17_ProblemsLimit(t *testing.T) {
	var as []model.AlertEvent
	for i := 0; i < model.ProblemsMax+3; i++ {
		as = append(as, model.AlertEvent{ID: uint64(i + 1), ProfileID: "p", Key: string(rune('a'+i%26)) + strings.Repeat("x", i), PointName: "x", To: model.LevelWarning})
	}
	ps, tr := model.ProblemsOf(as)
	if len(ps) != model.ProblemsMax || !tr {
		t.Fatalf("%d %v", len(ps), tr)
	}
}
