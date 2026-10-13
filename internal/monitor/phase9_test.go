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
	seq := m.Snapshot().Seq
	r, err := m.CheckNow("db1")
	if err != nil || r.Coalesced || r.CheckSeq != seq {
		t.Fatalf("first: %+v %v (seq %d)", r, err, seq)
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
	if p := m.EventsSince("run1", 0, 0); p.Reset || len(p.Events) != 2 || p.LastID != 2 || p.RunID != "run1" {
		t.Fatalf("all: %+v", p)
	}
	for _, c := range []struct {
		run   string
		since uint64
	}{{"", 0}, {"other", 1}, {"run1", 5}} {
		if p := m.EventsSince(c.run, c.since, 0); !p.Reset || len(p.Events) != 0 || p.LastID != 2 {
			t.Errorf("%+v: %+v", c, p)
		}
	}
	if p := m.EventsSince("run1", 2, 0); p.Reset || len(p.Events) != 0 || p.LastID != 2 {
		t.Errorf("up to date: %+v", p)
	}
	for i := uint64(3); i <= 5; i++ {
		m.ring.add(model.AlertEvent{ID: i})
	}
	if p := m.EventsSince("run1", 1, 0); !p.Reset {
		t.Errorf("gap must reset: %+v", p)
	}
	if p := m.EventsSince("run1", 2, 0); p.Reset || len(p.Events) != 3 || p.Events[0].ID != 3 {
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
	for _, a := range m.Snapshot().Alerts {
		_ = a
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
	if ie := p9IPCErr(t, err); ie.Key != model.MsgMonitorProfileInvalid || len(ie.Args) < 2 || ie.Args[0] != "server" || ie.Args[1] != model.ValServerForbidden {
		t.Errorf("np: %+v", ie)
	}
}
