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

	Installed Installed
	Service   ServiceInfo
	Dirs      map[string]bool
	Files     map[string][]byte
	DACLs     map[string]string
	Owners    map[string]bool // path -> takeOwnership called
	EventSrc  string
	TrayRun   string
	Uninst    []RegValue
	UserKey   bool
	FixedOK   bool
	FailAt    string // step prefix that returns error
	RebootRm  bool
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
		return fmt.Errorf("fake fail: %s", s)
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

func (f *recordingSystem) WriteFile(path string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := sha256.Sum256(data)
	if err := f.step("WriteFile|%s|%d|%s", path, len(data), hex.EncodeToString(sum[:8])); err != nil {
		return err
	}
	cp := append([]byte(nil), data...)
	f.Files[path] = cp
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
	if err := f.step("UpdateService|%s|setStart=%v|auto=%v", s.Name, setStart, s.AutoStart); err != nil {
		return err
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
	return "written", nil
}

func (f *recordingSystem) StopTray(installDir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.step("StopTray|%s", installDir)
}

func (f *recordingSystem) DeleteTrayRun() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.step("DeleteTrayRun"); err != nil {
		return err
	}
	f.TrayRun = ""
	return nil
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
