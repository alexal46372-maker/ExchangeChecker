package wininstall

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// recordingSystem is a fake System for CHK-SETUP-01..07 plan tests.
type recordingSystem struct {
	mu sync.Mutex
	// Log of "verb|arg" steps
	Steps []string

	Installed      Installed
	Service        ServiceInfo
	Dirs           map[string]bool
	Files          map[string][]byte
	DACLs          map[string]string
	Owners         map[string]bool // path -> takeOwnership called
	EventSrc       string
	TrayRun        string
	TrayRunCmd     string // full Run command for Read/WriteTrayRun
	Uninst         []RegValue
	UserKey        bool
	FixedOK        bool
	FailAt         string // step prefix that returns error
	FailOnce       bool   // clear FailAt after the first injected error
	FailMigrateAfter int    // copy N files then error (CHK-SETUP-17 mid-migrate)
	RebootRm       bool
	WriteReboot    bool   // WriteFile returns reboot=true
	FailAlso       string // second prefix that always fails (e.g. a rollback step)
	Specs          []ServiceSpec
	Long           map[string]string
	TrayWarn       []string
	Shortcut       string // target of the start-menu shortcut ("" = none)
	DesktopLnk     string // target of the Public Desktop shortcut ("" = none)
	LegacyShortcut bool   // pre-phase-13 ExchangeChecker.lnk still present
	Firewall       string // service exe of the SQL Browser firewall rule ("" = none)
	FwProfiles     int32  // active firewall profiles (FirewallCurrentProfiles)
}

func newFake() *recordingSystem {
	return &recordingSystem{
		Dirs:    map[string]bool{},
		Files:   map[string][]byte{},
		DACLs:   map[string]string{},
		Owners:  map[string]bool{},
		FixedOK: true,
	}
}

func (f *recordingSystem) step(format string, args ...interface{}) error {
	s := fmt.Sprintf(format, args...)
	f.Steps = append(f.Steps, s)
	if f.FailAt != "" && strings.HasPrefix(s, f.FailAt) {
		if f.FailOnce {
			f.FailAt = ""
		}
		return fmt.Errorf("injected failure: %s", s)
	}
	if f.FailAlso != "" && strings.HasPrefix(s, f.FailAlso) {
		return fmt.Errorf("injected failure: %s", s)
	}
	return nil
}

func (f *recordingSystem) MkdirAll(dir string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("MkdirAll|%s", dir); err != nil {
		return false, err
	}
	if f.Dirs[dir] {
		return false, nil
	}
	f.Dirs[dir] = true
	return true, nil
}

func (f *recordingSystem) SetDACL(path, sddl string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("SetDACL|%s|%s", path, sddl); err != nil {
		return err
	}
	f.DACLs[path] = sddl
	return nil
}

func (f *recordingSystem) TakeOwnership(path string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("TakeOwnership|%s", path); err != nil {
		return false, err
	}
	f.Owners[path] = true
	return false, nil
}

func (f *recordingSystem) CheckFixedDrive(dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("CheckFixedDrive|%s", dir); err != nil {
		return err
	}
	if !f.FixedOK {
		return fmt.Errorf("%w %q: not fixed", ErrBadDir, dir)
	}
	return nil
}

func (f *recordingSystem) WriteFile(path string, data []byte) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := sha256.Sum256(data)
	if err := f.step("WriteFile|%s|%d|%s", path, len(data), hex.EncodeToString(sum[:8])); err != nil {
		return false, err
	}
	cp := append([]byte(nil), data...)
	f.Files[path] = cp
	return f.WriteReboot, nil
}

func (f *recordingSystem) BackupFile(path string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("BackupFile|%s", path); err != nil {
		return false, err
	}
	data, ok := f.Files[path]
	if ok {
		f.Files[path+".bak"] = append([]byte(nil), data...)
	}
	return ok, nil
}

func (f *recordingSystem) RestoreBackup(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("RestoreBackup|%s", path); err != nil {
		return err
	}
	bak := path + ".bak"
	if data, ok := f.Files[bak]; ok {
		f.Files[path] = append([]byte(nil), data...)
		delete(f.Files, bak)
	}
	return nil
}

func (f *recordingSystem) RemoveBackup(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("RemoveBackup|%s", path); err != nil {
		return err
	}
	delete(f.Files, path+".bak")
	return nil
}

func (f *recordingSystem) RemoveFile(path string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("RemoveFile|%s", path); err != nil {
		return false, err
	}
	delete(f.Files, path)
	return f.RebootRm, nil
}

func (f *recordingSystem) RemoveDirIfEmpty(dir string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("RemoveDirIfEmpty|%s", dir); err != nil {
		return false, err
	}
	delete(f.Dirs, dir)
	return false, nil
}

func (f *recordingSystem) RemoveAll(dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("RemoveAll|%s", dir); err != nil {
		return err
	}
	delete(f.Dirs, dir)
	for k := range f.Files {
		if strings.HasPrefix(strings.ToLower(k), strings.ToLower(dir)) {
			delete(f.Files, k)
		}
	}
	return nil
}

func (f *recordingSystem) SelfDelete(exe string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("SelfDelete|%s", exe); err != nil {
		return false, err
	}
	delete(f.Files, exe)
	return true, nil
}

func (f *recordingSystem) QueryService(name string) (ServiceInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = f.step("QueryService|%s", name)
	return f.Service, nil
}

func (f *recordingSystem) CreateService(s ServiceSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("CreateService|%s|%s|auto=%v", s.Name, s.ImagePath, s.AutoStart); err != nil {
		return err
	}
	f.Service = ServiceInfo{Exists: true, ImagePath: s.ImagePath, AutoStart: s.AutoStart}
	return nil
}

func (f *recordingSystem) UpdateService(s ServiceSpec, setStart bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("UpdateService|%s|setStart=%v|auto=%v|img=%s", s.Name, setStart, s.AutoStart, s.ImagePath); err != nil {
		return err
	}
	f.Specs = append(f.Specs, s)
	f.Service.ImagePath = s.ImagePath
	if s.Description != "" {
		f.Service.Description = s.Description
	}
	if setStart {
		f.Service.AutoStart = s.AutoStart
	}
	f.Service.ImagePath = s.ImagePath
	if setStart {
		f.Service.AutoStart = s.AutoStart
	}
	return nil
}

func (f *recordingSystem) StartService(name string, timeout time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("StartService|%s", name); err != nil {
		return err
	}
	f.Service.Running = true
	return nil
}

func (f *recordingSystem) StopService(name string, timeout time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("StopService|%s", name); err != nil {
		return err
	}
	f.Service.Running = false
	return nil
}

func (f *recordingSystem) ForceStopService(name, dir string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("ForceStopService|%s", name); err != nil {
		return nil, err
	}
	f.Service.Running = false
	return nil, nil
}

func (f *recordingSystem) CreateStartShortcut(trayExe, workDir, desc string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("CreateStartShortcut|%s|%s", trayExe, workDir); err != nil {
		return "", err
	}
	f.Shortcut = trayExe
	return `C:\ProgramData\Microsoft\Windows\Start Menu\Programs\Exchange Checker\Exchange Checker.lnk`, nil
}

func (f *recordingSystem) DeleteStartShortcut() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("DeleteStartShortcut"); err != nil {
		return err
	}
	f.Shortcut = ""
	return nil
}

func (f *recordingSystem) CreateDesktopShortcut(trayExe, workDir, description string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("CreateDesktopShortcut|%s", trayExe); err != nil {
		return "", err
	}
	f.DesktopLnk = trayExe
	return `C:\Users\Public\Desktop\Exchange Checker.lnk`, nil
}

func (f *recordingSystem) DeleteDesktopShortcut() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("DeleteDesktopShortcut"); err != nil {
		return err
	}
	f.DesktopLnk = ""
	return nil
}

func (f *recordingSystem) AddSQLBrowserFirewallRule(serviceExe string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("AddSQLBrowserFirewallRule|%s", serviceExe); err != nil {
		return err
	}
	f.Firewall = serviceExe
	return nil
}

func (f *recordingSystem) RemoveSQLBrowserFirewallRule() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("RemoveSQLBrowserFirewallRule"); err != nil {
		return err
	}
	f.Firewall = ""
	return nil
}

func (f *recordingSystem) FirewallCurrentProfiles() (int32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("FirewallCurrentProfiles"); err != nil {
		return 0, err
	}
	return f.FwProfiles, nil
}

func (f *recordingSystem) LongPath(p string) string {
	if l, ok := f.Long[p]; ok {
		return l
	}
	return p
}

func (f *recordingSystem) DeleteService(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("DeleteService|%s", name); err != nil {
		return err
	}
	f.Service = ServiceInfo{}
	return nil
}

func (f *recordingSystem) SetEventSource(msgFile string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("SetEventSource|%s", msgFile); err != nil {
		return err
	}
	f.EventSrc = msgFile
	return nil
}

func (f *recordingSystem) DeleteEventSource() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("DeleteEventSource"); err != nil {
		return err
	}
	f.EventSrc = ""
	return nil
}

func (f *recordingSystem) ApplyTrayRun(serviceExe, dataDir string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("ApplyTrayRun|%s|%s", serviceExe, dataDir); err != nil {
		return "", err
	}
	f.TrayRun = serviceExe
	f.TrayRunCmd = serviceExe
	return "written", nil
}

func (f *recordingSystem) StopTray(installDir string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.TrayWarn, f.step("StopTray|%s", installDir)
}

func (f *recordingSystem) DeleteTrayRun() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("DeleteTrayRun"); err != nil {
		return err
	}
	f.TrayRun = ""
	f.TrayRunCmd = ""
	return nil
}

func (f *recordingSystem) ReadTrayRun() (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = f.step("ReadTrayRun")
	if f.TrayRunCmd != "" {
		return f.TrayRunCmd, true, nil
	}
	if f.TrayRun == "" {
		return "", false, nil
	}
	return f.TrayRun, true, nil
}

func (f *recordingSystem) WriteTrayRun(cmd string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("WriteTrayRun|%s", cmd); err != nil {
		return err
	}
	f.TrayRunCmd = cmd
	if cmd == "" {
		f.TrayRun = ""
	}
	return nil
}

func (f *recordingSystem) ReadUninstallKey() ([]RegValue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("ReadUninstallKey"); err != nil {
		return nil, err
	}
	if !f.Installed.Present {
		return nil, nil
	}
	out := make([]RegValue, len(f.Uninst))
	copy(out, f.Uninst)
	return out, nil
}

func (f *recordingSystem) WriteUninstallKey(vals []RegValue) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("WriteUninstallKey|%d", len(vals)); err != nil {
		return err
	}
	f.Uninst = append([]RegValue(nil), vals...)
	f.Installed = Installed{Present: true}
	for _, v := range vals {
		if v.Name == "DisplayVersion" {
			f.Installed.Version = v.Str
		}
		if v.Name == "InstallLocation" {
			f.Installed.Dir = v.Str
		}
	}
	return nil
}

func (f *recordingSystem) DeleteUninstallKey() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("DeleteUninstallKey"); err != nil {
		return err
	}
	f.Uninst = nil
	f.Installed = Installed{}
	return nil
}

func (f *recordingSystem) ReadInstalled() (Installed, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = f.step("ReadInstalled")
	return f.Installed, nil
}

func (f *recordingSystem) DeleteUserKey() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("DeleteUserKey"); err != nil {
		return err
	}
	f.UserKey = false
	return nil
}

type memLog struct{ lines []string }

func (m *memLog) Printf(format string, args ...interface{}) {
	m.lines = append(m.lines, fmt.Sprintf(format, args...))
}

func (f *recordingSystem) MigrateInstallDir(oldDir, newDir string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("MigrateInstallDir|%s|%s", oldDir, newDir); err != nil {
		return nil, err
	}
	if err := ValidateLegacyMigrateDir(oldDir); err != nil {
		return nil, err
	}
	oldDir, newDir = CleanDir(oldDir), CleanDir(newDir)
	prefix := strings.ToLower(oldDir) + `\`
	var copied []string
	seenDir := map[string]bool{}
	fail := func() error {
		if f.FailMigrateAfter > 0 && len(copied) >= f.FailMigrateAfter {
			return fmt.Errorf("injected migrate fail after %d", len(copied))
		}
		return nil
	}
	for path, data := range f.Files {
		lp := strings.ToLower(CleanDir(path))
		if lp == strings.ToLower(oldDir) || !strings.HasPrefix(lp, prefix) {
			continue
		}
		rel := path[len(oldDir)+1:]
		// Parent dirs under newDir (CHK-SETUP-17: failAfter counts dirs too).
		parts := strings.Split(rel, `\`)
		acc := newDir
		for i := 0; i < len(parts)-1; i++ {
			acc = joinDir(acc, parts[i])
			if !seenDir[acc] && !f.Dirs[acc] {
				f.Dirs[acc] = true
				seenDir[acc] = true
				copied = append(copied, acc)
				if err := fail(); err != nil {
					return copied, err
				}
			}
		}
		dst := joinDir(newDir, rel)
		if _, ok := f.Files[dst]; ok {
			continue
		}
		f.Files[dst] = append([]byte(nil), data...)
		copied = append(copied, dst)
		if err := fail(); err != nil {
			return copied, err
		}
	}
	return copied, nil
}

func (f *recordingSystem) RemoveLegacyInstallDir(dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("RemoveLegacyInstallDir|%s", dir); err != nil {
		return err
	}
	if err := ValidateLegacyMigrateDir(dir); err != nil {
		return err
	}
	dir = CleanDir(dir)
	prefix := strings.ToLower(dir) + `\`
	for path := range f.Files {
		lp := strings.ToLower(CleanDir(path))
		if lp == strings.ToLower(dir) || strings.HasPrefix(lp, prefix) {
			delete(f.Files, path)
		}
	}
	delete(f.Dirs, dir)
	return nil
}

func (f *recordingSystem) DeleteLegacyShortcuts() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("DeleteLegacyShortcuts"); err != nil {
		return err
	}
	f.LegacyShortcut = false
	return nil
}
