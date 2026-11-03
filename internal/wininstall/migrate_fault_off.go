//go:build !e2efault

package wininstall

// migrateTestFailAfter is a no-op in every normal / release build.
func migrateTestFailAfter() int { return 0 }

// setupTestFailStartService is always false outside e2efault builds.
func setupTestFailStartService() bool { return false }
