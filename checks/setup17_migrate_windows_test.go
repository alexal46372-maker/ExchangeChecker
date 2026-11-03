//go:build windows

package checks_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wininstall"
)

// TestCHK_SETUP_17_MigrateE2E: silent install into …\ExchangeChecker, then
// upgrade migrates to …\Exchange Checker. Gated like CHK-SETUP-12.
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

	log1 := filepath.Join(os.TempDir(), "ec-setup17-install.log")
	code := run(setup, "/S", "/DIR="+legacyArg, "/LOG="+log1, "/LANG=ru")
	if code != 0 {
		dumpLog(t, log1)
		t.Fatalf("legacy install exit %d", code)
	}
	legacy := winapi.LongPath(legacyArg)
	uninst := filepath.Join(legacy, config.UninstallExeName)
	t.Cleanup(func() {
		sys := wininstall.NewSystem()
		if inst, err := sys.ReadInstalled(); err == nil && inst.Present {
			u := filepath.Join(inst.Dir, config.UninstallExeName)
			_ = run(u, "/uninstall", "/S", "/PURGEDATA", "/LOG="+filepath.Join(os.TempDir(), "ec-setup17-cleanup.log"))
		} else if _, err := os.Stat(uninst); err == nil {
			_ = run(uninst, "/uninstall", "/S", "/PURGEDATA")
		}
		_ = os.RemoveAll(rootArg)
	})

	// Leftover file inside legacy install folder (must migrate).
	keep := filepath.Join(legacy, "keep-me.txt")
	if err := os.WriteFile(keep, []byte("userdata"), 0o644); err != nil {
		t.Fatal(err)
	}
	desk, err := winapi.SHGetFolderPath(winapi.CSIDL_COMMON_DESKTOPDIRECTORY)
	if err != nil {
		t.Fatal(err)
	}
	legacyLnk := filepath.Join(desk, wininstall.LegacyShortcutFile)
	_ = os.Remove(legacyLnk)
	tray := filepath.Join(legacy, config.TrayExeName)
	if err := winapi.WithCOM(func() error {
		return winapi.CreateShortcut(legacyLnk, winapi.ShortcutSpec{
			Target: tray, WorkDir: legacy, Description: "legacy", Icon: tray, IconIndex: 0,
		})
	}); err != nil {
		t.Fatalf("create legacy lnk: %v", err)
	}
	defer os.Remove(legacyLnk)

	logU := filepath.Join(os.TempDir(), "ec-setup17-upgrade.log")
	code = run(setupV2, "/S", "/LOG="+logU, "/LANG=ru")
	if code != 0 && code != 3010 {
		dumpLog(t, logU)
		t.Fatalf("migrate upgrade exit %d", code)
	}

	sys := wininstall.NewSystem()
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
	if _, err := os.Stat(legacyLnk); !os.IsNotExist(err) {
		t.Fatalf("legacy desktop lnk still present: %v", err)
	}
	if ents, err := os.ReadDir(legacy); err == nil {
		for _, e := range ents {
			for _, n := range wininstall.PayloadFiles {
				if strings.EqualFold(e.Name(), n) {
					t.Fatalf("legacy still has payload %s", n)
				}
			}
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat legacy: %v", err)
	}
}
