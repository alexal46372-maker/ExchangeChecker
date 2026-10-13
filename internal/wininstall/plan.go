package wininstall

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
)

// Payload names: the files the setup carries (cmd/exchsetup, -tags payload).
var PayloadFiles = []string{config.ServiceExeName, config.TrayExeName, config.UninstallExeName}

// ErrNoPayload: a setup built without -tags payload cannot install.
var ErrNoPayload = errors.New("setup: this build carries no product files (built without -tags payload)")

// InstallConfig is the input of Install.
type InstallConfig struct {
	Opts       Options
	DefaultDir string // %ProgramFiles%\ExchangeChecker
	DataDir    string // %ProgramData%\ExchangeChecker
	Files      map[string][]byte
	Version    string
	Lang       string
	Now        time.Time
}

// Result is the outcome of Install / Uninstall.
type Result struct {
	Code     int
	Mode     Mode
	Dir      string
	Reboot   bool
	Warnings []string
	Err      error
}

type undoStack []func() error

func (u *undoStack) push(f func() error) { *u = append(*u, f) }

func (u undoStack) run(log Logger) {
	for i := len(u) - 1; i >= 0; i-- {
		if err := u[i](); err != nil {
			log.Printf("rollback: %v", err)
		}
	}
}

// Install runs the install / reinstall plan (silent core of 7b; the
// wizard in 7d calls the same function). A failed fresh install is rolled
// back in reverse order; ProgramData is never removed by a rollback.
func Install(sys System, c InstallConfig, log Logger) Result {
	res := Result{Code: ExitError}
	fail := func(err error) Result {
		res.Err = err
		log.Printf("ERROR: %v", err)
		return res
	}
	warn := func(format string, a ...interface{}) {
		w := fmt.Sprintf(format, a...)
		res.Warnings = append(res.Warnings, w)
		log.Printf("WARNING: %s", w)
	}
	for _, n := range PayloadFiles {
		if len(c.Files[n]) == 0 {
			return fail(ErrNoPayload)
		}
	}
	inst, err := sys.ReadInstalled()
	if err != nil {
		return fail(errf("read Uninstall key", err))
	}
	svc, err := sys.QueryService(model.ServiceName)
	if err != nil {
		return fail(errf("query service", err))
	}
	if !inst.Present && svc.Exists {
		// broken install: the service is there without the Uninstall key
		inst = Installed{Present: true, Version: "", Dir: filepath.Dir(ExePathFromImagePath(svc.ImagePath))}
		warn("service %s exists without an Uninstall key: reinstalling into %s", model.ServiceName, inst.Dir)
	}
	mode, w := Decide(inst.Version, c.Version, inst.Present)
	if w != "" {
		warn("%s", w)
	}
	res.Mode = mode
	if mode == ModeDowngrade && !c.Opts.AllowDowngrade {
		res.Code = ExitDowngrade
		return fail(fmt.Errorf("installed version %s is newer than %s: downgrade refused (use /ALLOWDOWNGRADE)", inst.Version, c.Version))
	}
	dir := CleanDir(c.Opts.Dir)
	if inst.Present {
		if dir != "" && !strings.EqualFold(dir, CleanDir(inst.Dir)) {
			warn("already installed in %s: /DIR=%s ignored (uninstall first to move)", inst.Dir, dir)
		}
		dir = CleanDir(inst.Dir)
	} else if dir == "" {
		dir = CleanDir(c.DefaultDir)
	}
	res.Dir = dir
	if err := ValidateDir(dir, c.DataDir); err != nil {
		return fail(err)
	}
	if err := sys.CheckFixedDrive(dir); err != nil {
		return fail(err)
	}
	fresh := mode == ModeFresh
	log.Printf("%s %s: %s into %s (installed: %q)", version.ProductName, c.Version, mode, dir, inst.Version)

	var undo undoStack
	failRollback := func(err error) Result {
		if fresh {
			log.Printf("rolling back the fresh install")
			undo.run(log)
		}
		return fail(err)
	}

	// 1. data folder: owner + protected DACL (data is never rolled back)
	if _, err := sys.MkdirAll(c.DataDir); err != nil {
		return failRollback(errf("create "+c.DataDir, err))
	}
	if changed, err := sys.TakeOwnership(c.DataDir); err != nil {
		warn("owner of %s: %v", c.DataDir, err)
	} else if changed {
		warn("%s had a foreign owner: owner set to Administrators (recursively)", c.DataDir)
	}
	if err := sys.SetDACL(c.DataDir, DataDirSDDL); err != nil {
		return failRollback(errf("DACL "+c.DataDir, err))
	}
	// 2. install folder: protected DACL always
	created, err := sys.MkdirAll(dir)
	if err != nil {
		return failRollback(errf("create "+dir, err))
	}
	if created {
		undo.push(func() error { _, err := sys.RemoveDirIfEmpty(dir); return err })
	}
	if err := sys.SetDACL(dir, InstallDirSDDL); err != nil {
		return failRollback(errf("DACL "+dir, err))
	}
	// 3. stop a running service before its exe is replaced
	if svc.Exists && svc.Running {
		log.Printf("stopping service %s", model.ServiceName)
		if err := sys.StopService(model.ServiceName, StopTimeout); err != nil {
			return failRollback(errf("stop service", err))
		}
	}
	// 4. files
	var total int64
	for _, n := range PayloadFiles {
		p := joinDir(dir, n)
		if err := sys.WriteFile(p, c.Files[n]); err != nil {
			return failRollback(errf("write "+p, err))
		}
		total += int64(len(c.Files[n]))
		if fresh {
			undo.push(func() error { _, err := sys.RemoveFile(p); return err })
		}
		log.Printf("file %s (%d bytes)", p, len(c.Files[n]))
	}
	svcExe := joinDir(dir, config.ServiceExeName)
	// 5. service
	spec := ServiceSpec{
		Name: model.ServiceName, DisplayName: version.ProductName, ImagePath: ServiceImagePath(svcExe),
		Description: ServiceDescription(c.Lang), AutoStart: !c.Opts.NoStart,
		ResetPeriod: FailureResetPeriod, FailureDelays: FailureDelays, SDDL: ServiceSDDL,
	}
	if svc.Exists {
		// the start type chosen by the user (services.msc) is kept
		if err := sys.UpdateService(spec, false); err != nil {
			return failRollback(errf("update service", err))
		}
	} else {
		if err := sys.CreateService(spec); err != nil {
			return failRollback(errf("create service", err))
		}
		undo.push(func() error { return sys.DeleteService(model.ServiceName) })
	}
	log.Printf("service %s registered (auto start: %v)", model.ServiceName, spec.AutoStart || svc.AutoStart)
	// 6. Event Log source (message table in the service exe)
	if err := sys.SetEventSource(svcExe); err != nil {
		return failRollback(errf("event log source", err))
	}
	if fresh {
		undo.push(sys.DeleteEventSource)
	}
	// 7. HKLM Run of the tray — the service's own rule (CHK-CFG-04)
	if act, err := sys.ApplyTrayRun(svcExe, c.DataDir); err != nil {
		warn("tray autostart: %v", err)
	} else {
		log.Printf("tray autostart: %s", act)
		if fresh {
			undo.push(sys.DeleteTrayRun)
		}
	}
	// 8. Uninstall key, last
	vals := UninstallValues(dir, joinDir(dir, config.UninstallExeName), joinDir(dir, config.TrayExeName), c.Version, sizeKB(total), c.Now)
	if err := sys.WriteUninstallKey(vals); err != nil {
		return failRollback(errf("Uninstall key", err))
	}
	// 9. start
	start := (fresh && !c.Opts.NoStart) || (svc.Exists && svc.Running)
	if !fresh && !svc.Exists && !c.Opts.NoStart {
		start = true
	}
	res.Code = ExitOK
	if start {
		log.Printf("starting service %s", model.ServiceName)
		if err := sys.StartService(model.ServiceName, StartTimeout); err != nil {
			res.Code = ExitError
			res.Err = errf("start service (see "+joinDir(c.DataDir, `logs\exchsvc.log`)+")", err)
			log.Printf("ERROR: %v", res.Err)
			return res
		}
	}
	log.Printf("done: %s", mode)
	return res
}

// UninstallConfig is the input of Uninstall.
type UninstallConfig struct {
	DataDir   string
	SelfExe   string // the running exe (os.Executable)
	PurgeData bool
}

// Uninstall removes the product; every step runs even after a failure
// (idempotent, "missing" = done) and the Uninstall key is removed only
// when everything else succeeded, so the user can retry.
func Uninstall(sys System, c UninstallConfig, log Logger) Result {
	res := Result{Code: ExitOK}
	var errs []error
	step := func(name string, err error) {
		if err != nil {
			errs = append(errs, errf(name, err))
			log.Printf("ERROR: %s: %v", name, err)
		} else {
			log.Printf("%s: ok", name)
		}
	}
	inst, err := sys.ReadInstalled()
	step("read Uninstall key", err)
	svc, err := sys.QueryService(model.ServiceName)
	step("query service", err)
	dir := CleanDir(inst.Dir)
	if dir == "" && svc.Exists {
		dir = filepath.Dir(ExePathFromImagePath(svc.ImagePath))
	}
	if dir == "" && c.SelfExe != "" && strings.EqualFold(filepath.Base(c.SelfExe), config.UninstallExeName) {
		dir = filepath.Dir(c.SelfExe)
	}
	res.Dir = dir
	if svc.Exists {
		if svc.Running {
			step("stop service", sys.StopService(model.ServiceName, StopTimeout))
		}
		step("delete service", sys.DeleteService(model.ServiceName))
	}
	if dir != "" {
		step("stop tray", sys.StopTray(dir))
	}
	step("delete HKLM Run "+config.TrayRunValue, sys.DeleteTrayRun())
	step("delete event log source", sys.DeleteEventSource())
	if dir != "" {
		if err := ValidateDir(dir, c.DataDir); err != nil {
			step("install folder", err)
			dir = ""
		}
	}
	if dir != "" {
		for _, n := range []string{config.ServiceExeName, config.TrayExeName} {
			reboot, err := sys.RemoveFile(joinDir(dir, n))
			res.Reboot = res.Reboot || reboot
			step("remove "+n, err)
		}
		u := joinDir(dir, config.UninstallExeName)
		if c.SelfExe != "" && strings.EqualFold(filepath.Clean(c.SelfExe), filepath.Clean(u)) {
			reboot, err := sys.SelfDelete(c.SelfExe)
			res.Reboot = res.Reboot || reboot
			step("remove the uninstaller", err)
		} else {
			reboot, err := sys.RemoveFile(u)
			res.Reboot = res.Reboot || reboot
			step("remove "+config.UninstallExeName, err)
		}
		reboot, err := sys.RemoveDirIfEmpty(dir)
		res.Reboot = res.Reboot || reboot
		step("remove "+dir, err)
	}
	if c.PurgeData {
		step("remove "+c.DataDir, sys.RemoveAll(c.DataDir))
		step(`remove HKCU\`+UserKeyPath, sys.DeleteUserKey())
	} else {
		log.Printf("data kept: %s", c.DataDir)
	}
	if len(errs) == 0 {
		step("delete Uninstall key", sys.DeleteUninstallKey())
	}
	switch {
	case len(errs) > 0:
		res.Code, res.Err = ExitError, joinErrs(errs)
	case res.Reboot:
		res.Code = ExitRebootRequired
	}
	return res
}

// joinErrs: errors.Join is Go 1.20; kept explicit for the message format.
func joinErrs(errs []error) error {
	msg := make([]string, len(errs))
	for i, e := range errs {
		msg[i] = e.Error()
	}
	return errors.New(strings.Join(msg, "; "))
}
