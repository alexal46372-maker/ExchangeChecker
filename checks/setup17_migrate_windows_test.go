//go:build windows

package checks_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wininstall"
)

func runSetupExe(t *testing.T, exe string, extraEnv []string, args ...string) int {
	t.Helper()
	cmd := exec.Command(exe, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
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

func readTrayRun(t *testing.T) (string, bool) {
	t.Helper()
	s, ok, err := winapi.RegGetStringView(winapi.HKEY_LOCAL_MACHINE, config.RunKeyPath, config.TrayRunValue, winapi.KEY_WOW64_64KEY)
	if err != nil {
		t.Fatalf("ReadTrayRun: %v", err)
	}
	return s, ok
}

// TestCHK_SETUP_17_MigrateE2E: silent install into …\ExchangeChecker, then
// upgrade migrates to …\Exchange Checker. Also covers mid-migrate rollback
// (EC_MIGRATE_FAIL_AFTER) and uninstall cleanup. Gated like CHK-SETUP-12.
func TestCHK_SETUP_17_MigrateE2E(t *testing.T) {
	if os.Getenv("EC_SETUP_E2E") != "1" {
		t.Skip("set EC_SETUP_E2E=1")
	}
	setup := os.Getenv("EC_SETUP_EXE")
	setupV2 := os.Getenv("EC_SETUP_EXE_V2")
	if setup == "" || setupV2 == "" {
		t.Fatal("EC_SETUP_EXE and EC_SETUP_EXE_V2 required")
	}
	if admin, _ := winapi.ProcessIsAdmin(); !admin {
		t.Fatal("admin required")
	}

	rootArg := filepath.Join(os.TempDir(), "ExchangeCheckerMigrateE2E")
	_ = os.RemoveAll(rootArg)
	if err := os.MkdirAll(rootArg, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyArg := filepath.Join(rootArg, config.LegacyInstallDirName)
	data := config.DefaultDir()
	_ = os.MkdirAll(data, 0o755)
	cfgPath := filepath.Join(data, "settings.json")
	_ = os.Remove(cfgPath)
	if err := os.WriteFile(cfgPath, []byte(`{"trayAutostart":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgBefore, _ := os.ReadFile(cfgPath)

	run := func(exe string, args ...string) int { return runSetupExe(t, exe, nil, args...) }
	runFailEnv := func(exe string, args ...string) int {
		return runSetupExe(t, exe, []string{"EC_MIGRATE_FAIL_AFTER=1"}, args...)
	}

	log1 := filepath.Join(os.TempDir(), "ec-setup17-install.log")
	code := run(setup, "/S", "/DIR="+legacyArg, "/LOG="+log1, "/LANG=ru")
	if code != 0 {
		dumpLog(t, log1)
		t.Fatalf("legacy install exit %d", code)
	}
	legacy := winapi.LongPath(legacyArg)
	uninst := filepath.Join(legacy, config.UninstallExeName)
	sys := wininstall.NewSystem()
	t.Cleanup(func() {
		if inst, err := sys.ReadInstalled(); err == nil && inst.Present {
			u := filepath.Join(inst.Dir, config.UninstallExeName)
			_ = run(u, "/uninstall", "/S", "/PURGEDATA", "/LOG="+filepath.Join(os.TempDir(), "ec-setup17-cleanup.log"))
		} else if _, err := os.Stat(uninst); err == nil {
			_ = run(uninst, "/uninstall", "/S", "/PURGEDATA")
		}
		_ = os.RemoveAll(rootArg)
	})

	keep := filepath.Join(legacy, "keep-me.txt")
	if err := os.WriteFile(keep, []byte("userdata"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(legacy, "extra", "note.txt")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	desk, err := winapi.SHGetFolderPath(winapi.CSIDL_COMMON_DESKTOPDIRECTORY)
	if err != nil {
		t.Fatal(err)
	}
	progs, err := winapi.SHGetFolderPath(winapi.CSIDL_COMMON_PROGRAMS)
	if err != nil {
		t.Fatal(err)
	}
	legacyLnk := filepath.Join(desk, wininstall.LegacyShortcutFile)
	legacyStartDir := filepath.Join(progs, wininstall.LegacyStartMenuFolder)
	_ = os.Remove(legacyLnk)
	_ = os.MkdirAll(legacyStartDir, 0o755)
	_ = os.WriteFile(filepath.Join(legacyStartDir, wininstall.LegacyShortcutFile), []byte("x"), 0o644)
	tray := filepath.Join(legacy, config.TrayExeName)
	if err := winapi.WithCOM(func() error {
		return winapi.CreateShortcut(legacyLnk, winapi.ShortcutSpec{
			Target: tray, WorkDir: legacy, Description: "legacy", Icon: tray, IconIndex: 0,
		})
	}); err != nil {
		t.Fatalf("create legacy lnk: %v", err)
	}

	info0, err := sys.QueryService(model.ServiceName)
	if err != nil || !info0.Exists || !info0.Running {
		t.Fatalf("service before migrate: %+v err=%v", info0, err)
	}
	oldImg := info0.ImagePath
	oldRun, oldRunOK := readTrayRun(t)
	if !oldRunOK || !strings.Contains(strings.ToLower(oldRun), strings.ToLower(legacy)) {
		t.Fatalf("Run before migrate: %q ok=%v", oldRun, oldRunOK)
	}

	// --- mid-migrate failure + rollback ---
	logFail := filepath.Join(os.TempDir(), "ec-setup17-fail.log")
	code = runFailEnv(setupV2, "/S", "/LOG="+logFail, "/LANG=ru")
	if code == 0 || code == 3010 {
		dumpLog(t, logFail)
		t.Fatalf("injected migrate failure must not succeed (exit %d)", code)
	}
	infoFail, err := sys.QueryService(model.ServiceName)
	if err != nil || !infoFail.Exists || !infoFail.Running {
		t.Fatalf("service after failed migrate: %+v err=%v", infoFail, err)
	}
	if !strings.EqualFold(strings.Trim(infoFail.ImagePath, `"`), strings.Trim(oldImg, `"`)) &&
		!strings.Contains(strings.ToLower(infoFail.ImagePath), strings.ToLower(filepath.Join(legacy, config.ServiceExeName))) {
		t.Fatalf("ImagePath after rollback=%q want legacy %q", infoFail.ImagePath, oldImg)
	}
	runFail, _ := readTrayRun(t)
	if !strings.Contains(strings.ToLower(runFail), strings.ToLower(legacy)) {
		t.Fatalf("Run after rollback=%q want legacy", runFail)
	}
	neuPath := filepath.Join(filepath.Dir(legacy), config.InstallDirName)
	newStart := filepath.Join(progs, wininstall.StartMenuFolder, wininstall.ShortcutFile)
	newDesk := filepath.Join(desk, wininstall.ShortcutFile)
	// New shortcuts must not point at new dir after rollback (may be absent or restored to legacy).
	if sc, err := readShortcut(t, newStart); err == nil {
		if strings.Contains(strings.ToLower(sc), strings.ToLower(neuPath)) {
			t.Fatalf("start shortcut still points at new dir after rollback: %s", sc)
		}
	}
	if sc, err := readShortcut(t, newDesk); err == nil {
		if strings.Contains(strings.ToLower(sc), strings.ToLower(neuPath)) {
			t.Fatalf("desktop shortcut still points at new dir after rollback: %s", sc)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("legacy keep-me missing after rollback: %v", err)
	}

	// --- successful migrate ---
	logU := filepath.Join(os.TempDir(), "ec-setup17-upgrade.log")
	code = run(setupV2, "/S", "/LOG="+logU, "/LANG=ru")
	if code != 0 && code != 3010 {
		dumpLog(t, logU)
		t.Fatalf("migrate upgrade exit %d", code)
	}

	inst2, err := sys.ReadInstalled()
	if err != nil || !inst2.Present {
		t.Fatalf("installed after migrate: %+v %v", inst2, err)
	}
	if filepath.Base(inst2.Dir) != config.InstallDirName {
		t.Fatalf("InstallLocation base=%q want %q (dir=%q)", filepath.Base(inst2.Dir), config.InstallDirName, inst2.Dir)
	}
	info, err := sys.QueryService(model.ServiceName)
	if err != nil || !info.Exists || !info.Running {
		t.Fatalf("service after migrate: %+v err=%v", info, err)
	}
	wantImg := filepath.Join(inst2.Dir, config.ServiceExeName)
	if !strings.Contains(strings.ToLower(info.ImagePath), strings.ToLower(wantImg)) {
		t.Fatalf("ImagePath=%q want contain %q", info.ImagePath, wantImg)
	}
	runNew, ok := readTrayRun(t)
	if !ok || !strings.Contains(strings.ToLower(runNew), strings.ToLower(inst2.Dir)) {
		t.Fatalf("Run after migrate=%q want new dir %q", runNew, inst2.Dir)
	}
	cfgAfter, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config missing: %v", err)
	}
	if string(cfgAfter) != string(cfgBefore) {
		t.Fatalf("config changed: %q -> %q", cfgBefore, cfgAfter)
	}
	if _, err := os.Stat(filepath.Join(inst2.Dir, "keep-me.txt")); err != nil {
		t.Fatalf("migrated leftover missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(inst2.Dir, "extra", "note.txt")); err != nil {
		t.Fatalf("migrated nested leftover missing: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy folder must be gone, got err=%v", err)
	}
	if _, err := os.Stat(legacyLnk); !os.IsNotExist(err) {
		t.Fatalf("legacy desktop lnk still present: %v", err)
	}
	if _, err := os.Stat(legacyStartDir); !os.IsNotExist(err) {
		// allow non-empty foreign files? DeleteLegacyShortcuts removes dir if empty
		if ents, e := os.ReadDir(legacyStartDir); e == nil && len(ents) > 0 {
			t.Fatalf("legacy Start Menu folder still has entries: %v", ents)
		}
	}
	scStart, err := readShortcut(t, filepath.Join(progs, wininstall.StartMenuFolder, wininstall.ShortcutFile))
	if err != nil {
		t.Fatalf("start shortcut after migrate: %v", err)
	}
	if !strings.Contains(strings.ToLower(scStart), strings.ToLower(inst2.Dir)) {
		t.Fatalf("start shortcut target %q", scStart)
	}

	// --- uninstall migrated product ---
	un2 := filepath.Join(inst2.Dir, config.UninstallExeName)
	logUn := filepath.Join(os.TempDir(), "ec-setup17-uninstall.log")
	code = run(un2, "/uninstall", "/S", "/PURGEDATA", "/LOG="+logUn)
	if code != 0 && code != 3010 {
		dumpLog(t, logUn)
		t.Fatalf("uninstall exit %d", code)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		inf, err := sys.QueryService(model.ServiceName)
		if err != nil || !inf.Exists {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if inf, err := sys.QueryService(model.ServiceName); err == nil && inf.Exists {
		t.Fatalf("service still present after uninstall: %+v", inf)
	}
	if _, ok := readTrayRun(t); ok {
		t.Fatal("Run value still present after uninstall")
	}
	if inst, err := sys.ReadInstalled(); err == nil && inst.Present {
		t.Fatalf("uninstall key still present: %+v", inst)
	}
	if _, err := os.Stat(filepath.Join(progs, wininstall.StartMenuFolder, wininstall.ShortcutFile)); !os.IsNotExist(err) {
		t.Fatalf("start shortcut remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(desk, wininstall.ShortcutFile)); !os.IsNotExist(err) {
		t.Fatalf("desktop shortcut remains: %v", err)
	}
	if _, err := os.Stat(inst2.Dir); !os.IsNotExist(err) {
		// delayed delete of uninstall exe may leave the dir briefly; ensure no payload
		if ents, e := os.ReadDir(inst2.Dir); e == nil {
			for _, e := range ents {
				for _, n := range wininstall.PayloadFiles {
					if strings.EqualFold(e.Name(), n) {
						t.Fatalf("install dir still has %s", n)
					}
				}
			}
		}
	}
}

func readShortcut(t *testing.T, path string) (string, error) {
	t.Helper()
	var target string
	err := winapi.WithCOM(func() error {
		sc, err := winapi.ReadShortcut(path)
		if err != nil {
			return err
		}
		target = sc.Target
		return nil
	})
	return target, err
}
