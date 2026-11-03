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
	f.LegacyShortcut = true
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
	if !strings.Contains(steps, "RemoveFile|"+joinDir(legacy, config.ServiceExeName)) {
		t.Fatalf("legacy svc not removed: %s", steps)
	}
	if !strings.Contains(steps, "RemoveDirIfEmpty|"+legacy) {
		t.Fatalf("legacy dir not removed: %s", steps)
	}
	// ImagePath must point at new folder
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
	if f.Installed.Dir != neu && f.Installed.Present {
		// WriteUninstallKey updates Installed via fake
	}
	for _, v := range f.Uninst {
		if v.Name == "InstallLocation" && v.Str != neu {
			t.Fatalf("InstallLocation=%q", v.Str)
		}
		if v.Name == "DisplayName" && v.Str != version.ProductName {
			t.Fatalf("DisplayName=%q", v.Str)
		}
	}
	if f.LegacyShortcut {
		t.Fatal("legacy shortcut flag still set")
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
