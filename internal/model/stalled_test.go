package model

import (
	"testing"
	"time"
)

// TestCHK_MON_18_MarkStalled — counters points whose numbers have not
// changed for more than the threshold are stalled (since = last change;
// a baseline counts from the observation); not when a time-based point of
// the same node key stopped too (matched by key, never by name); not when
// the point is already warning / error by lag; never on a profile error
// or when off.
func TestCHK_MON_18_MarkStalled(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, DBZone)
	ago := func(m int) *time.Time { x := now.Add(-time.Duration(m) * time.Minute); return &x }
	cp := func(name, key string, m int) PointStatus {
		return PointStatus{ExchangePoint: ExchangePoint{Name: name, NodeKey: key, Source: PointSourceCounters}, FreshAt: ago(m)}
	}
	tp := func(name, key string, m int) PointStatus {
		return PointStatus{ExchangePoint: ExchangePoint{Name: name, NodeKey: key}, FreshAt: ago(m)}
	}
	warn := cp("w", "W", 200)
	warn.Level = LevelWarning
	errp := cp("x", "X", 200)
	errp.Level = LevelError
	ps := ProfileSnapshot{Points: []PointStatus{
		cp("a", "A", 130), cp("b", "B", 60),
		cp("c", "C", 200), tp("other name", "C", 5), // peer by key alive: stalled
		cp("d", "D", 200), tp("d renamed", "D", 300), // peer by key stopped: lag, no stall
		cp("f", "F", 200), tp("f", "F2", 300), // same name, other node: no peer
		cp("g", "G", 200), tp("g", "", 300), // peer without a key: never matched
		warn, errp, tp("e", "E", 500)}}
	MarkStalled(&ps, 120*time.Minute, now)
	want := map[int]bool{0: true, 1: false, 2: true, 3: false, 4: false, 5: false, 6: true, 7: false, 8: true, 9: false, 10: false, 11: false, 12: false}
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

// TestCHK_MON_18_PeerNodeKeyFromListPoints — time-based points carry
// ExchangePoint.NodeKey from sqlcheck ListPoints (EC SQL 97d3236, json
// nodeKey); through NewProfileSnapshot the stalled peer is matched by it:
// same key with a different name is the peer, an empty key or a different
// key is no peer.
func TestCHK_MON_18_PeerNodeKeyFromListPoints(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, DBZone)
	at := func(m int) *time.Time { x := now.Add(-time.Duration(m) * time.Minute); return &x }
	const key = "_Node12:0a1b2c"
	p := NewProfile("db1")
	p.StalledMinutes, p.WarnMinutes, p.ErrorMinutes = 60, 600, 1200
	g := DefaultSettings()
	counters := ExchangePoint{ProfileID: "db1", Name: "Магазин 1", NodeKey: key, Source: PointSourceCounters, LastSend: at(200), LastRecv: at(200)}
	for _, c := range []struct {
		name, peerName, peerKey string
		stalled                 bool
	}{
		{"same key, other name: peer stopped too", "Shop #1 (time)", key, false},
		{"empty key: no peer", "Магазин 1", "", true},
		{"other key: no peer", "Магазин 1", "_Node12:ffff", true},
	} {
		peer := ExchangePoint{ProfileID: "db1", Name: c.peerName, NodeKey: c.peerKey, LastSend: at(300), LastRecv: at(300)}
		snap := NewProfileSnapshot(p, ProfileStatus{ProfileID: "db1", Points: []ExchangePoint{counters, peer}}, g, now)
		if len(snap.Points) != 2 || snap.Points[1].NodeKey != c.peerKey || snap.Points[0].Stalled != c.stalled || snap.Stalled != c.stalled {
			t.Errorf("%s: %+v", c.name, snap.Points)
		}
	}
	// the alive peer of the same key: a real stall
	peer := ExchangePoint{ProfileID: "db1", Name: "x", NodeKey: key, LastSend: at(5), LastRecv: at(5)}
	if snap := NewProfileSnapshot(p, ProfileStatus{ProfileID: "db1", Points: []ExchangePoint{counters, peer}}, g, now); !snap.Points[0].Stalled {
		t.Errorf("alive peer: %+v", snap.Points)
	}
}
