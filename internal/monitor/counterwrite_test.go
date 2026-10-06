package monitor

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func counterState(sent int64) model.CounterState {
	return model.CounterState{Version: model.CounterStateVersion,
		Points: map[string]model.CounterPoint{"p": {SentNo: sent}}}
}

// TestCHK_MON_02_WriteDoesNotHoldStateLock — Вика (49d54ea..c163d0f): the
// file write (MoveFileEx retries up to ~1.9 s) runs without s.mu: while a
// write of profile a is blocked, Load / ProfileIDs return at once and a
// Save of profile b updates the in-memory state (its own write waits for
// the write lock, not the state lock).
func TestCHK_MON_02_WriteDoesNotHoldStateLock(t *testing.T) {
	s := OpenFileCounterStore(t.TempDir(), &memLog{})
	inWrite, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.write = func(dir, name string, data []byte, perm os.FileMode) error {
		once.Do(func() { close(inWrite); <-release })
		return config.WriteFileAtomic(dir, name, data, perm)
	}
	doneA := make(chan struct{})
	go func() { _ = s.Save("a", counterState(1)); close(doneA) }()
	<-inWrite // the write of a is in progress (blocked)

	quick := func(what string, f func()) {
		t.Helper()
		done := make(chan struct{})
		go func() { f(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s blocked while the file is written", what)
		}
	}
	quick("Load", func() {
		if st, _ := s.Load("a"); st.Points["p"].SentNo != 1 {
			t.Errorf("Load(a) = %+v", st)
		}
	})
	quick("ProfileIDs", func() { s.ProfileIDs() })
	doneB := make(chan struct{})
	go func() { _ = s.Save("b", counterState(2)); close(doneB) }()
	quick("Load(b) after a concurrent Save(b)", func() {
		for {
			if st, _ := s.Load("b"); st.Points["p"].SentNo == 2 {
				return
			}
			time.Sleep(time.Millisecond)
		}
	})
	close(release)
	<-doneA
	<-doneB
	doc := readCounters(t, s)
	if doc.Profiles["a"].Points["p"].SentNo != 1 || doc.Profiles["b"].Points["p"].SentNo != 2 {
		t.Fatalf("file: %+v", doc.Profiles)
	}
}

// TestCHK_MON_02_WriteGenerations — snapshots are numbered under s.mu; a
// write is skipped when a newer (or the same) generation is already on
// disk, so an older snapshot never overwrites a newer one; LastError is
// the result of the newest generation.
func TestCHK_MON_02_WriteGenerations(t *testing.T) {
	s := OpenFileCounterStore(t.TempDir(), &memLog{})
	var mu sync.Mutex
	var wrote []int64 // SentNo of profile a in each file write
	s.write = func(dir, name string, data []byte, perm os.FileMode) error {
		var doc countersDoc
		_ = json.Unmarshal(data, &doc)
		mu.Lock()
		wrote = append(wrote, doc.Profiles["a"].Points["p"].SentNo)
		mu.Unlock()
		return config.WriteFileAtomic(dir, name, data, perm)
	}
	// gen 1 (a=1) is held between serialization and the write lock until
	// gen 2 (a=1, b=2) has been written
	gen1, gen2Done := make(chan struct{}), make(chan struct{})
	s.beforeWrite = func(gen uint64) {
		if gen == 1 {
			close(gen1)
			<-gen2Done
		}
	}
	done1 := make(chan struct{})
	go func() { _ = s.Save("a", counterState(1)); close(done1) }()
	<-gen1
	_ = s.Save("b", counterState(2)) // gen 2, written at once
	close(gen2Done)
	<-done1
	mu.Lock()
	if len(wrote) != 1 {
		t.Fatalf("writes %v: the older generation 1 must be skipped after generation 2", wrote)
	}
	mu.Unlock()
	if s.written != 2 || s.gen != 2 || s.LastError() != nil {
		t.Fatalf("written %d gen %d lastErr %v", s.written, s.gen, s.LastError())
	}
	doc := readCounters(t, s)
	if doc.Profiles["a"].Points["p"].SentNo != 1 || doc.Profiles["b"].Points["p"].SentNo != 2 {
		t.Fatalf("file: %+v", doc.Profiles)
	}
	// later saves keep the order: each newer generation is written
	s.beforeWrite = nil
	for i := int64(3); i <= 5; i++ {
		_ = s.Save("a", counterState(i))
	}
	if doc := readCounters(t, s); doc.Profiles["a"].Points["p"].SentNo != 5 || s.written != 5 {
		t.Fatalf("after more saves: %+v written %d", doc.Profiles, s.written)
	}
	// Retain writes a new generation as well
	if err := s.Retain(map[string]bool{"a": true}); err != nil {
		t.Fatal(err)
	}
	if doc := readCounters(t, s); len(doc.Profiles) != 1 || s.written != 6 {
		t.Fatalf("after Retain: %+v written %d", doc.Profiles, s.written)
	}
}

func readCounters(t *testing.T, s *FileCounterStore) countersDoc {
	t.Helper()
	b, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	var doc countersDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
