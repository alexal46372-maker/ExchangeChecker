//go:build windows

package checks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wininstall"
)

// TestCHK_SETUP_20_DriverE2E — the e2efault setup exe on the runner:
// (1) EC_SETUP_FAIL_AT=InpOutDriver → exit 0, WARNING in the log, no
// driver service / file, the product itself installed; (2)
// EC_SETUP_FAIL_AT=AfterInpOutDriver → failure, the driver service and
// file are rolled back with the rest; (3) a normal install registers
// KERNEL_DRIVER / DEMAND_START / ImagePath, not started, file = sha256;
// uninstall removes service, file and driver\. Gated like CHK-SETUP-12.
func TestCHK_SETUP_20_DriverE2E(t *testing.T) {
	if os.Getenv("EC_SETUP_E2E") != "1" {
		t.Skip("set EC_SETUP_E2E=1")
	}
	setup := os.Getenv("EC_SETUP_EXE")
	if setup == "" {
		t.Fatal("EC_SETUP_EXE required")
	}
	if admin, _ := winapi.ProcessIsAdmin(); !admin {
		t.Fatal("admin required")
	}
	dir := winapi.LongPath(filepath.Join(os.TempDir(), "ExchangeCheckerDrvE2E"))
	_ = os.RemoveAll(dir)
	sys := wininstall.NewSystem()
	uninstall := func(label string) {
		t.Helper()
		u := filepath.Join(dir, config.UninstallExeName)
		exe := setup
		if _, err := os.Stat(u); err == nil {
			exe = u
		}
		code := runSetupExe(t, exe, nil, "/uninstall", "/S", "/PURGEDATA", "/LOG="+filepath.Join(os.TempDir(), "ec-drv-e2e-un-"+label+".log"))
		if code != 0 && code != wininstall.ExitRebootRequired {
			t.Fatalf("%s: uninstall exit %d", label, code)
		}
	}
	t.Cleanup(func() { uninstall("cleanup"); _ = os.RemoveAll(dir) })
	noDriver := func(label string) {
		t.Helper()
		if info, err := sys.QueryDriverService(config.InpOutServiceName); err != nil || info.Exists {
			t.Fatalf("%s: driver service present: %+v %v", label, info, err)
		}
		if _, err := os.Stat(wininstall.DriverFilePath(dir)); !os.IsNotExist(err) {
			t.Fatalf("%s: driver file present: %v", label, err)
		}
	}
	readLog := func(p string) string {
		b, _ := os.ReadFile(p)
		return string(b)
	}

	// (1) driver step fails → WARNING, install OK
	log1 := filepath.Join(os.TempDir(), "ec-drv-e2e-1.log")
	if code := runSetupExe(t, setup, []string{"EC_SETUP_FAIL_AT=InpOutDriver"}, "/S", "/NOSTART", "/DIR="+dir, "/LOG="+log1); code != 0 {
		dumpLog(t, log1)
		t.Fatalf("(1) exit %d, want 0", code)
	}
	if l := readLog(log1); !strings.Contains(l, "WARNING") || !strings.Contains(l, "setup test fail at InpOutDriver") {
		dumpLog(t, log1)
		t.Fatal("(1) WARNING for the driver step missing in the log")
	}
	noDriver("(1)")
	if info, err := sys.QueryService(model.ServiceName); err != nil || !info.Exists {
		t.Fatalf("(1) product service missing: %+v %v", info, err)
	}
	uninstall("1")

	// (2) failure after the driver step → rollback incl. the driver
	log2 := filepath.Join(os.TempDir(), "ec-drv-e2e-2.log")
	if code := runSetupExe(t, setup, []string{"EC_SETUP_FAIL_AT=AfterInpOutDriver"}, "/S", "/NOSTART", "/DIR="+dir, "/LOG="+log2); code == 0 {
		dumpLog(t, log2)
		t.Fatal("(2) exit 0, want failure")
	}
	if l := readLog(log2); !strings.Contains(l, "driver service "+config.InpOutServiceName+" registered") {
		dumpLog(t, log2)
		t.Fatal("(2) the driver step must have completed before the injected failure")
	}
	noDriver("(2)")
	if info, err := sys.QueryService(model.ServiceName); err != nil || info.Exists {
		t.Fatalf("(2) product service not rolled back: %+v %v", info, err)
	}

	// (3) normal install + uninstall
	log3 := filepath.Join(os.TempDir(), "ec-drv-e2e-3.log")
	if code := runSetupExe(t, setup, nil, "/S", "/NOSTART", "/DIR="+dir, "/LOG="+log3); code != 0 {
		dumpLog(t, log3)
		t.Fatalf("(3) exit %d", code)
	}
	b, err := os.ReadFile(wininstall.DriverFilePath(dir))
	if err != nil || !wininstall.DriverHashOK(b) {
		t.Fatalf("(3) driver file: %v hashOK=%v", err, err == nil && wininstall.DriverHashOK(b))
	}
	func() {
		scm, err := winapi.OpenSCManager(winapi.SCManagerConnect)
		if err != nil {
			t.Fatal(err)
		}
		defer scm.Close()
		svc, err := winapi.OpenService(scm, config.InpOutServiceName, winapi.ServiceQueryConfig|winapi.ServiceQueryStatus)
		if err != nil {
			t.Fatalf("(3) OpenService %s: %v", config.InpOutServiceName, err)
		}
		defer svc.Close()
		cfg, err := winapi.QueryServiceConfig(svc)
		if err != nil {
			t.Fatal(err)
		}
		state, _, _ := winapi.QueryServiceStatus(svc)
		if cfg.ServiceType != winapi.ServiceKernelDriver || cfg.StartType != winapi.ServiceDemandStart ||
			!strings.EqualFold(wininstall.DriverPathFromImagePath(cfg.BinaryPath), filepath.Join(dir, config.InpOutRelPath)) ||
			state != winapi.ServiceStopped {
			t.Fatalf("(3) driver service type=%d start=%d img=%q state=%d", cfg.ServiceType, cfg.StartType, cfg.BinaryPath, state)
		}
		t.Logf("PASS CHK-SETUP-20 E2E install: %s KERNEL_DRIVER DEMAND_START %s, not started", config.InpOutServiceName, cfg.BinaryPath)
	}()
	uninstall("3")
	noDriver("(3) after uninstall")
	if _, err := os.Stat(wininstall.DriverDir(dir)); !os.IsNotExist(err) {
		t.Fatalf("(3) driver folder left after uninstall: %v", err)
	}
	t.Logf("PASS CHK-SETUP-20 E2E: fault InpOutDriver = WARNING + exit 0; fault AfterInpOutDriver = rollback of service and file; uninstall removes service, file and driver\\")
}
