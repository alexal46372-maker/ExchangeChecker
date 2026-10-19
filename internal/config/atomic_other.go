//go:build !windows

package config

import "os"

// moveFileReplace is a portable fallback (rename is atomic on POSIX).
var moveFileReplace = os.Rename

// replaceRetryable: POSIX has no sharing violations (test hook).
var replaceRetryable = func(error) bool { return false }

// createSecret creates the temp file readable by the owner only.
func createSecret(tmp string) (*os.File, error) {
	return os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
}

// secureSecretFile restricts an existing secrets file to the owner.
func secureSecretFile(path string) error { return os.Chmod(path, 0o600) }
