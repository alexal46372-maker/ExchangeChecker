package model

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

var cStart = time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC) // 12:00 UTC+3

func obs(key string, sent, recv int64) CounterObservation {
	return CounterObservation{Key: key, Name: "N" + key, SentNo: sent, ReceivedNo: recv}
}

func timesByKey(ts []CounterTimes) map[string]CounterTimes {
	m := map[string]CounterTimes{}
	for _, t := range ts {
		m[t.Key] = t
	}
	return m
}

// TestCHK_SQL_03_ApplyCountersCases covers the was/now table of the
// counters mode (PLAN.md §5.3).
func TestCHK_SQL_03_ApplyCountersCases(t *testing.T) {
	t0 := cStart
	st, ts := ApplyCounters(CounterState{}, []CounterObservation{obs("a", 10, 20), obs("b", 5, 5)}, t0)
	if st.Version != CounterStateVersion || len(st.Points) != 2 || len(ts) != 2 {
		t.Fatalf("first observation: %+v %+v", st, ts)
	}
	for _, x := range ts {
		if !x.SendBaseline || !x.RecvBaseline || !x.LastSend.Equal(t0) || !x.LastRecv.Equal(t0) {
			t.Errorf("baseline: %+v", x)
		}
		if x.LastSend.Location() != DBZone || x.LastSend.Hour() != 12 {
			t.Errorf("times must be in UTC+3: %v", x.LastSend)
		}
	}
	if ts[0].Key != "a" || ts[0].Name != "Na" {
		t.Error("observation order / name")
	}

	cases := []struct {
		name               string
		sent, recv         int64
		sendAt, recvAt     time.Duration // offset from t0 of the expected change time
		sendBase, recvBase bool
	}{
		{"unchanged", 10, 20, 0, 0, true, true},
		{"sent changed", 11, 20, time.Hour, 0, false, true},
		{"received changed", 10, 21, 0, time.Hour, true, false},
		{"both changed", 11, 21, time.Hour, time.Hour, false, false},
		{"sent decreased (reset)", 1, 20, time.Hour, 0, false, true},
		{"both reset to zero", 0, 0, time.Hour, time.Hour, false, false},
	}
	t1 := t0.Add(time.Hour)
	for _, c := range cases {
		_, ts := ApplyCounters(st, []CounterObservation{obs("a", c.sent, c.recv), obs("b", 5, 5)}, t1)
		a := timesByKey(ts)["a"]
		if !a.LastSend.Equal(t0.Add(c.sendAt)) || !a.LastRecv.Equal(t0.Add(c.recvAt)) ||
			a.SendBaseline != c.sendBase || a.RecvBaseline != c.recvBase {
			t.Errorf("%s: %+v", c.name, a)
		}
		if b := timesByKey(ts)["b"]; !b.LastSend.Equal(t0) || !b.SendBaseline {
			t.Errorf("%s: untouched point changed: %+v", c.name, b)
		}
	}

	// a change after a change keeps the latest time; unchanged later keeps it
	s1, _ := ApplyCounters(st, []CounterObservation{obs("a", 11, 20), obs("b", 5, 5)}, t1)
	t2 := t1.Add(30 * time.Minute)
	s2, ts2 := ApplyCounters(s1, []CounterObservation{obs("a", 11, 20), obs("b", 5, 5)}, t2)
	if a := timesByKey(ts2)["a"]; !a.LastSend.Equal(t1) || a.SendBaseline {
		t.Errorf("unchanged after change: %+v", a)
	}
	if p := s2.Points["a"]; !p.LastSeen.Equal(t2) || !p.FirstSeen.Equal(t0) || p.SentNo != 11 {
		t.Errorf("state: %+v", p)
	}

	// point added / removed
	s3, ts3 := ApplyCounters(s2, []CounterObservation{obs("a", 11, 20), obs("c", 1, 1)}, t2)
	if _, ok := s3.Points["b"]; ok || len(s3.Points) != 2 {
		t.Errorf("removed point kept: %+v", s3.Points)
	}
	if c := timesByKey(ts3)["c"]; !c.SendBaseline || !c.RecvBaseline || !c.LastSend.Equal(t2) {
		t.Errorf("added point: %+v", c)
	}
	// a point that comes back is a new baseline
	_, ts4 := ApplyCounters(s3, []CounterObservation{obs("b", 5, 5)}, t2.Add(time.Minute))
	if b := ts4[0]; !b.SendBaseline || !b.LastSend.Equal(t2.Add(time.Minute)) {
		t.Errorf("returned point: %+v", b)
	}
	// duplicates: first wins
	_, ts5 := ApplyCounters(CounterState{}, []CounterObservation{obs("d", 1, 1), obs("d", 9, 9)}, t0)
	if len(ts5) != 1 {
		t.Errorf("duplicates: %+v", ts5)
	}
	// prev is not modified (pure function)
	if st.Points["a"].SentNo != 10 {
		t.Error("prev state mutated")
	}
}

func TestCHK_SQL_03_ApplyCountersZone(t *testing.T) {
	msk := time.FixedZone("X", -5*3600)
	now := time.Date(2026, 9, 29, 4, 0, 0, 0, msk) // 12:00 UTC+3
	st, ts := ApplyCounters(CounterState{}, []CounterObservation{obs("a", 1, 1)}, now)
	if ts[0].LastSend.Location() != DBZone || ts[0].LastSend.Hour() != 12 || st.Points["a"].FirstSeen.Location() != DBZone {
		t.Errorf("zone: %v", ts[0].LastSend)
	}
	pts := CounterPoints("p1", ts, DefaultSettings(), now.Add(40*time.Minute))
	p := pts[0]
	if p.Source != PointSourceCounters || !p.SendBaseline || !p.RecvBaseline || p.ProfileID != "p1" ||
		p.Level != LevelWarning || p.CheckedAt.Location() != DBZone {
		t.Errorf("CounterPoints: %+v", p)
	}
}

func TestCHK_SQL_03_CounterStateJSONAndStore(t *testing.T) {
	st, _ := ApplyCounters(CounterState{}, []CounterObservation{obs("a", 10, 20)}, cStart)
	st, _ = ApplyCounters(st, []CounterObservation{obs("a", 11, 20)}, cStart.Add(time.Minute))
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var back CounterState
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	pa, pb := st.Points["a"], back.Points["a"]
	if back.Version != 1 || pa.SentNo != pb.SentNo || !pa.SentChangedAt.Equal(pb.SentChangedAt) ||
		pa.SentBaseline != pb.SentBaseline || pb.ReceivedBaseline != true {
		t.Errorf("roundtrip:\n%s\n%+v", b, back)
	}
	// restored state continues without a new baseline
	_, ts := ApplyCounters(back, []CounterObservation{obs("a", 11, 20)}, cStart.Add(time.Hour))
	if ts[0].SendBaseline || !ts[0].LastSend.Equal(cStart.Add(time.Minute)) {
		t.Errorf("after restore: %+v", ts[0])
	}

	var s CounterStateStore = NewMemCounterStore()
	empty, err := s.Load("p1")
	if err != nil || empty.Points == nil || len(empty.Points) != 0 {
		t.Errorf("empty load: %+v %v", empty, err)
	}
	if err := s.Save("p1", st); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Load("p1")
	if !reflect.DeepEqual(got, st) {
		t.Errorf("load = %+v", got)
	}
	got.Points["x"] = CounterPoint{}
	if again, _ := s.Load("p1"); len(again.Points) != 1 {
		t.Error("store must copy state")
	}
	var zero MemCounterStore
	if err := zero.Save("z", st); err != nil || len(zero.ProfileIDs()) != 1 {
		t.Error("zero-value store")
	}

	// ExchangePoint JSON stays backward compatible: new fields are omitempty
	pb2, _ := json.Marshal(ExchangePoint{Name: "x"})
	if contains(string(pb2), "source") || contains(string(pb2), "Baseline") {
		t.Errorf("ExchangePoint JSON: %s", pb2)
	}
	var old ExchangePoint
	if err := json.Unmarshal([]byte(`{"name":"x","level":"warning"}`), &old); err != nil || old.Source != "" || old.SendBaseline {
		t.Errorf("old JSON: %+v %v", old, err)
	}
	cp, _ := json.Marshal(ExchangePoint{Name: "x", Source: PointSourceCounters, SendBaseline: true})
	if !contains(string(cp), `"source":"counters"`) || !contains(string(cp), `"sendBaseline":true`) || contains(string(cp), "recvBaseline") {
		t.Errorf("counters JSON: %s", cp)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
