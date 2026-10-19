package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errTestSharing = errors.New("test: sharing violation")

// withHooks injects the rename / read functions and a retryable error
// (portable: the real Windows errnos are covered by replace_windows_test).
func withHooks(t *testing.T, rename func(from, to string) error, read func(string) ([]byte, error)) *[]time.Duration {
	t.Helper()
	var sleeps []time.Duration
	var mu sync.Mutex
	oRen, oRead, oRetry, oSleep := moveFileReplace, readFile, replaceRetryable, replaceSleep
	if rename != nil {
		moveFileReplace = rename
	}
	if read != nil {
		readFile = read
	}
	replaceRetryable = func(err error) bool { return errors.Is(err, errTestSharing) || oRetry(err) }
	replaceSleep = func(d time.Duration) { mu.Lock(); sleeps = append(sleeps, d); mu.Unlock() }
	t.Cleanup(func() { moveFileReplace, readFile, replaceRetryable, replaceSleep = oRen, oRead, oRetry, oSleep })
	return &sleeps
}

// TestCHK_CFG_03_ReplaceRetryInjected — a rename failing N times with a
// sharing violation is retried until it succeeds (the file lands, the temp
// file is gone); a non-retryable error is returned at once.
func TestCHK_CFG_03_ReplaceRetryInjected(t *testing.T) {
	dir := t.TempDir()
	fails := 3
	var calls int
	sleeps := withHooks(t, func(from, to string) error {
		if calls++; calls <= fails {
			return &os.LinkError{Op: "MoveFileEx", Old: from, New: to, Err: errTestSharing}
		}
		return os.Rename(from, to)
	}, nil)
	if err := writeSecretAtomic(dir, SecretsFile, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, SecretsFile)); string(b) != "new" || calls != fails+1 || len(*sleeps) != fails {
		t.Fatalf("content %q, calls %d, sleeps %v", b, calls, *sleeps)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("temp left %v", ents)
	}
	calls = 0
	moveFileReplace = func(from, to string) error { calls++; return errors.New("other") }
	if err := writeFileAtomic(dir, "x.json", []byte("1"), 0o644); err == nil || calls != 1 {
		t.Fatalf("non-retryable: %v after %d", err, calls)
	}
	// held for good: len(replaceBackoff)+1 attempts, error returned
	calls = 0
	moveFileReplace = func(from, to string) error { calls++; return errTestSharing }
	if err := writeFileAtomic(dir, "x.json", []byte("1"), 0o644); !errors.Is(err, errTestSharing) || calls != len(replaceBackoff)+1 {
		t.Fatalf("give up: %v after %d", err, calls)
	}
}

// TestCHK_CFG_03_ReadRetryInjected — a read hitting a sharing violation
// is retried ReadAttempts times ReadPause apart; missing = at once.
func TestCHK_CFG_03_ReadRetryInjected(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveSecrets(Secrets{"p": "pw"}); err != nil {
		t.Fatal(err)
	}
	var calls int
	fails := ReadAttempts - 1
	sleeps := withHooks(t, nil, func(p string) ([]byte, error) {
		if calls++; calls <= fails {
			return nil, &os.PathError{Op: "open", Path: p, Err: errTestSharing}
		}
		return os.ReadFile(p)
	})
	if sec, err := s.LoadSecrets(); err != nil || sec["p"] != "pw" || calls != ReadAttempts || len(*sleeps) != fails || (*sleeps)[0] != ReadPause {
		t.Fatalf("read retry %v %v calls %d sleeps %v", sec, err, calls, *sleeps)
	}
	calls, fails = 0, 100
	if _, err := s.LoadSecrets(); !errors.Is(err, errTestSharing) || calls != ReadAttempts {
		t.Fatalf("give up %v after %d", err, calls)
	}
	calls, fails = 0, 0
	if b, err := ReadFileShared(filepath.Join(s.Dir, "missing.json")); !os.IsNotExist(err) || b != nil || calls != 1 {
		t.Fatalf("missing %v after %d", err, calls)
	}
}

// TestCHK_CFG_03_WritesSerializedPerPath — concurrent writes of one path
// never overlap (one rename at a time); other paths are not blocked.
func TestCHK_CFG_03_WritesSerializedPerPath(t *testing.T) {
	dir := t.TempDir()
	var inFlight, maxIn int32
	withHooks(t, func(from, to string) error {
		if strings.HasSuffix(to, "a.json") {
			n := atomic.AddInt32(&inFlight, 1)
			for {
				m := atomic.LoadInt32(&maxIn)
				if n <= m || atomic.CompareAndSwapInt32(&maxIn, m, n) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			defer atomic.AddInt32(&inFlight, -1)
		}
		return os.Rename(from, to)
	}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = writeFileAtomic(dir, "a.json", []byte("x"), 0o644) }()
		go func() {
			defer wg.Done()
			_ = writeFileAtomic(filepath.Join(dir, "."), "A.JSON.other", []byte("y"), 0o644)
		}()
	}
	wg.Wait()
	if maxIn != 1 {
		t.Fatalf("overlapping renames of one path: %d", maxIn)
	}
	if pathKey(filepath.Join(dir, "a.json")) != pathKey(filepath.Join(dir, ".", "a.json")) {
		t.Fatal("path key not cleaned")
	}
}
