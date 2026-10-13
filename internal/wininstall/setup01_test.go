package wininstall

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
)

func sampleFiles() map[string][]byte {
	return map[string][]byte{
		config.ServiceExeName:   []byte("SVC"),
		config.TrayExeName:      []byte("TRAY"),
		config.UninstallExeName: []byte("UNINST"),
	}
}

func TestCHK_SETUP_01_ParseArgs(t *testing.T) {
	o, err := ParseArgs([]string{"/s", `/DIR=D:\Apps\EC`, "-nostart", "/NOSHORTCUT", "/LOG=C:\\a.log", "/ALLOWDOWNGRADE", "/LANG=en"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.Silent || o.Dir != `D:\Apps\EC` || !o.NoStart || !o.NoShortcut || o.Log != `C:\a.log` || !o.AllowDowngrade || o.Lang != "en" {
		t.Fatalf("%+v", o)
	}
	o, err = ParseArgs([]string{"/uninstall", "/S", "/PURGEDATA"})
	if err != nil || !o.Uninstall || !o.PurgeData || !o.Silent {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := ParseArgs([]string{"/uninstall", "/DIR=C:\\x"}); !errors.Is(err, ErrUsage) {
		t.Fatalf("want ErrUsage, got %v", err)
	}
	if _, err := ParseArgs([]string{"/PURGEDATA"}); !errors.Is(err, ErrUsage) {
		t.Fatalf("want ErrUsage, got %v", err)
	}
	if _, err := ParseArgs([]string{"/LANG=de"}); !errors.Is(err, ErrUsage) {
		t.Fatalf("want ErrUsage, got %v", err)
	}
}

func TestCHK_SETUP_01_ValidateDir(t *testing.T) {
	data := `C:\ProgramData\ExchangeChecker`
	if err := ValidateDir(`C:\Program Files\ExchangeChecker`, data); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", `\\server\share`, `C:`, `C:\`, `relative`, `C:\ProgramData\ExchangeChecker\x`, `C:\ProgramData\ExchangeChecker`} {
		if err := ValidateDir(bad, data); err == nil {
			t.Errorf("ValidateDir(%q) accepted", bad)
		}
	}
}

func TestCHK_SETUP_01_Decide(t *testing.T) {
	cases := []struct {
		inst, neu string
		present   bool
		want      Mode
	}{
		{"", "1.0.0", false, ModeFresh},
		{"1.0.0", "1.1.0", true, ModeUpgrade},
		{"1.0.0", "1.0.0", true, ModeReinstall},
		{"1.1.0", "1.0.0", true, ModeDowngrade},
		{"1.0.0", "0.0.0-dev", true, ModeReinstall},
		{"0.0.0-ci.1", "0.0.0-ci.2", true, ModeReinstall},
		{"bogus", "1.0.0", true, ModeReinstall},
	}
	for _, c := range cases {
		got, _ := Decide(c.inst, c.neu, c.present)
		if got != c.want {
			t.Errorf("Decide(%q,%q,%v)=%s want %s", c.inst, c.neu, c.present, got, c.want)
		}
	}
}

func TestCHK_SETUP_01_FreshInstallPlan(t *testing.T) {
	f := newFake()
	log := &memLog{}
	dir := `C:\Program Files\ExchangeChecker`
	data := `C:\ProgramData\ExchangeChecker`
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: dir, DataDir: data,
		Files: sampleFiles(), Version: "1.0.0", Lang: "ru", Now: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
	}, log)
	if res.Code != ExitOK || res.Err != nil {
		t.Fatalf("code=%d err=%v log=%v", res.Code, res.Err, log.lines)
	}
	if res.Mode != ModeFresh {
		t.Fatalf("mode=%s", res.Mode)
	}
	steps := strings.Join(f.Steps, "\n")
	for _, want := range []string{
		"MkdirAll|" + data,
		"SetDACL|" + data + "|" + DataDirSDDL,
		"MkdirAll|" + dir,
		"SetDACL|" + dir + "|" + InstallDirSDDL,
		"CreateService|" + model.ServiceName,
		"SetEventSource|" + dir + `\` + config.ServiceExeName,
		"ApplyTrayRun|",
		"WriteUninstallKey|",
		"StartService|" + model.ServiceName,
	} {
		if !strings.Contains(steps, want) {
			t.Errorf("missing step %q in:\n%s", want, steps)
		}
	}
	// service stopped before Run delete is an uninstall concern; install order: files before service
	idxWrite := indexOf(f.Steps, "WriteFile|"+dir+`\`+config.ServiceExeName)
	idxCreate := indexOf(f.Steps, "CreateService|")
	idxUninst := indexOf(f.Steps, "WriteUninstallKey|")
	if idxWrite < 0 || idxCreate < 0 || idxWrite > idxCreate {
		t.Errorf("files before CreateService: write=%d create=%d", idxWrite, idxCreate)
	}
	if idxUninst < idxCreate {
		t.Errorf("Uninstall key must be last registrations: uninst=%d create=%d", idxUninst, idxCreate)
	}
	// Uninstall values
	var names []string
	for _, v := range f.Uninst {
		names = append(names, v.Name)
		if v.Name == "Publisher" {
			t.Error("Publisher must not be written (decision 5)")
		}
		if v.Name == "QuietUninstallString" && !strings.Contains(v.Str, "/uninstall /S") {
			t.Errorf("QuietUninstallString=%q", v.Str)
		}
		if v.Name == "DisplayName" && v.Str != version.ProductName {
			t.Errorf("DisplayName=%q", v.Str)
		}
	}
	for _, n := range []string{"DisplayName", "DisplayVersion", "InstallLocation", "UninstallString", "QuietUninstallString", "NoModify", "NoRepair"} {
		if !contains(names, n) {
			t.Errorf("missing Uninstall value %s", n)
		}
	}
}

func TestCHK_SETUP_01_NoStart(t *testing.T) {
	f := newFake()
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true, NoStart: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Lang: "en", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	for _, s := range f.Steps {
		if strings.HasPrefix(s, "StartService|") {
			t.Fatalf("StartService with /NOSTART: %v", f.Steps)
		}
		if strings.Contains(s, "CreateService|") && strings.Contains(s, "auto=true") {
			t.Fatalf("expected DEMAND_START: %s", s)
		}
	}
}

func TestCHK_SETUP_01_NoPayload(t *testing.T) {
	f := newFake()
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: map[string][]byte{}, Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitError || !errors.Is(res.Err, ErrNoPayload) {
		t.Fatalf("%d %v", res.Code, res.Err)
	}
}

func TestCHK_SETUP_01_DowngradeRefused(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "2.0.0", Dir: `C:\EC`}
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitDowngrade {
		t.Fatalf("code=%d err=%v", res.Code, res.Err)
	}
}

func TestCHK_SETUP_01_UninstallOrder(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	f.Service = ServiceInfo{Exists: true, Running: true, ImagePath: `"C:\EC\ExchangeCheckerSvc.exe"`}
	f.Dirs[`C:\EC`] = true
	res := Uninstall(f, UninstallConfig{DataDir: `C:\ProgramData\ExchangeChecker`, SelfExe: `C:\EC\` + config.UninstallExeName}, &memLog{})
	if res.Code != ExitRebootRequired && res.Code != ExitOK {
		t.Fatalf("code=%d err=%v", res.Code, res.Err)
	}
	steps := f.Steps
	stop := indexOf(steps, "StopService|")
	delSvc := indexOf(steps, "DeleteService|")
	stopTray := indexOf(steps, "StopTray|")
	delRun := indexOf(steps, "DeleteTrayRun")
	delKey := indexOf(steps, "DeleteUninstallKey")
	if stop < 0 || delSvc < 0 || stopTray < 0 || delRun < 0 || delKey < 0 {
		t.Fatalf("missing steps: %v", steps)
	}
	if stop > delRun || delSvc > delRun {
		t.Errorf("service stop/delete before Run delete: stop=%d svc=%d run=%d\n%v", stop, delSvc, delRun, steps)
	}
	if stopTray > delRun {
		t.Errorf("StopTray before Run delete: tray=%d run=%d", stopTray, delRun)
	}
	if delKey < delRun || delKey < delSvc {
		t.Errorf("Uninstall key must be last: key=%d run=%d svc=%d", delKey, delRun, delSvc)
	}
}

func TestCHK_SETUP_01_PurgeData(t *testing.T) {
	f := newFake()
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	data := `C:\ProgramData\ExchangeChecker`
	res := Uninstall(f, UninstallConfig{DataDir: data, PurgeData: true}, &memLog{})
	if res.Code != ExitOK && res.Code != ExitRebootRequired {
		t.Fatal(res.Err)
	}
	if indexOf(f.Steps, "RemoveAll|"+data) < 0 || indexOf(f.Steps, "DeleteUserKey") < 0 {
		t.Fatalf("purge steps missing: %v", f.Steps)
	}
}

func TestCHK_SETUP_01_RollbackFresh(t *testing.T) {
	f := newFake()
	f.FailAt = "CreateService|"
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitError {
		t.Fatal(res.Code)
	}
	// files should be rolled back
	if indexOf(f.Steps, "RemoveFile|") < 0 {
		t.Fatalf("expected rollback RemoveFile: %v", f.Steps)
	}
}

func indexOf(steps []string, prefix string) int {
	for i, s := range steps {
		if strings.HasPrefix(s, prefix) {
			return i
		}
	}
	return -1
}

func contains(ss []string, x string) bool {
	for _, s := range ss {
		if s == x {
			return true
		}
	}
	return false
}
