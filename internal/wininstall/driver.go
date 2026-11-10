package wininstall

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
)

// Phase 14 (decision of 10.11.2026, POLICY driver exception): the
// InpOutx64 kernel driver for the built-in «Тон» signal through the PC
// speaker. The setup copies the unmodified signed file and registers a
// SERVICE_KERNEL_DRIVER / SERVICE_DEMAND_START service that it never
// starts; EC Service checks the sha256 and starts it on demand
// (CHK-SETUP-19/20).

// DriverPayloadName is the key of the driver in InstallConfig.Files.
const DriverPayloadName = "inpoutx64.sys"

// DriverDisplayName is the DisplayName of the driver service.
const DriverDisplayName = "Exchange Checker — драйвер системного динамика (InpOutx64)"

// DriverDir is the driver folder under the install folder.
func DriverDir(installDir string) string {
	return joinDir(installDir, config.InpOutRelPath[:strings.LastIndex(config.InpOutRelPath, `\`)])
}

// DriverFilePath is <install folder>\driver\inpoutx64.sys.
func DriverFilePath(installDir string) string { return joinDir(installDir, config.InpOutRelPath) }

// DriverImagePath is the NT ImagePath of the driver service (\??\ + the
// Win32 path, no quotes: the kernel loader takes the string as a path).
func DriverImagePath(installDir string) string { return `\??\` + DriverFilePath(installDir) }

// DriverPathFromImagePath strips the \??\ prefix (also \\?\) of a driver
// ImagePath (QueryServiceConfig) to the Win32 file path.
func DriverPathFromImagePath(img string) string {
	for _, p := range []string{`\??\`, `\\?\`} {
		if strings.HasPrefix(img, p) {
			return img[len(p):]
		}
	}
	return img
}

// DriverSpecFor is the registration of the driver installed in dir.
func DriverSpecFor(installDir string) DriverSpec {
	return DriverSpec{Name: config.InpOutServiceName, DisplayName: DriverDisplayName,
		ImagePath: DriverImagePath(installDir), SDDL: DriverSDDL}
}

// DriverHashOK reports whether data is the expected inpoutx64.sys.
func DriverHashOK(data []byte) bool {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:]) == config.InpOutSHA256
}

// failAtHook: test seam over the e2efault EC_SETUP_FAIL_AT hook (always
// false in release builds); unit tests replace it.
var failAtHook = setupTestFailAt

// driverResult is what a completed driver step hands to the main plan.
type driverResult struct {
	undo   undoStack // appended to the main journal (rollback of later steps)
	bak    string    // driver .bak to drop after a successful install
	size   int64
	reboot bool
}

// installDriver is step 4b of Install. It is never fatal: any failure is a
// WARNING, the step's own partial changes are undone right here, and the
// main journal receives entries only for a completed step — so a driver
// failure never rolls the rest of the install back (CHK-SETUP-20).
func installDriver(sys System, dir string, data []byte, log Logger, warn func(string, ...interface{})) (driverResult, bool) {
	name := config.InpOutServiceName
	if len(data) == 0 {
		warn("драйвер InpOutx64 отсутствует в составе установщика: сигнал «Тон» через системный динамик будет недоступен")
		return driverResult{}, false
	}
	if !DriverHashOK(data) {
		warn("драйвер InpOutx64 не установлен: sha256 файла не совпадает с ожидаемым %s", config.InpOutSHA256)
		return driverResult{}, false
	}
	var local undoStack
	abort := func(what string, err error) (driverResult, bool) {
		if errs := local.run(log); len(errs) > 0 {
			warn("драйвер InpOutx64: откат шага драйвера неполный: %v", joinErrs(errs))
		}
		warn("драйвер InpOutx64 не установлен (%s: %v); установка продолжается, сигнал «Тон» через системный динамик будет недоступен", what, err)
		return driverResult{}, false
	}
	drv, err := sys.QueryDriverService(name)
	if err != nil {
		return abort("запрос службы "+name, err)
	}
	// the file of a loaded driver is locked: unload it first (EC Service
	// loads it again on demand)
	if drv.Exists && drv.Running {
		log.Printf("stopping driver service %s", name)
		if err := sys.StopDriverService(name); err != nil {
			return abort("остановка службы "+name, err)
		}
	}
	ddir := DriverDir(dir)
	created, err := sys.MkdirAll(ddir)
	if err != nil {
		return abort("создание "+ddir, err)
	}
	if created {
		local.push(func() error { _, err := sys.RemoveDirIfEmpty(ddir); return err })
	}
	p := DriverFilePath(dir)
	existed, err := sys.BackupFile(p)
	if err != nil {
		return abort("резервная копия "+p, err)
	}
	if existed {
		local.push(func() error { return sys.RestoreBackup(p) })
	} else {
		local.push(func() error { _, err := sys.RemoveFile(p); return err })
	}
	reboot, err := sys.WriteFile(p, data)
	if err != nil {
		return abort("запись "+p, err)
	}
	spec := DriverSpecFor(dir)
	if failAtHook("InpOutDriver") {
		return abort("служба "+name, errors.New("setup test fail at InpOutDriver"))
	}
	if drv.Exists {
		if err := sys.UpdateDriverService(spec); err != nil {
			return abort("обновление службы "+name, err)
		}
		if drv.ImagePath != "" && drv.ImagePath != spec.ImagePath {
			prev := spec
			prev.ImagePath = drv.ImagePath
			local.push(func() error { return sys.UpdateDriverService(prev) })
		}
	} else {
		if err := sys.CreateDriverService(spec); err != nil {
			return abort("создание службы "+name, err)
		}
		local.push(func() error { return sys.DeleteDriverService(name) })
	}
	log.Printf("driver service %s registered (kernel driver, demand start, not started): %s", name, spec.ImagePath)
	r := driverResult{undo: local, size: int64(len(data)), reboot: reboot}
	if existed {
		r.bak = p
	}
	return r, true
}

// uninstallDriver stops and deletes the driver service and removes the
// file and the driver folder if empty (CHK-SETUP-20). A driver that will
// not unload is a WARNING: the service is marked for delete and the file
// is removed at reboot.
func uninstallDriver(sys System, dir string, log Logger, step func(string, error)) (reboot bool) {
	name := config.InpOutServiceName
	if err := sys.StopDriverService(name); err != nil {
		log.Printf("WARNING: stop driver service %s: %v (removed after reboot)", name, err)
		reboot = true
	}
	step("delete driver service "+name, sys.DeleteDriverService(name))
	if dir != "" {
		rb, err := sys.RemoveFile(DriverFilePath(dir))
		reboot = reboot || rb
		step("remove "+DriverFilePath(dir), err)
	}
	if dir != "" {
		rb, err := sys.RemoveDirIfEmpty(DriverDir(dir))
		reboot = reboot || rb
		step("remove "+DriverDir(dir), err)
	}
	return reboot
}
