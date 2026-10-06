package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// fakeRun stands in for HKLM Run (config.RunRegistry) on every platform.
type fakeRun struct {
	mu     sync.Mutex
	vals   map[string]string
	setErr error
	delErr error
	sets   int
}

func (f *fakeRun) Get(name string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.vals[name]
	return v, ok, nil
}

func (f *fakeRun) Set(name, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets++
	if f.setErr != nil {
		return f.setErr
	}
	f.vals[name] = value
	return nil
}

func (f *fakeRun) Delete(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.delErr != nil {
		return f.delErr
	}
	delete(f.vals, name)
	return nil
}

func (f *fakeRun) value() (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.vals[config.TrayRunValue]
	return v, ok
}

const testTrayCmd = `"C:\Program Files\ExchangeChecker\ExchangeCheckerTray.exe"`

func adminSvc(reg config.RunRegistry, cmd string) func(*appConfig) {
	return func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Admin: true, Account: `TEST\admin`}, nil }
		ac.Autostart, ac.TrayCommand = reg, cmd
	}
}

func saveAutostart(t *testing.T, dir string, on bool) {
	t.Helper()
	st := model.DefaultSettings()
	st.TrayAutostart = on
	if err := config.NewStore(dir).SaveSettings(st); err != nil {
		t.Fatal(err)
	}
}

// TestCHK_CFG_04_ServiceStartReconciles — at start the service brings
// HKLM Run (fake) in line with settings.trayAutostart: no settings.json
// (default on) -> written; a stale path -> rewritten; the right value ->
// untouched; off -> removed; off and missing -> nothing to do.
func TestCHK_CFG_04_ServiceStartReconciles(t *testing.T) {
	cases := []struct {
		name     string
		settings *bool // nil = no settings.json
		before   map[string]string
		want     string // "" = absent
	}{
		{"default on, missing", nil, map[string]string{}, testTrayCmd},
		{"on, stale path", boolp(true), map[string]string{config.TrayRunValue: `C:\old\exchtray.exe`}, testTrayCmd},
		{"on, equal", boolp(true), map[string]string{config.TrayRunValue: testTrayCmd}, testTrayCmd},
		{"off, present", boolp(false), map[string]string{config.TrayRunValue: testTrayCmd, "Other": "x"}, ""},
		{"off, missing", boolp(false), map[string]string{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.settings != nil {
				saveAutostart(t, dir, *c.settings)
			}
			reg := &fakeRun{vals: c.before}
			other, hadOther := c.before["Other"]
			s := startSvcWith(t, dir, 40, nil, &fakeEvents{}, adminSvc(reg, testTrayCmd))
			defer s.stop()
			v, ok := reg.value()
			if c.want == "" && ok || c.want != "" && v != c.want {
				t.Fatalf("Run value %q %v, want %q", v, ok, c.want)
			}
			reg.mu.Lock()
			defer reg.mu.Unlock()
			if hadOther && reg.vals["Other"] != other {
				t.Error("another Run value was touched")
			}
			if c.name == "on, equal" && reg.sets != 0 {
				t.Errorf("equal value rewritten (%d writes)", reg.sets)
			}
		})
	}
}

func boolp(b bool) *bool { return &b }

// TestCHK_CFG_04_ServiceSaveSettings — save_settings over IPC (admin):
// trayAutostart=false removes the Run value, true writes the quoted path;
// with a registry error the settings are still saved (ok + new revision),
// the error goes to the service log and the Event Log (warning).
func TestCHK_CFG_04_ServiceSaveSettings(t *testing.T) {
	dir := t.TempDir()
	reg := &fakeRun{vals: map[string]string{}}
	ev := &fakeEvents{}
	s := startSvcWith(t, dir, 40, nil, ev, adminSvc(reg, testTrayCmd))
	defer s.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if v, _ := reg.value(); v != testTrayCmd {
		t.Fatalf("start: %q", v)
	}
	cfg, err := s.c.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	off := model.DefaultSettings()
	off.TrayAutostart = false
	r, err := s.c.SaveSettings(ctx, off, cfg.Revision)
	if err != nil || r.Type != model.MsgOK {
		t.Fatalf("save off: %+v %v", r, err)
	}
	if v, ok := reg.value(); ok {
		t.Fatalf("off: value still there: %q", v)
	}
	r, err = s.c.SaveSettings(ctx, model.DefaultSettings(), r.Revision)
	if err != nil || r.Type != model.MsgOK {
		t.Fatalf("save on: %+v %v", r, err)
	}
	if v, _ := reg.value(); v != testTrayCmd {
		t.Fatalf("on: %q", v)
	}
	// registry error: the settings are saved anyway
	reg.mu.Lock()
	reg.delErr = errors.New("RegDeleteValueW: Access is denied.")
	reg.mu.Unlock()
	r2, err := s.c.SaveSettings(ctx, off, r.Revision)
	if err != nil || r2.Type != model.MsgOK || r2.Revision == r.Revision {
		t.Fatalf("save with a registry error: %+v %v", r2, err)
	}
	if cur, err := config.NewStore(dir).LoadSettings(); err != nil || cur.TrayAutostart {
		t.Fatalf("settings.json not saved: %+v %v", cur, err)
	}
	if !ev.has("Access is denied") {
		t.Errorf("no Event Log warning: %v", ev.got)
	}
	if !logHas(t, dir, "settings saved, but applying them failed") {
		t.Error("no warning in the service log")
	}
	// the service still serves
	if st, err := s.c.Call(ctx, model.CmdGetStatus); err != nil || st.Status == nil || st.Status.Settings.TrayAutostart {
		t.Fatalf("get_status after the error: %+v %v", st, err)
	}
}

// TestCHK_CFG_04_ServiceRegistryErrorAtStart — a registry error at start
// (access denied) does not stop the service: it starts, serves get_status,
// the warning is in the log and the Event Log; an unreadable settings.json
// leaves the Run value unchanged (warning).
func TestCHK_CFG_04_ServiceRegistryErrorAtStart(t *testing.T) {
	dir := t.TempDir()
	reg := &fakeRun{vals: map[string]string{}, setErr: errors.New("RegSetValueExW: Access is denied.")}
	ev := &fakeEvents{}
	s := startSvcWith(t, dir, 40, nil, ev, adminSvc(reg, testTrayCmd))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if st, err := s.c.Call(ctx, model.CmdGetStatus); err != nil || st.Type != model.MsgStatus {
		t.Fatalf("get_status: %+v %v", st, err)
	}
	s.stop()
	if !ev.has("autostart at start") || !ev.has("Access is denied") {
		t.Errorf("no Event Log warning: %v", ev.got)
	}
	if !logHas(t, dir, "warning: autostart at start") {
		t.Error("no warning in the service log")
	}

	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, config.SettingsFile), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg2 := &fakeRun{vals: map[string]string{config.TrayRunValue: `C:\keep\me.exe`}}
	ev2 := &fakeEvents{}
	s2 := startSvcWith(t, dir2, 40, nil, ev2, adminSvc(reg2, testTrayCmd))
	s2.stop()
	if v, _ := reg2.value(); v != `C:\keep\me.exe` {
		t.Errorf("unreadable settings.json changed the Run value: %q", v)
	}
	if !ev2.has("left unchanged") {
		t.Errorf("no warning for unreadable settings: %v", ev2.got)
	}
}

// TestCHK_CFG_04_ServiceNotManaged — without a registry (console mode,
// tests, non-Windows) nothing is touched; the default command is the
// quoted ExchangeCheckerTray.exe next to the running executable.
func TestCHK_CFG_04_ServiceNotManaged(t *testing.T) {
	if f := trayAutostart(appConfig{}, stderrLog{}); f != nil {
		t.Fatal("nil registry must give no autostart function")
	}
	reg := &fakeRun{vals: map[string]string{}}
	f := trayAutostart(appConfig{Autostart: reg}, stderrLog{})
	if err := f.Apply(model.DefaultSettings()); err != nil || f.ErrorCode() != "" {
		t.Fatal(err, f.ErrorCode())
	}
	exe, _ := os.Executable()
	want := `"` + filepath.Join(filepath.Dir(exe), config.TrayExeName) + `"`
	if v, _ := reg.value(); v != want {
		t.Fatalf("default command %q, want %q", v, want)
	}
	if config.MachineRunRegistry() == nil && isWindows() {
		t.Fatal("no HKLM Run registry on Windows")
	}
}

func logHas(t *testing.T, dir, sub string) bool {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(dir, LogFile))
	return strings.Contains(string(b), sub)
}

func isWindows() bool { return filepath.Separator == '\\' }
