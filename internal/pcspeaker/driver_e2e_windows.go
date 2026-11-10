//go:build inpout_e2e && windows

package pcspeaker

import (
	"runtime"
	"testing"
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
}
