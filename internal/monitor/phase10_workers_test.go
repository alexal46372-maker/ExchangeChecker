package monitor

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// workerEnv runs a monitor (Run) with short real intervals.
func workerEnv(t *testing.T, f *fakeFactory, profiles []model.Profile, timeout time.Duration) (*Monitor, *MemConfig, func()) {
	t.Helper()
	src := NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: profiles})
	clk := &fakeClock{t: t0}
	opt := baseOpt(f, src, clk)
	opt.PollInterval = 30 * time.Millisecond
	opt.CommandInterval = -1
	opt.ProfileTimeout = func(model.Profile) time.Duration { return timeout }
	m := New(opt)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	return m, src, func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("Run did not stop")
		}
	}
}

func waitW(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestCHK_MON_16_HungProfileDoesNotBlockOthers — a database that hangs
// (ignoring its context) holds only its own worker: the healthy profile
// keeps being polled and published; the service still stops in time.
func TestCHK_MON_16_HungProfileDoesNotBlockOthers(t *testing.T) {
	f := newFactory()
	hang := make(chan struct{})
	defer close(hang)
	f.set("hang", behavior{hang: hang})
	f.set("ok", behavior{points: lagPoints(map[string]int{"A": 1})})
	m, _, stop := workerEnv(t, f, []model.Profile{prof("hang"), prof("ok")}, time.Hour)
	waitW(t, "ok polled 5 times", func() bool { return f.get(f.calls, "ok") >= 5 })
	if n := f.get(f.calls, "hang"); n != 1 {
		t.Fatalf("hang calls %d", n)
	}
	s := m.Snapshot()
	if p := profileByID(s, "ok"); !p.Polled || p.Error != "" {
		t.Fatalf("ok profile %+v", p)
	}
	if m.Workers() != 2 {
		t.Fatalf("workers %d", m.Workers())
	}
	stop()
	if m.Workers() != 0 {
		t.Fatalf("workers after stop %d", m.Workers())
	}
}

// TestCHK_MON_16_WorkerLifecycle — a removed or disabled profile stops its
// worker, a changed profile restarts it (new reader), unchanged profiles
// keep theirs.
func TestCHK_MON_16_WorkerLifecycle(t *testing.T) {
	f := newFactory()
	a, b := prof("a"), prof("b")
	m, src, stop := workerEnv(t, f, []model.Profile{a, b}, time.Second)
	defer stop()
	waitW(t, "2 workers polled", func() bool { return f.get(f.calls, "a") > 0 && f.get(f.calls, "b") > 0 })
	set := func(ps ...model.Profile) {
		src.Set(Config{Settings: model.DefaultSettings(), Profiles: ps})
		m.reloadConfig(true)
	}
	set(a) // b removed
	if m.Workers() != 1 {
		t.Fatalf("workers %d", m.Workers())
	}
	waitW(t, "b reader closed", func() bool { return f.get(f.closed, "b") == 1 })
	nb := f.get(f.calls, "b")
	time.Sleep(100 * time.Millisecond)
	if f.get(f.calls, "b") != nb {
		t.Fatalf("removed profile still polled")
	}
	ca := f.get(f.created, "a")
	a2 := a
	a2.Database = "other"
	set(a2) // changed: restarted with a new reader
	waitW(t, "a recreated", func() bool { return f.get(f.created, "a") == ca+1 })
	if m.Workers() != 1 {
		t.Fatalf("workers %d", m.Workers())
	}
	a3 := a2
	a3.Enabled = false
	set(a3)
	if m.Workers() != 0 {
		t.Fatalf("disabled: workers %d", m.Workers())
	}
	set(a2, b)
	waitW(t, "2 workers", func() bool { return m.Workers() == 2 })
}

// TestCHK_MON_16_Semaphore — 20 slow profiles: at most MaxSQLConnections
// polls run at the same time, and more than one does.
func TestCHK_MON_16_Semaphore(t *testing.T) {
	f := newFactory()
	var ps []model.Profile
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("p%02d", i)
		f.set(id, behavior{delay: 50 * time.Millisecond})
		ps = append(ps, prof(id))
	}
	_, _, stop := workerEnv(t, f, ps, time.Second)
	waitW(t, "every profile polled twice", func() bool {
		for _, p := range ps {
			if f.get(f.finished, p.ID) < 2 {
				return false
			}
		}
		return true
	})
	mx := atomic.LoadInt32(&f.maxActive)
	stop()
	if mx > MaxSQLConnections || mx < 2 {
		t.Fatalf("max concurrent polls %d", mx)
	}
}

// TestCHK_MON_16_CheckNowWorkers — check_now of one profile and of all
// profiles completes (lastCheckSeq >= checkSeq) only after polls started
// after the request, with a slow profile in the set.
func TestCHK_MON_16_CheckNowWorkers(t *testing.T) {
	f := newFactory()
	f.set("slow", behavior{delay: 300 * time.Millisecond})
	src := NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("fast"), prof("slow")}})
	clk := &fakeClock{t: t0}
	opt := baseOpt(f, src, clk)
	opt.PollInterval = time.Hour // only the first poll and the kicks
	opt.CommandInterval = -1
	opt.ProfileTimeout = func(model.Profile) time.Duration { return 5 * time.Second }
	m := New(opt)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	defer func() { cancel(); <-done }()
	waitW(t, "first polls", func() bool { return f.get(f.finished, "fast") == 1 && f.get(f.finished, "slow") == 1 })

	r, err := m.CheckNow("fast")
	if err != nil {
		t.Fatal(err)
	}
	waitW(t, "fast check", func() bool { return m.Snapshot().LastCheckSeq >= r.CheckSeq })
	if f.get(f.calls, "fast") != 2 || f.get(f.calls, "slow") != 1 {
		t.Fatalf("calls fast %d slow %d", f.get(f.calls, "fast"), f.get(f.calls, "slow"))
	}
	clk.Add(time.Hour) // past the check_now rate limit
	r, err = m.CheckNow("")
	if err != nil {
		t.Fatal(err)
	}
	waitW(t, "fast polled", func() bool { return f.get(f.finished, "fast") == 3 })
	if s := m.Snapshot(); s.LastCheckSeq >= r.CheckSeq && f.get(f.finished, "slow") < 2 {
		t.Fatalf("check of all completed before the slow profile was polled")
	}
	waitW(t, "all check", func() bool { return m.Snapshot().LastCheckSeq >= r.CheckSeq })
	if f.get(f.finished, "slow") != 2 {
		t.Fatalf("slow %d", f.get(f.finished, "slow"))
	}
}
