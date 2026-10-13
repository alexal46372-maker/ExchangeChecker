//go:build windows

package wininstall

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// NewSystem returns the WinAPI-backed System.
func NewSystem() System { return winSystem{} }

type winSystem struct{}

func (winSystem) MkdirAll(dir string) (bool, error) {
	_, err := os.Stat(dir)
	if err == nil {
		return false, nil
	}
	if !os.IsNotExist(err) {
		return false, err
	}
	return true, os.MkdirAll(dir, 0o755)
}

func (winSystem) SetDACL(path, sddl string) error { return winapi.SetFileDACL(path, sddl) }

func (winSystem) TakeOwnership(path string) (bool, error) { return winapi.TakeOwnershipTree(path) }

func (winSystem) CheckFixedDrive(dir string) error {
	dt, err := winapi.DriveType(dir)
	if err != nil {
		return err
	}
	if dt != winapi.DriveFixed {
		return fmt.Errorf("%w %q: not a fixed local disk (GetDriveType=%d)", ErrBadDir, dir, dt)
	}
	return nil
}

func (winSystem) WriteFile(path string, data []byte) (reboot bool, err error) {
	dir := filepath.Dir(path)
	name := filepath.Base(path)
	err = config.WriteFileAtomic(dir, name, data, 0o644)
	if err != nil && isFileLocked(err) {
		// §7.4: rename locked image to *.old, delete at reboot, write new
		old := path + ".old"
		_ = os.Remove(old)
		if err2 := winapi.MoveFileEx(path, old, 0); err2 != nil {
			return false, fmt.Errorf("write %s: locked (%v); rename to .old: %w", path, err, err2)
		}
		if err2 := winapi.MoveFileEx(old, "", winapi.MoveFileDelayUntilReboot); err2 != nil {
			return true, fmt.Errorf("write %s: schedule .old delete: %w", path, err2)
		}
		reboot = true
		if err = config.WriteFileAtomic(dir, name, data, 0o644); err != nil {
			return reboot, err
		}
	} else if err != nil {
		return false, err
	}
	got, err := os.ReadFile(path)
	if err != nil {
		return reboot, err
	}
	sumWant := sha256.Sum256(data)
	sumGot := sha256.Sum256(got)
	if sumWant != sumGot {
		return reboot, fmt.Errorf("write %s: sha256 mismatch after write", path)
	}
	return reboot, nil
}

func isFileLocked(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		var pe *os.PathError
		if errors.As(err, &pe) {
			errors.As(pe.Err, &errno)
		}
		var le *os.LinkError
		if errors.As(err, &le) {
			errors.As(le.Err, &errno)
		}
	}
	// only SHARING_VIOLATION=32 / LOCK_VIOLATION=33; ACCESS_DENIED (5) is a
	// real permission error, not a lock
	return errno == 32 || errno == 33
}

func (winSystem) BackupFile(path string) (bool, error) {
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if st.IsDir() {
		return false, fmt.Errorf("backup %s: is a directory", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	bak := path + ".bak"
	if err := os.WriteFile(bak, data, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func (winSystem) RestoreBackup(path string) error {
	bak := path + ".bak"
	data, err := os.ReadFile(bak)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := config.WriteFileAtomic(filepath.Dir(path), filepath.Base(path), data, 0o644); err != nil {
		return err
	}
	_ = os.Remove(bak)
	return nil
}

func (winSystem) RemoveBackup(path string) error {
	err := os.Remove(path + ".bak")
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}

func (winSystem) RemoveFile(path string) (bool, error) {
	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return false, nil
	}
	// in use: schedule deletion at reboot
	if err2 := winapi.MoveFileEx(path, "", winapi.MoveFileDelayUntilReboot); err2 != nil {
		return false, fmt.Errorf("remove %s: %v; delay-until-reboot: %w", path, err, err2)
	}
	return true, nil
}

func (winSystem) RemoveDirIfEmpty(dir string) (bool, error) {
	err := os.Remove(dir)
	if err == nil || os.IsNotExist(err) {
		return false, nil
	}
	// Non-empty: leave it; do not schedule reboot (no 3010 for leftovers).
	if isDirNotEmpty(err) {
		return false, nil
	}
	// in use: schedule deletion at reboot
	if err2 := winapi.MoveFileEx(dir, "", winapi.MoveFileDelayUntilReboot); err2 != nil {
		return false, err
	}
	return true, nil
}

func isDirNotEmpty(err error) bool {
	var pe *os.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		// ERROR_DIR_NOT_EMPTY = 145; ENOTEMPTY on some Go/Windows builds
		return errno == 145 || errno == syscall.ENOTEMPTY
	}
	return false
}

func (winSystem) RemoveAll(dir string) error {
	err := os.RemoveAll(dir)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (winSystem) SelfDelete(exe string) (bool, error) {
	winDir, err := winapi.WindowsDirectory()
	if err != nil {
		return false, err
	}
	tmpDir := filepath.Join(winDir, "Temp")
	_ = os.MkdirAll(tmpDir, 0o755)
	var rnd [4]byte
	_, _ = rand.Read(rnd[:])
	tmp := filepath.Join(tmpDir, fmt.Sprintf("ExchangeChecker-uninst-%s.exe", hex.EncodeToString(rnd[:])))
	// rename running image within the volume (no COPY_ALLOWED)
	if err := winapi.MoveFileEx(exe, tmp, 0); err != nil {
		// different volumes or rename refused: delay-delete in place
		if err2 := winapi.MoveFileEx(exe, "", winapi.MoveFileDelayUntilReboot); err2 != nil {
			return false, fmt.Errorf("self-delete: rename: %v; delay: %w", err, err2)
		}
		return true, nil
	}
	if err := winapi.MoveFileEx(tmp, "", winapi.MoveFileDelayUntilReboot); err != nil {
		return true, err // already moved out of install dir
	}
	return true, nil
}

func (winSystem) QueryService(name string) (ServiceInfo, error) {
	scm, err := winapi.OpenSCManager(winapi.SCManagerConnect)
	if err != nil {
		return ServiceInfo{}, err
	}
	defer scm.Close()
	svc, err := winapi.OpenService(scm, name, winapi.ServiceQueryStatus|winapi.ServiceQueryConfig)
	if err != nil {
		if isErrno(err, winapi.ErrServiceDoesNotExist) || isErrno(err, winapi.ErrServiceMarkedForDelete) {
			return ServiceInfo{}, nil
		}
		return ServiceInfo{}, err
	}
	defer svc.Close()
	if winapi.ServiceMarkedForDelete(svc) {
		return ServiceInfo{}, nil
	}
	cfg, err := winapi.QueryServiceConfig(svc)
	if err != nil {
		return ServiceInfo{}, err
	}
	state, _, err := winapi.QueryServiceStatus(svc)
	if err != nil {
		return ServiceInfo{}, err
	}
	desc, derr := winapi.QueryServiceDescription(svc)
	return ServiceInfo{
		Exists:        true,
		ImagePath:     cfg.BinaryPath,
		DisplayName:   cfg.DisplayName,
		Description:   desc,
		DescriptionOK: derr == nil,
		AutoStart:     cfg.StartType == winapi.ServiceAutoStart,
		Running:       state == winapi.ServiceRunning || state == winapi.ServiceStartPend,
	}, nil
}

func isErrno(err error, want syscall.Errno) bool {
	var e syscall.Errno
	return errors.As(err, &e) && e == want
}

// errServiceBusyClear: CreateService when the name is marked for delete or already exists.
var errServiceBusyClear = errors.New("служба занята или помечена на удаление: закройте services.msc или перезагрузитесь")

func (s winSystem) CreateService(spec ServiceSpec) error {
	scm, err := winapi.OpenSCManager(winapi.SCManagerConnect | winapi.SCManagerCreateService)
	if err != nil {
		return err
	}
	defer scm.Close()
	start := uint32(winapi.ServiceDemandStart)
	if spec.AutoStart {
		start = winapi.ServiceAutoStart
	}
	svc, err := winapi.CreateService(scm, spec.Name, spec.DisplayName, winapi.ServiceAccessForSetup,
		winapi.ServiceWin32OwnProcess, start, winapi.ServiceErrorNormal, spec.ImagePath)
	if err != nil {
		if isErrno(err, winapi.ErrServiceMarkedForDelete) || isErrno(err, winapi.ErrServiceExists) {
			return fmt.Errorf("%w (%v)", errServiceBusyClear, err)
		}
		return err
	}
	defer svc.Close()
	return s.applyServiceExtras(svc, spec, spec.AutoStart)
}

func (s winSystem) UpdateService(spec ServiceSpec, setStart bool) error {
	scm, err := winapi.OpenSCManager(winapi.SCManagerConnect)
	if err != nil {
		return err
	}
	defer scm.Close()
	svc, err := winapi.OpenService(scm, spec.Name, winapi.ServiceAccessForSetup)
	if err != nil {
		return err
	}
	defer svc.Close()
	start := uint32(winapi.ServiceNoChange)
	if setStart {
		start = winapi.ServiceDemandStart
		if spec.AutoStart {
			start = winapi.ServiceAutoStart
		}
	}
	if err := winapi.ChangeServiceConfig(svc, winapi.ServiceNoChange, start, winapi.ServiceNoChange,
		spec.ImagePath, spec.DisplayName); err != nil {
		return err
	}
	// delayed auto-start only when the service is (or becomes) AUTO_START
	delayed := spec.AutoStart
	if !setStart {
		if cfg, err := winapi.QueryServiceConfig(svc); err == nil {
			delayed = cfg.StartType == winapi.ServiceAutoStart
		}
	}
	return s.applyServiceExtras(svc, spec, delayed)
}

func (winSystem) applyServiceExtras(svc winapi.ServiceHandle, spec ServiceSpec, setDelayed bool) error {
	// empty = unknown (e.g. rollback when the old description was unreadable)
	if spec.Description != "" {
		if err := winapi.SetServiceDescription(svc, spec.Description); err != nil {
			return err
		}
	}
	delays := make([]uint32, len(spec.FailureDelays))
	for i, d := range spec.FailureDelays {
		delays[i] = uint32(d / time.Millisecond)
	}
	reset := uint32(spec.ResetPeriod / time.Second)
	if err := winapi.SetServiceFailureActions(svc, reset, delays); err != nil {
		return err
	}
	if setDelayed {
		if err := winapi.SetServiceDelayedAutoStart(svc, true); err != nil {
			return err
		}
	}
	if spec.SDDL != "" {
		if err := winapi.SetServiceDACL(svc, spec.SDDL); err != nil {
			return err
		}
	}
	return nil
}

func (winSystem) StartService(name string, timeout time.Duration) error {
	scm, err := winapi.OpenSCManager(winapi.SCManagerConnect)
	if err != nil {
		return err
	}
	defer scm.Close()
	svc, err := winapi.OpenService(scm, name, winapi.ServiceStart|winapi.ServiceQueryStatus)
	if err != nil {
		return err
	}
	defer svc.Close()
	if err := winapi.StartService(svc); err != nil && !isErrno(err, winapi.ErrServiceAlreadyRunning) {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, _, err := winapi.QueryServiceStatus(svc)
		if err != nil {
			return err
		}
		if state == winapi.ServiceRunning {
			// confirm it stays up ~3s
			time.Sleep(3 * time.Second)
			state, _, err = winapi.QueryServiceStatus(svc)
			if err != nil {
				return err
			}
			if state == winapi.ServiceRunning {
				return nil
			}
			return fmt.Errorf("service %s left RUNNING (state=%d)", name, state)
		}
		if state == winapi.ServiceStopped {
			return fmt.Errorf("service %s stopped while starting", name)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("service %s: start timeout", name)
}

func (winSystem) StopService(name string, timeout time.Duration) error {
	scm, err := winapi.OpenSCManager(winapi.SCManagerConnect)
	if err != nil {
		return err
	}
	defer scm.Close()
	svc, err := winapi.OpenService(scm, name, winapi.ServiceStop|winapi.ServiceQueryStatus)
	if err != nil {
		if isErrno(err, winapi.ErrServiceDoesNotExist) || isErrno(err, winapi.ErrServiceMarkedForDelete) {
			return nil
		}
		return err
	}
	defer svc.Close()
	state, pid, err := winapi.QueryServiceStatus(svc)
	if err != nil {
		return err
	}
	if state != winapi.ServiceStopped {
		if state != winapi.ServiceStopPend {
			if _, err := winapi.ControlService(svc, winapi.ServiceControlStop); err != nil &&
				!isErrno(err, winapi.ErrServiceNotActive) &&
				!isErrno(err, winapi.ErrServiceCannotAcceptCtrl) {
				return err
			}
			// 1061 / already STOP_PENDING: just wait for STOPPED
		}
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			state, pid, err = winapi.QueryServiceStatus(svc)
			if err != nil {
				return err
			}
			if state == winapi.ServiceStopped {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if state != winapi.ServiceStopped {
			return fmt.Errorf("service %s: stop timeout (state=%d)", name, state)
		}
	}
	if pid != 0 {
		_ = winapi.WaitProcessExit(pid, 15000)
	}
	return nil
}

func (s winSystem) ForceStopService(name, installDir string) error {
	stopErr := s.StopService(name, ForceStopTimeout)
	// candidates: the SCM-reported pid (if its image is ours) + any
	// ExchangeCheckerSvc.exe running from installDir (hung before the
	// dispatcher connected: SCM may not know its pid)
	pids := map[uint32]bool{}
	if _, pid, err := winapi.ServiceProcessID(name); err == nil && pid != 0 {
		if img, err := winapi.ProcessImagePath(pid); err == nil && ownSvcImage(img, installDir) {
			pids[pid] = true
		}
	}
	if procs, err := winapi.ProcessesInDir(installDir, config.ServiceExeName); err == nil {
		for _, p := range procs {
			pids[p.PID] = true
		}
	}
	if len(pids) == 0 {
		if stopErr != nil {
			if info, err := s.QueryService(name); err == nil && !info.Running {
				return nil
			}
		}
		return stopErr
	}
	_ = winapi.EnablePrivileges("SeDebugPrivilege")
	var errs []string
	for pid := range pids {
		if err := winapi.TerminatePID(pid); err != nil {
			errs = append(errs, fmt.Sprintf("terminate pid %d: %v", pid, err))
			continue
		}
		if err := winapi.WaitProcessExit(pid, 15000); err != nil {
			errs = append(errs, fmt.Sprintf("wait pid %d: %v", pid, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("force stop %s: %s", name, strings.Join(errs, "; "))
	}
	return nil
}

func ownSvcImage(img, installDir string) bool {
	want := strings.ToLower(filepath.Clean(winapi.LongPath(filepath.Join(installDir, config.ServiceExeName))))
	return strings.ToLower(filepath.Clean(winapi.LongPath(img))) == want
}

func (winSystem) DeleteService(name string) error {
	scm, err := winapi.OpenSCManager(winapi.SCManagerConnect)
	if err != nil {
		return err
	}
	defer scm.Close()
	svc, err := winapi.OpenService(scm, name, winapi.ServiceDelete|winapi.ServiceQueryStatus)
	if err != nil {
		if isErrno(err, winapi.ErrServiceDoesNotExist) || isErrno(err, winapi.ErrServiceMarkedForDelete) {
			return nil
		}
		return err
	}
	if err := winapi.DeleteService(svc); err != nil {
		svc.Close()
		if isErrno(err, winapi.ErrServiceDoesNotExist) || isErrno(err, winapi.ErrServiceMarkedForDelete) {
			return nil
		}
		return err
	}
	svc.Close() // must close before SCM drops a marked-for-delete service
	// Wait until the name is gone or only a mark-for-delete stub remains.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		h, err := winapi.OpenService(scm, name, winapi.ServiceQueryStatus|winapi.ServiceChangeConfig)
		if err != nil {
			if isErrno(err, winapi.ErrServiceDoesNotExist) || isErrno(err, winapi.ErrServiceMarkedForDelete) {
				return nil
			}
			return err
		}
		marked := winapi.ServiceMarkedForDelete(h)
		h.Close()
		if marked {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("DeleteService %s: still present after mark-for-delete", name)
}

func (winSystem) SetEventSource(msgFile string) error {
	view := uint32(winapi.KEY_WOW64_64KEY)
	if err := winapi.RegSetExpandStringView(winapi.HKEY_LOCAL_MACHINE, EventLogKeyPath, "EventMessageFile", msgFile, view); err != nil {
		return err
	}
	return winapi.RegSetDWORDView(winapi.HKEY_LOCAL_MACHINE, EventLogKeyPath, "TypesSupported", EventTypesSupported, view)
}

func (winSystem) DeleteEventSource() error {
	return winapi.RegDeleteKeyView(winapi.HKEY_LOCAL_MACHINE, EventLogKeyPath, winapi.KEY_WOW64_64KEY)
}

func (winSystem) ApplyTrayRun(serviceExe, dataDir string) (string, error) {
	cmd, err := config.TrayRunCommand(serviceExe)
	if err != nil {
		return "", err
	}
	st, err := config.NewStore(dataDir).LoadSettings()
	if err != nil {
		// unreadable settings → do not touch (same as the service, CHK-CFG-04)
		return "unchanged (settings unreadable)", nil
	}
	act, err := config.ApplyTrayAutostart(config.MachineRunRegistry(), st.TrayAutostart, cmd)
	return string(act), err
}

func (winSystem) DeleteTrayRun() error {
	return winapi.RegDeleteValueView(winapi.HKEY_LOCAL_MACHINE, config.RunKeyPath, config.TrayRunValue, winapi.KEY_WOW64_64KEY)
}

func (winSystem) ReadUninstallKey() ([]RegValue, error) {
	view := uint32(winapi.KEY_WOW64_64KEY)
	ok, err := winapi.RegKeyExistsView(winapi.HKEY_LOCAL_MACHINE, UninstallKeyPath, view)
	if err != nil || !ok {
		return nil, err
	}
	var out []RegValue
	for _, name := range []string{
		"DisplayName", "DisplayVersion", "InstallLocation", "UninstallString",
		"QuietUninstallString", "DisplayIcon", "InstallDate",
	} {
		s, present, err := winapi.RegGetStringView(winapi.HKEY_LOCAL_MACHINE, UninstallKeyPath, name, view)
		if err != nil {
			return nil, err
		}
		if present {
			out = append(out, RegValue{Name: name, Str: s})
		}
	}
	for _, name := range []string{"EstimatedSize", "NoModify", "NoRepair"} {
		n, present, err := winapi.RegGetDWORDView(winapi.HKEY_LOCAL_MACHINE, UninstallKeyPath, name, view)
		if err != nil {
			return nil, err
		}
		if present {
			out = append(out, RegValue{Name: name, DWord: true, Num: n})
		}
	}
	return out, nil
}

func (winSystem) WriteUninstallKey(vals []RegValue) error {
	view := uint32(winapi.KEY_WOW64_64KEY)
	for _, v := range vals {
		if v.DWord {
			if err := winapi.RegSetDWORDView(winapi.HKEY_LOCAL_MACHINE, UninstallKeyPath, v.Name, v.Num, view); err != nil {
				return err
			}
		} else {
			if err := winapi.RegSetStringView(winapi.HKEY_LOCAL_MACHINE, UninstallKeyPath, v.Name, v.Str, view); err != nil {
				return err
			}
		}
	}
	return nil
}

func (winSystem) DeleteUninstallKey() error {
	return winapi.RegDeleteKeyView(winapi.HKEY_LOCAL_MACHINE, UninstallKeyPath, winapi.KEY_WOW64_64KEY)
}

func (winSystem) ReadInstalled() (Installed, error) {
	view := uint32(winapi.KEY_WOW64_64KEY)
	ok, err := winapi.RegKeyExistsView(winapi.HKEY_LOCAL_MACHINE, UninstallKeyPath, view)
	if err != nil || !ok {
		return Installed{}, err
	}
	ver, _, err := winapi.RegGetStringView(winapi.HKEY_LOCAL_MACHINE, UninstallKeyPath, "DisplayVersion", view)
	if err != nil {
		return Installed{}, err
	}
	dir, _, err := winapi.RegGetStringView(winapi.HKEY_LOCAL_MACHINE, UninstallKeyPath, "InstallLocation", view)
	if err != nil {
		return Installed{}, err
	}
	return Installed{Present: true, Version: ver, Dir: dir}, nil
}

func (winSystem) DeleteUserKey() error {
	// HKCU\Software\ExchangeChecker (and Tray subkey); missing = ok
	_ = winapi.RegDeleteKey(winapi.HKEY_CURRENT_USER, UserKeyPath+`\Tray`)
	return winapi.RegDeleteKey(winapi.HKEY_CURRENT_USER, UserKeyPath)
}

func (winSystem) StopTray(installDir string) error {
	if installDir == "" {
		return nil
	}
	msg := winapi.RegisterWindowMessage(model.TrayExitMessage)
	selfSess, sessErr := winapi.CurrentSessionID()

	procs, err := winapi.ProcessesInDir(installDir, config.TrayExeName)
	if err != nil {
		return err
	}
	var list []TrayProc
	for _, p := range procs {
		// If our own session is unknown, try graceful exit for everyone first
		// (do not treat unknown PIDs as "other session").
		s := selfSess
		if sessErr == nil {
			if sid, e := winapi.ProcessSessionID(p.PID); e == nil {
				s = sid
			} else {
				s = selfSess // graceful first
			}
		}
		list = append(list, TrayProc{PID: p.PID, Session: s})
	}
	var post, other []uint32
	if sessErr != nil {
		for _, p := range list {
			post = append(post, p.PID)
		}
	} else {
		post, other = SplitTrayStop(selfSess, list)
	}

	if msg != 0 {
		want := map[uint32]bool{}
		for _, pid := range post {
			want[pid] = true
		}
		if len(want) > 0 {
			for _, class := range []string{
				model.TrayWindowClass, model.TraySettingsClass,
				model.TraySettingsViewClass, model.TrayDiscoveryClass,
			} {
				for _, hwnd := range winapi.EnumWindowsByClass(class) {
					if want[winapi.WindowProcessID(hwnd)] {
						winapi.PostMessage(hwnd, msg, 0, 0)
					}
				}
			}
		}
		// Broadcast also reaches a separate --settings process (CHK-TRAY-11).
		winapi.BroadcastMessage(msg, 2000)
	}

	var first error
	term := func(pid uint32, why string) {
		if err := winapi.TerminatePID(pid); err != nil {
			if first == nil {
				first = fmt.Errorf("stop tray pid %d (%s): %w", pid, why, err)
			}
			return
		}
		_ = winapi.WaitProcessExit(pid, 5000)
	}
	for _, pid := range other {
		term(pid, "other session")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		left, err := winapi.ProcessesInDir(installDir, config.TrayExeName)
		if err != nil {
			return err
		}
		if len(left) == 0 {
			return first
		}
		time.Sleep(200 * time.Millisecond)
	}
	left, err := winapi.ProcessesInDir(installDir, config.TrayExeName)
	if err != nil {
		return err
	}
	for _, p := range left {
		term(p.PID, "after wait")
	}
	return first
}
