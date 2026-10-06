package trayalert

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

var fresh = time.Date(2026, 10, 3, 12, 0, 0, 0, time.FixedZone("UTC+3", 3*3600))

func pointAlert(profile, name, key string, lvl model.AlertLevel, lag int) model.AlertEvent {
	return model.AlertEvent{ProfileID: profile, ProfileName: "DB-" + profile, PointName: name, PlanTable: "_Node1",
		Key: key, From: model.LevelOK, To: lvl, FreshAt: &fresh, LagMinutes: lag}
}

func dbAlert(profile string, lvl model.AlertLevel) model.AlertEvent {
	return model.AlertEvent{ProfileID: profile, ProfileName: "DB-" + profile, To: lvl, ErrorKey: model.MsgMonitorConnError, Error: "dial tcp: refused"}
}

func status(seq uint64, settings *model.Settings, alerts ...model.AlertEvent) trayipc.Status {
	return trayipc.Status{State: trayipc.StateConnected, Version: 2,
		Snapshot: &model.StatusSnapshot{Seq: seq, Alerts: alerts, Settings: settings}}
}

// TestCHK_TRAY_03_ShowOnNewAlert — a new active alert opens the window and
// starts the beep with the tone of the worst level from the settings.
func TestCHK_TRAY_03_ShowOnNewAlert(t *testing.T) {
	c := New(i18n.RU)
	if v := c.Update(status(1, nil), true); v.Visible || v.Sound {
		t.Fatal("no alerts: nothing shown")
	}
	set := model.DefaultSettings()
	set.Beep = model.BeepSettings{WarningFreqHz: 700, WarningDurMs: 200, ErrorFreqHz: 1200, ErrorDurMs: 400, GapMs: 900}
	v := c.Update(status(2, &set, pointAlert("a", "Склад", "_Node1/Склад", model.LevelWarning, 35)), true)
	if !v.Visible || !v.Sound || v.Level != model.LevelWarning || !v.CanAck {
		t.Fatalf("warning view %+v", v)
	}
	if v.Tone != (Tone{700, 200, 900}) {
		t.Errorf("warning tone %+v", v.Tone)
	}
	// settings are kept when a push snapshot has none
	v = c.Update(status(3, nil, pointAlert("a", "Склад", "_Node1/Склад", model.LevelWarning, 35), dbAlert("b", model.LevelError)), true)
	if v.Level != model.LevelError || v.Tone != (Tone{1200, 400, 900}) || len(v.Alerts) != 2 {
		t.Fatalf("error view %+v", v)
	}
	if v.Alerts[0].ProfileID != "b" {
		t.Error("worst first")
	}
}

// TestCHK_TRAY_03_ToneDefaults — missing or out-of-range beep settings use
// the defaults of PLAN §4.3.
func TestCHK_TRAY_03_ToneDefaults(t *testing.T) {
	d := model.DefaultSettings().Beep
	if got := ToneFor(model.LevelError, nil); got != (Tone{d.ErrorFreqHz, d.ErrorDurMs, d.GapMs}) {
		t.Errorf("nil settings %+v", got)
	}
	bad := model.DefaultSettings()
	bad.Beep = model.BeepSettings{WarningFreqHz: 10, WarningDurMs: 0, ErrorFreqHz: 99999, ErrorDurMs: 20000, GapMs: -1}
	if got := ToneFor(model.LevelWarning, &bad); got != (Tone{d.WarningFreqHz, d.WarningDurMs, d.GapMs}) {
		t.Errorf("bad warning %+v", got)
	}
	if got := ToneFor(model.LevelError, &bad); got != (Tone{d.ErrorFreqHz, d.ErrorDurMs, d.GapMs}) {
		t.Errorf("bad error %+v", got)
	}
}

// TestCHK_TRAY_03_CloseMutesUntilChange — PLAN §12 p.50: closing hides and
// mutes; the same alerts do not reopen it; a new alert or a worse level
// does; an improvement does not.
func TestCHK_TRAY_03_CloseMutesUntilChange(t *testing.T) {
	c := New(i18n.RU)
	w := pointAlert("a", "Склад", "k1", model.LevelWarning, 35)
	c.Update(status(1, nil, w), true)
	v := c.Close()
	if v.Visible || v.Sound || len(v.Alerts) != 1 {
		t.Fatalf("closed view %+v", v)
	}
	if v = c.Update(status(2, nil, w), true); v.Visible || v.Sound {
		t.Fatal("same alerts must stay muted")
	}
	e := pointAlert("a", "Склад", "k1", model.LevelError, 65)
	if v = c.Update(status(3, nil, e), true); !v.Visible || !v.Sound {
		t.Fatal("escalation must reopen")
	}
	c.Close()
	if v = c.Update(status(4, nil, w), true); v.Visible || v.Sound {
		t.Fatal("improvement error -> warning must not re-arm")
	}
	if v = c.Update(status(5, nil, w, pointAlert("a", "Магазин", "k2", model.LevelWarning, 31)), true); !v.Visible || !v.Sound {
		t.Fatal("new alert must reopen")
	}
	// everything cleared -> window and sound off
	if v = c.Update(status(6, nil), true); v.Visible || v.Sound || len(v.Alerts) != 0 {
		t.Fatalf("cleared %+v", v)
	}
}

// TestCHK_TRAY_03_AcknowledgedHidden — acknowledged and OK alerts are not
// shown; a disconnected session keeps the last state and disables ack.
func TestCHK_TRAY_03_AcknowledgedHidden(t *testing.T) {
	c := New(i18n.RU)
	ack := pointAlert("a", "Склад", "k1", model.LevelError, 70)
	ack.Acknowledged = true
	ok := pointAlert("a", "Магазин", "k2", model.LevelOK, 0)
	if v := c.Update(status(1, nil, ack, ok), true); v.Visible || len(v.Alerts) != 0 {
		t.Fatalf("acknowledged/OK shown: %+v", v)
	}
	c.Update(status(2, nil, pointAlert("a", "Склад", "k3", model.LevelWarning, 40)), true)
	v := c.Update(trayipc.Status{State: trayipc.StateNotResponding, Snapshot: &model.StatusSnapshot{Seq: 3}}, true)
	if !v.Visible || !v.Sound || len(v.Alerts) != 1 || v.CanAck {
		t.Fatalf("disconnected: keep the alert, no ack: %+v", v)
	}
	if reqs := c.BeginAck(); reqs != nil {
		t.Error("no ack while disconnected")
	}
	// v1 service: no ack_alert
	c2 := New(i18n.EN)
	if v := c2.Update(status(1, nil, pointAlert("a", "P", "k", model.LevelWarning, 31)), false); !v.Visible || v.CanAck {
		t.Fatalf("v1: %+v", v)
	}
}

// TestCHK_TRAY_03_AckRequests — one request per profile with an alert of
// the database itself (covers its points), otherwise per point.
func TestCHK_TRAY_03_AckRequests(t *testing.T) {
	c := New(i18n.RU)
	c.Update(status(1, nil,
		pointAlert("a", "Склад", "k1", model.LevelError, 70),
		pointAlert("a", "Магазин", "k2", model.LevelWarning, 40),
		dbAlert("b", model.LevelError),
		pointAlert("b", "Филиал", "k3", model.LevelWarning, 40)), true)
	reqs := c.BeginAck()
	if len(reqs) != 3 {
		t.Fatalf("requests %+v", reqs)
	}
	if r := reqs[0]; r.Req.Cmd != model.CmdAckAlert || r.Req.ProfileID != "b" || r.Req.PointKey != "" || len(r.IDs) != 2 {
		t.Errorf("profile request %+v", r)
	}
	keys := map[string]bool{}
	for _, r := range reqs[1:] {
		if r.Req.ProfileID != "a" || len(r.IDs) != 1 {
			t.Errorf("point request %+v", r)
		}
		keys[r.Req.PointKey] = true
	}
	if !keys["k1"] || !keys["k2"] {
		t.Errorf("point keys %v", keys)
	}
	v := c.View()
	if !v.Pending || v.CanAck || v.Message == "" {
		t.Errorf("pending view %+v", v)
	}
	if c.BeginAck() != nil {
		t.Error("no second ack while one is in flight")
	}
}

func okReply() model.IPCMessage {
	return model.IPCMessage{Type: model.MsgOK, Result: model.ResultAcknowledged, Acked: 1}
}

// TestCHK_TRAY_03_AckReplies — ok removes the alerts and, when nothing is
// left, closes the window and stops the sound; the suppression holds
// until the snapshot shows them acknowledged; not_found / no_active_alert
// are handled as already gone; other errors stay in the window.
func TestCHK_TRAY_03_AckReplies(t *testing.T) {
	a1 := pointAlert("a", "Склад", "k1", model.LevelError, 70)
	a2 := pointAlert("a", "Магазин", "k2", model.LevelWarning, 40)

	c := New(i18n.RU)
	c.Update(status(1, nil, a1, a2), true)
	reqs := c.BeginAck()
	v, refresh := c.AckDone([]AckResult{{Req: reqs[0], Msg: okReply()}, {Req: reqs[1], Msg: okReply()}})
	if v.Visible || v.Sound || len(v.Alerts) != 0 || !refresh || v.Pending {
		t.Fatalf("all acked: %+v refresh=%v", v, refresh)
	}
	// a stale push (before the ack reached the snapshot) must not reopen
	if v = c.Update(status(2, nil, a1, a2), true); v.Visible || v.Sound {
		t.Fatal("stale snapshot reopened acknowledged alerts")
	}
	// the service confirms; later an escalation of a2 is a new status
	ack1, ack2 := a1, a2
	ack1.Acknowledged, ack2.Acknowledged = true, true
	c.Update(status(3, nil, ack1, ack2), true)
	e2 := pointAlert("a", "Магазин", "k2", model.LevelError, 61)
	if v = c.Update(status(4, nil, ack1, e2), true); !v.Visible || len(v.Alerts) != 1 {
		t.Fatalf("escalation after ack: %+v", v)
	}

	// not_found / no_active_alert: gone, no crash, window closes when empty
	for _, code := range []string{model.ErrCodeNotFound, model.ErrCodeNoActiveAlert} {
		c := New(i18n.RU)
		c.Update(status(1, nil, a1), true)
		reqs := c.BeginAck()
		v, refresh := c.AckDone([]AckResult{{Req: reqs[0], Msg: model.IPCMessage{Type: model.MsgError, Code: code, ErrorKey: model.MsgMonitorNoActiveAlert}}})
		if v.Visible || v.Sound || !refresh {
			t.Errorf("%s: %+v", code, v)
		}
	}
	// partially gone: message, remaining alert stays
	c = New(i18n.RU)
	c.Update(status(1, nil, a1, a2), true)
	reqs = c.BeginAck()
	v, _ = c.AckDone([]AckResult{{Req: reqs[0], Msg: model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeNoActiveAlert}},
		{Req: reqs[1], Msg: model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeRateLimited, ErrorKey: model.MsgMonitorRateLimited, Error: "rate limited"}}})
	if !v.Visible || len(v.Alerts) != 1 || !strings.HasPrefix(v.Message, "Не удалось подтвердить: ") || !v.CanAck {
		t.Fatalf("partial: %+v", v)
	}
	if strings.Contains(v.Message, "monitor.") {
		t.Errorf("raw key in message %q", v.Message)
	}

	// transport errors
	c = New(i18n.EN)
	c.Update(status(1, nil, a1), true)
	reqs = c.BeginAck()
	v, refresh = c.AckDone([]AckResult{{Req: reqs[0], Err: trayipc.ErrNotConnected}})
	if !v.Visible || refresh || !strings.Contains(v.Message, "No connection") {
		t.Fatalf("not connected: %+v", v)
	}
	reqs = c.BeginAck()
	v, _ = c.AckDone([]AckResult{{Req: reqs[0], Err: errors.Join(trayipc.ErrUnsupported)}})
	if v.CanAck || !strings.Contains(v.Message, "cannot acknowledge") {
		t.Fatalf("unsupported: %+v", v)
	}
}

// TestCHK_TRAY_03_Texts — window title/heading by level, lines for points
// and for database errors (localized errorKey), RU/EN, overflow line.
func TestCHK_TRAY_03_Texts(t *testing.T) {
	c := New(i18n.RU)
	v := c.Update(status(1, nil, pointAlert("a", "Склад", "k1", model.LevelWarning, 35), dbAlert("b", model.LevelError)), true)
	if Title(v, i18n.RU) != "Exchange Checker — ошибка обмена" {
		t.Errorf("title %q", Title(v, i18n.RU))
	}
	txt := Text(v, i18n.RU)
	for _, want := range []string{"Обмен данными остановлен", "DB-a — Склад: предупреждение, нет обмена 35 мин",
		"DB-b: ошибка — " + model.MonitorMessage(model.MsgMonitorConnError, i18n.RU)} {
		if !strings.Contains(txt, want) {
			t.Errorf("text %q lacks %q", txt, want)
		}
	}
	never := pointAlert("a", "Новая", "k9", model.LevelWarning, 0)
	never.FreshAt = nil
	if l := Line(never, i18n.EN); l != "DB-a — Новая: warning, no exchange yet" {
		t.Errorf("never line %q", l)
	}
	w := New(i18n.EN)
	var many []model.AlertEvent
	for i := 0; i < MaxLines+3; i++ {
		many = append(many, pointAlert("a", "P"+string(rune('A'+i)), "k"+string(rune('A'+i)), model.LevelWarning, 31))
	}
	v = w.Update(status(1, nil, many...), true)
	if Title(v, i18n.EN) != "Exchange Checker — warning" || !strings.HasSuffix(Text(v, i18n.EN), "…and 3 more") {
		t.Errorf("overflow: %q", Text(v, i18n.EN))
	}
}

type fakeBeeper struct {
	mu    sync.Mutex
	calls []Tone
	err   error
}

func (f *fakeBeeper) Beep(freq, dur int) error {
	f.mu.Lock()
	f.calls = append(f.calls, Tone{FreqHz: freq, DurMs: dur})
	f.mu.Unlock()
	return f.err
}

func (f *fakeBeeper) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeBeeper) last() Tone {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return Tone{}
	}
	return f.calls[len(f.calls)-1]
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestCHK_TRAY_03_PlayerLoop — the beep repeats endlessly until stopped,
// switches tone, and a failing Beep (no speaker) never spins.
func TestCHK_TRAY_03_PlayerLoop(t *testing.T) {
	f := &fakeBeeper{}
	p := NewPlayer(f)
	defer p.Close()
	p.Set(true, Tone{800, 1, 0})
	waitFor(t, "repeated beeps", func() bool { return f.count() >= 3 })
	if f.last() != (Tone{800, 1, 0}) {
		t.Errorf("tone %+v", f.last())
	}
	p.Set(true, Tone{1500, 1, 0})
	waitFor(t, "new tone", func() bool { return f.last().FreqHz == 1500 })
	p.Set(false, Tone{})
	time.Sleep(100 * time.Millisecond)
	n := f.count()
	time.Sleep(200 * time.Millisecond)
	if f.count() != n || p.Playing() {
		t.Errorf("beeps after stop: %d -> %d", n, f.count())
	}

	bad := &fakeBeeper{err: errors.New("no speaker")}
	p2 := NewPlayer(bad)
	p2.Set(true, Tone{800, 1, 0})
	time.Sleep(300 * time.Millisecond)
	p2.Close()
	if c := bad.count(); c == 0 || c > 10 {
		t.Errorf("failing beeper called %d times in 300 ms (want a 50 ms rhythm)", c)
	}
	p2.Set(true, Tone{800, 1, 0}) // closed: ignored
	if p2.Playing() {
		t.Error("closed player restarted")
	}
}

// TestCHK_TRAY_03_PlayerMinGap — a gap of 1..49 ms is raised to 50 ms
// between Beep calls (gapMs = 10).
func TestCHK_TRAY_03_PlayerMinGap(t *testing.T) {
	f := &fakeBeeper{}
	p := NewPlayer(f)
	p.Set(true, Tone{800, 1, 10})
	time.Sleep(300 * time.Millisecond)
	p.Close()
	// 300 ms / (1 ms beep + 50 ms) ≈ 6; with the 10 ms gap it would be ~27
	if c := f.count(); c == 0 || c > 8 {
		t.Errorf("gapMs=10: %d beeps in 300 ms, want at most one per 50 ms", c)
	}
}

// TestCHK_TRAY_03_RaiseOnChange — Raise is set when an update brings a
// new alert or a worse level (also while the window is visible), not for
// an unchanged status or an improvement.
func TestCHK_TRAY_03_RaiseOnChange(t *testing.T) {
	c := New(i18n.RU)
	st := func(alerts ...model.AlertEvent) trayipc.Status {
		return trayipc.Status{State: trayipc.StateConnected, Snapshot: &model.StatusSnapshot{Alerts: alerts}}
	}
	w := model.AlertEvent{ProfileID: "a", Key: "k", To: model.LevelWarning}
	e := w
	e.To = model.LevelError
	other := model.AlertEvent{ProfileID: "b", Key: "k", To: model.LevelWarning}
	steps := []struct {
		alerts []model.AlertEvent
		raise  bool
	}{
		{[]model.AlertEvent{w}, true},
		{[]model.AlertEvent{w}, false},
		{[]model.AlertEvent{w, other}, true},
		{[]model.AlertEvent{e, other}, true},
		{[]model.AlertEvent{w, other}, false},
		{nil, false},
	}
	for i, s := range steps {
		if v := c.Update(st(s.alerts...), true); v.Raise != s.raise {
			t.Errorf("step %d: raise %v, want %v", i, v.Raise, s.raise)
		}
	}
	if c.View().Raise {
		t.Error("View() must not carry the one-shot Raise")
	}
}

// TestCHK_TRAY_03_BeepDisabled — settings.beep.enabled = false: the alert
// window is shown, no sound; unset (old file) = sound on.
func TestCHK_TRAY_03_BeepDisabled(t *testing.T) {
	a := model.AlertEvent{ProfileID: "a", Key: "k", To: model.LevelError}
	for _, tc := range []struct {
		enabled model.OptBool
		sound   bool
	}{{model.OptFalse, false}, {model.OptTrue, true}, {model.OptUnset, true}} {
		s := model.DefaultSettings()
		s.Beep.Enabled = tc.enabled
		c := New(i18n.RU)
		v := c.Update(trayipc.Status{State: trayipc.StateConnected,
			Snapshot: &model.StatusSnapshot{Alerts: []model.AlertEvent{a}, Settings: &s}}, true)
		if !v.Visible || v.Sound != tc.sound {
			t.Errorf("enabled %v: visible %v sound %v", tc.enabled, v.Visible, v.Sound)
		}
	}
}

// timedBeeper records when each Beep started and ended.
type timedBeeper struct {
	mu         sync.Mutex
	starts     []time.Time
	ends       []time.Time
	dur        time.Duration
	firstEnded chan struct{}
}

func (b *timedBeeper) Beep(freq, dur int) error {
	b.mu.Lock()
	b.starts = append(b.starts, time.Now())
	b.mu.Unlock()
	time.Sleep(b.dur)
	b.mu.Lock()
	b.ends = append(b.ends, time.Now())
	if len(b.ends) == 1 {
		close(b.firstEnded)
	}
	b.mu.Unlock()
	return nil
}

// TestCHK_TRAY_03_PlayerRetuneMinGap — a tone change (warning -> error)
// starts a new loop; its first Beep still waits minWait after the end of
// the old loop's last Beep.
func TestCHK_TRAY_03_PlayerRetuneMinGap(t *testing.T) {
	for i := 0; i < 5; i++ {
		b := &timedBeeper{dur: 20 * time.Millisecond, firstEnded: make(chan struct{})}
		p := NewPlayer(b)
		p.Set(true, Tone{800, 20, 700})
		<-b.firstEnded
		p.Set(true, Tone{1500, 20, 700}) // retune right after the Beep
		time.Sleep(150 * time.Millisecond)
		p.Close()
		b.mu.Lock()
		if len(b.starts) < 2 {
			t.Fatalf("retune did not beep: %d", len(b.starts))
		}
		if gap := b.starts[1].Sub(b.ends[0]); gap < minWait {
			t.Errorf("run %d: %v between Beeps after a retune, want >= %v", i, gap, minWait)
		}
		b.mu.Unlock()
	}
}
