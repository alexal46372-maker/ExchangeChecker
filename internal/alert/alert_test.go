package alert

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

var t0 = time.Date(2026, 9, 29, 12, 0, 0, 0, model.DBZone)

type step struct {
	lvl  model.AlertLevel
	from model.AlertLevel
	to   model.AlertLevel
	emit bool
}

func run(t *testing.T, tr *Tracker, key string, profile bool, steps []step) []model.AlertEvent {
	t.Helper()
	var evs []model.AlertEvent
	for i, s := range steps {
		ev, ok := tr.Observe(Observation{Key: key, Profile: profile, Level: s.lvl,
			Event: model.AlertEvent{ProfileID: "p", ProfileName: "P", PointName: "A"}}, t0.Add(time.Duration(i)*time.Minute).UTC())
		if ok != s.emit {
			t.Fatalf("step %d (%v): emitted %v, want %v", i, s.lvl, ok, s.emit)
		}
		if ok {
			if ev.From != s.from || ev.To != s.to {
				t.Fatalf("step %d: %v->%v, want %v->%v", i, ev.From, ev.To, s.from, s.to)
			}
			if ev.At.Location() != model.DBZone {
				t.Fatalf("event time not UTC+3: %v", ev.At)
			}
			evs = append(evs, ev)
		}
	}
	return evs
}

const (
	ok   = model.LevelOK
	warn = model.LevelWarning
	er   = model.LevelError
)

// TestCHK_MON_05_Transitions — ok->warning->error and back, one event per
// confirmed transition, none while the level is steady.
func TestCHK_MON_05_Transitions(t *testing.T) {
	tr := NewTracker(DefaultPolicy())
	evs := run(t, tr, PointKey("p", "A"), false, []step{
		{lvl: ok}, {lvl: ok},
		{lvl: warn, from: ok, to: warn, emit: true}, {lvl: warn}, {lvl: warn},
		{lvl: er, from: warn, to: er, emit: true}, {lvl: er}, {lvl: er},
		{lvl: ok}, {lvl: ok, from: er, to: ok, emit: true}, // recovery needs 2 polls
		{lvl: ok}, {lvl: ok},
	})
	if len(evs) != 3 || evs[0].ID >= evs[1].ID || !evs[1].Raised() || evs[2].Raised() {
		t.Errorf("events %+v", evs)
	}
	if tr.Level(PointKey("p", "A")) != ok || len(tr.Active()) != 0 {
		t.Error("final level")
	}
	// error -> warning -> ok (de-escalation in two confirmed steps)
	run(t, tr, PointKey("p", "B"), false, []step{
		{lvl: er, from: ok, to: er, emit: true},
		{lvl: warn}, {lvl: warn, from: er, to: warn, emit: true},
		{lvl: ok}, {lvl: ok, from: warn, to: ok, emit: true},
	})
	// direct ok -> error
	run(t, tr, PointKey("p", "C"), false, []step{{lvl: er, from: ok, to: er, emit: true}})
}

// TestCHK_MON_05_DebounceAntiFlap — flapping around a threshold and a
// single transient connection error produce no event spam.
func TestCHK_MON_05_DebounceAntiFlap(t *testing.T) {
	tr := NewTracker(DefaultPolicy())
	// warn/ok flapping every poll: only the first escalation is reported
	evs := run(t, tr, PointKey("p", "F"), false, []step{
		{lvl: warn, from: ok, to: warn, emit: true},
		{lvl: ok}, {lvl: warn}, {lvl: ok}, {lvl: warn}, {lvl: ok}, {lvl: warn},
	})
	if len(evs) != 1 {
		t.Errorf("flapping produced %d events", len(evs))
	}
	// profile-level connection error: one failed poll is ignored
	run(t, tr, ProfileKey("p"), true, []step{
		{lvl: er}, {lvl: ok}, {lvl: er}, {lvl: ok},
		{lvl: er}, {lvl: er, from: ok, to: er, emit: true}, {lvl: er}, {lvl: er},
		{lvl: ok}, {lvl: er}, {lvl: ok}, {lvl: ok, from: er, to: ok, emit: true},
	})
	// the pending counter restarts when the direction changes
	tr2 := NewTracker(Policy{RaisePolls: 3, ClearPolls: 3, ProfileRaisePolls: 3})
	run(t, tr2, "k", false, []step{{lvl: warn}, {lvl: warn}, {lvl: ok}, {lvl: warn}, {lvl: er}, {lvl: warn, from: ok, to: warn, emit: true}})
	if (Policy{}).norm() != (Policy{RaisePolls: 1, ClearPolls: 1, ProfileRaisePolls: 1}) {
		t.Error("zero policy must mean 1 poll")
	}
}

// TestCHK_MON_05_ActiveAndRetain — active alerts follow the confirmed
// level and are refreshed; Retain forgets removed keys silently.
func TestCHK_MON_05_ActiveAndRetain(t *testing.T) {
	tr := NewTracker(DefaultPolicy())
	f := t0.Add(-40 * time.Minute)
	tr.Observe(Observation{Key: PointKey("p1", "A"), Level: warn, Event: model.AlertEvent{ProfileID: "p1", PointName: "A", FreshAt: &f, LagMinutes: 40}}, t0)
	tr.Observe(Observation{Key: PointKey("p2", "B"), Level: er, Event: model.AlertEvent{ProfileID: "p2", PointName: "B"}}, t0)
	tr.Observe(Observation{Key: PointKey("p1", "A"), Level: warn, Event: model.AlertEvent{ProfileID: "p1", PointName: "A", FreshAt: &f, LagMinutes: 45}}, t0)
	act := tr.Active()
	if len(act) != 2 || act[0].PointName != "A" || act[0].LagMinutes != 45 {
		t.Fatalf("active %+v", act)
	}
	tr.Retain(func(k string) bool { return KeyProfile(k) == "p1" })
	if tr.Len() != 1 || len(tr.Active()) != 1 {
		t.Errorf("retain: %d", tr.Len())
	}
	if KeyProfile(ProfileKey("x")) != "x" || KeyProfile("plain") != "plain" {
		t.Error("KeyProfile")
	}
}

// TestCHK_MON_05_FormatRUEN — texts contain names, levels, UTC+3 time.
func TestCHK_MON_05_FormatRUEN(t *testing.T) {
	f := time.Date(2026, 9, 29, 8, 30, 0, 0, time.UTC) // 11:30 UTC+3
	e := model.AlertEvent{ProfileName: "Склад", PointName: "Магазин 1", PlanTable: "_Node123", From: ok, To: warn, FreshAt: &f, LagMinutes: 35, Baseline: true}
	ru, en := Format(e, model.LangRU), Format(e, model.LangEN)
	for _, want := range []string{"База «Склад»", "_Node123/Магазин 1", "норма -> предупреждение", "2026-09-29 11:30:00 UTC+3", "35 мин", model.MonitorMessage(model.MsgMonitorBaseline, model.LangRU)} {
		if !strings.Contains(ru, want) {
			t.Errorf("RU lacks %q: %s", want, ru)
		}
	}
	for _, want := range []string{`Database "Склад"`, "ok -> warning", "11:30:00 UTC+3", "35 min ago"} {
		if !strings.Contains(en, want) {
			t.Errorf("EN lacks %q: %s", want, en)
		}
	}
	pe := model.AlertEvent{ProfileName: "DB", From: ok, To: er, Error: "login failed"}
	if s := Format(pe, model.LangEN); !strings.Contains(s, "error: login failed") || strings.Contains(s, "point") {
		t.Errorf("profile event: %s", s)
	}
}

type fakeReporter struct {
	got []string
	err error
}

func (f *fakeReporter) Report(typ uint16, id uint32, msg string) error {
	f.got = append(f.got, fmt.Sprintf("%d/%d/%s", typ, id, msg))
	return f.err
}

type memLog struct{ lines []string }

func (m *memLog) Printf(format string, args ...interface{}) {
	m.lines = append(m.lines, fmt.Sprintf(format, args...))
}

// TestCHK_MON_05_EventLogSink — entry types by transition; failures go to
// the log file; Multi fans out.
func TestCHK_MON_05_EventLogSink(t *testing.T) {
	r := &fakeReporter{}
	lg := &memLog{}
	s := &EventLogSink{R: r, Lang: model.LangEN, Fallback: lg}
	var got []model.AlertEvent
	m := Multi{s, nil, LogSink{Log: lg, Lang: model.LangEN}, SinkFunc(func(e model.AlertEvent) { got = append(got, e) })}
	m.Alert(model.AlertEvent{ID: 1, ProfileName: "P", From: ok, To: warn})
	m.Alert(model.AlertEvent{ID: 2, ProfileName: "P", From: warn, To: er})
	m.Alert(model.AlertEvent{ID: 3, ProfileName: "P", From: er, To: ok})
	want := []string{"2/102/", "1/103/", "4/101/"}
	if len(r.got) != 3 || len(got) != 3 {
		t.Fatalf("reported %v", r.got)
	}
	for i, w := range want {
		if !strings.HasPrefix(r.got[i], w) {
			t.Errorf("entry %d = %s, want prefix %s", i, r.got[i], w)
		}
	}
	if len(lg.lines) != 3 || !strings.HasPrefix(lg.lines[0], "ALERT #1 ") {
		t.Errorf("log sink %v", lg.lines)
	}
	r.err = errors.New("access denied")
	s.Alert(model.AlertEvent{ID: 4, From: ok, To: er})
	if !strings.Contains(lg.lines[len(lg.lines)-1], "access denied") {
		t.Errorf("fallback %v", lg.lines)
	}
	var nilSink *EventLogSink
	nilSink.Alert(model.AlertEvent{}) // must not panic
}

// TestCHK_MON_05_LogFileRotation — size rotation keeps Keep files, lines
// carry UTC+3 timestamps, appends across reopen.
func TestCHK_MON_05_LogFileRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "exchsvc.log")
	lf, err := OpenRotatingFile(path, 200, 3)
	if err != nil {
		t.Fatal(err)
	}
	lf.Now = func() time.Time { return time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC) }
	for i := 0; i < 40; i++ {
		lf.Printf("line %02d %s", i, strings.Repeat("x", 30))
	}
	if err := lf.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"exchsvc.log", "exchsvc.log.1", "exchsvc.log.2", "exchsvc.log.3"} {
		fi, err := os.Stat(filepath.Join(dir, "logs", name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fi.Size() > 200 {
			t.Errorf("%s: %d bytes > max", name, fi.Size())
		}
	}
	if _, err := os.Stat(path + ".4"); !os.IsNotExist(err) {
		t.Errorf("keep=3 exceeded: %v", err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "2026-09-29 12:00:00.000 line 39") {
		t.Errorf("last line / UTC+3 stamp: %q", b)
	}
	old, _ := os.ReadFile(path + ".1")
	if !strings.Contains(string(old), "line 3") {
		t.Errorf("rotated content: %q", old)
	}
	// reopen appends
	lf, err = OpenRotatingFile(path, 1<<20, 0)
	if err != nil {
		t.Fatal(err)
	}
	lf.Printf("after reopen")
	lf.Close()
	b, _ = os.ReadFile(path)
	if !strings.Contains(string(b), "line 39") || !strings.Contains(string(b), "after reopen\n") {
		t.Errorf("append: %q", b)
	}
	if lf.Keep != DefaultLogKeep {
		t.Error("default keep")
	}
}
