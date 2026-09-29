package monitor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type memLog struct {
	mu    sync.Mutex
	lines []string
}

func (m *memLog) Printf(format string, args ...interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lines = append(m.lines, fmt.Sprintf(format, args...))
}

func (m *memLog) has(sub string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

var t0 = time.Date(2026, 9, 29, 12, 0, 0, 0, model.DBZone)

var _ model.CounterStateStore = (*FileCounterStore)(nil)

// TestCHK_MON_02_RestartKeepsBaseline — state saved by one "service run"
// is restored by the next; the baseline is not reset, unchanged counters
// keep their time, keys of several plans (Table/ prefix) stay separate.
func TestCHK_MON_02_RestartKeepsBaseline(t *testing.T) {
	dir := t.TempDir()
	s := OpenFileCounterStore(dir, nil)
	obs := []model.CounterObservation{
		{Key: "aa01", Name: "Магазин 1", SentNo: 10, ReceivedNo: 5},
		{Key: "_Node124/aa01", Name: "Магазин 1", SentNo: 1, ReceivedNo: 1}, // same point key, second plan
	}
	prev, _ := s.Load("db1")
	next, _ := model.ApplyCounters(prev, obs, t0)
	if err := s.Save("db1", next); err != nil {
		t.Fatal(err)
	}
	obs[0].SentNo = 11 // exchange happened in plan 1
	next, _ = model.ApplyCounters(next, obs, t0.Add(10*time.Minute))
	if err := s.Save("db1", next); err != nil || s.LastError() != nil {
		t.Fatal(err, s.LastError())
	}
	_ = s.Save("db2", model.CounterState{Points: map[string]model.CounterPoint{"x": {Name: "X"}}})

	// restart: a new store instance over the same file
	s2 := OpenFileCounterStore(dir, nil)
	if ids := s2.ProfileIDs(); len(ids) != 2 || ids[0] != "db1" {
		t.Fatalf("profiles after restart: %v", ids)
	}
	st, _ := s2.Load("db1")
	if len(st.Points) != 2 {
		t.Fatalf("points: %+v", st.Points)
	}
	later := t0.Add(3 * time.Hour)
	_, times := model.ApplyCounters(st, obs, later) // nothing changed while restarting
	byKey := map[string]model.CounterTimes{}
	for _, x := range times {
		byKey[x.Key] = x
	}
	p1, p2 := byKey["aa01"], byKey["_Node124/aa01"]
	if !p1.LastSend.Equal(t0.Add(10*time.Minute)) || p1.SendBaseline {
		t.Errorf("plan 1 send time lost on restart: %+v", p1)
	}
	if !p1.LastRecv.Equal(t0) || !p1.RecvBaseline {
		t.Errorf("plan 1 recv baseline reset on restart: %+v", p1)
	}
	if !p2.LastSend.Equal(t0) || !p2.SendBaseline {
		t.Errorf("plan 2 baseline reset on restart: %+v", p2)
	}
	if p1.LastSend.Location() != model.DBZone {
		t.Error("times must be UTC+3")
	}
	// Load returns a copy
	st.Points["zzz"] = model.CounterPoint{}
	if again, _ := s2.Load("db1"); len(again.Points) != 2 {
		t.Error("Load must return a copy")
	}
	// deleted profiles are dropped
	if err := s2.Retain(map[string]bool{"db1": true}); err != nil {
		t.Fatal(err)
	}
	if ids := OpenFileCounterStore(dir, nil).ProfileIDs(); len(ids) != 1 || ids[0] != "db1" {
		t.Errorf("retain: %v", ids)
	}
	if empty, _ := s2.Load("unknown"); empty.Points == nil || len(empty.Points) != 0 {
		t.Error("unknown profile = empty state")
	}
}

// TestCHK_MON_02_CorruptFile — garbage, wrong version, truncated JSON: no
// crash, state reset, file kept as .corrupt, log entry written.
func TestCHK_MON_02_CorruptFile(t *testing.T) {
	for name, content := range map[string]string{
		"garbage":   "\x00\x01not json",
		"truncated": `{"version":1,"profiles":{"db1":{"version":1,"points":{"k":{"name":"A"`,
		"version":   `{"version":99,"profiles":{}}`,
		"wrongtype": `{"version":1,"profiles":[1,2,3]}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, CountersFile)
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			lg := &memLog{}
			s := OpenFileCounterStore(dir, lg)
			if ids := s.ProfileIDs(); len(ids) != 0 {
				t.Errorf("state not reset: %v", ids)
			}
			if !lg.has("corrupt") || !lg.has("state reset") {
				t.Errorf("no log entry: %v", lg.lines)
			}
			if b, err := os.ReadFile(path + ".corrupt"); err != nil || string(b) != content {
				t.Errorf("corrupt copy: %v", err)
			}
			// the store keeps working and writes a valid file again
			if err := s.Save("db1", model.CounterState{Points: map[string]model.CounterPoint{"k": {Name: "A"}}}); err != nil {
				t.Fatal(err)
			}
			if st, _ := OpenFileCounterStore(dir, nil).Load("db1"); st.Points["k"].Name != "A" {
				t.Errorf("after reset: %+v", st)
			}
		})
	}
	// a profile entry with null points is accepted
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, CountersFile), []byte(`{"version":1,"profiles":{"p":{"version":1,"points":null}}}`), 0o644)
	if st, _ := OpenFileCounterStore(dir, nil).Load("p"); st.Points == nil {
		t.Error("null points")
	}
}

// TestCHK_MON_02_AtomicWrite — concurrent saves never expose a partial
// file to a reader; a failed write keeps the previous file; leftover temp
// files of a crashed write are ignored.
func TestCHK_MON_02_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	s := OpenFileCounterStore(dir, nil)
	big := model.CounterState{Points: map[string]model.CounterPoint{}}
	for i := 0; i < 300; i++ {
		big.Points[fmt.Sprintf("key%03d", i)] = model.CounterPoint{Name: strings.Repeat("n", 50), SentNo: int64(i)}
	}
	if err := s.Save("db0", big); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				_ = s.Save(fmt.Sprintf("db%d", w), big)
			}
		}(w)
	}
	readErr := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				readErr <- nil
				return
			default:
			}
			b, err := os.ReadFile(filepath.Join(dir, CountersFile))
			if err != nil {
				continue // Windows: replaced between open attempts
			}
			var doc countersDoc
			if err := json.Unmarshal(b, &doc); err != nil {
				readErr <- fmt.Errorf("partial file observed (%d bytes): %v", len(b), err)
				return
			}
		}
	}()
	wg.Wait()
	close(stop)
	if err := <-readErr; err != nil {
		t.Fatal(err)
	}
	if s.LastError() != nil {
		t.Fatalf("write error: %v", s.LastError())
	}
	// failing write: previous file intact, error logged, Save still ok
	before, _ := os.ReadFile(filepath.Join(dir, CountersFile))
	lg := &memLog{}
	s.log = lg
	s.write = func(string, string, []byte, os.FileMode) error { return errors.New("disk full") }
	if err := s.Save("db9", big); err != nil {
		t.Errorf("Save must not fail monitoring: %v", err)
	}
	if s.LastError() == nil || !lg.has("disk full") {
		t.Error("write error not reported")
	}
	after, _ := os.ReadFile(filepath.Join(dir, CountersFile))
	if string(before) != string(after) {
		t.Error("failed write modified the file")
	}
	// leftover temp file from a crash is ignored on load
	os.WriteFile(filepath.Join(dir, CountersFile+".tmp-123"), []byte("{broken"), 0o644)
	if ids := OpenFileCounterStore(dir, nil).ProfileIDs(); len(ids) != 4 { // db9 was never written
		t.Errorf("ids %v", ids)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") && e.Name() != CountersFile+".tmp-123" {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}
