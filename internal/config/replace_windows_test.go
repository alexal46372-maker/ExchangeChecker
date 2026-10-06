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
//
// Deterministic (windows CI run 37424283107, #119: "MoveFileEx called 1
// times" — the holder was closed by a 150 ms timer that started before
// WriteFileAtomic, and the temp write + fsync took longer, so the first
// attempt already found the file free): the holder is closed by the hook
// right after the first, failed attempt, so the second one succeeds; the
// backoff sleeps are recorded, not slept.
func TestCHK_MON_02_ReplaceRetriesWhileHeld(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "state.json")) // no FILE_SHARE_DELETE
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var sleeps []time.Duration
	origSleep := replaceSleep
	replaceSleep = func(d time.Duration) { sleeps = append(sleeps, d) }
	defer func() { replaceSleep = origSleep }()
	var calls int
	var first error
	orig := moveFileReplace
	moveFileReplace = func(from, to string) error {
		err := orig(from, to)
		if calls++; calls == 1 {
			first = err
			f.Close() // the holder lets go only after the first attempt
		}
		return err
	}
	defer func() { moveFileReplace = orig }()
	if err := WriteFileAtomic(dir, "state.json", []byte("new"), 0o644); err != nil {
		t.Fatalf("write while held: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "state.json")); string(b) != "new" {
		t.Fatalf("content %q", b)
	}
	if first == nil || !replaceRetryable(first) {
		t.Fatalf("first MoveFileEx while held: %v (want ERROR_ACCESS_DENIED / SHARING_VIOLATION)", first)
	}
	if calls != 2 || len(sleeps) != 1 || sleeps[0] != replaceBackoff[0] {
		t.Fatalf("attempts %d, sleeps %v; want 2 attempts and one %v sleep", calls, sleeps, replaceBackoff[0])
	}
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

// TestCHK_MON_02_ReplaceAfterSharedReaderCloses — even a reader with
// FILE_SHARE_DELETE blocks MOVEFILE_REPLACE_EXISTING on Windows (CI run
// 37126745802: ERROR_ACCESS_DENIED for the whole retry budget), so the
// guarantee is the retry: once the reader closes within the budget, the
// write lands. The first attempt's outcome is logged. The reader is
// closed by the hook right after the first attempt (not by a timer, which
// could fire before the first attempt on a slow runner — #119 logged
// "attempts 1"); backoff sleeps are skipped.
func TestCHK_MON_02_ReplaceAfterSharedReaderCloses(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, _ := syscall.UTF16PtrFromString(target)
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|0x4, /* FILE_SHARE_DELETE */
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	origSleep := replaceSleep
	replaceSleep = func(time.Duration) {}
	defer func() { replaceSleep = origSleep }()
	var calls int
	var first error
	orig := moveFileReplace
	moveFileReplace = func(from, to string) error {
		err := orig(from, to)
		if calls++; calls == 1 {
			first = err
			syscall.CloseHandle(h) // the reader closes after the first attempt
		}
		return err
	}
	defer func() { moveFileReplace = orig }()
	if err := WriteFileAtomic(dir, "state.json", []byte("new"), 0o644); err != nil {
		t.Fatalf("write after the reader closed: %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Fatalf("content %q", b)
	}
	if calls > 2 || (first == nil) != (calls == 1) {
		t.Fatalf("attempts %d, first %v: the write must land at most one attempt after the reader closed", calls, first)
	}
	t.Logf("first MoveFileEx with a FILE_SHARE_DELETE reader open: %v; attempts %d", first, calls)
}
