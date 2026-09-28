//go:build windows

package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_CFG_02_SecretsRealDPAPI — secrets.dpapi roundtrip with the real
// DPAPI machine-key protector (CI windows job).
func TestCHK_CFG_02_SecretsRealDPAPI(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "ExchangeChecker"))
	if _, ok := s.Protector.(DPAPIProtector); !ok {
		t.Fatalf("default protector is %T", s.Protector)
	}
	sec := Secrets{"p1": "S3cret!", "p2": "пароль"}
	if err := s.SaveSecrets(sec); err != nil {
		t.Fatalf("SaveSecrets: %v", err)
	}
	raw, err := os.ReadFile(s.Path(SecretsFile))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("S3cret!")) || bytes.Contains(raw, []byte("passwords")) {
		t.Fatal("secrets.dpapi contains plaintext")
	}
	got, err := s.LoadSecrets()
	if err != nil || !reflect.DeepEqual(got, sec) {
		t.Fatalf("LoadSecrets = %v, %v", got, err)
	}
	other := &Store{Dir: s.Dir, Protector: DPAPIProtector{Entropy: []byte("other")}}
	if _, err := other.LoadSecrets(); err == nil {
		t.Error("different entropy decrypted secrets")
	}
}

// TestCHK_CFG_02_AtomicSaveWindows — repeated saves replace the file via
// MoveFileEx and leave no temp files.
func TestCHK_CFG_02_AtomicSaveWindows(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "ExchangeChecker"))
	st := model.DefaultSettings()
	for i := 1; i <= 3; i++ {
		st.WarnMinutes = 10 * i
		st.ErrorMinutes = 10*i + 30
		if err := s.SaveSettings(st); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	got, err := s.LoadSettings()
	if err != nil || got.WarnMinutes != 30 {
		t.Fatalf("LoadSettings = %+v, %v", got, err)
	}
	ents, _ := os.ReadDir(s.Dir)
	if len(ents) != 1 {
		t.Errorf("expected only settings.json, got %d entries", len(ents))
	}
}
