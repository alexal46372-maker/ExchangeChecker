package model

import (
	"testing"
	"time"
)

// TestCHK_MON_18_MarkStalled — counters points whose numbers have not
// changed for more than the threshold are stalled (since = last change;
// a baseline counts from the observation); not when a time-based point of
// the same name stopped too; never on a profile error or when off.
func TestCHK_MON_18_MarkStalled(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, DBZone)
	ago := func(m int) *time.Time { x := now.Add(-time.Duration(m) * time.Minute); return &x }
	cp := func(name string, m int) PointStatus {
		return PointStatus{ExchangePoint: ExchangePoint{Name: name, Source: PointSourceCounters}, FreshAt: ago(m)}
	}
	tp := func(name string, m int) PointStatus {
		return PointStatus{ExchangePoint: ExchangePoint{Name: name}, FreshAt: ago(m)}
	}
	ps := ProfileSnapshot{Points: []PointStatus{cp("a", 130), cp("b", 60), cp("c", 200), tp("c", 5), cp("d", 200), tp("d", 300), tp("e", 500)}}
	MarkStalled(&ps, 120*time.Minute, now)
	want := map[int]bool{0: true, 1: false, 2: true, 3: false, 4: false, 5: false, 6: false}
	for i, w := range want {
		if ps.Points[i].Stalled != w {
			t.Errorf("point %d (%s) stalled %v", i, ps.Points[i].Name, ps.Points[i].Stalled)
		}
	}
	if !ps.Stalled || !ps.Points[0].StalledSince.Equal(*ago(130)) {
		t.Errorf("profile %v since %v", ps.Stalled, ps.Points[0].StalledSince)
	}
	ps.Error = "x"
	MarkStalled(&ps, 120*time.Minute, now)
	if ps.Stalled || ps.Points[0].Stalled {
		t.Error("stalled on error")
	}
	ps.Error = ""
	MarkStalled(&ps, 0, now)
	if ps.Stalled {
		t.Error("stalled while off")
	}
}

// TestCHK_MON_18_StalledSettings — inherit / override / off, validation
// 0 or 10..1440 (settings and profile), default 120, problem kind.
func TestCHK_MON_18_StalledSettings(t *testing.T) {
	g := DefaultSettings()
	if *g.StalledMinutes != 120 || (Profile{}).StalledAfter(g) != 120*time.Minute || (Profile{}).StalledAfter(Settings{}) != 120*time.Minute {
		t.Fatal("default")
	}
	off := 0
	g.StalledMinutes = &off
	if (Profile{}).StalledAfter(g) != 0 || (Profile{StalledMinutes: 30}).StalledAfter(g) != 30*time.Minute {
		t.Error("off / override")
	}
	for _, v := range []int{-1, 5, 9, 1441} {
		s := DefaultSettings()
		s.StalledMinutes = &v
		if fe := AsFieldError(s.Validate()); fe == nil || fe.Field != "stalledMinutes" {
			t.Errorf("settings %d: %v", v, s.Validate())
		}
		p := Profile{StalledMinutes: v}
		if fe := AsFieldError(p.validateThresholds()); fe == nil || fe.Field != "stalledMinutes" {
			t.Errorf("profile %d", v)
		}
	}
	for _, v := range []int{0, 10, 1440} {
		s := DefaultSettings()
		s.StalledMinutes = &v
		if err := s.Validate(); err != nil {
			t.Errorf("settings %d: %v", v, err)
		}
		if err := (Profile{StalledMinutes: v}).validateThresholds(); err != nil {
			t.Errorf("profile %d: %v", v, err)
		}
	}
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, DBZone)
	pr, _ := ProblemsOf([]AlertEvent{{ProfileID: "p", Key: "a", PointName: "a", To: LevelWarning, From: LevelOK, At: at, Stalled: true, StalledMinutes: 120},
		{ProfileID: "p", Key: "b", PointName: "b", To: LevelWarning, From: LevelOK, At: at}})
	if len(pr) != 2 || pr[0].Kind != ProblemWarning || pr[1].Kind != ProblemStalled || pr[1].StalledMinutes != 120 || pr[1].PointKey != "a" {
		t.Errorf("problems %+v", pr)
	}
	if ViberEventOf(AlertEvent{PointName: "a", From: LevelOK, To: LevelWarning, Stalled: true}) != ViberEventWarning {
		t.Error("viber mapping")
	}
}
