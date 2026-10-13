package monitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func p9IPCErr(t *testing.T, err error) *model.IPCError {
	t.Helper()
	var ie *model.IPCError
	if !errors.As(err, &ie) {
		t.Fatalf("not an IPCError: %v", err)
	}
	return ie
}

// TestCHK_SVC_04_CheckNow — limits 30 s (all) / 10 s (profile, also after
// all), coalescing with a queued poll, rate_limited + retryAfterMs,
// checkSeq, unknown / disabled profile; poll_now unaffected.
func TestCHK_SVC_04_CheckNow(t *testing.T) {
	clk := &fakeClock{t: t0}
	off := prof("db2")
	off.Enabled = false
	m := New(baseOpt(newFactory(), NewMemConfig(Config{Settings: model.DefaultSettings(),
		Profiles: []model.Profile{prof("db1"), off}}), clk))
	m.LoadConfig()
	r, err := m.CheckNow("db1")
	if err != nil || r.Coalesced || r.CheckSeq != 1 {
		t.Fatalf("first: %+v %v", r, err)
	}
	if r, err := m.CheckNow("db1"); err != nil || !r.Coalesced {
		t.Fatalf("queued poll must coalesce: %+v %v", r, err)
	}
	m.takePending()
	_, err = m.CheckNow("db1")
	if ie := p9IPCErr(t, err); ie.Code != model.ErrCodeRateLimited || ie.Key != model.MsgCheckRateLimited || ie.RetryAfterMs != 10000 {
		t.Fatalf("profile limit: %+v", ie)
	}
	clk.Add(4 * time.Second)
	_, err = m.CheckNow("db1")
	if ie := p9IPCErr(t, err); ie.RetryAfterMs != 6000 {
		t.Fatalf("retry after: %+v", ie)
	}
	clk.Add(6 * time.Second)
	if _, err := m.CheckNow("db1"); err != nil {
		t.Fatalf("after 10 s: %v", err)
	}
	m.takePending()
	if _, err := m.CheckNow(""); err != nil {
		t.Fatalf("all: %v", err)
	}
	m.takePending()
	clk.Add(5 * time.Second)
	_, err = m.CheckNow("")
	if ie := p9IPCErr(t, err); ie.RetryAfterMs != 25000 {
		t.Fatalf("all limit: %+v", ie)
	}
	_, err = m.CheckNow("db1")
	if ie := p9IPCErr(t, err); ie.Code != model.ErrCodeRateLimited || ie.RetryAfterMs != 5000 {
		t.Fatalf("profile after all: %+v", ie)
	}
	clk.Add(5 * time.Second)
	if _, err := m.CheckNow("db1"); err != nil {
		t.Fatalf("profile 10 s after all: %v", err)
	}
	m.takePending()
	if _, err := m.CheckNow("nope"); p9IPCErr(t, err).Code != model.ErrCodeNotFound {
		t.Errorf("unknown: %v", err)
	}
	if _, err := m.CheckNow("db2"); p9IPCErr(t, err).Key != model.MsgMonitorProfileOff {
		t.Errorf("disabled: %v", err)
	}
	if c, err := m.PollNow("db1"); err != nil || c {
		t.Errorf("poll_now must not be limited by check_now: %v %v", c, err)
	}
}

// TestCHK_SVC_05_EventsSince — run id, ring buffer, reset on another run,
// future id or a gap, limit.
func TestCHK_SVC_05_EventsSince(t *testing.T) {
	m := New(Options{RunID: "run1"})
	if m.Heartbeat().RunID != "run1" {
		t.Fatal("heartbeat runId")
	}
	if id := New(Options{}).RunID(); len(id) != 16 {
		t.Fatalf("random run id %q", id)
	}
	m.ring.size = 3
	m.ring.add(model.AlertEvent{ID: 1})
	m.ring.add(model.AlertEvent{ID: 2})
	if p := m.EventsSince("run1", 0, 0); p.ResetRequired || len(p.Events) != 2 || p.LastID != 2 || p.RunID != "run1" {
		t.Fatalf("all: %+v", p)
	}
	for _, c := range []struct {
		run   string
		since uint64
	}{{"", 0}, {"other", 1}, {"run1", 5}} {
		if p := m.EventsSince(c.run, c.since, 0); !p.ResetRequired || len(p.Events) != 0 || p.LastID != 2 {
			t.Errorf("%+v: %+v", c, p)
		}
	}
	if p := m.EventsSince("run1", 2, 0); p.ResetRequired || len(p.Events) != 0 || p.LastID != 2 {
		t.Errorf("up to date: %+v", p)
	}
	for i := uint64(3); i <= 5; i++ {
		m.ring.add(model.AlertEvent{ID: i})
	}
	if p := m.EventsSince("run1", 1, 0); !p.ResetRequired {
		t.Errorf("gap must reset: %+v", p)
	}
	if p := m.EventsSince("run1", 2, 0); p.ResetRequired || len(p.Events) != 3 || p.Events[0].ID != 3 {
		t.Errorf("after eviction: %+v", p)
	}
	if p := m.EventsSince("run1", 2, 2); !p.Truncated || len(p.Events) != 2 || p.LastID != 4 {
		t.Errorf("limit: %+v", p)
	}
	if New(Options{}).ring.size != model.EventRingSize {
		t.Error("ring size")
	}
}

// TestCHK_SVC_05_PollEvents — events of a real poll carry the run id and
// enter the ring; the snapshot carries latencyMs (CHK-SVC-03).
func TestCHK_SVC_05_PollEvents(t *testing.T) {
	clk := &fakeClock{t: t0}
	opt := baseOpt(newFactory(), NewMemConfig(Config{Settings: model.DefaultSettings(),
		Profiles: []model.Profile{prof("db1")}}), clk)
	opt.RunID = "r9"
	m := New(opt)
	m.LoadConfig()
	m.pollAll(context.Background())
	ps := profileByID(m.Snapshot(), "db1")
	if ps.LatencyMs == nil || *ps.LatencyMs < 0 {
		t.Errorf("latencyMs %v", ps.LatencyMs)
	}
	p := m.EventsSince("r9", 0, 0)
	for _, e := range p.Events {
		if e.RunID != "r9" {
			t.Errorf("event without run id: %+v", e)
		}
	}
	sn := m.Snapshot()
	if sn.ServerNow.IsZero() || ps.LastQueryMs == nil {
		t.Errorf("serverNow / lastQueryMs: %v %v", sn.ServerNow, ps.LastQueryMs)
	}
	clk.Add(time.Hour)
	res, err := m.CheckNow("")
	if err != nil {
		t.Fatal(err)
	}
	m.pollSome(context.Background(), nil)
	if sn := m.Snapshot(); sn.LastCheckSeq < res.CheckSeq || sn.LastCheckSeq == 0 {
		t.Errorf("lastCheckSeq %d < checkSeq %d", sn.LastCheckSeq, res.CheckSeq)
	}
}

// TestCHK_SVC_05_EventKinds — eventId, kind alert / recovered and
// durationMin since the point left ok.
func TestCHK_SVC_05_EventKinds(t *testing.T) {
	m := New(Options{RunID: "r"})
	k := func(id uint64, from, to model.AlertLevel, min int) model.AlertEvent {
		e := model.AlertEvent{ID: id, ProfileID: "db1", Key: "k1", From: from, To: to, At: t0.Add(time.Duration(min) * time.Minute)}
		m.mu.Lock()
		m.classifyEventLocked(&e)
		m.mu.Unlock()
		return e
	}
	if e := k(1, model.LevelOK, model.LevelWarning, 0); e.Kind != model.AlertKindAlert || e.EventID != 1 || e.RunID != "r" || e.DurationMin != nil {
		t.Errorf("raise %+v", e)
	}
	if e := k(2, model.LevelWarning, model.LevelError, 5); e.Kind != model.AlertKindAlert {
		t.Errorf("escalate %+v", e)
	}
	if e := k(3, model.LevelError, model.LevelOK, 17); e.Kind != model.AlertKindRecovered || e.DurationMin == nil || *e.DurationMin != 17 {
		t.Errorf("recover %+v", e)
	}
	if p := m.EventsSince("r", 0, 0); len(p.Events) != 3 || p.Events[2].Kind != model.AlertKindRecovered {
		t.Errorf("ring %+v", p)
	}
}

// TestCHK_CFG_05_SaveProfileTarget — save_profile refuses a changed
// server / port without a new password and a forbidden server name.
func TestCHK_CFG_05_SaveProfileTarget(t *testing.T) {
	e := newCfgEnv(t)
	p := sqlProf6("db1")
	if _, err := e.m.SaveProfile(p, "pw1", false, e.rev(), "acct"); err != nil {
		t.Fatal(err)
	}
	for _, mod := range []func(*model.Profile){
		func(q *model.Profile) { q.Port = 1500 },
		func(q *model.Profile) { q.Server = `other\INST` },
		func(q *model.Profile) { q.Server = `srv\OTHER` },
	} {
		q := p
		mod(&q)
		_, err := e.m.SaveProfile(q, "", false, e.rev(), "acct")
		if ie := p9IPCErr(t, err); ie.Code != model.ErrCodeInvalidRequest || ie.Key != model.MsgPasswordTargetChange {
			t.Errorf("%s:%d: %+v", q.Server, q.Port, ie)
		}
	}
	if e.secrets()["db1"] != "pw1" {
		t.Fatal("secret changed")
	}
	q := p
	q.Name = "renamed"
	if _, err := e.m.SaveProfile(q, "", false, e.rev(), "acct"); err != nil {
		t.Errorf("same target reuses the password: %v", err)
	}
	q.Port = 1500
	if _, err := e.m.SaveProfile(q, "pw2", false, e.rev(), "acct"); err != nil || e.secrets()["db1"] != "pw2" {
		t.Errorf("new target with a new password: %v", err)
	}
	q.Server = `np:\\srv\pipe\sql\query`
	_, err := e.m.SaveProfile(q, "pw3", false, e.rev(), "acct")
	if ie := p9IPCErr(t, err); ie.Key != model.MsgMonitorProfileInvalid || len(ie.Args) < 2 || ie.Args[0] != "server" || ie.Args[1] != model.ValSQLServerForbidden {
		t.Errorf("np: %+v", ie)
	}
}

// TestCHK_SVC_04_LastCheckSeq — the completion rule lastCheckSeq >=
// checkSeq: not reached by an unrelated status (publish without a poll)
// nor by a poll of another profile, reached once the requested poll is
// published; a check registered later stays pending; a full cycle
// completes every check.
func TestCHK_SVC_04_LastCheckSeq(t *testing.T) {
	clk := &fakeClock{t: t0}
	m := New(baseOpt(newFactory(), NewMemConfig(Config{Settings: model.DefaultSettings(),
		Profiles: []model.Profile{prof("db1"), prof("db2")}}), clk))
	m.LoadConfig()
	ctx := context.Background()
	m.pollAll(ctx)
	r1, err := m.CheckNow("db1")
	if err != nil || r1.CheckSeq == 0 {
		t.Fatal(r1, err)
	}
	last := func() uint64 { return m.Snapshot().LastCheckSeq }
	m.publish(nil) // a status for another reason (ack, config)
	if last() >= r1.CheckSeq {
		t.Fatal("completed by an unrelated status")
	}
	m.pollSome(ctx, map[string]bool{"db2": true})
	if last() >= r1.CheckSeq {
		t.Fatal("completed by a poll of another profile")
	}
	clk.Add(11 * time.Second)
	r2, err := m.CheckNow("db2")
	if err != nil || r2.CheckSeq <= r1.CheckSeq {
		t.Fatal(r2, err)
	}
	m.takePending()
	m.pollSome(ctx, map[string]bool{"db1": true})
	if l := last(); l < r1.CheckSeq || l >= r2.CheckSeq {
		t.Fatalf("after db1 poll: last %d, want >= %d and < %d", l, r1.CheckSeq, r2.CheckSeq)
	}
	clk.Add(31 * time.Second)
	r3, err := m.CheckNow("")
	if err != nil {
		t.Fatal(err)
	}
	m.takePending()
	m.pollAll(ctx)
	if l := last(); l < r3.CheckSeq || l < r2.CheckSeq {
		t.Fatalf("after a full cycle: last %d < %d", l, r3.CheckSeq)
	}
}

// TestCHK_SVC_04_DeletedProfileCheck — a pending check of a profile that is
// deleted is dropped and lastCheckSeq is settled at once (no wait for a
// full cycle).
func TestCHK_SVC_04_DeletedProfileCheck(t *testing.T) {
	clk := &fakeClock{t: t0}
	cfg := NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("db1"), prof("db2")}})
	m := New(baseOpt(newFactory(), cfg, clk))
	m.LoadConfig()
	r, err := m.CheckNow("db2")
	if err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().LastCheckSeq >= r.CheckSeq {
		t.Fatal("completed before any poll")
	}
	cfg.Set(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("db1")}})
	m.LoadConfig()
	m.mu.Lock()
	last, pend := m.lastCheckSeq, len(m.checkPend)
	m.mu.Unlock()
	if last < r.CheckSeq || pend != 0 {
		t.Fatalf("after deletion: lastCheckSeq %d < %d, pending %d", last, r.CheckSeq, pend)
	}
}
