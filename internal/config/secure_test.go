package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_CFG_03_SecureSecretsFile — the start-up call does nothing for a
// missing secrets.dpapi (no file is created) and restricts an existing one
// (0600 off Windows; the Windows DACL is checked by
// TestCHK_CFG_03_SecretsDACLAtStart).
func TestCHK_CFG_03_SecureSecretsFile(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "cfg"))
	if present, err := s.SecureSecretsFile(); present || err != nil {
		t.Fatalf("no directory: %v %v", present, err)
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if present, err := s.SecureSecretsFile(); present || err != nil {
		t.Fatalf("missing file: %v %v", present, err)
	}
	if _, err := os.Stat(s.Path(SecretsFile)); !os.IsNotExist(err) {
		t.Fatalf("file created: %v", err)
	}
	if err := os.WriteFile(s.Path(SecretsFile), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if present, err := s.SecureSecretsFile(); !present || err != nil {
		t.Fatalf("existing file: %v %v", present, err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(s.Path(SecretsFile)); fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", fi.Mode().Perm())
		}
	}
}

// TestCHK_CFG_03_BeepEnabledFile — beep.enabled in settings.json: a file
// without the field (before phase 6) and an explicit null read as true;
// false is saved and read back as false; true again as true.
func TestCHK_CFG_03_BeepEnabledFile(t *testing.T) {
	s := NewStore(t.TempDir())
	if st, err := s.LoadSettings(); err != nil || st.Beep.Enabled != model.OptTrue {
		t.Fatalf("missing file: %v %v", st.Beep.Enabled, err)
	}
	old := `{"version":1,"pollIntervalSec":60,"warnMinutes":30,"errorMinutes":60,` +
		`"beep":{"warningFreqHz":800,"warningDurMs":300,"errorFreqHz":1500,"errorDurMs":500,"gapMs":700},"language":"ru","trayAutostart":true}`
	for name, body := range map[string]string{"old file": old, "null": strings.Replace(old, `"gapMs":700`, `"gapMs":700,"enabled":null`, 1)} {
		if err := os.WriteFile(s.Path(SettingsFile), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		st, err := s.LoadSettings()
		if err != nil || st.Beep.Enabled != model.OptTrue || st.Language != model.LangRU {
			t.Fatalf("%s: %+v %v", name, st, err)
		}
	}
	st := model.DefaultSettings()
	st.Beep.Enabled = model.OptFalse
	if err := s.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.Path(SettingsFile))
	if !strings.Contains(string(raw), `"enabled": false`) {
		t.Fatalf("false not written:\n%s", raw)
	}
	if got, err := s.LoadSettings(); err != nil || got.Beep.Enabled != model.OptFalse || got.Beep.BeepEnabled() {
		t.Fatalf("false read back: %v %v", got.Beep.Enabled, err)
	}
	st.Beep.Enabled = model.OptTrue
	if err := s.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadSettings(); got.Beep.Enabled != model.OptTrue {
		t.Fatalf("true read back: %v", got.Beep.Enabled)
	}
}
