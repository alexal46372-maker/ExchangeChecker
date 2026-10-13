package wininstall

import (
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
