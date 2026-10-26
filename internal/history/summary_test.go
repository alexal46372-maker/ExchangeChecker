package history

import (
	"context"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_SVC_07_SummarySeries — buckets cover [from, from+n*d), each is
// present (nil = nothing polled), level is the worst profile level, lag
// the largest point lag, lastOkAt the last ok poll; unknown (deleted)
// profiles and records outside the window are ignored.
func TestCHK_SVC_07_SummarySeries(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, nil)
	from := h0.Add(-time.Hour)
	s.RecordSnapshot(snapAt(from.Add(-time.Minute), 1)) // before the window
	s.RecordSnapshot(snapAt(from.Add(2*time.Minute), 2))
	sn := snapAt(from.Add(5*time.Minute), 1)
	sn.Profiles[0].Level = model.LevelWarning
	sn.Profiles[0].Points[0].LagMinutes = 77
	s.RecordSnapshot(sn)
	s.RecordSnapshot(snapAt(from.Add(25*time.Minute), 1))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open(t, dir, nil)
	defer s.Close()
	got, err := s.Summary(context.Background(), from, 6, 10*time.Minute, map[string]bool{"db1": true, "db2": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["db3"] != nil {
		t.Fatalf("profiles %v", got)
	}
	b := got["db1"].Buckets
	if len(b) != 6 || !b[0].T.Equal(from) || !b[5].T.Equal(from.Add(50*time.Minute)) {
		t.Fatalf("buckets %+v", b)
	}
	if b[0].Level == nil || *b[0].Level != model.LevelWarning || b[0].LagMaxMin == nil || *b[0].LagMaxMin != 77 {
		t.Errorf("bucket 0 %+v", b[0])
	}
	if b[1].Level != nil || b[1].LagMaxMin != nil {
		t.Errorf("empty bucket %+v", b[1])
	}
	if b[2].Level == nil || *b[2].Level != model.LevelOK || *b[2].LagMaxMin != 5 {
		t.Errorf("bucket 2 %+v", b[2])
	}
	if l := got["db1"].LastOKAt; l == nil || !l.Equal(from.Add(25*time.Minute)) {
		t.Errorf("lastOk %v", l)
	}
	if e := got["db2"]; e.LastOKAt != nil || e.Buckets[0].Level == nil || *e.Buckets[0].Level != model.LevelError {
		t.Errorf("db2 %+v", e)
	}
}

// TestCHK_SVC_07_SummaryDisabled — a disabled history gives empty
// buckets, no error.
func TestCHK_SVC_07_SummaryDisabled(t *testing.T) {
	s := &Store{err: context.Canceled}
	got, err := s.Summary(context.Background(), h0, 3, time.Hour, map[string]bool{"a": true})
	if err != nil || len(got["a"].Buckets) != 3 || got["a"].Buckets[2].Level != nil {
		t.Fatal(got, err)
	}
}
