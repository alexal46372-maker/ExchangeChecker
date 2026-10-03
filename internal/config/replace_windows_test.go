//go:build windows

package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestCHK_MON_02_ReplaceRetriesWhileHeld — the atomic write survives a
// reader that holds the target WITHOUT FILE_SHARE_DELETE for a while (an
// antivirus / indexer / os.Open): MoveFileEx fails with
// ERROR_ACCESS_DENIED / SHARING_VIOLATION and is retried with backoff
// until the holder closes (windows CI 37124067521).
func TestCHK_MON_02_ReplaceRetriesWhileHeld(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls int32
	orig := moveFileReplace
	moveFileReplace = func(from, to string) error {
		atomic.AddInt32(&calls, 1)
		return orig(from, to)
	}
	defer func() { moveFileReplace = orig }()
	f, err := os.Open(filepath.Join(dir, "state.json")) // no FILE_SHARE_DELETE
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		f.Close()
	}()
	if err := WriteFileAtomic(dir, "state.json", []byte("new"), 0o644); err != nil {
		t.Fatalf("write while held: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "state.json")); string(b) != "new" {
		t.Fatalf("content %q", b)
	}
	n := atomic.LoadInt32(&calls)
	if n < 2 {
		t.Fatalf("MoveFileEx called %d times: the held file did not block the first attempt", n)
	}
	t.Logf("MoveFileEx attempts while held: %d", n)
}

// TestCHK_MON_02_ReplaceGivesUp — a file held for good: the write fails
// after len(replaceBackoff)+1 attempts with the original error; the temp
// file is removed; a non-retryable error is returned at once.
func TestCHK_MON_02_ReplaceGivesUp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	origSleep := replaceSleep
	replaceSleep = func(time.Duration) {}
	defer func() { replaceSleep = origSleep }()
	var calls int32
	orig := moveFileReplace
	moveFileReplace = func(from, to string) error {
		atomic.AddInt32(&calls, 1)
		return orig(from, to)
	}
	defer func() { moveFileReplace = orig }()
	f, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	err = WriteFileAtomic(dir, "state.json", []byte("new"), 0o644)
	var e syscall.Errno
	if err == nil || !errors.As(err, &e) || !replaceRetryable(err) {
		t.Fatalf("held for good: %v", err)
	}
	if n := atomic.LoadInt32(&calls); int(n) != len(replaceBackoff)+1 {
		t.Fatalf("attempts %d, want %d", n, len(replaceBackoff)+1)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("temp file left: %v", ents)
	}
	// not retryable: once
	atomic.StoreInt32(&calls, 0)
	moveFileReplace = func(from, to string) error {
		atomic.AddInt32(&calls, 1)
		return &os.LinkError{Op: "MoveFileEx", Old: from, New: to, Err: syscall.Errno(3)} // ERROR_PATH_NOT_FOUND
	}
	if err := replaceFile("a", "b"); err == nil || atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("non-retryable: %v after %d calls", err, calls)
	}
}

// TestCHK_MON_02_SharedReaderDoesNotBlockReplace — our own reader
// (config.ReadFile / openShared, FILE_SHARE_DELETE) keeps the old content
// and does not make the atomic replace fail even if it holds the file open
// for longer than the whole retry budget.
func TestCHK_MON_02_SharedReaderDoesNotBlockReplace(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := openShared(target)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var calls int32
	orig := moveFileReplace
	moveFileReplace = func(from, to string) error {
		atomic.AddInt32(&calls, 1)
		return orig(from, to)
	}
	defer func() { moveFileReplace = orig }()
	start := time.Now()
	if err := WriteFileAtomic(dir, "state.json", []byte("new"), 0o644); err != nil {
		t.Fatalf("replace while a FILE_SHARE_DELETE reader is open: %v (attempts %d)", err, calls)
	}
	buf := make([]byte, 16)
	n, _ := f.Read(buf)
	if string(buf[:n]) != "old" {
		t.Errorf("open reader sees %q, want the old content", buf[:n])
	}
	if b, _ := ReadFile(target); string(b) != "new" {
		t.Fatalf("new content %q", b)
	}
	t.Logf("attempts %d, %v", atomic.LoadInt32(&calls), time.Since(start))
}
