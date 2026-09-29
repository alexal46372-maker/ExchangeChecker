package monitor

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// ackEnv is a monitor with profile "a" (one point A whose lag is set by
// the test) and profile "b" (always OK).
type ackEnv struct {
	m    *Monitor
	f    *fakeFactory
	clk  *fakeClock
	src  *MemConfig
	mu   sync.Mutex
	lag  int
	acks []model.AlertEvent
	evs  []model.AlertEvent
	warn *warnRec
}

func newAckEnv(t *testing.T, store AckStore, policy alert.Policy) *ackEnv {
	t.Helper()
	e := &ackEnv{f: newFactory(), clk: &fakeClock{t: t0}, warn: &warnRec{}}
	e.f.set("a", behavior{points: func(now time.Time) []model.ExchangePoint {
		e.mu.Lock()
		l := e.lag
		e.mu.Unlock()
		ts := now.Add(-time.Duration(l) * time.Minute)
		return []model.ExchangePoint{{Name: "A", LastSend: &ts, PlanTable: "_Node1"}}
	}})
	e.f.set("b", behavior{points: lagPoints(map[string]int{"B": 1})})
	e.src = NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a"), prof("b")}})
	opt := baseOpt(e.f, e.src, e.clk)
	opt.Policy = policy
	opt.Acks = store
	opt.Warn = e.warn.warn
	opt.OnAck = func(ev model.AlertEvent) { e.mu.Lock(); e.acks = append(e.acks, ev); e.mu.Unlock() }
	opt.Sink = alert.SinkFunc(func(ev model.AlertEvent) { e.mu.Lock(); e.evs = append(e.evs, ev); e.mu.Unlock() })
	e.m = New(opt)
	e.m.reloadConfig(true)
	return e
}

func (e *ackEnv) poll(lag int) model.StatusSnapshot {
	e.mu.Lock()
	e.lag = lag
	e.mu.Unlock()
	e.clk.Add(time.Minute)
	e.m.pollAll(context.Background())
	return e.m.Snapshot()
}

func (e *ackEnv) nAcks() int { e.mu.Lock(); defer e.mu.Unlock(); return len(e.acks) }
func (e *ackEnv) lastEvent() model.AlertEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.evs) == 0 {
		return model.AlertEvent{}
	}
	return e.evs[len(e.evs)-1]
}

func pointA(s model.StatusSnapshot) model.PointStatus {
	for _, p := range profileByID(s, "a").Points {
		if p.Name == "A" {
			return p
		}
	}
	return model.PointStatus{}
}

func codeOf(err error) string {
	var ie *model.IPCError
	if errors.As(err, &ie) {
		return ie.Code
	}
	return ""
}

var fast = alert.Policy{RaisePolls: 1, ClearPolls: 1, ProfileRaisePolls: 1}

// TestCHK_IPC_02_AckAlert — ack_alert acknowledges the active alert of a
// profile (all or one point): the level stays warning, the point, the
// active alert and the snapshot carry Acknowledged; unknown / deleted
// profile -> not_found, unknown point -> not_found, nothing active ->
// no_active_alert; the Event Log gets only the informational OnAck entry
// (no warning); re-acknowledging writes nothing new.
func TestCHK_IPC_02_AckAlert(t *testing.T) {
	store := &MemAckStore{}
	e := newAckEnv(t, store, fast)
	s := e.poll(40) // warning
	if pa := pointA(s); pa.Level != model.LevelWarning || pa.Key != "_Node1/A" || pa.Acknowledged {
		t.Fatalf("point before ack %+v", pa)
	}
	if len(s.Alerts) != 1 || s.Alerts[0].Key != "_Node1/A" {
		t.Fatalf("alerts %+v", s.Alerts)
	}
	if _, err := e.m.AckAlert("zzz", ""); !errors.Is(err, ErrUnknownProfile) || codeOf(err) != model.ErrCodeNotFound {
		t.Errorf("unknown profile: %v", err)
	}
	if _, err := e.m.AckAlert("b", ""); codeOf(err) != model.ErrCodeNoActiveAlert {
		t.Errorf("profile without alert: %v", err)
	}
	if _, err := e.m.AckAlert("a", "_Node1/Nope"); !errors.Is(err, ErrUnknownPoint) || codeOf(err) != model.ErrCodeNotFound {
		t.Errorf("unknown point: %v", err)
	}
	n, err := e.m.AckAlert("a", "_Node1/A")
	if err != nil || n != 1 {
		t.Fatalf("ack: %d %v", n, err)
	}
	s = e.m.Snapshot()
	pa := pointA(s)
	if !pa.Acknowledged || pa.Level != model.LevelWarning || profileByID(s, "a").Level != model.LevelWarning {
		t.Errorf("point after ack %+v", pa)
	}
	if profileByID(s, "a").Acknowledged {
		t.Error("profile-level flag set by a point ack")
	}
	if len(s.Alerts) != 1 || !s.Alerts[0].Acknowledged || s.Alerts[0].AckedAt == nil || s.Alerts[0].To != model.LevelWarning {
		t.Errorf("active alert after ack %+v", s.Alerts)
	}
	if e.nAcks() != 1 || e.warn.n() != 0 {
		t.Errorf("OnAck %d, warnings %d", e.nAcks(), e.warn.n())
	}
	if recs, _ := store.Load(); len(recs) != 1 || recs[0].ProfileID != "a" || recs[0].PointKey != "_Node1/A" || recs[0].Level != model.LevelWarning {
		t.Errorf("stored %+v", recs)
	}
	// still warning on the next polls: stays acknowledged, no event
	s = e.poll(45)
	if !pointA(s).Acknowledged || !s.Alerts[0].Acknowledged {
		t.Error("ack lost while the level is unchanged")
	}
	if n, err := e.m.AckAlert("a", ""); err != nil || n != 1 || e.nAcks() != 1 {
		t.Errorf("re-ack %d %v, OnAck %d", n, err, e.nAcks())
	}
	// the other profile's snapshot is untouched
	if profileByID(s, "b").Acknowledged || profileByID(s, "b").Points[0].Acknowledged {
		t.Error("ack leaked to profile b")
	}
	// profile deleted: acks dropped, ack -> not_found
	e.src.Set(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("b")}})
	e.m.reloadConfig(false)
	if _, err := e.m.AckAlert("a", ""); codeOf(err) != model.ErrCodeNotFound {
		t.Errorf("deleted profile: %v", err)
	}
	if recs, _ := store.Load(); len(recs) != 0 {
		t.Errorf("acks of a deleted profile kept: %+v", recs)
	}
}

// TestCHK_IPC_02_AckResetRules — an acknowledgement is dropped when the
// level worsens (warning -> error) or returns to OK, and kept (lowered)
// when error improves to warning; a profile-level (connection) alert is
// acknowledged the same way.
func TestCHK_IPC_02_AckResetRules(t *testing.T) {
	store := &MemAckStore{}
	e := newAckEnv(t, store, fast)
	e.poll(40)
	if _, err := e.m.AckAlert("a", ""); err != nil {
		t.Fatal(err)
	}
	// worsens: warning -> error resets the ack, the event is not acked
	s := e.poll(70)
	if pa := pointA(s); pa.Level != model.LevelError || pa.Acknowledged {
		t.Errorf("after worsening %+v", pa)
	}
	if ev := e.lastEvent(); ev.To != model.LevelError || ev.Acknowledged {
		t.Errorf("worsening event %+v", ev)
	}
	if recs, _ := store.Load(); len(recs) != 0 {
		t.Errorf("stored after worsening %+v", recs)
	}
	// ack the error, it improves to warning: kept, lowered to warning
	if _, err := e.m.AckAlert("a", "_Node1/A"); err != nil {
		t.Fatal(err)
	}
	s = e.poll(40)
	if pa := pointA(s); pa.Level != model.LevelWarning || !pa.Acknowledged {
		t.Errorf("error -> warning must stay acknowledged: %+v", pa)
	}
	if recs, _ := store.Load(); len(recs) != 1 || recs[0].Level != model.LevelWarning {
		t.Errorf("lowered ack %+v", recs)
	}
	// warning -> error again (worse than the lowered ack): reset
	s = e.poll(70)
	if pointA(s).Acknowledged {
		t.Error("lowered ack must reset when it worsens again")
	}
	// ack, back to OK: reset
	e.m.AckAlert("a", "")
	s = e.poll(1)
	if pa := pointA(s); pa.Level != model.LevelOK || pa.Acknowledged || len(s.Alerts) != 0 {
		t.Errorf("after recovery %+v alerts %+v", pa, s.Alerts)
	}
	if recs, _ := store.Load(); len(recs) != 0 {
		t.Errorf("stored after recovery %+v", recs)
	}
	if _, err := e.m.AckAlert("a", ""); codeOf(err) != model.ErrCodeNoActiveAlert {
		t.Errorf("nothing to ack after recovery: %v", err)
	}
	// profile-level alert
	e.f.set("a", behavior{err: &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "ping", Err: errors.New("down")}})
	e.poll(1)
	if n, err := e.m.AckAlert("a", ""); err != nil || n != 1 {
		t.Fatalf("profile ack %d %v", n, err)
	}
	s = e.m.Snapshot()
	if ps := profileByID(s, "a"); !ps.Acknowledged || ps.Level != model.LevelError {
		t.Errorf("profile-level ack %+v", ps)
	}
	if _, err := e.m.AckAlert("a", "_Node1/A"); codeOf(err) != model.ErrCodeNoActiveAlert {
		t.Errorf("point ack while only the profile alert is active: %v", err)
	}
	e.f.set("a", behavior{points: lagPoints(map[string]int{"A": 1})})
	s = e.poll(1)
	if profileByID(s, "a").Acknowledged {
		t.Error("profile ack kept after recovery")
	}
}

// TestCHK_IPC_02_AckSurvivesRestart — acknowledgements are stored in
// ack-state.json (atomic write) and restored by a new Monitor ("service
// restart"): the same alarm coming back after the restart is still
// acknowledged (also while a profile error is still being debounced),
// a recovery after the restart drops it; a corrupt file is set aside.
func TestCHK_IPC_02_AckSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	pol := alert.Policy{RaisePolls: 1, ClearPolls: 2, ProfileRaisePolls: 2}
	e := newAckEnv(t, OpenFileAckStore(dir, nil), pol)
	e.poll(70) // error
	if _, err := e.m.AckAlert("a", ""); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(OpenFileAckStore(dir, nil).Path())
	if err != nil || !strings.Contains(string(b), `"_Node1/A"`) || !strings.Contains(string(b), `"error"`) {
		t.Fatalf("ack-state.json: %v %s", err, b)
	}
	// restart
	e2 := newAckEnv(t, OpenFileAckStore(dir, nil), pol)
	if len(e2.m.acks) != 1 {
		t.Fatalf("restored acks %+v", e2.m.acks)
	}
	s := e2.poll(70) // ok -> error raised again after the restart
	if pa := pointA(s); !pa.Acknowledged || pa.Level != model.LevelError {
		t.Errorf("after restart %+v", pa)
	}
	if ev := e2.lastEvent(); ev.To != model.LevelError || !ev.Acknowledged {
		t.Errorf("re-raised event after restart must be acknowledged: %+v", ev)
	}
	// profile-level ack across a restart while the error is debounced
	e2.f.set("a", behavior{err: &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "ping", Err: errors.New("down")}})
	e2.poll(70)
	e2.poll(70) // confirmed after 2 polls
	if _, err := e2.m.AckAlert("a", ""); err != nil {
		t.Fatal(err)
	}
	e3 := newAckEnv(t, OpenFileAckStore(dir, nil), pol)
	e3.f.set("a", behavior{err: &sqlcheck.Error{Kind: sqlcheck.KindConn, Op: "ping", Err: errors.New("down")}})
	e3.poll(70) // 1st failure: not confirmed yet, ack kept
	if len(e3.m.acks) != 2 {
		t.Errorf("acks lost while debouncing: %+v", e3.m.acks)
	}
	s = e3.poll(70) // confirmed error, same level
	if ps := profileByID(s, "a"); !ps.Acknowledged {
		t.Errorf("profile ack after restart %+v", ps)
	}
	// recovery after a restart drops both
	e4 := newAckEnv(t, OpenFileAckStore(dir, nil), pol)
	e4.poll(1)
	if len(e4.m.acks) != 0 {
		t.Errorf("acks after recovery %+v", e4.m.acks)
	}
	if recs, _ := OpenFileAckStore(dir, nil).Load(); len(recs) != 0 {
		t.Errorf("file after recovery %+v", recs)
	}
	// corrupt file: set aside, nothing acknowledged
	st := OpenFileAckStore(dir, nil)
	os.WriteFile(st.Path(), []byte("{garbage"), 0o644)
	lg := &memLog{}
	recs, err := OpenFileAckStore(dir, lg).Load()
	if err == nil || len(recs) != 0 || !lg.has("corrupt") {
		t.Errorf("corrupt: %v %+v", err, recs)
	}
	if _, err := os.Stat(st.Path() + ".corrupt"); err != nil {
		t.Errorf("corrupt file not kept: %v", err)
	}
}
