package winsvc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu  sync.Mutex
	got []Status
}

func (r *recorder) set(s Status) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, s)
	return nil
}

func (r *recorder) states() []uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []uint32
	for _, s := range r.got {
		out = append(out, s.CurrentState)
	}
	return out
}

func (r *recorder) last() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.got[len(r.got)-1]
}

// TestCHK_MON_07_LifecycleStop — START_PENDING -> RUNNING (accepts Stop +
// Shutdown) -> Stop: STOP_PENDING + context canceled -> STOPPED.
func TestCHK_MON_07_LifecycleStop(t *testing.T) {
	for _, code := range []uint32{ControlStop, ControlShutdown} {
		r := &recorder{}
		c := newController(r.set)
		started := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- c.serve(func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				return nil
			})
		}()
		<-started
		deadline := time.Now().Add(5 * time.Second)
		for c.state().CurrentState != StateRunning {
			if time.Now().After(deadline) {
				t.Fatal("not running")
			}
			time.Sleep(time.Millisecond)
		}
		if st := c.state(); st.ControlsAccepted != AcceptStop|AcceptShutdown || st.ServiceType != ServiceWin32OwnProcess {
			t.Errorf("running status %+v", st)
		}
		if res := c.control(ControlInterrogate); res != 0 {
			t.Errorf("interrogate = %d", res)
		}
		if res := c.control(code); res != 0 {
			t.Errorf("control %d = %d", code, res)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("service did not stop")
		}
		want := []uint32{StateStartPending, StateRunning, StateRunning, StateStopPending, StateStopped}
		got := r.states()
		if len(got) != len(want) {
			t.Fatalf("control %d: states %v, want %v", code, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("control %d: states %v, want %v", code, got, want)
			}
		}
		if last := r.last(); last.Win32ExitCode != 0 || last.ControlsAccepted != 0 || last.CheckPoint != 0 {
			t.Errorf("stopped status %+v", last)
		}
		if r.got[3].WaitHint != StopWaitHint || r.got[0].WaitHint != StartWaitHint || r.got[3].CheckPoint == 0 {
			t.Errorf("wait hints %+v", r.got)
		}
	}
}

// TestCHK_MON_07_ErrorAndUnknownControl — a failing service function is
// reported as a service-specific error; unknown controls are refused.
func TestCHK_MON_07_ErrorAndUnknownControl(t *testing.T) {
	r := &recorder{}
	c := newController(r.set)
	if res := c.control(0x80); res != errorCallNotImplemented {
		t.Errorf("unknown control = %d", res)
	}
	err := c.serve(func(context.Context) error { return errors.New("config dir not writable") })
	if err == nil {
		t.Fatal("error lost")
	}
	if last := r.last(); last.CurrentState != StateStopped || last.Win32ExitCode != errorServiceSpecificError || last.ServiceSpecificExitCode != 1 {
		t.Errorf("stopped with error %+v", last)
	}
	c2 := newController(nil)
	if err := c2.serve(func(context.Context) error { panic("x") }); err == nil {
		t.Error("panic must be reported as an error")
	}
	if c2.state().CurrentState != StateStopped {
		t.Error("state after panic")
	}
}

// TestCHK_MON_07_RunOutsideSCM — started from a console / test runner the
// dispatcher is refused and Run returns ErrNotService (on Windows through
// StartServiceCtrlDispatcherW = ERROR_FAILED_SERVICE_CONTROLLER_CONNECT).
func TestCHK_MON_07_RunOutsideSCM(t *testing.T) {
	called := false
	done := make(chan error, 1)
	go func() { done <- Run("ExchangeChecker-test", func(context.Context) error { called = true; return nil }) }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNotService) {
			t.Errorf("Run = %v, want ErrNotService", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("Run blocked outside the SCM")
	}
	if called {
		t.Error("service function must not run outside the SCM")
	}
}

// stopHarness drives a controller with an injected clock and ticker:
// every SetServiceStatus goes to reports, ticks are delivered one by one
// (unbuffered: a send returns only when the loop took the tick, and the
// next send only after that tick was fully processed).
type stopHarness struct {
	c       *controller
	reports chan Status
	ticks   chan time.Time
	release chan struct{}
	done    chan error
	mu      sync.Mutex
	now     time.Time
}

func newStopHarness(t *testing.T) *stopHarness {
	h := &stopHarness{reports: make(chan Status, 100), ticks: make(chan time.Time), release: make(chan struct{}),
		done: make(chan error, 1), now: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	h.c = newController(func(s Status) error { h.reports <- s; return nil })
	h.c.ticks = h.ticks
	h.c.now = func() time.Time { h.mu.Lock(); defer h.mu.Unlock(); return h.now }
	go func() {
		h.done <- h.c.serve(func(ctx context.Context) error {
			<-ctx.Done()
			<-h.release // slow cleanup until the test releases it
			return nil
		})
	}()
	for _, want := range []uint32{StateStartPending, StateRunning} {
		if s := h.next(t); s.CurrentState != want {
			t.Fatalf("start: %+v", s)
		}
	}
	return h
}

func (h *stopHarness) advance(d time.Duration) { h.mu.Lock(); h.now = h.now.Add(d); h.mu.Unlock() }

func (h *stopHarness) tick(t *testing.T) {
	t.Helper()
	select {
	case h.ticks <- h.now:
	case <-time.After(5 * time.Second):
		t.Fatal("serve loop does not take ticks")
	}
}

func (h *stopHarness) next(t *testing.T) Status {
	t.Helper()
	select {
	case s := <-h.reports:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("no status report")
	}
	return Status{}
}

func (h *stopHarness) none(t *testing.T) {
	t.Helper()
	h.tick(t) // a taken tick proves the previous one was processed
	select {
	case s := <-h.reports:
		t.Fatalf("unexpected report %+v", s)
	default:
	}
}

// TestCHK_MON_14_StopPendingCheckpoints — while the service function is
// still stopping, every progress tick re-reports STOP_PENDING with
// CheckPoint+1 and the wait hint; ticks while RUNNING report nothing; the
// end is STOPPED with CheckPoint 0. Injected ticker: no sleeps.
func TestCHK_MON_14_StopPendingCheckpoints(t *testing.T) {
	if StopCheckpointEvery*3 > StopWaitHint*time.Millisecond {
		t.Fatalf("checkpoint period %s too long for wait hint %d ms", StopCheckpointEvery, StopWaitHint)
	}
	h := newStopHarness(t)
	// No ticks before the stop: a tick taken by the loop may still be in
	// flight when control() runs and would add a STOP_PENDING report
	// (RUNNING ticks are covered by TestCHK_MON_14_NoProgressWhileRunning).
	h.c.control(ControlStop)
	s := h.next(t)
	if s.CurrentState != StateStopPending || s.CheckPoint != 1 || s.WaitHint != StopWaitHint {
		t.Fatalf("stop: %+v", s)
	}
	for i := uint32(2); i <= 6; i++ {
		h.advance(StopCheckpointEvery)
		h.tick(t)
		if s := h.next(t); s.CurrentState != StateStopPending || s.CheckPoint != i || s.WaitHint != StopWaitHint {
			t.Fatalf("progress %d: %+v", i, s)
		}
	}
	close(h.release)
	if err := <-h.done; err != nil {
		t.Fatal(err)
	}
	if s := h.next(t); s.CurrentState != StateStopped || s.CheckPoint != 0 {
		t.Errorf("final %+v", s)
	}
}

// TestCHK_MON_14_NoProgressWhileRunning — progress ticks while RUNNING
// report nothing. The stop afterwards is only cleanup: the last taken tick
// may be processed after control(), so any number of STOP_PENDING reports
// is accepted before the final STOPPED.
func TestCHK_MON_14_NoProgressWhileRunning(t *testing.T) {
	h := newStopHarness(t)
	h.tick(t)
	h.none(t)
	h.none(t)
	h.c.control(ControlStop)
	close(h.release)
	if err := <-h.done; err != nil {
		t.Fatal(err)
	}
	for {
		s := h.next(t)
		if s.CurrentState == StateStopped {
			break
		}
		if s.CurrentState != StateStopPending {
			t.Fatalf("after stop: %+v", s)
		}
	}
}

// TestCHK_MON_14_StopCheckpointCap — after StopCheckpointMax (60 s) of
// STOP_PENDING the CheckPoint stops growing (no more reports), so the SCM
// can detect a hung stop via the wait hint. Fake clock + ticker.
func TestCHK_MON_14_StopCheckpointCap(t *testing.T) {
	if StopCheckpointMax != 60*time.Second || StopCheckpointMax <= StopCheckpointEvery {
		t.Fatalf("cap %s", StopCheckpointMax)
	}
	h := newStopHarness(t)
	h.c.control(ControlStop)
	last := h.next(t).CheckPoint
	for el := StopCheckpointEvery; el < StopCheckpointMax; el += StopCheckpointEvery {
		h.advance(StopCheckpointEvery)
		h.tick(t)
		s := h.next(t)
		if s.CheckPoint != last+1 {
			t.Fatalf("at %s: checkpoint %d after %d", el, s.CheckPoint, last)
		}
		last = s.CheckPoint
	}
	if last != uint32(StopCheckpointMax/StopCheckpointEvery) {
		t.Errorf("checkpoints within the cap: %d", last)
	}
	h.advance(StopCheckpointEvery) // 60 s reached: no more progress
	h.tick(t)
	h.none(t)
	h.advance(time.Hour)
	h.none(t)
	if st := h.c.state(); st.CurrentState != StateStopPending || st.CheckPoint != last {
		t.Errorf("after the cap: %+v", st)
	}
	close(h.release)
	if err := <-h.done; err != nil {
		t.Fatal(err)
	}
	if s := h.next(t); s.CurrentState != StateStopped {
		t.Errorf("final %+v", s)
	}
}
