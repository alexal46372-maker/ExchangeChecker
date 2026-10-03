//go:build windows

package config

import (
	"errors"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Windows errors of MoveFileEx while another process (antivirus, search
// indexer, backup, an editor, a reader without FILE_SHARE_DELETE) has the
// target or the temp file open.
const (
	errorAccessDenied     syscall.Errno = 5
	errorSharingViolation syscall.Errno = 32
	errorLockViolation    syscall.Errno = 33
)

// replaceBackoff: waits between MoveFileEx attempts (~1.9 s in total).
// Holders like antivirus scanners keep a just-written file open for a few
// milliseconds; without the retry the write failed outright (windows CI
// runs 37124067521: counters-state.json "Access is denied").
var replaceBackoff = []time.Duration{
	5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond,
	80 * time.Millisecond, 160 * time.Millisecond, 250 * time.Millisecond, 250 * time.Millisecond,
	250 * time.Millisecond, 250 * time.Millisecond, 250 * time.Millisecond, 250 * time.Millisecond,
}

// test hooks
var (
	moveFileReplace = winapi.MoveFileReplace
	replaceSleep    = time.Sleep
)

// replaceFile uses MoveFileExW(MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH)
// and retries with a short backoff while the file is held open by someone
// else (ERROR_ACCESS_DENIED / ERROR_SHARING_VIOLATION / ERROR_LOCK_VIOLATION);
// any other error is returned at once.
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

func replaceRetryable(err error) bool {
	var e syscall.Errno
	if !errors.As(err, &e) {
		return false
	}
	return e == errorAccessDenied || e == errorSharingViolation || e == errorLockViolation
}

const fileShareDelete = 0x4

// openShared opens path for reading with FILE_SHARE_READ |
// FILE_SHARE_WRITE | FILE_SHARE_DELETE: unlike os.Open (no
// FILE_SHARE_DELETE), our own reader does not make a concurrent
// MoveFileEx replace of the file fail.
func openShared(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|fileShareDelete,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// ReadFile reads a whole service file (config, counters, acks) through
// openShared; a missing file -> an error satisfying errors.Is(err,
// os.ErrNotExist), like os.ReadFile.
func ReadFile(path string) ([]byte, error) {
	f, err := openShared(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// createSecret creates the temp file with SecretsSDDL applied at creation.
func createSecret(tmp string) (*os.File, error) { return winapi.CreateFileSDDL(tmp, SecretsSDDL) }

// secureSecretFile replaces the DACL of an existing secrets.dpapi with the
// protected SecretsSDDL (SetNamedSecurityInfoW +
// PROTECTED_DACL_SECURITY_INFORMATION): inherited ACEs (e.g. Users-read
// from %ProgramData%) are removed.
func secureSecretFile(path string) error { return winapi.SetFileDACL(path, SecretsSDDL) }
