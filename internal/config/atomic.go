package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to dir/name via a temp file in the same
// directory, fsync, then replaceFile (MoveFileEx on Windows).
func writeFileAtomic(dir, name string, data []byte, perm os.FileMode) (err error) {
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: create dir: %w", err)
	}
	f, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		return fmt.Errorf("config: create temp: %w", err)
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("config: write %s: %w", name, err)
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("config: sync %s: %w", name, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("config: close %s: %w", name, err)
	}
	if err = os.Chmod(tmp, perm); err != nil {
		return fmt.Errorf("config: chmod %s: %w", name, err)
	}
	if err = replaceFile(tmp, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("config: replace %s: %w", name, err)
	}
	return nil
}
