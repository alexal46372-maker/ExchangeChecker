package monitor

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_MON_10_FirstLoadFailedSettings — Вика (49d54ea..c163d0f): when
// the FIRST configuration load fails (profiles.json broken, nothing
// applied yet), get_status never shows zero settings (thresholds 0,
// trayAutostart=false): it shows settings.json as parsed when that file
// is fine (defaults when it is missing) or no settings at all when it is
// broken too; configError + configErrorKey = monitor.config_invalid say
// why. A settings.json change while profiles.json is still broken is
// published; after the fix the applied settings are shown and the error
// is gone.
func TestCHK_MON_10_FirstLoadFailedSettings(t *testing.T) {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(st.Path(name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mon := func() *Monitor {
		opt := baseOpt(newFactory(), FileConfig{Store: st}, &fakeClock{t: t0})
		opt.Log, opt.CommandInterval = &memLog{}, -1
		m := New(opt)
		m.LoadConfig()
		return m
	}
	wantErr := func(what string, s model.StatusSnapshot) {
		t.Helper()
		if s.ConfigError == "" || s.ConfigErrorKey != model.MsgMonitorConfigInvalid || s.ConfigStale || s.ConfigRevision != "" {
			t.Errorf("%s: configError %q key %q stale %v rev %q", what, s.ConfigError, s.ConfigErrorKey, s.ConfigStale, s.ConfigRevision)
		}
	}
	custom := model.DefaultSettings()
	custom.WarnMinutes, custom.ErrorMinutes, custom.TrayAutostart = 15, 45, false
	if err := st.SaveSettings(custom); err != nil {
		t.Fatal(err)
	}
	write(config.ProfilesFile, "{broken")

	// 1) settings.json fine, profiles.json broken: the real settings
	m := mon()
	s := m.Snapshot()
	wantErr("profiles broken", s)
	if s.Settings == nil || s.Settings.WarnMinutes != 15 || s.Settings.ErrorMinutes != 45 || s.Settings.TrayAutostart ||
		s.Settings.Beep.Enabled != model.OptTrue {
		t.Fatalf("profiles broken: settings %+v", s.Settings)
	}
	// 2) settings.json changes while profiles.json is still broken
	custom.WarnMinutes = 20
	if err := st.SaveSettings(custom); err != nil {
		t.Fatal(err)
	}
	m.reloadConfig(false)
	if s = m.Snapshot(); s.Settings == nil || s.Settings.WarnMinutes != 20 {
		t.Fatalf("settings change not published: %+v", s.Settings)
	}
	wantErr("profiles still broken", s)

	// 3) both broken: no settings at all (omitted in JSON)
	write(config.SettingsFile, "{broken")
	m.reloadConfig(false)
	s = m.Snapshot()
	wantErr("both broken", s)
	if s.Settings != nil {
		t.Fatalf("both broken: settings %+v", s.Settings)
	}
	if b, _ := json.Marshal(s); strings.Contains(string(b), `"settings"`) {
		t.Fatalf("both broken: JSON %s", b)
	}
	// a fresh service with both files broken: the same
	if s = mon().Snapshot(); s.Settings != nil {
		t.Fatalf("fresh, both broken: %+v", s.Settings)
	}

	// 4) settings.json missing: the defaults (what the service would apply)
	if err := os.Remove(st.Path(config.SettingsFile)); err != nil {
		t.Fatal(err)
	}
	s = mon().Snapshot()
	wantErr("settings missing", s)
	if s.Settings == nil || *s.Settings != model.DefaultSettings().Normalized() {
		t.Fatalf("settings missing: %+v", s.Settings)
	}

	// 5) profiles.json fixed: applied settings, no error, a revision
	if err := st.SaveSettings(custom); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProfiles([]model.Profile{}); err != nil {
		t.Fatal(err)
	}
	m.reloadConfig(false)
	s = m.Snapshot()
	if s.Settings == nil || s.Settings.WarnMinutes != 20 || s.ConfigError != "" || s.ConfigErrorKey != "" || s.ConfigRevision == "" {
		t.Fatalf("fixed: settings %+v err %q key %q rev %q", s.Settings, s.ConfigError, s.ConfigErrorKey, s.ConfigRevision)
	}
}
