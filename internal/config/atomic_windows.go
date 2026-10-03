//go:build windows

package config

import (
	"errors"
	"os"
	"syscall"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Windows errors of MoveFileEx while another process (antivirus, search
// indexer, backup, an editor, any reader) has the target or the temp file
// open. Opening readers with FILE_SHARE_DELETE does not help: windows CI
// (run 37126745802) showed MOVEFILE_REPLACE_EXISTING still failing with
// ERROR_ACCESS_DENIED while such a handle is open, so the retry is the fix.
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

// createSecret creates the temp file with SecretsSDDL applied at creation.
func createSecret(tmp string) (*os.File, error) { return winapi.CreateFileSDDL(tmp, SecretsSDDL) }

// secureSecretFile replaces the DACL of an existing secrets.dpapi with the
// protected SecretsSDDL (SetNamedSecurityInfoW +
// PROTECTED_DACL_SECURITY_INFORMATION): inherited ACEs (e.g. Users-read
// from %ProgramData%) are removed.
func secureSecretFile(path string) error { return winapi.SetFileDACL(path, SecretsSDDL) }
