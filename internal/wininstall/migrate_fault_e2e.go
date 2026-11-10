//go:build e2efault

package wininstall

import (
	"os"
	"strconv"
	"strings"
)

// migrateTestFailAfter reads EC_MIGRATE_FAIL_AFTER (test-only, e2efault).
// Never part of a release build (CHK-SETUP-17).
func migrateTestFailAfter() int {
	n, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("EC_MIGRATE_FAIL_AFTER")))
	if n < 0 {
		return 0
	}
	return n
}

// setupTestFailStartService is set by EC_SETUP_FAIL_AT=StartService (e2efault).
func setupTestFailStartService() bool { return setupTestFailAt("StartService") }

// setupTestFailAt reports EC_SETUP_FAIL_AT=<step> (e2efault): StartService,
// InpOutDriver (the driver step fails → WARNING, CHK-SETUP-20),
// AfterInpOutDriver (fatal error right after the driver step → rollback).
func setupTestFailAt(step string) bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("EC_SETUP_FAIL_AT")), step)
}
