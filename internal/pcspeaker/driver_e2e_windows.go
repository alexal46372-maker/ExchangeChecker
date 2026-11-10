//go:build inpout_e2e && windows

package pcspeaker

import (
	"runtime"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_SVC_12_RealInpOut talks to a real ExchangeCheckerInpOut service
// and official inpoutx64.sys on a Windows amd64 machine. It is not part of
// default CI (build tag inpout_e2e). Do not Skip: if the driver cannot be
// loaded, fail and follow docs/ACCEPTANCE-WIN7.md (section «Системный динамик»).
func TestCHK_SVC_12_RealInpOut(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Fatalf("InpOutx64 is amd64-only, GOARCH=%s", runtime.GOARCH)
	}
	p := NewInpOut(InpOutOptions{})
	st := p.Status()
	if !st.Available {
		t.Fatalf("speaker not available (reason=%s, version=%s); install service ExchangeCheckerInpOut and the official signed inpoutx64.sys, see docs/ACCEPTANCE-WIN7.md", st.Reason, st.DriverVersion)
	}
	if st.DriverVersion != inpOutDriverVersion {
		t.Fatalf("driverVersion %q", st.DriverVersion)
	}
	d := New(Options{Port: p})
	if err := d.Tone("e2e", 440, 50); err != nil {
		t.Fatalf("tone: %v", err)
	}
	d.Silence()
	p.Unload()
	if got := queryInpOutState(t); got != winapi.ServiceStopped {
		t.Fatalf("after Unload state=%d, want STOPPED (%d)", got, winapi.ServiceStopped)
	}

	// Stop (not Silence) → idle unload ≤2 s → STOPPED within 2.5 s.
	if err := d.Tone("e2e", 440, 50); err != nil {
		t.Fatalf("tone 2: %v", err)
	}
	if err := d.Stop("e2e"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	deadline := time.Now().Add(2500 * time.Millisecond)
	var state uint32
	for {
		state = queryInpOutState(t)
		if state == winapi.ServiceStopped {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("after Stop, state=%d after 2.5s, want STOPPED", state)
		}
		<-time.After(50 * time.Millisecond)
	}

	// Wrong sha256 must not start the driver.
	bad := NewInpOut(InpOutOptions{SHA256: "0000000000000000000000000000000000000000000000000000000000000000"})
	st = bad.Status()
	if st.Available || st.Reason != model.SpeakerBlocked {
		t.Fatalf("wrong hash status %+v", st)
	}
	if err := New(Options{Port: bad}).Tone("e2e", 440, 50); err == nil {
		t.Fatal("tone with wrong hash")
	}
	if got := queryInpOutState(t); got != winapi.ServiceStopped {
		t.Fatalf("wrong hash started the driver: state=%d", got)
	}
}

func queryInpOutState(t *testing.T) uint32 {
	t.Helper()
	scm, err := winapi.OpenSCManager(winapi.SCManagerConnect)
	if err != nil {
		t.Fatalf("OpenSCManager: %v", err)
	}
	defer scm.Close()
	svc, err := winapi.OpenService(scm, config.InpOutServiceName, uint32(winapi.ServiceQueryStatus))
	if err != nil {
		t.Fatalf("OpenService: %v", err)
	}
	defer svc.Close()
	state, _, err := winapi.QueryServiceStatus(svc)
	if err != nil {
		t.Fatalf("QueryServiceStatus: %v", err)
	}
	return state
}
