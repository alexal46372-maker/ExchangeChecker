package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SecretsSDDL is the DACL of secrets.dpapi (phase 6, CHK-CFG-03, Вика's
// decision of 03.10): SYSTEM and Administrators full access, protected (no
// inherited ACEs), so ordinary users cannot read the DPAPI machine-key blob
// (which any local user could otherwise decrypt). It is applied when the
// temp file is created, so the data is never readable under an inherited
// DACL; an existing file with the old (inherited) DACL is fixed at service
// start (Store.SecureSecretsFile, phase 6b) and replaced on the next
// write. settings.json / profiles.json stay Users-read (§12 p.9).
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
	// one writer per target path at a time (CHK-CFG-03): concurrent saves
	// of the same file no longer race their MoveFileEx calls
	unlock := LockPath(filepath.Join(dir, name))
	defer unlock()
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

// Per-path write serialization (CHK-CFG-03).
var (
	pathLocksMu sync.Mutex
	pathLocks   = map[string]*sync.Mutex{}
)

func pathKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	if os.PathSeparator == '\\' {
		path = strings.ToLower(path) // NTFS names are case-insensitive
	}
	return path
}

// LockPath locks the write mutex of path (one per cleaned absolute path)
// and returns the unlock function. Every atomic write and the removal of
// a config file take it.
func LockPath(path string) (unlock func()) {
	k := pathKey(path)
	pathLocksMu.Lock()
	m := pathLocks[k]
	if m == nil {
		m = &sync.Mutex{}
		pathLocks[k] = m
	}
	pathLocksMu.Unlock()
	m.Lock()
	return m.Unlock
}

// replaceBackoff: waits between MoveFileEx attempts (~1.9 s in total).
// Holders like antivirus scanners keep a just-written file open for a few
// milliseconds; without the retry the write failed outright (windows CI
// runs 37124067521: counters-state.json "Access is denied"). It is longer
// than the read retry on purpose: a failed replace loses the write.
var replaceBackoff = []time.Duration{
	5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond,
	80 * time.Millisecond, 160 * time.Millisecond, 250 * time.Millisecond, 250 * time.Millisecond,
	250 * time.Millisecond, 250 * time.Millisecond, 250 * time.Millisecond, 250 * time.Millisecond,
}

// test hook
var replaceSleep = time.Sleep

// replaceFile moves from over to (MoveFileExW on Windows) and retries with
// a short backoff while the file is held open by someone else
// (replaceRetryable); any other error is returned at once.
func replaceFile(from, to string) error {
	err := moveFileReplace(from, to)
	for _, d := range replaceBackoff {
		if err == nil || !replaceRetryable(err) {
			return err
		}
		replaceSleep(d)
		err = moveFileReplace(from, to)
	}
	return err
}

// ReadAttempts / ReadPause: a read that hits a sharing violation (the
// file is being replaced or held by a scanner) is retried (CHK-CFG-03).
const (
	ReadAttempts = 5
	ReadPause    = 50 * time.Millisecond
)

// test hook
var readFile = os.ReadFile

// ReadFileShared is os.ReadFile with up to ReadAttempts attempts, ReadPause
// apart, while the error is a sharing / lock violation or access denied
// (Windows); a missing file and other errors are returned at once.
func ReadFileShared(path string) ([]byte, error) {
	b, err := readFile(path)
	for i := 1; i < ReadAttempts && err != nil && replaceRetryable(err); i++ {
		replaceSleep(ReadPause)
		b, err = readFile(path)
	}
	return b, err
}
