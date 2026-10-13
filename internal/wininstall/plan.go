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
		inst = Installed{Present: true, Version: "", Dir: ParentDir(ExePathFromImagePath(svc.ImagePath))}
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
		if len(undo) > 0 {
			if fresh {
				log.Printf("rolling back the fresh install")
			} else {
				log.Printf("rolling back the upgrade")
			}
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
	wasRunning := svc.Exists && svc.Running
	if wasRunning {
		log.Printf("stopping service %s", model.ServiceName)
		if err := sys.StopService(model.ServiceName, StopTimeout); err != nil {
			return failRollback(errf("stop service", err))
		}
		undo.push(func() error {
			return sys.StartService(model.ServiceName, StartTimeout)
		})
	}
	// 3b. stop tray before replacing its image (upgrade / reinstall)
	if !fresh {
		log.Printf("stopping tray in %s", dir)
		if err := sys.StopTray(dir); err != nil {
			warn("stop tray: %v", err)
		}
	}
	// 4. files (upgrade: *.bak then atomic replace; rollback restores bak
	// or deletes a file that did not exist before)
	var total int64
	var bakPaths []string
	needReboot := false
	for _, n := range PayloadFiles {
		p := joinDir(dir, n)
		if !fresh {
			existed, err := sys.BackupFile(p)
			if err != nil {
				return failRollback(errf("backup "+p, err))
			}
			path := p
			if existed {
				bakPaths = append(bakPaths, p)
				undo.push(func() error { return sys.RestoreBackup(path) })
			} else {
				undo.push(func() error { _, err := sys.RemoveFile(path); return err })
			}
		}
		reboot, err := sys.WriteFile(p, c.Files[n])
		if err != nil {
			return failRollback(errf("write "+p, err))
		}
		needReboot = needReboot || reboot
		total += int64(len(c.Files[n]))
		if fresh {
			undo.push(func() error { _, err := sys.RemoveFile(p); return err })
		}
		log.Printf("file %s (%d bytes)", p, len(c.Files[n]))
	}
	svcExe := joinDir(dir, config.ServiceExeName)
	// 5. service — journal previous ImagePath / description for upgrade rollback (§7.4)
	spec := ServiceSpec{
		Name: model.ServiceName, DisplayName: version.ProductName, ImagePath: ServiceImagePath(svcExe),
		Description: ServiceDescription(c.Lang), AutoStart: !c.Opts.NoStart,
		ResetPeriod: FailureResetPeriod, FailureDelays: FailureDelays, SDDL: ServiceSDDL,
	}
	if svc.Exists {
		prev := ServiceSpec{
			Name: model.ServiceName, DisplayName: version.ProductName,
			ImagePath: svc.ImagePath, Description: svc.Description,
			AutoStart:   svc.AutoStart,
			ResetPeriod: FailureResetPeriod, FailureDelays: FailureDelays, SDDL: ServiceSDDL,
		}
		if !fresh {
			undo.push(func() error { return sys.UpdateService(prev, false) })
		}
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
	} else if svc.Exists && svc.ImagePath != "" {
		prevExe := ExePathFromImagePath(svc.ImagePath)
		undo.push(func() error { return sys.SetEventSource(prevExe) })
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
	// 8. Uninstall key — journal previous values (DisplayVersion etc.) for rollback
	if !fresh {
		if prevVals, err := sys.ReadUninstallKey(); err != nil {
			warn("read Uninstall key for rollback: %v", err)
		} else if len(prevVals) > 0 {
			pv := prevVals
			undo.push(func() error { return sys.WriteUninstallKey(pv) })
		}
	}
	vals := UninstallValues(dir, joinDir(dir, config.UninstallExeName), joinDir(dir, config.TrayExeName), c.Version, sizeKB(total), c.Now)
	if err := sys.WriteUninstallKey(vals); err != nil {
		return failRollback(errf("Uninstall key", err))
	}
	// 9. start — on upgrade, keep *.bak until V2 StartService succeeds
	start := (fresh && !c.Opts.NoStart) || (!fresh && wasRunning)
	if !fresh && !svc.Exists && !c.Opts.NoStart {
		start = true
	}
	if start {
		log.Printf("starting service %s", model.ServiceName)
		if err := sys.StartService(model.ServiceName, StartTimeout); err != nil {
			res.Err = errf("start service (see "+joinDir(c.DataDir, `logs\exchsvc.log`)+")", err)
			log.Printf("ERROR: %v", res.Err)
			if !fresh {
				log.Printf("rolling back the upgrade after StartService failure")
				undo.run(log)
			}
			res.Code = ExitError
			return res
		}
		// Start succeeded: drop the "restart old service" undo entry's effect by
		// clearing undo only for bak removal path — keep undo unused on success.
	}
	// Drop *.bak: after successful StartService when wasRunning; otherwise after
	// registrations when the service was not running (no start to wait for).
	if !fresh && (wasRunning && start || !wasRunning) {
		for _, p := range bakPaths {
			if err := sys.RemoveBackup(p); err != nil {
				warn("remove backup %s: %v", p, err)
			}
		}
	}
	res.Code = ExitOK
	if needReboot {
		res.Code = ExitRebootRequired
		res.Reboot = true
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
		dir = ParentDir(ExePathFromImagePath(svc.ImagePath))
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
