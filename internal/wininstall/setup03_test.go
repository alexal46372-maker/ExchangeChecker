package wininstall

import (
	"testing"
	"unsafe"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_SETUP_03_StructSizes — SERVICE_* layouts on amd64 (docs/PHASE7.md §2.2).
func TestCHK_SETUP_03_StructSizes(t *testing.T) {
	if unsafe.Sizeof(winapi.SCAction{}) != 8 {
		t.Fatalf("SCAction=%d", unsafe.Sizeof(winapi.SCAction{}))
	}
	if unsafe.Sizeof(winapi.ServiceFailureActionsW{}) != 40 {
		t.Fatalf("ServiceFailureActionsW=%d want 40", unsafe.Sizeof(winapi.ServiceFailureActionsW{}))
	}
	if unsafe.Sizeof(winapi.ServiceDescriptionW{}) != unsafe.Sizeof(uintptr(0)) {
		t.Fatalf("ServiceDescriptionW=%d", unsafe.Sizeof(winapi.ServiceDescriptionW{}))
	}
	if unsafe.Sizeof(winapi.ServiceDelayedAutoStartInfo{}) != 4 {
		t.Fatalf("Delayed=%d", unsafe.Sizeof(winapi.ServiceDelayedAutoStartInfo{}))
	}
	if unsafe.Sizeof(winapi.ServiceFailureActionsFlag{}) != 4 {
		t.Fatalf("Flag=%d", unsafe.Sizeof(winapi.ServiceFailureActionsFlag{}))
	}
}

// TestCHK_SETUP_03_ImagePathQuoted — CreateService ImagePath is quoted.
func TestCHK_SETUP_03_ImagePathQuoted(t *testing.T) {
	p := ServiceImagePath(`C:\Program Files\ExchangeChecker\ExchangeCheckerSvc.exe`)
	if p != `"C:\Program Files\ExchangeChecker\ExchangeCheckerSvc.exe"` {
		t.Fatal(p)
	}
	if ExePathFromImagePath(p) != `C:\Program Files\ExchangeChecker\ExchangeCheckerSvc.exe` {
		t.Fatal(ExePathFromImagePath(p))
	}
}

// TestCHK_SETUP_03_ServiceSDDL — constant matches PLAN §2.3 / decision.
func TestCHK_SETUP_03_ServiceSDDL(t *testing.T) {
	if ServiceSDDL == "" || DataDirSDDL == "" || InstallDirSDDL == "" {
		t.Fatal("empty SDDL")
	}
	if want := "D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"; DataDirSDDL != want {
		t.Fatalf("DataDirSDDL=%q", DataDirSDDL)
	}
	if want := "D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FRFX;;;BU)"; InstallDirSDDL != want {
		t.Fatalf("InstallDirSDDL=%q", InstallDirSDDL)
	}
}
