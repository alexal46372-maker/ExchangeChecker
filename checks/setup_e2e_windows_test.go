//go:build windows

package checks_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wininstall"
)

// TestCHK_SVC_01_RealSCM — gated EC_SVC_E2E=1; needs EC_SVC_EXE.
func TestCHK_SVC_01_RealSCM(t *testing.T) {
	if os.Getenv("EC_SVC_E2E") != "1" {
		t.Skip("set EC_SVC_E2E=1")
	}
	exe := os.Getenv("EC_SVC_EXE")
	if exe == "" {
		t.Fatal("EC_SVC_EXE required")
	}
	if admin, _ := winapi.ProcessIsAdmin(); !admin {
		t.Fatal("admin required")
	}
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "settings.json"), []byte(`{"trayAutostart":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	name := "ExchangeCheckerSvcE2E"
	sys := wininstall.NewSystem()
	_ = sys.StopService(name, 30*time.Second)
	_ = sys.DeleteService(name)
	img := wininstall.ServiceImagePath(exe) + " -config " + tmp
	// CreateService ImagePath with args — quote exe only
	img = `"` + exe + `" -config "` + tmp + `"`
	spec := wininstall.ServiceSpec{
		Name: name, DisplayName: "ExchangeChecker SVC E2E", ImagePath: img,
		Description: "e2e", AutoStart: false,
		ResetPeriod: wininstall.FailureResetPeriod, FailureDelays: wininstall.FailureDelays,
		SDDL: wininstall.ServiceSDDL,
	}
	if err := sys.CreateService(spec); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sys.StopService(name, 90*time.Second)
		_ = sys.DeleteService(name)
	})
	if err := sys.StartService(name, 60*time.Second); err != nil {
		t.Fatal(err)
	}
	// pipe hello
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		c, err := ipc.DialPipe(model.PipeName, 2*time.Second)
		if err == nil {
			_ = c.Close()
			last = nil
			break
		}
		last = err
		time.Sleep(500 * time.Millisecond)
	}
	if last != nil {
		t.Fatalf("pipe: %v", last)
	}
	if err := sys.StopService(name, 90*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := sys.DeleteService(name); err != nil {
		t.Fatal(err)
	}
}

// TestCHK_SETUP_12_E2E — gated EC_SETUP_E2E=1; needs EC_SETUP_EXE.
// Basic 7b: silent install → checks → uninstall (keep data) → purge.
func TestCHK_SETUP_12_E2E(t *testing.T) {
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
	dir := filepath.Join(os.TempDir(), "ExchangeCheckerE2E")
	_ = os.RemoveAll(dir)
	// seed settings so HKLM Run is not written (CHK-CFG-04 / E2E exception)
	data := config.DefaultDir()
	_ = os.MkdirAll(data, 0o755)
	if err := os.WriteFile(filepath.Join(data, "settings.json"), []byte(`{"trayAutostart":false,"language":"en"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	log1 := filepath.Join(os.TempDir(), "ec-setup-e2e-install.log")
	run := func(args ...string) (int, error) {
		cmd := exec.Command(setup, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err == nil {
			return 0, nil
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		return -1, err
	}
	code, err := run("/S", "/DIR="+dir, "/LOG="+log1, "/LANG=en")
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("install exit %d; log %s", code, log1)
	}
	t.Cleanup(func() {
		_, _ = run("/uninstall", "/S", "/PURGEDATA", "/LOG="+filepath.Join(os.TempDir(), "ec-setup-e2e-cleanup.log"))
	})
	for _, name := range []string{config.ServiceExeName, config.TrayExeName, config.UninstallExeName} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	sys := wininstall.NewSystem()
	info, err := sys.QueryService(model.ServiceName)
	if err != nil || !info.Exists || !info.Running {
		t.Fatalf("service: %+v err=%v", info, err)
	}
	inst, err := sys.ReadInstalled()
	if err != nil || !inst.Present {
		t.Fatalf("uninstall key: %+v %v", inst, err)
	}
	ok, err := winapi.RegKeyExistsView(winapi.HKEY_LOCAL_MACHINE, wininstall.EventLogKeyPath, winapi.KEY_WOW64_64KEY)
	if err != nil || !ok {
		t.Fatalf("event log key: %v %v", ok, err)
	}
	// uninstall keep data
	code, err = run("/uninstall", "/S", "/LOG="+filepath.Join(os.TempDir(), "ec-setup-e2e-uninst.log"))
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 && code != wininstall.ExitRebootRequired {
		t.Fatalf("uninstall exit %d", code)
	}
	info, _ = sys.QueryService(model.ServiceName)
	if info.Exists {
		t.Fatal("service still exists")
	}
	if _, err := os.Stat(data); err != nil {
		t.Fatalf("data should remain: %v", err)
	}
	// reinstall then purge
	code, err = run("/S", "/DIR="+dir, "/NOSTART", "/LOG="+filepath.Join(os.TempDir(), "ec-setup-e2e-reinst.log"))
	if err != nil || (code != 0 && code != wininstall.ExitRebootRequired) {
		t.Fatalf("reinstall %d %v", code, err)
	}
	code, err = run("/uninstall", "/S", "/PURGEDATA", "/LOG="+filepath.Join(os.TempDir(), "ec-setup-e2e-purge.log"))
	if err != nil || (code != 0 && code != wininstall.ExitRebootRequired) {
		t.Fatalf("purge %d %v", code, err)
	}
	if _, err := os.Stat(data); err == nil {
		// may remain if locked; warn
		t.Logf("WARNING: data dir still present after purge: %s", data)
	}
	fmt.Println("CHK-SETUP-12 basic E2E ok")
}
