//go:build windows

package wininstall

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/third_party/inpout"
)

// TestCHK_SETUP_20_RealSCMDriver — the real SCM on the windows runner:
// CreateDriverService registers SERVICE_KERNEL_DRIVER / DEMAND_START with
// the NT ImagePath and DriverSDDL, never started; UpdateDriverService is
// idempotent and moves ImagePath; a second Create fails; Stop on a
// stopped driver is success; Delete removes it. The driver is never
// loaded on the runner.
func TestCHK_SETUP_20_RealSCMDriver(t *testing.T) {
	requireSetupE2E(t)
	if admin, _ := winapi.ProcessIsAdmin(); !admin {
		t.Skip("admin required")
	}
	var rnd [4]byte
	_, _ = rand.Read(rnd[:])
	name := "ExchangeCheckerInpOutTest-" + hex.EncodeToString(rnd[:])
	dir := winapi.LongPath(t.TempDir())
	if err := os.MkdirAll(DriverDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(DriverFilePath(dir), inpout.SysFile, 0o644); err != nil {
		t.Fatal(err)
	}
	sys := NewSystem()
	spec := DriverSpecFor(dir)
	spec.Name = name
	if err := sys.CreateDriverService(spec); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sys.DeleteDriverService(name) })
	check := func(wantDir string) {
		t.Helper()
		scm, err := winapi.OpenSCManager(winapi.SCManagerConnect)
		if err != nil {
			t.Fatal(err)
		}
		defer scm.Close()
		svc, err := winapi.OpenService(scm, name, winapi.ServiceQueryConfig|winapi.ServiceQueryStatus|winapi.ServiceReadControl)
		if err != nil {
			t.Fatal(err)
		}
		defer svc.Close()
		cfg, err := winapi.QueryServiceConfig(svc)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ServiceType != winapi.ServiceKernelDriver || cfg.StartType != winapi.ServiceDemandStart || cfg.ErrorControl != winapi.ServiceErrorNormal {
			t.Fatalf("CHK-SETUP-20: type=%d start=%d error=%d, want KERNEL_DRIVER(1) DEMAND_START(3) NORMAL(1)", cfg.ServiceType, cfg.StartType, cfg.ErrorControl)
		}
		if !strings.EqualFold(DriverPathFromImagePath(cfg.BinaryPath), DriverFilePath(wantDir)) {
			t.Fatalf("CHK-SETUP-20: ImagePath %q, want %q", cfg.BinaryPath, DriverImagePath(wantDir))
		}
		if cfg.DisplayName != DriverDisplayName {
			t.Fatalf("DisplayName %q", cfg.DisplayName)
		}
		state, _, err := winapi.QueryServiceStatus(svc)
		if err != nil || state != winapi.ServiceStopped {
			t.Fatalf("CHK-SETUP-20: driver must not be started (state=%d err=%v)", state, err)
		}
		sddl, err := winapi.ServiceDACL(svc)
		if err != nil {
			t.Fatal(err)
		}
		for _, ace := range strings.SplitAfter(sddl, ")") {
			if strings.Contains(ace, "RP") && !strings.HasSuffix(ace, ";;;SY)") && !strings.HasSuffix(ace, ";;;BA)") {
				t.Fatalf("CHK-SETUP-20: driver service DACL lets %s start it (%s)", ace, sddl)
			}
		}
		info, err := sys.QueryDriverService(name)
		if err != nil || !info.Exists || !info.KernelDriver || !info.DemandStart || info.Running {
			t.Fatalf("QueryDriverService %+v %v", info, err)
		}
		t.Logf("PASS CHK-SETUP-20 SCM: %s type=%d start=%d ImagePath=%s DACL=%s", name, cfg.ServiceType, cfg.StartType, cfg.BinaryPath, sddl)
	}
	check(dir)
	// negative: a second create of the same name fails
	if err := sys.CreateDriverService(spec); err == nil {
		t.Fatal("CHK-SETUP-20: second CreateDriverService succeeded")
	}
	// idempotent update, then moved ImagePath
	if err := sys.UpdateDriverService(spec); err != nil {
		t.Fatal(err)
	}
	check(dir)
	dir2 := filepath.Join(dir, "moved")
	spec2 := DriverSpecFor(dir2)
	spec2.Name = name
	if err := sys.UpdateDriverService(spec2); err != nil {
		t.Fatal(err)
	}
	check(dir2)
	if err := sys.StopDriverService(name); err != nil {
		t.Fatalf("stop of a stopped driver: %v", err)
	}
	if err := sys.DeleteDriverService(name); err != nil {
		t.Fatal(err)
	}
	if info, err := sys.QueryDriverService(name); err != nil || info.Exists {
		t.Fatalf("CHK-SETUP-20: driver service still present after delete: %+v %v", info, err)
	}
	// missing service: stop / delete are success (idempotent uninstall)
	if err := sys.StopDriverService(name); err != nil {
		t.Fatalf("stop missing: %v", err)
	}
	if err := sys.DeleteDriverService(name); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}
