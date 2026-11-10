package wininstall

import (
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/third_party/inpout"
)

// CHK-SETUP-20 — the InpOutx64 driver step of the install / uninstall /
// rollback plans (phase 14). Every positive case has a negative twin.

const (
	drvTestDir  = `C:\Program Files\Exchange Checker`
	drvTestData = `C:\ProgramData\ExchangeChecker`
)

func filesWithDriver() map[string][]byte {
	f := sampleFiles()
	f[DriverPayloadName] = inpout.SysFile
	return f
}

func drvInstall(f *recordingSystem, files map[string][]byte, ver string) (Result, *memLog) {
	log := &memLog{}
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: drvTestDir, DataDir: drvTestData,
		Files: files, Version: ver, Now: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
	}, log)
	return res, log
}

func indexEq(steps []string, s string) int {
	for i, x := range steps {
		if x == s {
			return i
		}
	}
	return -1
}

func hasStep(steps []string, prefix string) bool { return indexOf(steps, prefix) >= 0 }

func driverWarnings(res Result) []string {
	var w []string
	for _, s := range res.Warnings {
		if strings.Contains(s, "InpOutx64") {
			w = append(w, s)
		}
	}
	return w
}

func withFailAt(t *testing.T, step string) {
	t.Helper()
	old := failAtHook
	failAtHook = func(s string) bool { return s == step }
	t.Cleanup(func() { failAtHook = old })
}

func TestCHK_SETUP_20_Paths(t *testing.T) {
	if got := DriverFilePath(drvTestDir); got != drvTestDir+`\driver\inpoutx64.sys` {
		t.Fatalf("DriverFilePath=%q", got)
	}
	if got := DriverDir(drvTestDir); got != drvTestDir+`\driver` {
		t.Fatalf("DriverDir=%q", got)
	}
	img := DriverImagePath(drvTestDir)
	if img != `\??\`+drvTestDir+`\driver\inpoutx64.sys` || strings.Contains(img, `"`) {
		t.Fatalf("DriverImagePath=%q (NT path, unquoted)", img)
	}
	if DriverPathFromImagePath(img) != DriverFilePath(drvTestDir) || DriverPathFromImagePath(`C:\x.sys`) != `C:\x.sys` {
		t.Fatal("DriverPathFromImagePath")
	}
	s := DriverSpecFor(drvTestDir)
	if s.Name != "ExchangeCheckerInpOut" || s.SDDL != DriverSDDL || s.ImagePath != img {
		t.Fatalf("spec %+v", s)
	}
	// only SY / BA may start the driver (RP); IU / SU / AU must not
	for _, ace := range strings.SplitAfter(DriverSDDL, ")") {
		if strings.Contains(ace, "RP") && !strings.HasSuffix(ace, ";;;SY)") && !strings.HasSuffix(ace, ";;;BA)") {
			t.Errorf("DriverSDDL grants start to %s", ace)
		}
	}
	if strings.Contains(DriverSDDL, ";;;AU)") || strings.Contains(DriverSDDL, ";;;WD)") || strings.Contains(DriverSDDL, ";;;BU)") {
		t.Error("DriverSDDL must not list AU / WD / BU")
	}
	if !DriverHashOK(inpout.SysFile) {
		t.Fatal("DriverHashOK(inpout.SysFile)=false")
	}
	bad := append([]byte(nil), inpout.SysFile...)
	bad[100] ^= 0x80
	if DriverHashOK(bad) || DriverHashOK(nil) {
		t.Fatal("DriverHashOK accepts a corrupt / empty file")
	}
}

func TestCHK_SETUP_20_FreshInstall(t *testing.T) {
	f := newFake()
	res, log := drvInstall(f, filesWithDriver(), "1.0.0")
	if res.Code != ExitOK || res.Err != nil {
		t.Fatalf("code=%d err=%v\n%s", res.Code, res.Err, strings.Join(log.lines, "\n"))
	}
	if w := driverWarnings(res); len(w) != 0 {
		t.Fatalf("driver warnings on a clean install: %v", w)
	}
	p := DriverFilePath(drvTestDir)
	if !DriverHashOK(f.Files[p]) {
		t.Fatalf("driver file %s not written byte for byte", p)
	}
	d := f.Driver
	if !d.Exists || !d.KernelDriver || !d.DemandStart || d.ImagePath != DriverImagePath(drvTestDir) || d.SDDL != DriverSDDL || d.DisplayName != DriverDisplayName {
		t.Fatalf("driver service %+v", d)
	}
	if hasStep(f.Steps, "StartService|"+config.InpOutServiceName) || d.Running {
		t.Fatal("the installer must not start the driver service")
	}
	// order: install folder → driver dir → file → driver service → main service
	iDir := indexOf(f.Steps, "MkdirAll|"+DriverDir(drvTestDir))
	iFile := indexOf(f.Steps, "WriteFile|"+p)
	iSvc := indexOf(f.Steps, "CreateDriverService|"+config.InpOutServiceName+"|"+DriverImagePath(drvTestDir))
	iMain := indexOf(f.Steps, "CreateService|"+model.ServiceName)
	if !(iDir >= 0 && iDir < iFile && iFile < iSvc && iSvc < iMain) {
		t.Fatalf("order dir=%d file=%d drvsvc=%d main=%d\n%s", iDir, iFile, iSvc, iMain, strings.Join(f.Steps, "\n"))
	}
}

// negative: a corrupt driver in the payload is never written; WARNING, the
// install itself succeeds
func TestCHK_SETUP_20_CorruptDriverRefused(t *testing.T) {
	files := filesWithDriver()
	bad := append([]byte(nil), inpout.SysFile...)
	bad[len(bad)-1] ^= 0x01
	files[DriverPayloadName] = bad
	f := newFake()
	res, _ := drvInstall(f, files, "1.0.0")
	if res.Code != ExitOK {
		t.Fatalf("code=%d err=%v", res.Code, res.Err)
	}
	if w := driverWarnings(res); len(w) != 1 || !strings.Contains(w[0], "sha256") {
		t.Fatalf("want one sha256 WARNING, got %v", res.Warnings)
	}
	if hasStep(f.Steps, "WriteFile|"+DriverFilePath(drvTestDir)) || hasStep(f.Steps, "CreateDriverService|") || f.Driver.Exists {
		t.Fatal("a corrupt driver must not be written or registered")
	}
	// missing from the payload: WARNING, no driver steps
	f2 := newFake()
	res2, _ := drvInstall(f2, sampleFiles(), "1.0.0")
	if res2.Code != ExitOK || len(driverWarnings(res2)) != 1 || hasStep(f2.Steps, "CreateDriverService|") {
		t.Fatalf("missing driver: code=%d warnings=%v", res2.Code, res2.Warnings)
	}
}

// the driver step fails (SCM error / e2efault hook): WARNING, its own file
// and folder are removed, nothing else is rolled back, exit 0
func TestCHK_SETUP_20_DriverFailureIsWarning(t *testing.T) {
	check := func(t *testing.T, f *recordingSystem, res Result, log *memLog) {
		t.Helper()
		if res.Code != ExitOK || res.Err != nil {
			t.Fatalf("code=%d err=%v\n%s", res.Code, res.Err, strings.Join(log.lines, "\n"))
		}
		if w := driverWarnings(res); len(w) != 1 || !strings.Contains(w[0], "установка продолжается") {
			t.Fatalf("want one driver WARNING, got %v", res.Warnings)
		}
		if !strings.Contains(strings.Join(log.lines, "\n"), "WARNING: драйвер InpOutx64 не установлен") {
			t.Fatal("WARNING line missing in the log")
		}
		if _, ok := f.Files[DriverFilePath(drvTestDir)]; ok || f.Dirs[DriverDir(drvTestDir)] || f.Driver.Exists {
			t.Fatalf("driver step left changes: file/dir/service %v %v %+v", ok, f.Dirs[DriverDir(drvTestDir)], f.Driver)
		}
		if !f.Service.Exists || hasStep(f.Steps, "DeleteService|"+model.ServiceName) || f.Installed.Version != "1.0.0" {
			t.Fatal("a driver failure must not roll the install back")
		}
		if !hasStep(f.Steps, "StartService|"+model.ServiceName) {
			t.Fatal("the main service must still be started")
		}
	}
	t.Run("SCM", func(t *testing.T) {
		f := newFake()
		f.FailAt = "CreateDriverService|"
		res, log := drvInstall(f, filesWithDriver(), "1.0.0")
		check(t, f, res, log)
	})
	t.Run("FaultHook", func(t *testing.T) {
		withFailAt(t, "InpOutDriver")
		f := newFake()
		res, log := drvInstall(f, filesWithDriver(), "1.0.0")
		check(t, f, res, log)
		if hasStep(f.Steps, "CreateDriverService|") {
			t.Fatal("hook must fire before CreateDriverService")
		}
		if !hasStep(f.Steps, "WriteFile|"+DriverFilePath(drvTestDir)) || !hasStep(f.Steps, "RemoveFile|"+DriverFilePath(drvTestDir)) {
			t.Fatal("hook must fire after the file was written, and the file must be removed")
		}
	})
	// negative twin: without the failure the driver is installed
	f := newFake()
	if res, _ := drvInstall(f, filesWithDriver(), "1.0.0"); res.Code != ExitOK || !f.Driver.Exists {
		t.Fatal("control install without failure did not register the driver")
	}
}

// a failure after the driver step rolls the driver service and file back
// together with the rest of the fresh install
func TestCHK_SETUP_20_LaterFailureRollsDriverBack(t *testing.T) {
	check := func(t *testing.T, f *recordingSystem, res Result) {
		t.Helper()
		if res.Code != ExitError || res.Err == nil {
			t.Fatalf("want failure, code=%d", res.Code)
		}
		if !hasStep(f.Steps, "DeleteDriverService|"+config.InpOutServiceName) || f.Driver.Exists {
			t.Fatalf("driver service not rolled back: %+v", f.Driver)
		}
		if _, ok := f.Files[DriverFilePath(drvTestDir)]; ok {
			t.Fatal("driver file not rolled back")
		}
		iDrvDel := indexOf(f.Steps, "DeleteDriverService|")
		iFileRm := indexOf(f.Steps, "RemoveFile|"+DriverFilePath(drvTestDir))
		iDirRm := indexOf(f.Steps, "RemoveDirIfEmpty|"+DriverDir(drvTestDir))
		iRootRm := indexEq(f.Steps, "RemoveDirIfEmpty|"+drvTestDir)
		if !(iDrvDel < iFileRm && iFileRm < iDirRm && iDirRm < iRootRm) {
			t.Fatalf("rollback order svc=%d file=%d dir=%d root=%d", iDrvDel, iFileRm, iDirRm, iRootRm)
		}
	}
	t.Run("MainServiceFails", func(t *testing.T) {
		f := newFake()
		f.FailAt = "CreateService|" + model.ServiceName
		res, _ := drvInstall(f, filesWithDriver(), "1.0.0")
		check(t, f, res)
	})
	t.Run("FaultHookAfterDriver", func(t *testing.T) {
		withFailAt(t, "AfterInpOutDriver")
		f := newFake()
		res, _ := drvInstall(f, filesWithDriver(), "1.0.0")
		check(t, f, res)
		if hasStep(f.Steps, "CreateService|") {
			t.Fatal("hook must fire before the main service step")
		}
	})
}

// upgrade over an installed driver: stop before replacing the file,
// ChangeServiceConfig (no second CreateService), .bak dropped on success;
// a later failure restores the previous file
func TestCHK_SETUP_20_UpgradeIdempotent(t *testing.T) {
	f := newFake()
	if res, _ := drvInstall(f, filesWithDriver(), "1.0.0"); res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	f.Driver.Running = true // EC Service loaded it
	f.Steps = nil
	res, log := drvInstall(f, filesWithDriver(), "1.1.0")
	if res.Code != ExitOK || len(driverWarnings(res)) != 0 {
		t.Fatalf("code=%d err=%v warn=%v\n%s", res.Code, res.Err, res.Warnings, strings.Join(log.lines, "\n"))
	}
	p := DriverFilePath(drvTestDir)
	iStop := indexOf(f.Steps, "StopDriverService|"+config.InpOutServiceName)
	iWrite := indexOf(f.Steps, "WriteFile|"+p)
	if iStop < 0 || iWrite < 0 || iStop > iWrite {
		t.Fatalf("stop=%d write=%d: the driver must be stopped before its file is replaced", iStop, iWrite)
	}
	if hasStep(f.Steps, "CreateDriverService|") || !hasStep(f.Steps, "UpdateDriverService|"+config.InpOutServiceName) {
		t.Fatal("upgrade must update, not create, the driver service")
	}
	if !f.Driver.Exists || !f.Driver.KernelDriver || !f.Driver.DemandStart || f.Driver.Running {
		t.Fatalf("driver after upgrade %+v (must stay registered, not started)", f.Driver)
	}
	if !hasStep(f.Steps, "RemoveBackup|"+p) {
		t.Fatal("driver .bak must be dropped after a successful upgrade")
	}
	// negative: a stop failure leaves the loaded file alone (WARNING, exit 0)
	f.Driver.Running = true
	f.FailAt = "StopDriverService|"
	f.Steps = nil
	res, _ = drvInstall(f, filesWithDriver(), "1.2.0")
	if res.Code != ExitOK || len(driverWarnings(res)) != 1 || hasStep(f.Steps, "WriteFile|"+p) {
		t.Fatalf("stop failure: code=%d warn=%v", res.Code, res.Warnings)
	}
	// rollback of an upgrade restores the previous driver file
	f.FailAt = "WriteUninstallKey|"
	f.Driver.Running = false
	f.Files[p] = []byte("OLD-DRIVER")
	f.Steps = nil
	res, _ = drvInstall(f, filesWithDriver(), "1.3.0")
	if res.Code != ExitError {
		t.Fatalf("want failure, code=%d", res.Code)
	}
	if string(f.Files[p]) != "OLD-DRIVER" || !hasStep(f.Steps, "RestoreBackup|"+p) || !f.Driver.Exists {
		t.Fatalf("upgrade rollback: file=%q service=%+v", f.Files[p], f.Driver)
	}
}

func TestCHK_SETUP_20_Uninstall(t *testing.T) {
	f := newFake()
	if res, _ := drvInstall(f, filesWithDriver(), "1.0.0"); res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	f.Driver.Running = true
	f.Steps = nil
	res := Uninstall(f, UninstallConfig{DataDir: drvTestData}, &memLog{})
	if res.Code != ExitOK && res.Code != ExitRebootRequired {
		t.Fatalf("code=%d err=%v", res.Code, res.Err)
	}
	p := DriverFilePath(drvTestDir)
	iStop := indexOf(f.Steps, "StopDriverService|"+config.InpOutServiceName)
	iDel := indexOf(f.Steps, "DeleteDriverService|"+config.InpOutServiceName)
	iFile := indexOf(f.Steps, "RemoveFile|"+p)
	iDir := indexOf(f.Steps, "RemoveDirIfEmpty|"+DriverDir(drvTestDir))
	iRoot := indexEq(f.Steps, "RemoveDirIfEmpty|"+drvTestDir)
	if !(iStop >= 0 && iStop < iDel && iDel < iFile && iFile < iDir && iDir < iRoot) {
		t.Fatalf("uninstall order stop=%d del=%d file=%d dir=%d root=%d\n%s", iStop, iDel, iFile, iDir, iRoot, strings.Join(f.Steps, "\n"))
	}
	if f.Driver.Exists {
		t.Fatal("driver service left after uninstall")
	}
	if _, ok := f.Files[p]; ok {
		t.Fatal("driver file left after uninstall")
	}
	// negative: a driver that will not unload → WARNING + reboot, the
	// service is still deleted
	f2 := newFake()
	if res, _ := drvInstall(f2, filesWithDriver(), "1.0.0"); res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	f2.FailAt = "StopDriverService|"
	log := &memLog{}
	res2 := Uninstall(f2, UninstallConfig{DataDir: drvTestData}, log)
	if !res2.Reboot || f2.Driver.Exists || !strings.Contains(strings.Join(log.lines, "\n"), "WARNING: stop driver service") {
		t.Fatalf("stop failure: reboot=%v driver=%+v", res2.Reboot, f2.Driver)
	}
	// negative: DeleteDriverService failing is an uninstall error (the
	// Uninstall key stays so the user can retry)
	f3 := newFake()
	if res, _ := drvInstall(f3, filesWithDriver(), "1.0.0"); res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	f3.FailAt = "DeleteDriverService|"
	res3 := Uninstall(f3, UninstallConfig{DataDir: drvTestData}, &memLog{})
	if res3.Code != ExitError || hasStep(f3.Steps, "DeleteUninstallKey") {
		t.Fatalf("delete failure: code=%d", res3.Code)
	}
}
