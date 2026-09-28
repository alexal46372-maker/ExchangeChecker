//go:build !windows

package config

import "os"

// replaceFile is a portable fallback (rename is atomic on POSIX).
func replaceFile(from, to string) error { return os.Rename(from, to) }
