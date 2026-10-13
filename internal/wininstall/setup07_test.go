package wininstall

import (
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
)

// TestCHK_SETUP_07_UninstallKeyAndSelfDelete — values + self-delete → reboot code.
func TestCHK_SETUP_07_UninstallKeyAndSelfDelete(t *testing.T) {
	vals := UninstallValues(`C:\EC`, `C:\EC\`+config.UninstallExeName, `C:\EC\`+config.TrayExeName, "1.2.3", 100, time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	byName := map[string]RegValue{}
	for _, v := range vals {
		byName[v.Name] = v
	}
	if byName["DisplayVersion"].Str != "1.2.3" {
		t.Fatal(byName["DisplayVersion"])
	}
	if !strings.HasSuffix(byName["QuietUninstallString"].Str, "/uninstall /S") {
		t.Fatal(byName["QuietUninstallString"].Str)
	}
	if _, ok := byName["Publisher"]; ok {
		t.Fatal("Publisher present")
	}
	if byName["InstallDate"].Str != "20261013" {
		t.Fatal(byName["InstallDate"])
	}

	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	self := `C:\EC\` + config.UninstallExeName
	res := Uninstall(f, UninstallConfig{DataDir: `C:\ProgramData\ExchangeChecker`, SelfExe: self}, &memLog{})
	if res.Code != ExitRebootRequired {
		t.Fatalf("code=%d (want 3010), err=%v steps=%v", res.Code, res.Err, f.Steps)
	}
	if indexOf(f.Steps, "SelfDelete|"+self) < 0 {
		t.Fatalf("SelfDelete missing: %v", f.Steps)
	}
}

// TestCHK_SETUP_07_KeepDataWithoutPurge — data dir kept.
func TestCHK_SETUP_07_KeepDataWithoutPurge(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	data := `C:\ProgramData\ExchangeChecker`
	f.Dirs[data] = true
	_ = Uninstall(f, UninstallConfig{DataDir: data, PurgeData: false}, &memLog{})
	if indexOf(f.Steps, "RemoveAll|"+data) >= 0 {
		t.Fatal("RemoveAll without purge")
	}
}
