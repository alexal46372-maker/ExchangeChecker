package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
