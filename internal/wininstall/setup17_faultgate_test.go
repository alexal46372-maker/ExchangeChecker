package wininstall

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCHK_SETUP_17_MigrateFailHookGated — EC_MIGRATE_FAIL_AFTER / EC_SETUP_FAIL_AT
// live only under //go:build e2efault (migrate_fault_e2e.go). Release sources
// must not read those env vars (CHK-SETUP-17 review).
func TestCHK_SETUP_17_MigrateFailHookGated(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	off, err := os.ReadFile(filepath.Join(dir, "migrate_fault_off.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(off), "//go:build !e2efault") {
		t.Fatal("migrate_fault_off.go must be !e2efault")
	}
	if migrateTestFailAfter() != 0 {
		t.Fatalf("release migrateTestFailAfter=%d want 0", migrateTestFailAfter())
	}
	if setupTestFailStartService() {
		t.Fatal("release setupTestFailStartService must be false")
	}
	syso, err := os.ReadFile(filepath.Join(dir, "system_windows.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(syso), "EC_MIGRATE_FAIL_AFTER") || strings.Contains(string(syso), "EC_SETUP_FAIL_AT") {
		t.Fatal("system_windows.go must not reference E2E fault env vars")
	}
	e2e, err := os.ReadFile(filepath.Join(dir, "migrate_fault_e2e.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(e2e), "//go:build e2efault") {
		t.Fatal("migrate_fault_e2e.go must be e2efault")
	}
	if !strings.Contains(string(e2e), "EC_MIGRATE_FAIL_AFTER") || !strings.Contains(string(e2e), "EC_SETUP_FAIL_AT") {
		t.Fatal("migrate_fault_e2e.go must define the E2E env hooks")
	}
}
