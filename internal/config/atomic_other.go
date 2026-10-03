//go:build !windows

package config

import "os"

// replaceFile is a portable fallback (rename is atomic on POSIX).
func replaceFile(from, to string) error { return os.Rename(from, to) }

// createSecret creates the temp file readable by the owner only.
func createSecret(tmp string) (*os.File, error) {
	return os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
}
