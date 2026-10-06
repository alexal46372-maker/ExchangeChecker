package monitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// gateReader: its operation waits for gate (ignoring ctx: a driver that
// does not see the cancel) and returns a result, or panics when
// panicWork; Close panics when panicClose.
type gateReader struct {
	gate                  chan struct{}
	panicWork, panicClose bool
	closed                chan struct{}
}

func (r *gateReader) op() error {
	<-r.gate
	if r.panicWork {
		panic("work")
	}
	return nil
}
func (r *gateReader) Discover(ctx context.Context) ([]model.DiscoveryCandidate, error) {
	return []model.DiscoveryCandidate{{Table: "_Node1"}}, r.op()
}
func (r *gateReader) DiscoverByValue(ctx context.Context, q model.ValueQuery) ([]model.DiscoveryCandidate, error) {
	return r.Discover(ctx)
}
func (r *gateReader) NodePoints(ctx context.Context) ([]model.NodePoint, error) {
	return []model.NodePoint{{Name: "A"}}, r.op()
}
func (r *gateReader) PreviewMapping(ctx context.Context, m model.Mapping, limit int) ([]model.SampleRow, error) {
	return nil, r.op()
}
func (r *gateReader) Close() error {
	close(r.closed)
	if r.panicClose {
		panic("close")
	}
	return nil
}

func waitAbandoned(t *testing.T, busy func() int, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for busy() != want {
		if time.Now().After(deadline) {
			t.Fatalf("abandoned %d, want %d (goroutine stuck)", busy(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestCHK_IPC_08_AbandonedPanicInClose (Вика 06.10) — the caller left on
// cctx.Done(), then the driver returns, its result lands in ch (nobody
// reads it) and the deferred Close panics: the goroutine must not block
// on a second send; abandoned goes back to 0 and the next commands are
// not busy. Repeated 3 times (MaxAbandoned is 2), also with a panic in
// the work itself and in both, heavy and light.
func TestCHK_IPC_08_AbandonedPanicInClose(t *testing.T) {
	var cur *gateReader
	x := &Explorer{New: func(p model.Profile, pw string, opt sqlcheck.Options) (InteractiveReader, error) { return cur, nil }}
	x.defaults()
	abandoned := func() int { _, _, a := x.Busy(); return a }
	gone, cancel := context.WithCancel(context.Background())
	cancel() // the caller is gone
	p := sqlProf("p1")
	for i, c := range []struct{ work, close, heavy bool }{
		{false, true, true}, {false, true, false}, {false, true, true},
		{true, true, false}, {true, false, true},
	} {
		cur = &gateReader{gate: make(chan struct{}), panicWork: c.work, panicClose: c.close, closed: make(chan struct{})}
		var err error
		if c.heavy {
			_, err = exploreRun(x, gone, true, "id:p1", p, "", func(ctx context.Context, r InteractiveReader) ([]model.DiscoveryCandidate, error) {
				return r.Discover(ctx)
			})
		} else {
			_, err = exploreRun(x, gone, false, "id:p1", p, "", func(ctx context.Context, r InteractiveReader) ([]model.NodePoint, error) {
				return r.NodePoints(ctx)
			})
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%d: %v", i, err)
		}
		if h, l, a := x.Busy(); h != 0 || l != 0 || a != 1 {
			t.Fatalf("%d: busy %d %d %d", i, h, l, a)
		}
		close(cur.gate) // the driver returns: result into ch, then Close (panics)
		<-cur.closed
		waitAbandoned(t, abandoned, 0)
	}
	// not busy afterwards
	cur = &gateReader{gate: make(chan struct{}), closed: make(chan struct{})}
	close(cur.gate)
	cands, err := exploreRun(x, context.Background(), true, "id:p1", p, "", func(ctx context.Context, r InteractiveReader) ([]model.DiscoveryCandidate, error) {
		return r.Discover(ctx)
	})
	if err != nil || len(cands) != 1 {
		t.Fatalf("after: %v %v", cands, err)
	}
	// a panic in Close of a completed request: the result still comes
	cur = &gateReader{gate: make(chan struct{}), panicClose: true, closed: make(chan struct{})}
	close(cur.gate)
	if cands, err = exploreRun(x, context.Background(), true, "id:p1", p, "", func(ctx context.Context, r InteractiveReader) ([]model.DiscoveryCandidate, error) {
		return r.Discover(ctx)
	}); err != nil || len(cands) != 1 {
		t.Fatalf("panic in Close after the reply: %v %v", cands, err)
	}
	waitAbandoned(t, abandoned, 0)
	if h, l, _ := x.Busy(); h != 0 || l != 0 {
		t.Fatalf("slots %d %d", h, l)
	}
}

// TestCHK_IPC_07_TestAbandonedPanic — the same for test_profile (Tester,
// CHK-IPC-03/07): the caller left, then the driver panics; abandoned
// goes back to 0, 3 times in a row (MaxAbandoned 2), then a test runs.
func TestCHK_IPC_07_TestAbandonedPanic(t *testing.T) {
	var gate chan struct{}
	tr := &Tester{Test: func(ctx context.Context, p model.Profile, pw string) (TestInfo, error) {
		g := gate
		<-g
		panic("driver")
	}}
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	abandoned := func() int { _, a := tr.Busy(); return a }
	for i := 0; i < 3; i++ {
		gate = make(chan struct{})
		g := gate
		res, err := tr.Run(gone, sqlProf("p1"), "")
		if err != nil || res.OK {
			t.Fatalf("%d: %+v %v", i, res, err)
		}
		if run, a := tr.Busy(); run != 0 || a != 1 {
			t.Fatalf("%d: running %d abandoned %d", i, run, a)
		}
		close(g)
		waitAbandoned(t, abandoned, 0)
	}
	tr.mu.Lock()
	tr.Test = func(ctx context.Context, p model.Profile, pw string) (TestInfo, error) {
		return TestInfo{Version: "10.50"}, nil
	}
	tr.mu.Unlock()
	if res, err := tr.Run(context.Background(), sqlProf("p1"), ""); err != nil || !res.OK {
		t.Fatalf("after: %+v %v", res, err)
	}
}
