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
