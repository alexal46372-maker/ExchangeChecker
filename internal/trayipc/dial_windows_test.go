//go:build windows

package trayipc

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

func testPipeName(tag string) string {
	return fmt.Sprintf(`\\.\pipe\ExchangeChecker-tray-test-%s-%d`, tag, os.Getpid())
}

// TestCHK_IPC_01_NamedPipe — the session over a real named pipe; a missing
// pipe is StateNotRunning; G7 against the real APIs: the pipe server PID
// is this process, which is not the SCM service -> untrusted.
func TestCHK_IPC_01_NamedPipe(t *testing.T) {
	name := testPipeName("ok")
	l, err := ipc.ListenPipe(name, "")
	if err != nil {
		t.Fatal(err)
	}
	h := &fakeHandler{snap: model.StatusSnapshot{Seq: 9}, hb: model.Heartbeat{Protocol: 2, ServiceStartedAt: time.Unix(5, 0)}}
	srv := ipc.NewServer(h, nil)
	go srv.Serve(l)
	defer srv.Close()

	clk := newFakeClock()
	s := New(testOptions(clk, PipeDialer(name, 2*time.Second)))
	runSession(t, s)
	waitFor(t, "connected over the pipe", connected(s))
	if s.Status().Snapshot.Seq != 9 {
		t.Error("snapshot over the pipe")
	}

	// GetNamedPipeServerProcessId sees this test process
	rw, err := ipc.DialPipe(name, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := ipc.ServerProcessID(rw)
	rw.Close()
	if err != nil || pid != uint32(os.Getpid()) {
		t.Fatalf("pipe server PID %d (%v), want %d", pid, err, os.Getpid())
	}

	// the G7 verifier rejects it: "ExchangeChecker" is not installed on
	// the runner / is another process
	clk2 := newFakeClock()
	opt := testOptions(clk2, PipeDialer(name, 2*time.Second))
	opt.Verify = ServiceVerifier(model.ServiceName)
	s2 := New(opt)
	runSession(t, s2)
	waitFor(t, "untrusted", func() bool { return s2.Status().State == StateUntrusted })
	var ue *UntrustedError
	if !errors.As(s2.Status().Err, &ue) {
		t.Errorf("err %v", s2.Status().Err)
	}

	// missing pipe -> not running
	clk3 := newFakeClock()
	s3 := New(testOptions(clk3, PipeDialer(testPipeName("missing"), 50*time.Millisecond)))
	runSession(t, s3)
	waitFor(t, "not running", func() bool { return s3.Status().State == StateNotRunning })
	if !errors.Is(s3.Status().Err, ErrServiceNotRunning) {
		t.Errorf("err %v", s3.Status().Err)
	}
}

// TestCHK_IPC_01_ServiceProcessID — QueryServiceStatusEx returns the PID
// of a running system service (EventLog), and an error for a missing one.
func TestCHK_IPC_01_ServiceProcessID(t *testing.T) {
	state, pid, err := winapi.ServiceProcessID("EventLog")
	if err != nil || state != winapi.ServiceRunning || pid == 0 {
		t.Fatalf("EventLog: state %d pid %d err %v", state, pid, err)
	}
	if _, _, err := winapi.ServiceProcessID("ExchangeChecker-no-such-service"); err == nil {
		t.Error("missing service must fail")
	}
}
