//go:build windows

package checks_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_SETUP_07_RenameRunning — MoveFileEx renames a *running* image
// (copy of this test binary started with os.StartProcess). Child processes
// are allowed only in checks/ tests (CHK-POL-01).
func TestCHK_SETUP_07_RenameRunning(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	running := filepath.Join(dir, "running.exe")
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(running, data, 0o755); err != nil {
		t.Fatal(err)
	}
	attr := &os.ProcAttr{
		Dir:   dir,
		Files: []*os.File{nil, nil, nil},
		Env:   append(os.Environ(), "EC_RENAME_CHILD=1"),
	}
	proc, err := os.StartProcess(running, []string{running, "-test.run=TestCHK_SETUP_07_renameChildPark", "-test.count=1"}, attr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proc.Kill(); _, _ = proc.Wait() }()
	time.Sleep(500 * time.Millisecond)
	renamed := filepath.Join(dir, "renamed.exe")
	if err := winapi.MoveFileEx(running, renamed, 0); err != nil {
		t.Fatalf("rename running image: %v", err)
	}
	if _, err := os.Stat(running); !os.IsNotExist(err) {
		t.Fatalf("old path still exists: %v", err)
	}
	if _, err := os.Stat(renamed); err != nil {
		t.Fatal(err)
	}
}

// TestCHK_SETUP_07_renameChildPark parks when started as the rename-child.
func TestCHK_SETUP_07_renameChildPark(t *testing.T) {
	if os.Getenv("EC_RENAME_CHILD") != "1" {
		t.Skip("child park")
	}
	time.Sleep(60 * time.Second)
}
