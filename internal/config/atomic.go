package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// SecretsSDDL is the DACL of secrets.dpapi (phase 6, CHK-CFG-03, Вика's
// decision of 03.10): SYSTEM and Administrators full access, protected (no
// inherited ACEs), so ordinary users cannot read the DPAPI machine-key blob
// (which any local user could otherwise decrypt). It is applied when the
// temp file is created, so the data is never readable under an inherited
// DACL; an existing file with the old (inherited) DACL is replaced on the
// next write. settings.json / profiles.json stay Users-read (§12 p.9).
const SecretsSDDL = "D:P(A;;FA;;;SY)(A;;FA;;;BA)"

// writeFileAtomic writes data to dir/name via a temp file in the same
// directory, fsync, then replaceFile (MoveFileEx on Windows).
func writeFileAtomic(dir, name string, data []byte, perm os.FileMode) error {
	return writeAtomic(dir, name, data, func(tmp string) (*os.File, error) {
		f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, err
		}
		if err := os.Chmod(tmp, perm); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return nil, err
		}
		return f, nil
	})
}

// writeSecretAtomic is writeFileAtomic with the temp file created under
// SecretsSDDL (Windows; 0600 elsewhere).
func writeSecretAtomic(dir, name string, data []byte) error {
	return writeAtomic(dir, name, data, createSecret)
}

func writeAtomic(dir, name string, data []byte, create func(tmp string) (*os.File, error)) (err error) {
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: create dir: %w", err)
	}
	var rnd [8]byte
	if _, err = rand.Read(rnd[:]); err != nil {
		return fmt.Errorf("config: temp name: %w", err)
	}
	tmp := filepath.Join(dir, name+".tmp-"+hex.EncodeToString(rnd[:]))
	f, err := create(tmp)
	if err != nil {
		return fmt.Errorf("config: create temp: %w", err)
	}
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
	if err = replaceFile(tmp, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("config: replace %s: %w", name, err)
	}
	return nil
}

// WriteFileAtomic is the exported atomic writer (temp file in dir, fsync,
// MoveFileExW MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH on Windows,
// rename elsewhere) for other ProgramData files of the service, e.g. the
// counters state (CHK-MON-02).
func WriteFileAtomic(dir, name string, data []byte, perm os.FileMode) error {
	return writeFileAtomic(dir, name, data, perm)
}
