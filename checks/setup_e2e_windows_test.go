//go:build windows

package checks_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

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
	img := `"` + exe + `" -config "` + tmp + `"`
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
	data := config.DefaultDir()
	_ = os.MkdirAll(data, 0o755)
	// Fresh install should write HKLM Run (default trayAutostart=true).
	// Remove any pre-seeded settings so defaults apply.
	_ = os.Remove(filepath.Join(data, "settings.json"))

	run := func(exe string, args ...string) int {
		t.Helper()
		cmd := exec.Command(exe, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err == nil {
			return 0
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		t.Fatalf("run %s: %v", exe, err)
		return -1
	}

	log1 := filepath.Join(os.TempDir(), "ec-setup-e2e-install.log")
	code := run(setup, "/S", "/DIR="+dir, "/LOG="+log1, "/LANG=en")
	if code != 0 {
		t.Fatalf("install exit %d; log %s", code, log1)
	}
	uninst := filepath.Join(dir, config.UninstallExeName)
	t.Cleanup(func() {
		if _, err := os.Stat(uninst); err == nil {
			_ = run(uninst, "/uninstall", "/S", "/PURGEDATA", "/LOG="+filepath.Join(os.TempDir(), "ec-setup-e2e-cleanup.log"))
		} else {
			_ = run(setup, "/uninstall", "/S", "/PURGEDATA")
		}
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
	if !info.AutoStart {
		t.Fatal("service start type is not AUTO")
	}

	// delayed auto-start, failure actions, service DACL
	scm, err := winapi.OpenSCManager(winapi.SCManagerConnect)
	if err != nil {
		t.Fatal(err)
	}
	defer scm.Close()
	svc, err := winapi.OpenService(scm, model.ServiceName, winapi.ServiceQueryConfig|winapi.ServiceQueryStatus|winapi.ServiceReadControl)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	delBuf, err := winapi.QueryServiceConfig2Bytes(svc, winapi.ServiceConfigDelayedAutoStart)
	if err != nil {
		t.Fatalf("delayed: %v", err)
	}
	if len(delBuf) < 4 || delBuf[0] == 0 {
		t.Fatalf("delayed auto-start not set: %v", delBuf)
	}
	faBuf, err := winapi.QueryServiceConfig2Bytes(svc, winapi.ServiceConfigFailureActions)
	if err != nil || len(faBuf) < int(unsafe.Sizeof(winapi.ServiceFailureActionsW{})) {
		t.Fatalf("failure actions: %v len=%d", err, len(faBuf))
	}
	// ResetPeriod at offset 0 (uint32) = 86400
	reset := uint32(faBuf[0]) | uint32(faBuf[1])<<8 | uint32(faBuf[2])<<16 | uint32(faBuf[3])<<24
	if reset != 86400 {
		t.Fatalf("failure ResetPeriod=%d want 86400", reset)
	}
	sddl, err := winapi.ServiceDACL(svc)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("service SDDL: %s", sddl)
	if !strings.Contains(sddl, "AU") && !strings.Contains(sddl, "S-1-5-11") {
		t.Fatalf("service DACL missing Authenticated Users: %s", sddl)
	}

	// folder ACLs
	dataDACL, err := winapi.FileDACL(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("data DACL: %s", dataDACL)
	if strings.Contains(dataDACL, "BU") || strings.Contains(dataDACL, "S-1-5-32-545") {
		t.Fatalf("Users on ProgramData: %s", dataDACL)
	}
	instDACL, err := winapi.FileDACL(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("install DACL: %s", instDACL)

	// Uninstall key: no Publisher; QuietUninstallString present
	view := uint32(winapi.KEY_WOW64_64KEY)
	inst, err := sys.ReadInstalled()
	if err != nil || !inst.Present {
		t.Fatalf("uninstall key: %+v %v", inst, err)
	}
	pub, pubOK, err := winapi.RegGetStringView(winapi.HKEY_LOCAL_MACHINE, wininstall.UninstallKeyPath, "Publisher", view)
	if err != nil {
		t.Fatal(err)
	}
	if pubOK && pub != "" {
		t.Fatalf("Publisher must be absent, got %q", pub)
	}
	quiet, ok, err := winapi.RegGetStringView(winapi.HKEY_LOCAL_MACHINE, wininstall.UninstallKeyPath, "QuietUninstallString", view)
	if err != nil || !ok || !strings.Contains(quiet, "/uninstall /S") {
		t.Fatalf("QuietUninstallString=%q ok=%v err=%v", quiet, ok, err)
	}

	// Event Log source
	ok, err = winapi.RegKeyExistsView(winapi.HKEY_LOCAL_MACHINE, wininstall.EventLogKeyPath, view)
	if err != nil || !ok {
		t.Fatalf("event log key: %v %v", ok, err)
	}

	// HKLM Run present (default trayAutostart=true)
	runVal, runOK, err := winapi.RegGetStringView(winapi.HKEY_LOCAL_MACHINE, config.RunKeyPath, config.TrayRunValue, view)
	if err != nil {
		t.Fatal(err)
	}
	wantRun, _ := config.TrayRunCommand(filepath.Join(dir, config.ServiceExeName))
	if !runOK || runVal != wantRun {
		t.Fatalf("HKLM Run=%q ok=%v want %q", runVal, runOK, wantRun)
	}

	// uninstall via installed ExchangeCheckerUninstall.exe (keep data)
	uninstLog := filepath.Join(os.TempDir(), "ec-setup-e2e-uninst.log")
	code = run(uninst, "/uninstall", "/S", "/LOG="+uninstLog)
	if code != 0 && code != wininstall.ExitRebootRequired {
		dumpLog(t, uninstLog)
		t.Fatalf("uninstall exit %d", code)
	}
	if err := waitServiceGone(t, sys, model.ServiceName, 60*time.Second); err != nil {
		dumpLog(t, uninstLog)
		t.Fatal(err)
	}
	if _, err := os.Stat(data); err != nil {
		t.Fatalf("data should remain without /PURGEDATA: %v", err)
	}
	runVal, runOK, err = winapi.RegGetStringView(winapi.HKEY_LOCAL_MACHINE, config.RunKeyPath, config.TrayRunValue, view)
	if err != nil {
		t.Fatal(err)
	}
	if runOK {
		t.Fatalf("HKLM Run still present after uninstall: %q", runVal)
	}

	// reinstall then purge via uninstall.exe
	code = run(setup, "/S", "/DIR="+dir, "/NOSTART", "/LOG="+filepath.Join(os.TempDir(), "ec-setup-e2e-reinst.log"))
	if code != 0 && code != wininstall.ExitRebootRequired {
		t.Fatalf("reinstall %d", code)
	}
	uninst = filepath.Join(dir, config.UninstallExeName)
	code = run(uninst, "/uninstall", "/S", "/PURGEDATA", "/LOG="+filepath.Join(os.TempDir(), "ec-setup-e2e-purge.log"))
	if code != 0 && code != wininstall.ExitRebootRequired {
		t.Fatalf("purge uninstall %d", code)
	}
	if _, err := os.Stat(data); err == nil {
		t.Errorf("data dir still present after /PURGEDATA: %s", data)
	}
}

func waitServiceGone(t *testing.T, sys wininstall.System, name string, d time.Duration) error {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		info, err := sys.QueryService(name)
		if err != nil {
			return err
		}
		if !info.Exists {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("service %s still exists after uninstall", name)
}

func dumpLog(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Logf("log %s: %v", path, err)
		return
	}
	t.Logf("---- %s ----\n%s", path, string(b))
}
