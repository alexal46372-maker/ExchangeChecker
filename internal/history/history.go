package history

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Logger is the line logger of the service.
type Logger interface {
	Printf(format string, args ...interface{})
}

type nopLogger struct{}

func (nopLogger) Printf(string, ...interface{}) {}

// Options configure a Store; zero values = model.History* defaults.
type Options struct {
	// Dir is the history folder (created when missing).
	Dir string
	// Prepare secures Dir after it was created (the service sets the
	// protected SYSTEM + Administrators DACL); an error disables the
	// history (nothing is written to an insecure folder).
	Prepare  func(dir string) error
	Days     int
	DayCap   int64
	TotalCap int64
	// Now is the clock (tests); nil = time.Now.
	Now      func() time.Time
	Log      Logger
	QueueLen int
	// SyncEvery: FlushFileBuffers at most this often (default 5 s).
	SyncEvery time.Duration
}

// capSlack: profile / event records still written above DayCap.
const capSlack = 1 << 20

// Record kinds on disk.
const (
	kindPoint   = "pt"
	kindProfile = "pr"
	kindEvent   = "ev"
	kindRecov   = "rc"
)

// diskRec is one line of a day file (short field names).
type diskRec struct {
	V   int               `json:"v"`
	K   string            `json:"k"`
	T   time.Time         `json:"t"`
	P   string            `json:"p"`
	PK  string            `json:"pk,omitempty"`
	N   string            `json:"n,omitempty"`
	PL  string            `json:"pl,omitempty"`
	L   *model.AlertLevel `json:"l,omitempty"`
	FA  *time.Time        `json:"fa,omitempty"`
	Lag *int              `json:"lag,omitempty"`
	BL  bool              `json:"bl,omitempty"`
	Src string            `json:"src,omitempty"`
	MS  *int64            `json:"ms,omitempty"`
	QM  *int64            `json:"qm,omitempty"`
	DM  *int64            `json:"dm,omitempty"`
	St  string            `json:"st,omitempty"`
	EK  string            `json:"ek,omitempty"`
	F   *model.AlertLevel `json:"f,omitempty"`
	To  *model.AlertLevel `json:"to,omitempty"`
	ID  uint64            `json:"id,omitempty"`
	Run string            `json:"run,omitempty"`
}

var kindNames = map[string]string{kindPoint: model.HistoryKindPoint, kindProfile: model.HistoryKindProfile, kindEvent: model.HistoryKindEvent,
	kindRecov: model.HistoryKindRecovered}

// record converts a disk line to the IPC record (full names).
func (d diskRec) record() model.HistoryRecord {
	return model.HistoryRecord{Kind: kindNames[d.K], CheckedAt: d.T, ProfileID: d.P, PointKey: d.PK, PointName: d.N,
		PlanTable: d.PL, Level: d.L, FreshAt: d.FA, LagMin: d.Lag, Baseline: d.BL, Source: d.Src, LatencyMs: d.MS,
		QueryMs: d.QM, Status: d.St, ErrorKey: d.EK, From: d.F, To: d.To, DurationMin: d.DM, EventID: d.ID, RunID: d.Run}
}

type line struct {
	day  string
	kind string
	b    []byte
}

var dayFile = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.jsonl$`)

func dayOf(t time.Time) string { return t.In(model.DBZone).Format("2006-01-02") }

// Store writes and reads the history.
type Store struct {
	opt     Options
	err     error
	q       chan []line
	done    chan struct{}
	closeMu sync.Mutex
	closed  bool
	dropped int64

	mu   sync.Mutex
	last map[string]time.Time // profile -> CheckedAt of the last record

	// writer goroutine state
	cur      *os.File
	curDay   string
	curSize  int64
	lastSync time.Time
	dirty    bool
	capDay   string
	rotDay   string
	rotAt    time.Time
}

// Open prepares the folder and starts the writer. It never returns nil:
// on failure the store is disabled (Err) and drops every record.
func Open(opt Options) *Store {
	if opt.Days <= 0 {
		opt.Days = model.HistoryDays
	}
	if opt.DayCap <= 0 {
		opt.DayCap = model.HistoryDayCap
	}
	if opt.TotalCap <= 0 {
		opt.TotalCap = model.HistoryTotalCap
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Log == nil {
		opt.Log = nopLogger{}
	}
	if opt.QueueLen <= 0 {
		opt.QueueLen = 256
	}
	if opt.SyncEvery <= 0 {
		opt.SyncEvery = 5 * time.Second
	}
	s := &Store{opt: opt, last: map[string]time.Time{}, done: make(chan struct{})}
	if opt.Dir == "" {
		s.err = errors.New("history: no folder")
	} else if err := os.MkdirAll(opt.Dir, 0o750); err != nil {
		s.err = err
	} else if opt.Prepare != nil {
		s.err = opt.Prepare(opt.Dir)
	}
	if s.err != nil {
		opt.Log.Printf("history disabled: %v", s.err)
		close(s.done)
		return s
	}
	s.q = make(chan []line, opt.QueueLen)
	go s.run()
	return s
}

// Err is the reason the history is disabled (nil = enabled).
func (s *Store) Err() error { return s.err }

// Dropped returns the number of batches dropped because the queue was full.
func (s *Store) Dropped() int64 { return atomic.LoadInt64(&s.dropped) }

func (s *Store) enqueue(ls []line) {
	if s.err != nil || len(ls) == 0 {
		return
	}
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.q <- ls:
	default:
		if atomic.AddInt64(&s.dropped, 1) == 1 {
			s.opt.Log.Printf("history: writer queue full, records dropped")
		}
	}
}

func encode(d diskRec) line {
	d.V = 1
	b, _ := json.Marshal(d)
	return line{day: dayOf(d.T), kind: d.K, b: append(b, '\n')}
}

// knownKey: only keys with a text in model are recorded (never free text).
func knownKey(k string) string {
	if k == "" {
		return ""
	}
	if model.MonitorMessage(k, model.LangEN) != "" || model.SQLMessage(k, model.LangEN) != "" {
		return k
	}
	return ""
}

func profileStatus(ps model.ProfileSnapshot) string {
	if ps.Error == "" {
		switch ps.Level {
		case model.LevelOK:
			return model.HistoryStatusOK
		case model.LevelWarning:
			return model.HistoryStatusWarning
		}
		return model.HistoryStatusError
	}
	switch ps.ErrorKey {
	case model.MsgMonitorTimeout:
		return model.HistoryStatusTimeout
	case model.MsgMonitorBusy:
		return model.HistoryStatusBusy
	case model.MsgMonitorHung:
		return model.HistoryStatusHung
	}
	return model.HistoryStatusError
}

// RecordSnapshot records the profiles of snap that were polled since the
// last call (CheckedAt changed): one profile record and, without a
// profile error, one record per point (monitor OnSnapshot).
func (s *Store) RecordSnapshot(snap model.StatusSnapshot) {
	if s.err != nil {
		return
	}
	var ls []line
	s.mu.Lock()
	for _, ps := range snap.Profiles {
		if !ps.Polled || !ps.Enabled || ps.CheckedAt.IsZero() {
			continue
		}
		if t, ok := s.last[ps.ProfileID]; ok && t.Equal(ps.CheckedAt) {
			continue
		}
		s.last[ps.ProfileID] = ps.CheckedAt
		lvl := ps.Level
		ls = append(ls, encode(diskRec{K: kindProfile, T: ps.CheckedAt, P: ps.ProfileID, L: &lvl, MS: ps.LatencyMs, QM: ps.LastQueryMs,
			St: profileStatus(ps), EK: knownKey(ps.ErrorKey)}))
		if ps.Error != "" {
			continue
		}
		for _, pt := range ps.Points {
			pl := pt.Level
			d := diskRec{K: kindPoint, T: ps.CheckedAt, P: ps.ProfileID, PK: pt.Key, N: pt.Name, PL: pt.PlanTable,
				L: &pl, FA: pt.FreshAt, BL: pt.Baseline, Src: pt.Source}
			if pt.LagMinutes >= 0 {
				lag := pt.LagMinutes
				d.Lag = &lag
			}
			if pt.Message == model.ReasonNoTimestamps {
				d.EK = model.ReasonNoTimestamps
			}
			ls = append(ls, encode(d))
		}
	}
	s.mu.Unlock()
	s.enqueue(ls)
}

// Alert records a transition event (alert.Sink).
func (s *Store) Alert(e model.AlertEvent) {
	if s.err != nil {
		return
	}
	from, to := e.From, e.To
	k := kindEvent
	if e.Kind == model.AlertKindRecovered {
		k = kindRecov
	}
	s.enqueue([]line{encode(diskRec{K: k, DM: e.DurationMin, T: e.At, P: e.ProfileID, PK: e.Key, N: e.PointName, PL: e.PlanTable,
		F: &from, To: &to, ID: e.ID, Run: e.RunID, EK: knownKey(e.ErrorKey)})})
}

// Close flushes the queue, syncs and closes the current file.
func (s *Store) Close() error {
	if s.err != nil {
		return nil
	}
	s.closeMu.Lock()
	if !s.closed {
		s.closed = true
		close(s.q)
	}
	s.closeMu.Unlock()
	<-s.done
	return nil
}

func (s *Store) run() {
	defer close(s.done)
	s.rotate(s.opt.Now())
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case ls, ok := <-s.q:
			if !ok {
				s.closeCur()
				return
			}
			s.write(ls)
		case <-tick.C:
		}
		now := s.opt.Now()
		if s.dirty && now.Sub(s.lastSync) >= s.opt.SyncEvery {
			s.sync(now)
		}
		if dayOf(now) != s.rotDay || now.Sub(s.rotAt) >= time.Hour || now.Before(s.rotAt) {
			s.rotate(now)
		}
	}
}

func (s *Store) sync(now time.Time) {
	if s.cur != nil {
		_ = s.cur.Sync()
	}
	s.dirty, s.lastSync = false, now
}

func (s *Store) closeCur() {
	if s.cur != nil {
		_ = s.cur.Sync()
		_ = s.cur.Close()
		s.cur, s.curDay = nil, ""
	}
}

// openDay opens (and repairs) the file of day for appending.
func (s *Store) openDay(day string) error {
	if s.curDay == day && s.cur != nil {
		return nil
	}
	s.closeCur()
	p := filepath.Join(s.opt.Dir, day+".jsonl")
	if fi, err := os.Lstat(p); err == nil && !fi.Mode().IsRegular() {
		return errors.New("history: " + p + " is not a regular file")
	}
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0o640)
	if err != nil {
		return err
	}
	size, err := repairTail(f)
	if err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Seek(size, io.SeekStart); err != nil {
		_ = f.Close()
		return err
	}
	s.cur, s.curDay, s.curSize = f, day, size
	return nil
}

// repairTail cuts a torn last line (no trailing newline) and returns the
// new size.
func repairTail(f *os.File) (int64, error) {
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := fi.Size()
	if size == 0 {
		return 0, nil
	}
	var one [1]byte
	if _, err := f.ReadAt(one[:], size-1); err != nil {
		return 0, err
	}
	if one[0] == '\n' {
		return size, nil
	}
	buf := make([]byte, 4096)
	end := size
	for end > 0 {
		n := int64(len(buf))
		if end < n {
			n = end
		}
		if _, err := f.ReadAt(buf[:n], end-n); err != nil {
			return 0, err
		}
		if i := bytes.LastIndexByte(buf[:n], '\n'); i >= 0 {
			end = end - n + int64(i) + 1
			return end, f.Truncate(end)
		}
		end -= n
	}
	return 0, f.Truncate(0)
}

func (s *Store) write(ls []line) {
	for len(ls) > 0 {
		day := ls[0].day
		n := 1
		for n < len(ls) && ls[n].day == day {
			n++
		}
		s.writeDay(day, ls[:n])
		ls = ls[n:]
	}
}

func (s *Store) writeDay(day string, ls []line) {
	if err := s.openDay(day); err != nil {
		s.opt.Log.Printf("history: %v", err)
		return
	}
	var buf []byte
	dropped := 0
	for _, l := range ls {
		size := s.curSize + int64(len(buf)) + int64(len(l.b))
		if (l.kind == kindPoint && size > s.opt.DayCap) || size > s.opt.DayCap+capSlack {
			dropped++
			continue
		}
		buf = append(buf, l.b...)
	}
	if dropped > 0 && s.capDay != day {
		s.capDay = day
		s.opt.Log.Printf("history: %s reached the daily limit of %d bytes, point records dropped", day, s.opt.DayCap)
	}
	if len(buf) == 0 {
		return
	}
	n, err := s.cur.Write(buf)
	s.curSize += int64(n)
	s.dirty = true
	if err != nil {
		s.opt.Log.Printf("history: write: %v", err)
	}
}

// rotate removes day files older than Days and the oldest days above
// TotalCap (never today's). Only regular files named YYYY-MM-DD.jsonl are
// touched (no reparse points).
func (s *Store) rotate(now time.Time) {
	s.rotDay, s.rotAt = dayOf(now), now
	ents, err := os.ReadDir(s.opt.Dir)
	if err != nil {
		s.opt.Log.Printf("history: rotation: %v", err)
		return
	}
	today := dayOf(now)
	cutoff := now.In(model.DBZone).AddDate(0, 0, -(s.opt.Days - 1)).Format("2006-01-02")
	type df struct {
		day  string
		size int64
	}
	var keep []df
	var total int64
	for _, e := range ents {
		m := dayFile.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		p := filepath.Join(s.opt.Dir, e.Name())
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if m[1] < cutoff && m[1] != today {
			s.remove(m[1])
			continue
		}
		keep = append(keep, df{m[1], fi.Size()})
		total += fi.Size()
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i].day < keep[j].day })
	for _, f := range keep {
		if total <= s.opt.TotalCap || f.day >= today {
			break
		}
		if s.remove(f.day) {
			total -= f.size
		}
	}
}

func (s *Store) remove(day string) bool {
	if s.curDay == day {
		s.closeCur()
	}
	if err := os.Remove(filepath.Join(s.opt.Dir, day+".jsonl")); err != nil {
		s.opt.Log.Printf("history: rotation: %v", err)
		return false
	}
	return true
}
