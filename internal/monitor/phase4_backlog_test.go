package monitor

import (
	"context"
	"sync"
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
	if len(s.locks.keys()) != 0 {
		t.Errorf("locks kept after the last unlock: %v", s.locks.keys())
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

// TestCHK_MON_13_CounterLocksCleaned — backlog (c), reference counted
// since phase 4b: the monitor's per-profile counters locks exist only
// while held or waited for, so nothing is left behind for removed (or
// any idle) profiles, and nothing is deleted while in use.
func TestCHK_MON_13_CounterLocksCleaned(t *testing.T) {
	f := newFactory()
	f.set("a", behavior{counter: true})
	f.set("b", behavior{counter: true})
	f.counters["a"] = []model.CounterObservation{{Key: "k", Name: "N", SentNo: 1, ReceivedNo: 1}}
	f.counters["b"] = []model.CounterObservation{{Key: "k", Name: "N", SentNo: 1, ReceivedNo: 1}}
	src := NewMemConfig(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a"), prof("b")}})
	opt := baseOpt(f, src, &fakeClock{t: t0})
	opt.Counters = struct{ model.CounterStateStore }{model.NewMemCounterStore()} // no Update: the monitor's locks are used
	m := New(opt)
	m.reloadConfig(true)
	m.pollAll(context.Background())
	if k := m.cLocks.keys(); len(k) != 0 {
		t.Fatalf("idle locks kept: %v", k)
	}
	src.Set(Config{Settings: model.DefaultSettings(), Profiles: []model.Profile{prof("a")}})
	m.reloadConfig(false)
	m.pollAll(context.Background())
	if k := m.cLocks.keys(); len(k) != 0 {
		t.Errorf("locks after removing b: %v", k)
	}
}

// TestCHK_MON_13_LockSharedAcrossRemoveReadd — phase 4b (review item 6):
// an Update of profile x waits while x is removed (Retain) and re-added;
// the waiting old Update, Retain and a new Update of x all use ONE
// reference-counted mutex, so no two critical sections of x ever run at
// the same time (before: Retain deleted the lock under the waiter and the
// new Update got a second mutex). Synchronized by the queue hook, no
// sleeps.
func TestCHK_MON_13_LockSharedAcrossRemoveReadd(t *testing.T) {
	s := OpenFileCounterStore(t.TempDir(), nil)
	queued := make(chan int, 10)
	s.locks.queued = func(key string, refs int) { queued <- refs }
	var mu sync.Mutex
	inside := 0
	enter := func(name string, entered chan<- string, release <-chan struct{}) func(model.CounterState) (model.CounterState, error) {
		return func(prev model.CounterState) (model.CounterState, error) {
			mu.Lock()
			inside++
			n := inside
			mu.Unlock()
			if n != 1 {
				t.Errorf("%s: %d critical sections of x at once", name, n)
			}
			entered <- name
			<-release
			mu.Lock()
			inside--
			mu.Unlock()
			prev.Points["k"] = model.CounterPoint{SentNo: 1}
			return prev, nil
		}
	}
	entered := make(chan string, 3)
	relA, relB, relC := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan error, 4)
	go func() { done <- s.Update("x", enter("A", entered, relA)) }()
	if r := <-queued; r != 1 {
		t.Fatalf("A refs %d", r)
	}
	if n := <-entered; n != "A" {
		t.Fatalf("first in: %s", n)
	}
	go func() { done <- s.Update("x", enter("B-old", entered, relB)) }()
	if r := <-queued; r != 2 {
		t.Fatalf("B refs %d", r)
	}
	go func() { done <- s.Retain(map[string]bool{}) }() // x removed
	if r := <-queued; r != 3 {
		t.Fatalf("Retain refs %d", r)
	}
	go func() { done <- s.Update("x", enter("C-new", entered, relC)) }() // x re-added
	if r := <-queued; r != 4 {
		t.Fatalf("C refs %d: the re-added profile got a new lock", r)
	}
	close(relA)
	rel := map[string]chan struct{}{"B-old": relB, "C-new": relC}
	for len(rel) > 0 {
		n := <-entered // results stay buffered in done
		close(rel[n])
		delete(rel, n)
	}
	for i := 0; i < 4; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if k := s.locks.keys(); len(k) != 0 {
		t.Errorf("locks kept: %v", k)
	}
}
