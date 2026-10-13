//go:build windows

package checks_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	// Close SCM handles before uninstall: an open handle keeps a
	// mark-for-delete service visible in the SCM database.
	func() {
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
		fa := (*winapi.ServiceFailureActionsW)(unsafe.Pointer(&faBuf[0]))
		if fa.ResetPeriod != 86400 {
			t.Fatalf("failure ResetPeriod=%d want 86400", fa.ResetPeriod)
		}
		if fa.ActionsLen != 3 || fa.Actions == nil {
			t.Fatalf("failure actions: len=%d ptr=%v", fa.ActionsLen, fa.Actions)
		}
		actions := unsafe.Slice(fa.Actions, fa.ActionsLen)
		wantDelays := []uint32{60000, 120000, 300000}
		for i, want := range wantDelays {
			if actions[i].Type != winapi.SCActionRestart || actions[i].Delay != want {
				t.Fatalf("failure action[%d]=%+v want RESTART delay=%d", i, actions[i], want)
			}
		}
		sddl, err := winapi.ServiceDACL(svc)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("service SDDL: %s", sddl)
		if !strings.Contains(sddl, "AU") && !strings.Contains(sddl, "S-1-5-11") {
			t.Fatalf("service DACL missing Authenticated Users: %s", sddl)
		}
	}()

	// folder ACLs
	dataDACL, err := winapi.FileDACL(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("data DACL: %s", dataDACL)
	assertProtectedSYBA(t, "ProgramData", dataDACL)
	instDACL, err := winapi.FileDACL(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("install DACL: %s", instDACL)
	assertInstallDACL(t, instDACL)

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

	// --- upgrade to V2 (CHK-SETUP-12 / 7c) ---
	setupV2 := os.Getenv("EC_SETUP_EXE_V2")
	if setupV2 == "" {
		t.Fatal("EC_SETUP_EXE_V2 required for upgrade E2E")
	}
	hashData := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		_ = filepath.Walk(data, func(path string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(data, path)
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "logs/") || strings.EqualFold(filepath.Ext(rel), ".log") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			sum := sha256.Sum256(b)
			out[rel] = hex.EncodeToString(sum[:])
			return nil
		})
		return out
	}
	seed := filepath.Join(data, "settings.json")
	if _, err := os.Stat(seed); err != nil {
		if err := os.WriteFile(seed, []byte(`{"trayAutostart":true,"language":"en"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := hashData()
	if len(before) == 0 {
		t.Fatal("ProgramData empty before upgrade")
	}
	if inst.Version != "1.0.0" {
		t.Fatalf("V1 DisplayVersion=%q want 1.0.0", inst.Version)
	}
	// Start V1 tray from the install folder; upgrade must terminate it.
	trayPath := filepath.Join(dir, config.TrayExeName)
	trayProc, err := os.StartProcess(trayPath, []string{trayPath}, &os.ProcAttr{
		Dir:   dir,
		Files: []*os.File{nil, nil, nil},
	})
	if err != nil {
		t.Fatalf("start V1 tray: %v", err)
	}
	trayPID := trayProc.Pid
	t.Cleanup(func() { _ = trayProc.Kill(); _, _ = trayProc.Wait() })
	time.Sleep(500 * time.Millisecond)

	logU := filepath.Join(os.TempDir(), "ec-setup-e2e-upgrade.log")
	code = run(setupV2, "/S", "/DIR="+dir, "/LOG="+logU, "/LANG=en")
	if code != 0 {
		dumpLog(t, logU)
		t.Fatalf("upgrade exit %d; log %s", code, logU)
	}
	// tray must be gone (StopTray during upgrade)
	deadline := time.Now().Add(10 * time.Second)
	for {
		left, err := winapi.ProcessesInDir(dir, config.TrayExeName)
		if err != nil {
			t.Fatal(err)
		}
		alive := false
		for _, p := range left {
			if int(p.PID) == trayPID {
				alive = true
			}
		}
		if !alive {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("V1 tray pid %d still running after upgrade", trayPID)
		}
		time.Sleep(200 * time.Millisecond)
	}
	_, _ = trayProc.Wait()
	after := hashData()
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("ProgramData %s hash changed: %s -> %s", k, v, after[k])
		}
	}
	inst2, err := sys.ReadInstalled()
	if err != nil || !inst2.Present {
		t.Fatalf("uninstall key after upgrade: %+v %v", inst2, err)
	}
	if inst2.Version != "1.1.0" {
		t.Fatalf("DisplayVersion after V2=%q want 1.1.0", inst2.Version)
	}
	info2, err := sys.QueryService(model.ServiceName)
	if err != nil || !info2.Exists || !info2.Running {
		t.Fatalf("service after upgrade: %+v err=%v", info2, err)
	}
	if !info2.AutoStart {
		t.Fatal("start type not preserved (want AUTO) after upgrade")
	}
	t.Logf("upgraded 1.0.0 -> %s; tray terminated; ProgramData hashes stable (%d files)", inst2.Version, len(before))

	// Real rollback: upgrade with a non-service binary as ExchangeCheckerSvc.exe
	v2Svc, err := os.ReadFile(filepath.Join(dir, config.ServiceExeName))
	if err != nil {
		t.Fatal(err)
	}
	v2Sum := sha256.Sum256(v2Svc)
	badFiles := map[string][]byte{}
	for _, n := range wininstall.PayloadFiles {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		badFiles[n] = b
	}
	badFiles[config.ServiceExeName] = badFiles[config.TrayExeName] // not a service
	res := wininstall.Install(sys, wininstall.InstallConfig{
		Opts:       wininstall.Options{Silent: true},
		DefaultDir: dir, DataDir: data, Files: badFiles,
		Version: "1.2.0", Lang: "en", Now: time.Now(),
	}, &e2eLog{t})
	if res.Code != wininstall.ExitError {
		t.Fatalf("bad upgrade code=%d err=%v (want exit 1 + rollback)", res.Code, res.Err)
	}
	inst3, err := sys.ReadInstalled()
	if err != nil || !inst3.Present || inst3.Version != "1.1.0" {
		t.Fatalf("after rollback DisplayVersion=%+v want 1.1.0: %v", inst3, err)
	}
	info3, err := sys.QueryService(model.ServiceName)
	if err != nil || !info3.Exists || !info3.Running {
		t.Fatalf("service after rollback: %+v err=%v", info3, err)
	}
	svcBytes, err := os.ReadFile(filepath.Join(dir, config.ServiceExeName))
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(svcBytes) != v2Sum {
		t.Fatal("svc file not restored to V2 image after rollback")
	}
	t.Logf("rollback ok: version=%s service running", inst3.Version)

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

type e2eLog struct{ t *testing.T }

func (e *e2eLog) Printf(format string, args ...interface{}) {
	e.t.Logf(format, args...)
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

// assertProtectedSYBA: DACL starts with D:P (protected)+AI; ACEs only SY and BA.
func assertProtectedSYBA(t *testing.T, label, sddl string) {
	t.Helper()
	if !strings.HasPrefix(sddl, "D:P") {
		t.Fatalf("%s DACL not protected (want D:P…): %s", label, sddl)
	}
	ctrlEnd := strings.Index(sddl, "(")
	if ctrlEnd < 0 {
		ctrlEnd = len(sddl)
	}
	if !strings.Contains(sddl[:ctrlEnd], "AI") {
		t.Fatalf("%s DACL missing AI (want P/AI): %s", label, sddl)
	}
	aces := parseSDDLACEs(sddl)
	if len(aces) == 0 {
		t.Fatalf("%s DACL has no ACEs: %s", label, sddl)
	}
	okSID := map[string]bool{
		"SY": true, "BA": true,
		"S-1-5-18": true, "S-1-5-32-544": true,
	}
	seenSY, seenBA := false, false
	for _, a := range aces {
		sid := strings.ToUpper(a.SID)
		if !okSID[sid] {
			t.Fatalf("%s DACL has non-SY/BA ACE %q: %s", label, a.SID, sddl)
		}
		if sid == "SY" || sid == "S-1-5-18" {
			seenSY = true
		}
		if sid == "BA" || sid == "S-1-5-32-544" {
			seenBA = true
		}
	}
	if !seenSY || !seenBA {
		t.Fatalf("%s DACL missing SY or BA: %s", label, sddl)
	}
}

// assertInstallDACL: protected P/AI; each ACE checked — BU only FRFX/0x1200a9 (no write).
func assertInstallDACL(t *testing.T, sddl string) {
	t.Helper()
	if !strings.HasPrefix(sddl, "D:P") {
		t.Fatalf("install DACL not protected: %s", sddl)
	}
	ctrlEnd := strings.Index(sddl, "(")
	if ctrlEnd < 0 {
		ctrlEnd = len(sddl)
	}
	if !strings.Contains(sddl[:ctrlEnd], "AI") {
		t.Fatalf("install DACL missing AI (want P/AI): %s", sddl)
	}
	aces := parseSDDLACEs(sddl)
	seenSY, seenBA, seenBU := false, false, false
	for _, a := range aces {
		sid := strings.ToUpper(a.SID)
		rights := strings.ToUpper(a.Rights)
		switch {
		case sid == "SY" || sid == "S-1-5-18":
			seenSY = true
		case sid == "BA" || sid == "S-1-5-32-544":
			seenBA = true
		case sid == "BU" || sid == "S-1-5-32-545":
			seenBU = true
			if rights != "FRFX" && rights != "0X1200A9" {
				t.Fatalf("install DACL Users ACE rights=%q want FRFX/0x1200a9: %s", a.Rights, sddl)
			}
			// reject any write-ish right tokens if rights were a list
			for _, bad := range []string{"FA", "FW", "GA", "GW", "WD", "WO", "SD", "WA", "WEA", "WC"} {
				if rights != "FRFX" && strings.Contains(rights, bad) {
					t.Fatalf("install DACL Users ACE has write right %s: %s", bad, sddl)
				}
			}
		default:
			t.Fatalf("install DACL unexpected ACE SID %q: %s", a.SID, sddl)
		}
	}
	if !seenSY || !seenBA || !seenBU {
		t.Fatalf("install DACL missing SY/BA/BU: %s", sddl)
	}
}

type sddlACE struct{ Rights, SID string }

func parseSDDLACEs(sddl string) []sddlACE {
	re := regexp.MustCompile(`\(([^;]*);([^;]*);([^;]*);([^;]*);([^;]*);([^)]+)\)`)
	m := re.FindAllStringSubmatch(sddl, -1)
	out := make([]sddlACE, 0, len(m))
	for _, g := range m {
		out = append(out, sddlACE{Rights: g[3], SID: g[6]})
	}
	return out
}
