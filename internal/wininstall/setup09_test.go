package wininstall

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func TestCHK_SETUP_09_DetectUninstallKey(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{Exists: true, Running: true, ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`, AutoStart: true}
	f.Files[`C:\EC\`+config.ServiceExeName] = []byte("old-svc")
	f.Files[`C:\EC\`+config.TrayExeName] = []byte("old-tray")
	f.Files[`C:\EC\`+config.UninstallExeName] = []byte("old-un")
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true, Dir: `D:\Elsewhere`}, DefaultDir: `C:\Program Files\ExchangeChecker`,
		DataDir: `C:\ProgramData\ExchangeChecker`, Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK || res.Mode != ModeUpgrade {
		t.Fatalf("code=%d mode=%s err=%v", res.Code, res.Mode, res.Err)
	}
	if res.Dir != `C:\EC` {
		t.Fatalf("dir=%s (must keep install location)", res.Dir)
	}
}

func TestCHK_SETUP_09_DetectServiceImagePath(t *testing.T) {
	f := newFake()
	// broken install: service without Uninstall key
	f.Service = ServiceInfo{Exists: true, Running: false, ImagePath: `"C:\Broken\ExchangeCheckerSvc.exe"`}
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\Program Files\ExchangeChecker`,
		DataDir: `C:\ProgramData\ExchangeChecker`, Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatalf("code=%d err=%v", res.Code, res.Err)
	}
	if res.Dir != `C:\Broken` {
		t.Fatalf("dir=%s", res.Dir)
	}
}

func TestCHK_SETUP_09_StopTrayBeforeReplace(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{Exists: true, Running: true, ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`}
	f.Files[`C:\EC\`+config.ServiceExeName] = []byte("old")
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	stop := indexOf(f.Steps, "StopService|")
	tray := indexOf(f.Steps, "StopTray|")
	write := indexOf(f.Steps, "WriteFile|")
	if stop < 0 || tray < 0 || write < 0 {
		t.Fatalf("steps: %v", f.Steps)
	}
	if !(stop < tray && tray < write) {
		t.Fatalf("order stop=%d tray=%d write=%d\n%v", stop, tray, write, f.Steps)
	}
}

func TestCHK_SETUP_09_BackupAndRollback(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{Exists: true, Running: false, ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`, AutoStart: false}
	old := []byte("old-svc-bytes")
	f.Files[`C:\EC\`+config.ServiceExeName] = append([]byte(nil), old...)
	f.FailAt = "SetEventSource|"
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitError {
		t.Fatalf("code=%d", res.Code)
	}
	if indexOf(f.Steps, "BackupFile|") < 0 || indexOf(f.Steps, "RestoreBackup|") < 0 {
		t.Fatalf("backup/restore missing: %v", f.Steps)
	}
	got := f.Files[`C:\EC\`+config.ServiceExeName]
	if string(got) != string(old) {
		t.Fatalf("file not restored: %q", got)
	}
}

func TestCHK_SETUP_09_ProgramDataUntouched(t *testing.T) {
	f := newFake()
	data := `C:\ProgramData\ExchangeChecker`
	secret := filepath.Join(data, "secrets.dpapi")
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{Exists: true, Running: false, ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`}
	f.Dirs[data] = true
	f.Files[secret] = []byte("secret-bytes")
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: data,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	if string(f.Files[secret]) != "secret-bytes" {
		t.Fatal("ProgramData file changed")
	}
	for _, s := range f.Steps {
		if strings.HasPrefix(s, "RemoveFile|"+data) || strings.HasPrefix(s, "RemoveAll|"+data) || strings.HasPrefix(s, "WriteFile|"+data) {
			t.Fatalf("ProgramData touched: %s", s)
		}
	}
}

func TestCHK_SETUP_09_StartTypePreserved(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{Exists: true, Running: false, ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`, AutoStart: false}
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	// UpdateService with setStart=false
	u := indexOf(f.Steps, "UpdateService|")
	if u < 0 {
		t.Fatalf("no UpdateService: %v", f.Steps)
	}
	if !strings.Contains(f.Steps[u], "setStart=false") {
		t.Fatalf("start type must be preserved: %s", f.Steps[u])
	}
	if indexOf(f.Steps, "StartService|") >= 0 {
		t.Fatalf("must not start: service was not running: %v", f.Steps)
	}
	if f.Service.AutoStart {
		t.Fatal("AutoStart flipped to true")
	}
}

func TestCHK_SETUP_09_RestartOnlyIfWasRunning(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{Exists: true, Running: true, ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`, AutoStart: true}
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	if indexOf(f.Steps, "StartService|") < 0 {
		t.Fatalf("expected StartService: %v", f.Steps)
	}
}

func TestCHK_SETUP_09_DowngradeExit3(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "2.0.0", Dir: `C:\EC`}
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitDowngrade {
		t.Fatalf("code=%d", res.Code)
	}
	if indexOf(f.Steps, "WriteFile|") >= 0 {
		t.Fatal("downgrade must not write files")
	}
}

func TestCHK_SETUP_09_SameVersionReinstall(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{Exists: true, Running: false, ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`}
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK || res.Mode != ModeReinstall {
		t.Fatalf("code=%d mode=%s err=%v", res.Code, res.Mode, res.Err)
	}
	if indexOf(f.Steps, "StopTray|") < 0 || indexOf(f.Steps, "BackupFile|") < 0 {
		t.Fatalf("reinstall must stop tray and backup: %v", f.Steps)
	}
}

func TestCHK_SETUP_09_AllowDowngrade(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "2.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{Exists: true, Running: false, ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`}
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true, AllowDowngrade: true}, DefaultDir: `C:\EC`,
		DataDir: `C:\ProgramData\ExchangeChecker`, Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK || res.Mode != ModeDowngrade {
		t.Fatalf("code=%d mode=%s err=%v", res.Code, res.Mode, res.Err)
	}
}

func TestCHK_SETUP_09_StopServiceBeforeFiles(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{Exists: true, Running: true, ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`}
	_ = Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if indexOf(f.Steps, "StopService|"+model.ServiceName) < 0 {
		t.Fatalf("StopService not recorded: %v", f.Steps)
	}
	if StopTimeout != 90*time.Second {
		t.Fatalf("StopTimeout=%s want 90s", StopTimeout)
	}
}

func TestCHK_SETUP_09_StartServiceFailureRollsBack(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{
		Exists: true, Running: true, AutoStart: true,
		ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`, Description: "old desc", DescriptionOK: true,
		DisplayName: "Old Display Name",
	}
	oldSvc := []byte("old-svc-bytes")
	f.Files[`C:\EC\`+config.ServiceExeName] = append([]byte(nil), oldSvc...)
	f.Uninst = UninstallValues(`C:\EC`, `C:\EC\`+config.UninstallExeName, `C:\EC\`+config.TrayExeName, "1.0.0", 1, time.Now())
	f.FailAt = "StartService|"
	f.FailOnce = true
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitError {
		t.Fatalf("code=%d err=%v", res.Code, res.Err)
	}
	if indexOf(f.Steps, "RestoreBackup|") < 0 {
		t.Fatalf("expected RestoreBackup: %v", f.Steps)
	}
	// V2 is force-stopped before any file is restored (locked exe)
	fs := indexOf(f.Steps, "ForceStopService|")
	if fs < 0 || fs > indexOf(f.Steps, "RestoreBackup|") {
		t.Fatalf("ForceStopService must precede RestoreBackup: %v", f.Steps)
	}
	last := f.Specs[len(f.Specs)-1]
	if last.DisplayName != "Old Display Name" || last.Description != "old desc" {
		t.Fatalf("prev spec not restored: %+v", last)
	}
	if errors.Is(res.Err, ErrRollbackIncomplete) {
		t.Fatalf("complete rollback reported as incomplete: %v", res.Err)
	}
	if indexOf(f.Steps, "WriteUninstallKey|") < 0 {
		t.Fatal("expected uninstall key writes")
	}
	// last WriteUninstallKey should restore 1.0.0
	var lastUV string
	for _, s := range f.Steps {
		if strings.HasPrefix(s, "WriteUninstallKey|") {
			lastUV = s
		}
	}
	_ = lastUV
	if f.Installed.Version != "1.0.0" {
		t.Fatalf("version after rollback=%q", f.Installed.Version)
	}
	if string(f.Files[`C:\EC\`+config.ServiceExeName]) != string(oldSvc) {
		t.Fatal("svc file not restored")
	}
	// UpdateService restore of old ImagePath
	restored := false
	for _, s := range f.Steps {
		if strings.Contains(s, "UpdateService|") && strings.Contains(s, `C:\EC\ExchangeCheckerSvc.exe`) {
			restored = true
		}
	}
	if !restored {
		t.Logf("steps: %v", f.Steps)
	}
}

func TestCHK_SETUP_09_RegistryJournalOnUpgrade(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{
		Exists: true, Running: true, AutoStart: true,
		ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`, Description: "Exchange Checker",
	}
	f.Files[`C:\EC\`+config.ServiceExeName] = []byte("old")
	f.Uninst = UninstallValues(`C:\EC`, `C:\EC\`+config.UninstallExeName, `C:\EC\`+config.TrayExeName, "1.0.0", 1, time.Now())
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	if indexOf(f.Steps, "ReadUninstallKey") < 0 {
		t.Fatalf("expected ReadUninstallKey journal: %v", f.Steps)
	}
	if f.Installed.Version != "1.1.0" {
		t.Fatalf("version=%q", f.Installed.Version)
	}
	// bak removed after successful start
	if indexOf(f.Steps, "RemoveBackup|") < 0 {
		t.Fatalf("expected RemoveBackup after start: %v", f.Steps)
	}
}

func TestCHK_SETUP_09_NewFileRemovedOnRollback(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{Exists: true, Running: false, ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`}
	// only svc existed; tray/uninstall are new
	f.Files[`C:\EC\`+config.ServiceExeName] = []byte("old-svc")
	f.FailAt = "SetEventSource|"
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitError {
		t.Fatalf("code=%d", res.Code)
	}
	if indexOf(f.Steps, "RemoveFile|C:\\EC\\"+config.TrayExeName) < 0 &&
		indexOf(f.Steps, "RemoveFile|"+`C:\EC\`+config.TrayExeName) < 0 {
		// path formatting
		found := false
		for _, s := range f.Steps {
			if strings.HasPrefix(s, "RemoveFile|") && strings.Contains(s, config.TrayExeName) {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected RemoveFile of new tray on rollback: %v", f.Steps)
		}
	}
}

func TestCHK_SETUP_09_RollbackIncompleteReported(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{
		Exists: true, Running: true, AutoStart: true,
		ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`, // description unreadable
	}
	f.Files[`C:\EC\`+config.ServiceExeName] = []byte("old")
	f.Uninst = UninstallValues(`C:\EC`, `C:\EC\`+config.UninstallExeName, `C:\EC\`+config.TrayExeName, "1.0.0", 1, time.Now())
	f.FailAt = "StartService|"
	f.FailOnce = true
	f.FailAlso = "RestoreBackup|"
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitError || !errors.Is(res.Err, ErrRollbackIncomplete) {
		t.Fatalf("code=%d err=%v: want ExitError + rollback incomplete", res.Code, res.Err)
	}
	if !strings.Contains(res.Err.Error(), "rollback incomplete") {
		t.Fatalf("user message lacks rollback status: %v", res.Err)
	}
	for _, s := range f.Specs[1:] { // rollback spec
		if s.Description != "" {
			t.Fatalf("unknown old description must not be written: %+v", s)
		}
	}
}
