package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestCHK_MON_02_ReadFileMissing — config.ReadFile (FILE_SHARE_DELETE on
// Windows) reports a missing file like os.ReadFile and reads content.
func TestCHK_MON_02_ReadFileMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadFile(filepath.Join(dir, "nope")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := ReadFile(filepath.Join(dir, "f")); err != nil || string(b) != "abc" {
		t.Fatalf("read: %q %v", b, err)
	}
}
