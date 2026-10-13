package history

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

var h0 = time.Date(2026, 10, 13, 12, 0, 0, 0, model.DBZone)

func open(t *testing.T, dir string, mod func(*Options)) *Store {
	t.Helper()
	opt := Options{Dir: dir, Now: func() time.Time { return h0 }}
	if mod != nil {
		mod(&opt)
	}
	s := Open(opt)
	if s.Err() != nil {
		t.Fatal(s.Err())
	}
	return s
}

func snapAt(at time.Time, points int) model.StatusSnapshot {
	fresh := at.Add(-5 * time.Minute)
	ms := int64(42)
	ps := model.ProfileSnapshot{ProfileID: "db1", Name: "DB 1", Enabled: true, Polled: true, CheckedAt: at,
		Level: model.LevelOK, LatencyMs: &ms}
	for i := 0; i < points; i++ {
		ps.Points = append(ps.Points, model.PointStatus{ExchangePoint: model.ExchangePoint{Name: "Магазин " + itoa(i),
			Level: model.LevelOK}, FreshAt: &fresh, LagMinutes: 5, Key: "k" + itoa(i)})
	}
	bad := model.ProfileSnapshot{ProfileID: "db2", Name: "DB 2", Enabled: true, Polled: true, CheckedAt: at,
		Level: model.LevelError, Error: "login failed for user 'sa' on srv01", ErrorKey: model.MsgMonitorConnError}
	off := model.ProfileSnapshot{ProfileID: "db3", Enabled: false, Polled: true, CheckedAt: at}
	return model.StatusSnapshot{Profiles: []model.ProfileSnapshot{ps, bad, off}}
}

func itoa(i int) string { return strconv.Itoa(i) }

func all(t *testing.T, s *Store, q Query) []model.HistoryRecord {
	t.Helper()
	var out []model.HistoryRecord
	for i := 0; i < 10000; i++ {
		p, err := s.Query(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p.Records...)
		if !p.Truncated {
			if p.NextCursor != "" {
				t.Fatal("cursor without truncated")
			}
			return out
		}
		if p.NextCursor == "" || p.TruncatedReason == "" {
			t.Fatalf("truncated without cursor/reason: %+v", p)
		}
		q.Cursor = p.NextCursor
	}
	t.Fatal("too many pages")
	return nil
}

// TestCHK_SVC_03_WriteAndQuery — profile / point / event records, no free
// error text, full field names over IPC, filters, deleted profiles.
func TestCHK_SVC_03_WriteAndQuery(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, nil)
	s.RecordSnapshot(snapAt(h0, 2))
	s.RecordSnapshot(snapAt(h0, 2)) // same CheckedAt: not recorded again
	s.RecordSnapshot(snapAt(h0.Add(time.Minute), 2))
	s.Alert(model.AlertEvent{ID: 7, RunID: "r1", ProfileID: "db1", PointName: "Магазин 0", Key: "k0",
		From: model.LevelOK, To: model.LevelWarning, At: h0.Add(30 * time.Second)})
	s.Close()
	raw, err := os.ReadFile(filepath.Join(dir, "2026-10-13.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "login failed") || strings.Contains(string(raw), "srv01") {
		t.Fatalf("free error text on disk: %s", raw)
	}
	if n := strings.Count(string(raw), "\n"); n != 2*(1+2+1)+1 {
		t.Fatalf("%d lines:\n%s", n, raw)
	}
	q := Query{From: h0.Add(-time.Hour), To: h0.Add(time.Hour)}
	recs := all(t, s, q)
	if len(recs) != 9 {
		t.Fatalf("%d records", len(recs))
	}
	var pr, pt, ev int
	for _, r := range recs {
		switch r.Kind {
		case model.HistoryKindProfile:
			pr++
			if r.ProfileID == "db1" && (r.LatencyMs == nil || *r.LatencyMs != 42 || r.Status != model.HistoryStatusOK) {
				t.Errorf("profile %+v", r)
			}
			if r.ProfileID == "db2" && (r.Status != model.HistoryStatusError || r.ErrorKey != model.MsgMonitorConnError) {
				t.Errorf("profile %+v", r)
			}
		case model.HistoryKindPoint:
			pt++
			if r.FreshAt == nil || r.LagMinutes == nil || *r.LagMinutes != 5 || r.PointKey == "" || r.Level == nil {
				t.Errorf("point %+v", r)
			}
		case model.HistoryKindEvent:
			ev++
			if r.EventID != 7 || r.RunID != "r1" || r.From == nil || *r.To != model.LevelWarning {
				t.Errorf("event %+v", r)
			}
		}
	}
	if pr != 4 || pt != 4 || ev != 1 {
		t.Errorf("profile %d point %d event %d", pr, pt, ev)
	}
	q2 := q
	q2.Kinds = []string{model.HistoryKindEvent}
	if r := all(t, s, q2); len(r) != 1 {
		t.Errorf("kinds: %d", len(r))
	}
	q3 := q
	q3.ProfileID, q3.PointKey = "db1", "k1"
	if r := all(t, s, q3); len(r) != 2 {
		t.Errorf("point filter: %d", len(r))
	}
	q4 := q
	q4.Known = func(id string) bool { return id != "db1" }
	if r := all(t, s, q4); len(r) != 2 {
		t.Errorf("deleted profile filtered: %d", len(r))
	}
}

// TestCHK_SVC_03_TornTail — a torn last line is cut when the file is
// reopened; readers skip it.
func TestCHK_SVC_03_TornTail(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "2026-10-13.jsonl")
	good := `{"v":1,"k":"pr","t":"2026-10-13T11:00:00+03:00","p":"db1","st":"ok"}` + "\n"
	if err := os.WriteFile(p, []byte(good+`{"v":1,"k":"pr","t":"2026-10-13T11:01`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := open(t, dir, nil)
	if r := all(t, s, Query{From: h0.Add(-2 * time.Hour), To: h0.Add(time.Hour)}); len(r) != 1 {
		t.Fatalf("reader with torn tail: %d", len(r))
	}
	s.RecordSnapshot(snapAt(h0, 0))
	s.Close()
	raw, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(raw), good) || strings.Contains(string(raw), "11:01") || !strings.HasSuffix(string(raw), "\n") {
		t.Fatalf("not repaired: %q", raw)
	}
	if r := all(t, s, Query{From: h0.Add(-2 * time.Hour), To: h0.Add(time.Hour)}); len(r) != 3 {
		t.Fatalf("after repair: %d", len(r))
	}
}

// TestCHK_SVC_03_DayCap — above the daily cap point records are dropped,
// profile / event records continue.
func TestCHK_SVC_03_DayCap(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, func(o *Options) { o.DayCap = 2000 })
	for i := 0; i < 20; i++ {
		s.RecordSnapshot(snapAt(h0.Add(time.Duration(i)*time.Minute), 5))
	}
	s.Close()
	recs := all(t, s, Query{From: h0.Add(-time.Hour), To: h0.Add(time.Hour)})
	var pr, pt int
	for _, r := range recs {
		if r.Kind == model.HistoryKindProfile {
			pr++
		} else {
			pt++
		}
	}
	if pr != 40 || pt == 0 || pt >= 100 {
		t.Fatalf("profile %d point %d", pr, pt)
	}
	fi, _ := os.Stat(filepath.Join(dir, "2026-10-13.jsonl"))
	if fi.Size() > 2000+capSlack {
		t.Fatalf("size %d", fi.Size())
	}
}

// TestCHK_SVC_03_Rotation — days older than Days and the oldest above
// TotalCap are removed; other names and directories are untouched.
func TestCHK_SVC_03_Rotation(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, n int) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("x", n-1)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("2026-09-01.jsonl", 10)  // > 30 days
	write("2026-09-14.jsonl", 10)  // 29 days before 10-13: kept by age
	write("2026-10-10.jsonl", 100) // kept
	write("2026-10-11.jsonl", 100)
	write("2026-10-12.jsonl", 100)
	write("2026-10-13.jsonl", 100) // today
	write("notes.txt", 10)
	if err := os.Mkdir(filepath.Join(dir, "2026-01-01.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := open(t, dir, func(o *Options) { o.TotalCap = 250 })
	s.Close()
	for name, want := range map[string]bool{"2026-09-01.jsonl": false, "2026-09-14.jsonl": false, "2026-10-10.jsonl": false,
		"2026-10-11.jsonl": false, "2026-10-12.jsonl": true, "2026-10-13.jsonl": true, "notes.txt": true, "2026-01-01.jsonl": true} {
		_, err := os.Lstat(filepath.Join(dir, name))
		if (err == nil) != want {
			t.Errorf("%s exists=%v want %v", name, err == nil, want)
		}
	}
}

// TestCHK_SVC_03_Paging — replies within 64 KiB, cursor continues without
// gaps or duplicates; limit and scan budget truncate with a cursor.
func TestCHK_SVC_03_Paging(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir, func(o *Options) { o.QueueLen = 4096 })
	for i := 0; i < 300; i++ {
		s.RecordSnapshot(snapAt(h0.Add(time.Duration(i)*time.Second), 9))
	}
	s.Close()
	q := Query{From: h0.Add(-time.Hour), To: h0.Add(time.Hour)}
	p, err := s.Query(context.Background(), q)
	if err != nil || !p.Truncated || p.TruncatedReason != model.TruncatedSize {
		t.Fatalf("first page %v %v %q", err, p.Truncated, p.TruncatedReason)
	}
	if b := mustJSON(t, model.IPCMessage{Type: model.MsgHistory, History: &p}); len(b) > model.IPCMaxInteractiveReply {
		t.Fatalf("reply %d bytes", len(b))
	}
	recs := all(t, s, q)
	if len(recs) != 300*(1+9+1) {
		t.Fatalf("%d records", len(recs))
	}
	seen := map[string]bool{}
	for _, r := range recs {
		k := r.Kind + r.ProfileID + r.PointKey + r.CheckedAt.String()
		if seen[k] {
			t.Fatalf("duplicate %s", k)
		}
		seen[k] = true
	}
	q.Limit = 100
	if p, _ := s.Query(context.Background(), q); len(p.Records) != 100 || p.TruncatedReason != model.TruncatedLimit {
		t.Errorf("limit: %d %q", len(p.Records), p.TruncatedReason)
	}
	q.Limit, q.ScanLines = 0, 10
	if p, _ := s.Query(context.Background(), q); len(p.Records) != 10 || p.TruncatedReason != model.TruncatedScan {
		t.Errorf("scan: %d %q", len(p.Records), p.TruncatedReason)
	}
	q.ScanLines = 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Query(ctx, q); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
}

func keyOf(err error) string {
	var ie *model.IPCError
	if errors.As(err, &ie) && ie.Code == model.ErrCodeInvalidRequest {
		return ie.Key
	}
	return "?"
}

// TestCHK_SVC_03_BadRequests — error keys of invalid requests; a disabled
// history answers history.disabled.
func TestCHK_SVC_03_BadRequests(t *testing.T) {
	s := open(t, t.TempDir(), nil)
	s.Close()
	ok := Query{From: h0.Add(-time.Hour), To: h0}
	for _, c := range []struct {
		mod func(*Query)
		key string
	}{
		{func(q *Query) { q.From = time.Time{} }, model.MsgHistoryBadRange},
		{func(q *Query) { q.To = q.From }, model.MsgHistoryBadRange},
		{func(q *Query) { q.From = q.To.Add(-32 * 24 * time.Hour) }, model.MsgHistoryRangeTooLong},
		{func(q *Query) { q.PointKey = "k" }, model.MsgHistoryBadFilter},
		{func(q *Query) { q.Kinds = []string{"bogus"} }, model.MsgHistoryBadFilter},
		{func(q *Query) { q.Cursor = "!!" }, model.MsgHistoryBadCursor},
		{func(q *Query) { q.Cursor = encodeCursor(cursor{D: "2026-10-13", H: "other"}) }, model.MsgHistoryBadCursor},
	} {
		q := ok
		c.mod(&q)
		if _, err := s.Query(context.Background(), q); keyOf(err) != c.key {
			t.Errorf("%+v: %v, want %s", q, err, c.key)
		}
	}
	d := Open(Options{Dir: t.TempDir(), Prepare: func(string) error { return errors.New("acl") }})
	if d.Err() == nil {
		t.Fatal("prepare failure must disable")
	}
	d.RecordSnapshot(snapAt(h0, 1))
	d.Close()
	if _, err := d.Query(context.Background(), ok); keyOf(err) != model.MsgHistoryDisabled {
		t.Errorf("disabled: %v", err)
	}
}

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
