package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_MON_13_RetainLocksAllKnownIDs — backlog (a): the very first
// Update of a profile (no stored state yet) is running when the profile
// is removed; Retain waits for its lock (known from the lock table, not
// from the data) and removes the state afterwards: no orphan in
// counters-state.json.
func TestCHK_MON_13_RetainLocksAllKnownIDs(t *testing.T) {
	dir := t.TempDir()
	s := OpenFileCounterStore(dir, nil)
	inFn := make(chan struct{})
	release := make(chan struct{})
	updated := make(chan error, 1)
	go func() {
		updated <- s.Update("x", func(prev model.CounterState) (model.CounterState, error) {
			close(inFn)
			<-release
			prev.Points["k"] = model.CounterPoint{SentNo: 1}
			return prev, nil
		})
	}()
	<-inFn
	locking := make(chan []string, 1)
	s.retainHook = func(ids []string) { locking <- ids }
	retained := make(chan error, 1)
	go func() { retained <- s.Retain(map[string]bool{}) }()
	select {
	case ids := <-locking:
		if len(ids) != 1 || ids[0] != "x" {
			t.Fatalf("Retain locks %v", ids)
		}
	case err := <-retained:
		t.Fatalf("Retain returned (%v) without waiting for the first write", err)
	}
	close(release)
	if err := <-updated; err != nil {
		t.Fatal(err)
	}
	if err := <-retained; err != nil {
		t.Fatal(err)
	}
	if hasID(s.ProfileIDs(), "x") || hasID(OpenFileCounterStore(dir, nil).ProfileIDs(), "x") {
		t.Error("orphan state of a removed profile")
	}
	s.mu.Lock()
	_, lk := s.locks["x"]
	s.mu.Unlock()
	if lk {
		t.Error("lock of the removed profile kept")
	}
}

// TestCHK_MON_13_RetiredHungReaderClosed — backlog (b): the profile
// changes while its poll hangs forever; the retired entry's reader (its
// sql.DB) is closed after HungFactor poll timeouts like a live one, and
// the retired entry is forgotten.
func TestCHK_MON_13_RetiredHungReaderClosed(t *testing.T) {
	f := newFactory()
	hang := make(chan struct{})
	defer close(hang)
	f.set("c", behavior{hang: hang})
	clk := &fakeClock{t: t0}
	src := NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("c")}})
	opt := baseOpt(f, src, clk)
	opt.ProfileTimeout = func(model.Profile) time.Duration { return 20 * time.Millisecond }
	lg := &memLog{}
	opt.Log = lg
	m := New(opt)
	m.reloadConfig(true)
	m.pollAll(context.Background()) // hangs -> timeout, busy
	pc := prof("c")
	pc.Database = "moved"
	src.Set(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{pc}})
	m.reloadConfig(false)
	if f.get(f.closed, "c") != 0 {
		t.Fatal("busy retired reader closed at once")
	}
	f.set("c", behavior{})
	clk.Add(30 * time.Millisecond) // < 3 × 20 ms
	m.pollAll(context.Background())
	if f.get(f.closed, "c") != 0 || len(m.retired) != 1 {
		t.Fatalf("closed before the hang limit: %d, retired %d", f.get(f.closed, "c"), len(m.retired))
	}
	clk.Add(time.Second)
	m.pollAll(context.Background())
	waitFor(t, func() bool { return f.get(f.closed, "c") == 1 })
	if len(m.retired) != 0 || !lg.has("retired") {
		t.Errorf("retired entries %d", len(m.retired))
	}
	// a retired entry that finished normally is forgotten without closing twice
	pc.Database = "again"
	src.Set(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{pc}})
	m.reloadConfig(false)
	m.pollAll(context.Background())
	if len(m.retired) != 0 || f.get(f.closed, "c") != 2 {
		t.Errorf("idle retired entry: retired %d closed %d", len(m.retired), f.get(f.closed, "c"))
	}
}

// TestCHK_MON_13_CounterLocksCleaned — backlog (c): the monitor's
// per-profile counters lock is dropped when the profile is removed and
// kept (shared by the old and new entry) when it only changes.
func TestCHK_MON_13_CounterLocksCleaned(t *testing.T) {
	f := newFactory()
	f.set("a", behavior{})
	f.set("b", behavior{})
	src := NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a"), prof("b")}})
	m := New(baseOpt(f, src, &fakeClock{t: t0}))
	m.reloadConfig(true)
	m.pollAll(context.Background())
	la := m.counterLock("a")
	if len(m.cLocks) != 2 {
		t.Fatalf("locks %d", len(m.cLocks))
	}
	pa := prof("a")
	pa.Database = "changed"
	src.Set(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{pa}})
	m.reloadConfig(false)
	m.clMu.Lock()
	_, hasB := m.cLocks["b"]
	keptA := m.cLocks["a"] == la
	m.clMu.Unlock()
	if hasB || !keptA {
		t.Errorf("after removing b: b kept %v, a kept %v", hasB, keptA)
	}
}
