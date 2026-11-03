package wininstall

import (
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
)

func TestCHK_SETUP_17_LegacyDirHelpers(t *testing.T) {
	if config.InstallDirName != "Exchange Checker" {
		t.Fatalf("InstallDirName=%q", config.InstallDirName)
	}
	if config.LegacyInstallDirName != "ExchangeChecker" {
		t.Fatalf("LegacyInstallDirName=%q", config.LegacyInstallDirName)
	}
	legacy := `C:\Program Files\ExchangeChecker`
	if !IsLegacyInstallDir(legacy) {
		t.Fatal("expected legacy")
	}
	if err := ValidateLegacyMigrateDir(legacy); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLegacyMigrateDir(`C:\`); err == nil {
		t.Fatal("drive root must be rejected")
	}
	if IsLegacyInstallDir(`C:\Program Files\Exchange Checker`) {
		t.Fatal("two-word must not be legacy")
	}
	got := MigratedInstallDir(legacy)
	want := `C:\Program Files\Exchange Checker`
	if got != want {
		t.Fatalf("MigratedInstallDir=%q want %q", got, want)
	}
	if version.ProductName != "Exchange Checker" {
		t.Fatalf("ProductName=%q", version.ProductName)
	}
	if ShortcutFile != "Exchange Checker.lnk" || StartMenuFolder != "Exchange Checker" {
		t.Fatalf("shortcut names %q / %q", StartMenuFolder, ShortcutFile)
	}
}

func TestCHK_SETUP_17_MigrateOnUpgrade(t *testing.T) {
	f := newFake()
	legacy := `C:\Program Files\ExchangeChecker`
	neu := `C:\Program Files\Exchange Checker`
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: legacy}
	f.Service = ServiceInfo{
		Exists: true, Running: true,
		ImagePath: `"C:\Program Files\ExchangeChecker\ExchangeCheckerSvc.exe"`,
		AutoStart: true, DisplayName: "Exchange Checker", DescriptionOK: true,
		Description: "Exchange Checker",
	}
	f.Files[joinDir(legacy, config.ServiceExeName)] = []byte("old-svc")
	f.Files[joinDir(legacy, config.TrayExeName)] = []byte("old-tray")
	f.Files[joinDir(legacy, config.UninstallExeName)] = []byte("old-un")
	f.Files[joinDir(legacy, "keep-me.txt")] = []byte("userdata")
	f.Files[joinDir(legacy, `notes\readme.txt`)] = []byte("notes")
	f.LegacyShortcut = true
	f.TrayRunCmd = `"C:\Program Files\ExchangeChecker\ExchangeCheckerTray.exe"`
	f.Uninst = UninstallValues(legacy, joinDir(legacy, config.UninstallExeName), joinDir(legacy, config.TrayExeName), "1.0.0", 1, time.Now(), true)

	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: neu, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Lang: "ru", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK && res.Code != ExitRebootRequired {
		t.Fatalf("code=%d err=%v", res.Code, res.Err)
	}
	if res.Dir != neu {
		t.Fatalf("Dir=%q want %q", res.Dir, neu)
	}
	steps := strings.Join(f.Steps, "\n")
	if !strings.Contains(steps, "MigrateInstallDir|"+legacy+"|"+neu) {
		t.Fatalf("missing MigrateInstallDir: %s", steps)
	}
	if !strings.Contains(steps, "DeleteLegacyShortcuts") {
		t.Fatalf("missing DeleteLegacyShortcuts: %s", steps)
	}
	if !strings.Contains(steps, "RemoveLegacyInstallDir|"+legacy) {
		t.Fatalf("legacy dir not removed: %s", steps)
	}
	if strings.Contains(steps, "RemoveDirIfEmpty|"+legacy) {
		t.Fatal("must use RemoveLegacyInstallDir, not RemoveDirIfEmpty")
	}
	found := false
	for _, s := range f.Steps {
		if strings.Contains(s, "UpdateService|") && strings.Contains(s, neu) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("UpdateService not targeting new dir: %v", f.Steps)
	}
	if _, ok := f.Files[joinDir(neu, "keep-me.txt")]; !ok {
		t.Fatal("non-payload file was not migrated")
	}
	if _, ok := f.Files[joinDir(neu, `notes\readme.txt`)]; !ok {
		t.Fatal("nested non-payload was not migrated")
	}
	if _, ok := f.Files[joinDir(legacy, "keep-me.txt")]; ok {
		t.Fatal("legacy keep-me still present after RemoveLegacyInstallDir")
	}
	for _, v := range f.Uninst {
		if v.Name == "InstallLocation" && v.Str != neu {
			t.Fatalf("InstallLocation=%q", v.Str)
		}
	}
	if f.LegacyShortcut {
		t.Fatal("legacy shortcut flag still set")
	}
	if !strings.Contains(steps, "ReadTrayRun") {
		t.Fatal("expected ReadTrayRun before ApplyTrayRun")
	}
}

func TestCHK_SETUP_17_MigrateRollback(t *testing.T) {
	f := newFake()
	legacy := `C:\Program Files\ExchangeChecker`
	neu := `C:\Program Files\Exchange Checker`
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: legacy}
	f.Service = ServiceInfo{
		Exists: true, Running: true,
		ImagePath: `"C:\Program Files\ExchangeChecker\ExchangeCheckerSvc.exe"`,
		AutoStart: true, DisplayName: "Exchange Checker", DescriptionOK: true,
		Description: "old",
	}
	f.Files[joinDir(legacy, config.ServiceExeName)] = []byte("old-svc")
	f.Files[joinDir(legacy, config.TrayExeName)] = []byte("old-tray")
	f.Files[joinDir(legacy, config.UninstallExeName)] = []byte("old-un")
	f.Files[joinDir(legacy, "keep-me.txt")] = []byte("userdata")
	f.TrayRunCmd = `"C:\Program Files\ExchangeChecker\ExchangeCheckerTray.exe"`
	f.Uninst = UninstallValues(legacy, joinDir(legacy, config.UninstallExeName), joinDir(legacy, config.TrayExeName), "1.0.0", 1, time.Now(), true)
	f.FailAt = "StartService|"
	f.FailOnce = true

	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: neu, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Lang: "ru", Now: time.Now(),
	}, &memLog{})
	if res.Code == ExitOK {
		t.Fatal("expected failure")
	}
	steps := strings.Join(f.Steps, "\n")
	if strings.Contains(steps, "RemoveLegacyInstallDir|") {
		t.Fatal("must not delete legacy folder before commit")
	}
	// Service ImagePath restored to legacy
	restored := false
	for _, s := range f.Steps {
		if strings.HasPrefix(s, "UpdateService|") && strings.Contains(s, legacy) {
			restored = true
		}
	}
	if !restored {
		t.Fatalf("expected UpdateService rollback to legacy: %s", steps)
	}
	if !strings.Contains(steps, "WriteTrayRun|") {
		t.Fatalf("expected WriteTrayRun undo: %s", steps)
	}
	if !strings.Contains(steps, "CreateStartShortcut|"+joinDir(legacy, config.TrayExeName)) {
		t.Fatalf("expected start shortcut restore to legacy: %s", steps)
	}
	if !strings.Contains(steps, "CreateDesktopShortcut|"+joinDir(legacy, config.TrayExeName)) {
		t.Fatalf("expected desktop shortcut restore to legacy: %s", steps)
	}
	// Migrated keep-me should be removed by undo
	if _, ok := f.Files[joinDir(neu, "keep-me.txt")]; ok {
		t.Fatal("copied keep-me should be removed on rollback")
	}
	if _, ok := f.Files[joinDir(legacy, "keep-me.txt")]; !ok {
		t.Fatal("legacy keep-me must remain")
	}
}

func TestCHK_SETUP_17_FreshUsesTwoWordDefault(t *testing.T) {
	f := newFake()
	neu := `C:\Program Files\Exchange Checker`
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true, NoStart: true}, DefaultDir: neu, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Lang: "ru", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatalf("code=%d err=%v", res.Code, res.Err)
	}
	if res.Dir != neu {
		t.Fatalf("Dir=%q", res.Dir)
	}
	if strings.Contains(strings.Join(f.Steps, "\n"), "MigrateInstallDir|") {
		t.Fatal("fresh must not migrate")
	}
}

func TestCHK_SETUP_17_DirIgnoredRussian(t *testing.T) {
	f := newFake()
	legacy := `C:\Program Files\ExchangeChecker`
	neu := `C:\Program Files\Exchange Checker`
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: legacy}
	f.Service = ServiceInfo{Exists: true, Running: false, ImagePath: `"C:\Program Files\ExchangeChecker\ExchangeCheckerSvc.exe"`}
	f.Files[joinDir(legacy, config.ServiceExeName)] = []byte("old")
	f.Uninst = UninstallValues(legacy, joinDir(legacy, config.UninstallExeName), joinDir(legacy, config.TrayExeName), "1.0.0", 1, time.Now(), true)
	lg := &memLog{}
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true, Dir: `D:\Elsewhere`, NoStart: true}, DefaultDir: neu,
		DataDir: `C:\ProgramData\ExchangeChecker`, Files: sampleFiles(), Version: "1.1.0", Lang: "ru", Now: time.Now(),
	}, lg)
	if res.Code != ExitOK && res.Code != ExitRebootRequired {
		t.Fatalf("code=%d err=%v", res.Code, res.Err)
	}
	joined := strings.Join(lg.lines, "\n")
	if !strings.Contains(joined, "/DIR=") || !strings.Contains(joined, "игнорирован") {
		t.Fatalf("expected Russian /DIR ignored warning: %q", joined)
	}
}
