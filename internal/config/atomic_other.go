//go:build !windows

package config

import "os"

// replaceFile is a portable fallback (rename is atomic on POSIX).
func replaceFile(from, to string) error { return os.Rename(from, to) }

// ReadFile is os.ReadFile (the Windows version opens with
// FILE_SHARE_DELETE so a concurrent replace is not blocked).
func ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// createSecret creates the temp file readable by the owner only.
func createSecret(tmp string) (*os.File, error) {
	return os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
}

// secureSecretFile restricts an existing secrets file to the owner.
func secureSecretFile(path string) error { return os.Chmod(path, 0o600) }
