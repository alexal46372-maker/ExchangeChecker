package monitor

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_CFG_04_SaveSettingsApplies — save_settings calls
// Options.ApplySettings with the saved settings (trayAutostart) after the
// write; a failure there does not undo the save: the reply is ok with the
// new revision, the file holds the new settings, the error goes to the
// log and to Warn; a refused save (conflict) does not apply anything.
func TestCHK_CFG_04_SaveSettingsApplies(t *testing.T) {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	lg := &memLog{}
	var mu sync.Mutex
	var applied []bool
	var warns []string
	var fail error
	opt := baseOpt(newFactory(), FileConfig{Store: st}, &fakeClock{t: t0})
	opt.Log, opt.CommandInterval = lg, -1
	opt.Warn = func(msg string) { mu.Lock(); warns = append(warns, msg); mu.Unlock() }
	opt.ApplySettings = func(s model.Settings) error {
		mu.Lock()
		defer mu.Unlock()
		applied = append(applied, s.TrayAutostart)
		return fail
	}
	m := New(opt)
	m.reloadConfig(true)
	rev0, err := configRevision(m.configWriter())
	if err != nil {
		t.Fatal(err)
	}

	off := model.DefaultSettings()
	off.TrayAutostart = false
	rev1, err := m.SaveSettings(off, rev0, "x")
	if err != nil || rev1 == rev0 {
		t.Fatalf("save off: %q %v", rev1, err)
	}
	mu.Lock()
	if len(applied) != 1 || applied[0] || len(warns) != 0 {
		t.Fatalf("applied %v warns %v", applied, warns)
	}
	fail = errors.New("RegSetValueExW: Access is denied.")
	mu.Unlock()

	rev2, err := m.SaveSettings(model.DefaultSettings(), rev1, "x")
	if err != nil || rev2 == rev1 {
		t.Fatalf("save on with a registry error must succeed: %q %v", rev2, err)
	}
	if cur, err := st.LoadSettings(); err != nil || !cur.TrayAutostart {
		t.Fatalf("settings not saved: %+v %v", cur, err)
	}
	if s := m.Snapshot(); s.ConfigRevision != rev2 || s.Settings == nil || !s.Settings.TrayAutostart {
		t.Fatalf("snapshot after save: %s %+v", s.ConfigRevision, s.Settings)
	}
	mu.Lock()
	if len(applied) != 2 || !applied[1] || len(warns) != 1 || !strings.Contains(warns[0], "Access is denied") {
		t.Fatalf("applied %v warns %v", applied, warns)
	}
	mu.Unlock()
	if !lg.has("settings saved, but applying them failed") {
		t.Error("registry error not logged")
	}

	// a conflict writes nothing and applies nothing
	if _, err := m.SaveSettings(off, rev0, "x"); err == nil {
		t.Fatal("stale revision accepted")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(applied) != 2 {
		t.Errorf("applied on conflict: %v", applied)
	}
}
