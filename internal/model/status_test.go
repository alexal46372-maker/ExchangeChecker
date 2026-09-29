package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// fake clock: a fixed instant in UTC+3 (PLAN.md §12 p.3).
var monNow = time.Date(2026, 9, 29, 12, 0, 0, 0, DBZone)

func ago(min int) *time.Time { t := monNow.Add(-time.Duration(min) * time.Minute); return &t }

// TestCHK_MON_01_FreshAtMaxAndUTC3 — FreshAt = max(LastSend, LastRecv),
// NULLs ignored, everything in UTC+3 even when the input is UTC.
func TestCHK_MON_01_FreshAtMaxAndUTC3(t *testing.T) {
	s := DefaultSettings()
	utcSend := monNow.Add(-40 * time.Minute).UTC()
	p := ExchangePoint{Name: "A", LastSend: &utcSend, LastRecv: ago(10)}
	ps := NewPointStatus(p, s, monNow.UTC())
	if ps.FreshAt == nil || !ps.FreshAt.Equal(*ago(10)) || ps.FreshAt.Location() != DBZone {
		t.Fatalf("FreshAt = %v", ps.FreshAt)
	}
	if ps.LastSend.Location() != DBZone || ps.CheckedAt.Location() != DBZone {
		t.Errorf("times not in UTC+3: %v %v", ps.LastSend, ps.CheckedAt)
	}
	if _, off := ps.FreshAt.Zone(); off != 3*3600 {
		t.Errorf("offset %d", off)
	}
	if ps.LagMinutes != 10 || ps.Level != LevelOK {
		t.Errorf("lag %d level %v", ps.LagMinutes, ps.Level)
	}
	// only one side known
	ps = NewPointStatus(ExchangePoint{Name: "B", LastRecv: ago(45)}, s, monNow)
	if !ps.FreshAt.Equal(*ago(45)) || ps.Level != LevelWarning || ps.LagMinutes != 45 {
		t.Errorf("recv only: %+v", ps)
	}
	// both NULL -> error no_timestamps, lag unknown
	ps = NewPointStatus(ExchangePoint{Name: "C"}, s, monNow)
	if ps.FreshAt != nil || ps.Level != LevelError || ps.Message != ReasonNoTimestamps || ps.LagMinutes != -1 {
		t.Errorf("no timestamps: %+v", ps)
	}
	// DB clock ahead: lag clamped to 0
	ps = NewPointStatus(ExchangePoint{Name: "D", LastSend: ago(-5)}, s, monNow)
	if ps.LagMinutes != 0 || ps.Level != LevelOK {
		t.Errorf("future: %+v", ps)
	}
}

// TestCHK_MON_01_LevelsDefaultThresholds — 30/60 min boundaries.
func TestCHK_MON_01_LevelsDefaultThresholds(t *testing.T) {
	s := DefaultSettings()
	cases := []struct {
		min  int
		want AlertLevel
	}{{0, LevelOK}, {29, LevelOK}, {30, LevelWarning}, {59, LevelWarning}, {60, LevelError}, {600, LevelError}}
	for _, c := range cases {
		ps := NewPointStatus(ExchangePoint{Name: "x", LastSend: ago(c.min)}, s, monNow)
		if ps.Level != c.want {
			t.Errorf("lag %d min: %v, want %v", c.min, ps.Level, c.want)
		}
	}
}

// TestCHK_MON_01_ProfileThresholds — per-profile overrides, fallbacks.
func TestCHK_MON_01_ProfileThresholds(t *testing.T) {
	g := DefaultSettings()
	p := Profile{ID: "p"}
	if s := p.EffectiveSettings(g); s.WarnMinutes != 30 || s.ErrorMinutes != 60 {
		t.Errorf("defaults: %d/%d", s.WarnMinutes, s.ErrorMinutes)
	}
	if s := p.EffectiveSettings(Settings{}); s.WarnMinutes != 30 || s.ErrorMinutes != 60 {
		t.Errorf("zero global: %d/%d", s.WarnMinutes, s.ErrorMinutes)
	}
	p.WarnMinutes, p.ErrorMinutes = 5, 10
	if s := p.EffectiveSettings(g); s.WarnMinutes != 5 || s.ErrorMinutes != 10 || s.PollIntervalSec != g.PollIntervalSec {
		t.Errorf("override: %+v", s)
	}
	p.WarnMinutes, p.ErrorMinutes = 90, 0 // would exceed global error 60 -> global pair
	if s := p.EffectiveSettings(g); s.WarnMinutes != 30 || s.ErrorMinutes != 60 {
		t.Errorf("inconsistent single override: %d/%d", s.WarnMinutes, s.ErrorMinutes)
	}
	p.WarnMinutes, p.ErrorMinutes = 0, 120
	if s := p.EffectiveSettings(g); s.WarnMinutes != 30 || s.ErrorMinutes != 120 {
		t.Errorf("error only: %d/%d", s.WarnMinutes, s.ErrorMinutes)
	}
	// validation
	base := Profile{ID: "p", Server: "s", Database: "d", AuthMode: AuthWindows}
	for _, c := range []struct {
		w, e int
		ok   bool
	}{{0, 0, true}, {5, 10, true}, {10, 10, false}, {-1, 0, false}, {0, MaxThresholdMinutes + 1, false}, {20, 0, true}} {
		q := base
		q.WarnMinutes, q.ErrorMinutes = c.w, c.e
		if err := q.Validate(); (err == nil) != c.ok {
			t.Errorf("validate %d/%d: %v", c.w, c.e, err)
		}
	}
	// thresholds applied in the profile snapshot
	q := base
	q.WarnMinutes, q.ErrorMinutes = 5, 10
	snap := NewProfileSnapshot(q, ProfileStatus{Points: []ExchangePoint{{Name: "a", LastSend: ago(7)}}}, g, monNow)
	if snap.Level != LevelWarning || snap.WarnMinutes != 5 || snap.ErrorMinutes != 10 {
		t.Errorf("snapshot with overrides: %+v", snap)
	}
	// JSON omitempty: old profiles unchanged
	b, _ := json.Marshal(base)
	if strings.Contains(string(b), "warnMinutes") {
		t.Errorf("omitempty: %s", b)
	}
}

// TestCHK_MON_01_WorstStatusAndError — profile = worst point; error = Error.
func TestCHK_MON_01_WorstStatusAndError(t *testing.T) {
	g := DefaultSettings()
	p := Profile{ID: "db1", Name: "База 1", Enabled: true}
	st := ProfileStatus{ProfileID: "db1", CheckedAt: monNow.UTC(), Points: []ExchangePoint{
		{Name: "ok", LastSend: ago(1)}, {Name: "warn", LastRecv: ago(35)}, {Name: "ok2", LastRecv: ago(3), LastSend: ago(100)},
	}}
	snap := NewProfileSnapshot(p, st, g, monNow)
	if snap.Level != LevelWarning || !snap.FreshAt.Equal(*ago(1)) || snap.Name != "База 1" || !snap.Polled {
		t.Fatalf("worst warning: %+v", snap)
	}
	if snap.CheckedAt.Location() != DBZone {
		t.Error("checkedAt not UTC+3")
	}
	st.Points = append(st.Points, ExchangePoint{Name: "err", LastSend: ago(61)})
	if snap = NewProfileSnapshot(p, st, g, monNow); snap.Level != LevelError {
		t.Errorf("worst error: %v", snap.Level)
	}
	// connection error: level error with an ErrorKey even without a specific one
	snap = NewProfileSnapshot(p, ProfileStatus{Error: "login failed"}, g, monNow)
	if snap.Level != LevelError || snap.ErrorKey != MsgMonitorPollError || len(snap.Points) != 0 {
		t.Errorf("conn error: %+v", snap)
	}
	snap = NewProfileSnapshot(p, ProfileStatus{Error: "tls", ErrorKey: "sql.tls_enable_tls10"}, g, monNow)
	if snap.ErrorKey != "sql.tls_enable_tls10" {
		t.Errorf("key kept: %q", snap.ErrorKey)
	}
	// worst over enabled profiles only
	all := []ProfileSnapshot{{Enabled: true, Level: LevelWarning}, {Enabled: false, Level: LevelError}}
	if WorstProfileLevel(all) != LevelWarning {
		t.Error("disabled profile must not count")
	}
	pend := PendingProfileSnapshot(p, g)
	if pend.Polled || pend.Level != LevelOK || pend.Points == nil {
		t.Errorf("pending: %+v", pend)
	}
}

// TestCHK_MON_01_CountersBaselineFlag — "counting since observation".
func TestCHK_MON_01_CountersBaselineFlag(t *testing.T) {
	g := DefaultSettings()
	st, times := ApplyCounters(CounterState{}, []CounterObservation{{Key: "k", Name: "A", SentNo: 1, ReceivedNo: 1}}, monNow)
	pts := CounterPoints("p", times, g, monNow)
	ps := NewPointStatus(pts[0], g, monNow)
	if !ps.Baseline || ps.Source != PointSourceCounters {
		t.Fatalf("first observation must be baseline: %+v", ps)
	}
	later := monNow.Add(10 * time.Minute)
	_, times = ApplyCounters(st, []CounterObservation{{Key: "k", Name: "A", SentNo: 2, ReceivedNo: 1}}, later)
	ps = NewPointStatus(CounterPoints("p", times, g, later)[0], g, later)
	if ps.Baseline || !ps.FreshAt.Equal(later) {
		t.Errorf("changed counter: FreshAt is a real change time: %+v", ps)
	}
	snap := NewProfileSnapshot(Profile{ID: "p"}, ProfileStatus{Points: CounterPoints("p", times, g, later)}, g, later)
	if snap.Baseline {
		t.Error("profile baseline must follow the freshest side")
	}
	if MonitorMessage(MsgMonitorBaseline, LangEN) == "" || MonitorMessage(MsgMonitorBaseline, LangRU) == "" {
		t.Error("baseline text")
	}
}

// TestCHK_MON_01_MessagesAndJSON — every monitor key has RU+EN text; the
// snapshot JSON roundtrips with levels as strings.
func TestCHK_MON_01_MessagesAndJSON(t *testing.T) {
	for _, k := range MonitorMessageKeys() {
		if MonitorMessage(k, LangRU) == "" || MonitorMessage(k, LangEN) == "" || MonitorMessage(k, LangRU) == MonitorMessage(k, LangEN) {
			t.Errorf("key %s", k)
		}
	}
	if MonitorMessage("nope", LangRU) != "" {
		t.Error("unknown key")
	}
	g := DefaultSettings()
	snap := StatusSnapshot{Seq: 3, Now: monNow, Level: LevelWarning, Profiles: []ProfileSnapshot{
		NewProfileSnapshot(Profile{ID: "p", Enabled: true}, ProfileStatus{Points: []ExchangePoint{{Name: "a", LastSend: ago(31)}}}, g, monNow)}}
	b, err := json.Marshal(IPCMessage{Type: MsgStatus, Status: &snap})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"level":"warning"`, `"freshAt":"2026-09-29T11:29:00+03:00"`, `"lagMinutes":31`, `"type":"status"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("JSON lacks %s: %s", want, b)
		}
	}
	var back IPCMessage
	if err := json.Unmarshal(b, &back); err != nil || back.Status.Profiles[0].Points[0].Level != LevelWarning {
		t.Errorf("roundtrip: %v %+v", err, back)
	}
	ev := AlertEvent{From: LevelOK, To: LevelWarning}
	if !ev.Raised() || !ev.ProfileLevel() {
		t.Error("event helpers")
	}
}

// TestCHK_MON_08_NewProfileDefaultPreset2008 — new bases default to the
// SQL Server 2008 preset (target: 2008 R2 RTM without TLS 1.2).
func TestCHK_MON_08_NewProfileDefaultPreset2008(t *testing.T) {
	p := NewProfile("new1")
	if p.ServerPreset != ServerPresetMSSQL2008 || DefaultServerPreset != ServerPresetMSSQL2008 {
		t.Fatalf("preset %q", p.ServerPreset)
	}
	if p.EffectiveTLSMinVersion() != TLSMin10 || p.TLSMinVersion != TLSMin10 {
		t.Errorf("TLS min %q/%q", p.TLSMinVersion, p.EffectiveTLSMinVersion())
	}
	if !p.Enabled || p.AuthMode != AuthSQL || p.Encrypt != DefaultEncrypt || p.EffectiveEncrypt() != EncryptDisable {
		t.Errorf("defaults %+v", p)
	}
	p.Server, p.Database, p.Login = "srv", "db", "u"
	if err := p.Validate(); err != nil {
		t.Errorf("filled new profile must validate: %v", err)
	}
	b, _ := json.Marshal(p)
	if !strings.Contains(string(b), `"serverPreset":"mssql2008"`) {
		t.Errorf("preset not persisted: %s", b)
	}
}
