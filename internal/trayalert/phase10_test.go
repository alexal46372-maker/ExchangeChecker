package trayalert

import (
	"encoding/binary"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

// TestCHK_TRAY_33_SoundPrefs — two profiles, defaults 80 / 100 %, stored
// JSON round trip, out-of-range values clamped, supported file types.
func TestCHK_TRAY_33_SoundPrefs(t *testing.T) {
	d := DefaultSoundPrefs(nil)
	if d.Warning.Volume != 80 || d.Error.Volume != 100 || d.Master != 100 || !d.Warning.Loop || d.Error.Kind != SoundBeep {
		t.Fatalf("defaults %+v", d)
	}
	tone := ToneFor(model.LevelError, nil)
	if d.Error.FreqHz != tone.FreqHz || d.Error.DurMs != tone.DurMs || d.Error.GapMs != tone.GapMs {
		t.Errorf("error tone %+v vs %+v", d.Error, tone)
	}
	p := d
	p.Warning = SoundProfile{Kind: SoundFile, File: ` C:\snd\a.mp3 `, Volume: 150, FreqHz: 5, DurMs: 0, GapMs: -1, Loop: false}
	p.Master = -3
	got, err := ParseSoundPrefs(p.String(), d)
	if err != nil {
		t.Fatal(err)
	}
	w := got.Warning
	if w.Kind != SoundFile || w.File != `C:\snd\a.mp3` || w.Volume != 100 || w.FreqHz != d.Warning.FreqHz ||
		w.DurMs != d.Warning.DurMs || w.GapMs != d.Warning.GapMs || w.Loop || got.Master != 0 {
		t.Errorf("normalized %+v master %d", w, got.Master)
	}
	if got.For(model.LevelWarning) != w || got.For(model.LevelError) != got.Error {
		t.Error("For")
	}
	// a file profile without a file is a tone
	if n := (SoundProfile{Kind: SoundFile}).Normalized(d.Error); n.Kind != SoundBeep {
		t.Errorf("empty file %+v", n)
	}
	if g, err := ParseSoundPrefs("{bad", d); err == nil || g != d {
		t.Error("invalid JSON must give the defaults and an error")
	}
	if g, err := ParseSoundPrefs("", d); err != nil || g != d {
		t.Error("empty = defaults")
	}
	for f, ok := range map[string]bool{"a.WAV": true, "b.mp3": true, "c.wma": true, "d.MID": true, "e.midi": true, "f.ogg": false, "g": false} {
		if IsSoundFile(f) != ok {
			t.Errorf("IsSoundFile(%q)", f)
		}
	}
	if MCIType("x.mid") != "sequencer" || MCIType("x.mp3") != "mpegvideo" || MCIType("x.wav") != "mpegvideo" {
		t.Error("MCI types")
	}
	if MCIVolume(80) != 800 || MCIVolume(200) != 1000 {
		t.Error("MCI volume")
	}
}

func peak(wav []byte) int {
	m := 0
	for i := 44; i+1 < len(wav); i += 2 {
		v := int(int16(binary.LittleEndian.Uint16(wav[i:])))
		if v < 0 {
			v = -v
		}
		if v > m {
			m = v
		}
	}
	return m
}

// TestCHK_TRAY_34_SineWAV — the beep is a 16-bit PCM mono sine WAV built
// in memory; the volume is the amplitude (PLAN §12 p.115).
func TestCHK_TRAY_34_SineWAV(t *testing.T) {
	wav := SineWAV(1000, 200, 100)
	if string(wav[0:4]) != "RIFF" || string(wav[8:16]) != "WAVEfmt " || string(wav[36:40]) != "data" {
		t.Fatal("header")
	}
	if binary.LittleEndian.Uint16(wav[20:]) != 1 || binary.LittleEndian.Uint16(wav[22:]) != 1 ||
		binary.LittleEndian.Uint32(wav[24:]) != WAVRate || binary.LittleEndian.Uint16(wav[34:]) != 16 {
		t.Fatal("format")
	}
	n := int(binary.LittleEndian.Uint32(wav[40:]))
	if n != 2*WAVRate*200/1000 || len(wav) != 44+n || int(binary.LittleEndian.Uint32(wav[4:])) != 36+n {
		t.Fatalf("sizes %d %d", n, len(wav))
	}
	full, half := peak(wav), peak(SineWAV(1000, 200, 50))
	if full < 32000 || half < 16000 || half > 16500 {
		t.Errorf("peaks %d %d", full, half)
	}
	if peak(SineWAV(1000, 200, 0)) != 0 {
		t.Error("0 % is silence")
	}
	// fade in: the first sample is silent (no click)
	if binary.LittleEndian.Uint16(wav[44:]) != 0 {
		t.Error("ramp")
	}
}

type fakeSounder struct {
	mu    sync.Mutex
	plays []Sound
}

func (f *fakeSounder) Play(s Sound, stop <-chan struct{}) error {
	f.mu.Lock()
	f.plays = append(f.plays, s)
	f.mu.Unlock()
	select {
	case <-stop:
	case <-time.After(time.Duration(s.Profile.DurMs) * time.Millisecond):
	}
	return nil
}

func (f *fakeSounder) n() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.plays)
}

// TestCHK_TRAY_35_PlayerAndMaster — looped or once, the gap between
// repeats, the effective volume = profile × master.
func TestCHK_TRAY_35_PlayerAndMaster(t *testing.T) {
	if EffectiveVolume(80, 50) != 40 || EffectiveVolume(100, 100) != 100 || EffectiveVolume(120, -1) != 0 {
		t.Error("effective volume")
	}
	f := &fakeSounder{}
	p := NewSoundPlayer(f)
	loop := Sound{Profile: SoundProfile{Kind: SoundBeep, FreqHz: 800, DurMs: 10, GapMs: 10, Loop: true}, Volume: 40}
	p.Set(true, loop)
	deadline := time.Now().Add(2 * time.Second)
	for f.n() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	p.Set(false, Sound{})
	if f.n() < 3 || f.plays[0].Volume != 40 || p.Playing() {
		t.Fatalf("loop plays %d", f.n())
	}
	f2 := &fakeSounder{}
	p2 := NewSoundPlayer(f2)
	once := loop
	once.Profile.Loop = false
	p2.Set(true, once)
	time.Sleep(150 * time.Millisecond)
	if f2.n() != 1 || p2.Playing() {
		t.Errorf("once: %d playing=%v", f2.n(), p2.Playing())
	}
	p.Close()
	p2.Close()
}

func snapAt(seq uint64, checked time.Time, alerts ...model.AlertEvent) trayipc.Status {
	st := status(seq, nil, alerts...)
	st.Snapshot.Profiles = []model.ProfileSnapshot{{ProfileID: "a", Name: "DB-a", Enabled: true, CheckedAt: checked}}
	return st
}

// TestCHK_TRAY_36_AckUntilNextCheck — «Подтвердить» hides the alarm only
// until a later check of the profile still sees it (status checkedAt); an
// older service (no checkedAt) keeps the old behaviour; alert_repeat
// (optional) forces it; «Звук выкл» mutes only the current alarm.
func TestCHK_TRAY_36_AckUntilNextCheck(t *testing.T) {
	t0 := fresh
	a1 := pointAlert("a", "Склад", "k1", model.LevelError, 70)
	c := New(i18n.RU)
	c.Update(snapAt(1, t0, a1), true)
	reqs := c.BeginAck()
	if v, _ := c.AckDone([]AckResult{{Req: reqs[0], Msg: okReply()}}); v.Visible {
		t.Fatal("acked: hidden")
	}
	acked := a1
	acked.Acknowledged = true
	at := t0.Add(time.Second)
	acked.AckedAt = &at
	if v := c.Update(snapAt(2, t0, acked), true); v.Visible || len(v.Alerts) != 0 {
		t.Fatalf("same check: still hidden %+v", v)
	}
	v := c.Update(snapAt(3, t0.Add(time.Minute), acked), true)
	if !v.Visible || !v.Sound || !v.Raise || len(v.Alerts) != 1 {
		t.Fatalf("next check sees it: shown again %+v", v)
	}
	// muting the window
	if v = c.ToggleMute(); v.Sound || !v.Muted || !v.Visible {
		t.Fatalf("muted %+v", v)
	}
	if v = c.ToggleMute(); !v.Sound || v.Muted {
		t.Fatalf("unmuted %+v", v)
	}
	c.ToggleMute()
	// acknowledged again, then recovery and a new alarm: sound again
	reqs = c.BeginAck()
	c.AckDone([]AckResult{{Req: reqs[0], Msg: okReply()}})
	if v = c.Update(snapAt(4, t0.Add(time.Minute), acked), true); v.Visible {
		t.Fatal("re-acked: hidden")
	}
	c.Update(snapAt(5, t0.Add(2*time.Minute)), true)
	if v = c.Update(snapAt(6, t0.Add(3*time.Minute), a1), true); !v.Visible || !v.Sound || v.Muted {
		t.Fatalf("new alarm %+v", v)
	}

	// an older service: no checkedAt, no ackedAt — hidden until a change
	old := New(i18n.RU)
	ack0 := a1
	ack0.Acknowledged = true
	for i := uint64(1); i < 4; i++ {
		if v := old.Update(snapAt(i, time.Time{}, ack0), true); v.Visible {
			t.Fatal("old service: stays hidden")
		}
	}
	// alert_repeat forces the next update to show it
	old.Repeat("a")
	if v := old.Update(snapAt(5, time.Time{}, ack0), true); !v.Visible || !v.Raise {
		t.Fatalf("alert_repeat %+v", v)
	}
	if old.Policy == nil {
		t.Error("policy")
	}
}

// TestCHK_TRAY_38_Blink — red for an error or no connection, yellow for
// warnings only, none when all is fine; problems are derived from the
// snapshot, or taken from the optional phase10-svc fields.
func TestCHK_TRAY_38_Blink(t *testing.T) {
	st := snapAt(1, fresh, pointAlert("a", "Склад", "k1", model.LevelWarning, 40))
	ps := Problems(st, StatusExt{}, i18n.RU)
	if len(ps) != 1 || BlinkFor(ps) != BlinkYellow || ps[0].Kind != ProblemWarning || !strings.Contains(ps[0].Text, "Склад") {
		t.Fatalf("warning %+v", ps)
	}
	st.Snapshot.Profiles = append(st.Snapshot.Profiles, model.ProfileSnapshot{ProfileID: "b", Name: "DB-b", Enabled: true, Error: "refused"})
	if ps = Problems(st, StatusExt{}, i18n.RU); len(ps) != 2 || BlinkFor(ps) != BlinkRed {
		t.Fatalf("sql error %+v", ps)
	}
	if ps = Problems(trayipc.Status{State: trayipc.StateUnavailable}, StatusExt{}, i18n.EN); len(ps) != 1 ||
		ps[0].Kind != ProblemNoConnection || BlinkFor(ps) != BlinkRed || ps[0].Text != i18n.T(i18n.KeyPanelNoService, i18n.EN) {
		t.Fatalf("no service %+v", ps)
	}
	if ps = Problems(trayipc.Status{State: trayipc.StateConnecting}, StatusExt{}, i18n.RU); ps != nil {
		t.Error("starting is not a problem")
	}
	if BlinkFor(Problems(snapAt(1, fresh), StatusExt{}, i18n.RU)) != BlinkNone {
		t.Error("all fine")
	}
	// optional fields (phase10-svc)
	x := ParseStatusExt([]byte(`{"seq":5,"hasProblems":true,"problems":[{"kind":"no_connection","profileId":"a","since":"2026-10-19T10:00:00+03:00"}],"problemsTruncated":true}`))
	if x.HasProblems == nil || !*x.HasProblems || !x.ProblemsTruncated || len(x.Problems) != 1 {
		t.Fatalf("ext %+v", x)
	}
	ps = Problems(snapAt(2, fresh), x, i18n.RU)
	if len(ps) != 1 || BlinkFor(ps) != BlinkRed || !strings.Contains(ps[0].Text, "DB-a") {
		t.Fatalf("ext problems %+v", ps)
	}
	// phase10-svc (capability problems): status.problems[]
	sv := snapAt(3, fresh)
	sv.Capabilities = []string{model.CapProblems}
	sv.Snapshot.HasProblems = true
	sv.Snapshot.Problems = []model.Problem{{Kind: model.ProblemWarning, ProfileID: "a", PointKey: "k9", Since: &fresh}}
	if ps = Problems(sv, ExtOf(sv), i18n.RU); len(ps) != 1 || BlinkFor(ps) != BlinkYellow || ps[0].PointKey != "k9" {
		t.Fatalf("svc problems %+v", ps)
	}
	sv.Snapshot.HasProblems, sv.Snapshot.Problems = false, nil
	if Problems(sv, ExtOf(sv), i18n.RU) != nil {
		t.Error("svc hasProblems=false")
	}
	if ExtOf(snapAt(1, fresh)).HasProblems != nil {
		t.Error("no capability: derived")
	}
	if _, ok := RepeatOf(model.AlertEvent{ProfileID: "a", Acknowledged: true}); ok {
		t.Error("acknowledged repeat must be filtered")
	}
	if r, ok := RepeatOf(model.AlertEvent{ProfileID: "a", Level: model.LevelError, CheckSeq: 3}); !ok || r.CheckSeq != 3 {
		t.Error("repeat")
	}
	no := false
	if Problems(st, StatusExt{HasProblems: &no}, i18n.RU) != nil {
		t.Error("hasProblems=false")
	}
	if x := ParseStatusExt([]byte(`{"seq":1}`)); x.HasProblems != nil {
		t.Error("missing = old behaviour")
	}
	r, ok := ParseAlertRepeat([]byte(`{"profileId":"a","level":"error","checkSeq":7,"at":"2026-10-19T10:00:00+03:00","acknowledged":true}`))
	if !ok || r.ProfileID != "a" || r.Level != model.LevelError || r.CheckSeq != 7 || !r.Acknowledged {
		t.Errorf("repeat %+v", r)
	}
	if _, ok := ParseAlertRepeat([]byte(`{}`)); ok {
		t.Error("empty repeat")
	}
}

// TestCHK_TRAY_39_PanelLines — errors first, limited with «…и ещё N».
func TestCHK_TRAY_39_PanelLines(t *testing.T) {
	var ps []Problem
	ps = append(ps, Problem{Kind: ProblemWarning, Text: "w"})
	for i := 0; i < 10; i++ {
		ps = append(ps, Problem{Kind: ProblemError, Text: "e"})
	}
	l := PanelLines(ps, i18n.RU)
	if len(l) != MaxPanelLines || l[0] != "e" || l[len(l)-1] != "…и ещё 4" {
		t.Fatalf("%q", l)
	}
	if PanelLines(nil, i18n.RU) != nil {
		t.Error("empty")
	}
}

func TestCHK_TRAY_53_TestBadge(t *testing.T) {
	v := View{Level: model.LevelWarning, Alerts: []model.AlertEvent{{ProfileID: model.AlarmTestProfileID, To: model.LevelWarning, Test: true}}}
	if title := Title(v, i18n.RU); !strings.Contains(title, "ТЕСТ") {
		t.Fatalf("title %q", title)
	}
	if body := Text(v, i18n.RU); !strings.Contains(body, "ТЕСТ") {
		t.Fatalf("body %q", body)
	}
	if titleEN := Title(v, i18n.EN); !strings.Contains(titleEN, "TEST") {
		t.Fatalf("en title %q", titleEN)
	}
}


func TestCHK_TRAY_54_ProblemsFilter(t *testing.T) {
	at := time.Now()
	st := trayipc.Status{State: trayipc.StateConnected, Snapshot: &model.StatusSnapshot{
		Alerts: []model.AlertEvent{
			{ProfileID: "p1", ProfileName: "A", To: model.LevelWarning, At: at, Test: true},
			{ProfileID: "p2", ProfileName: "B", Key: "k", To: model.LevelWarning, At: at, Stalled: true},
			{ProfileID: "p3", ProfileName: "C", To: model.LevelError, At: at},
		},
		Profiles: []model.ProfileSnapshot{{ProfileID: "p1", Name: "A"}, {ProfileID: "p2", Name: "B"}, {ProfileID: "p3", Name: "C"}},
	}}
	ps := Problems(st, StatusExt{}, i18n.RU)
	if len(ps) != 2 {
		t.Fatalf("want 2 problems (test filtered), got %d %+v", len(ps), ps)
	}
	if ps[0].Kind != ProblemStalled || ps[1].Kind != ProblemError {
		t.Fatalf("kinds %q %q", ps[0].Kind, ps[1].Kind)
	}
}
