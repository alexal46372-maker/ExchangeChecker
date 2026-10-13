package history

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Query is one history request (IPC history).
type Query struct {
	ProfileID string
	PointKey  string
	From, To  time.Time
	Kinds     []string
	Limit     int
	Cursor    string
	// Known filters the profiles of the current configuration (deleted
	// profiles are not returned); nil = all.
	Known func(profileID string) bool
	// MaxBytes caps the serialized records (default
	// model.IPCMaxInteractiveReply minus the reply envelope).
	MaxBytes int
	// ScanLines / ScanTime: scan budget (defaults model.HistoryScan*).
	ScanLines int
	ScanTime  time.Duration
}

// envelope: room for the reply around the records.
const envelope = 1024

type cursor struct {
	D string `json:"d"`
	O int64  `json:"o"`
	H string `json:"h"`
}

func bad(key, msg string, args ...string) error {
	return &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: key, Message: "history: " + msg, Args: args}
}

func (q Query) hash() string {
	h := sha256.Sum256([]byte(strings.Join([]string{q.ProfileID, q.PointKey, q.From.UTC().Format(time.RFC3339Nano),
		q.To.UTC().Format(time.RFC3339Nano), strings.Join(q.Kinds, ",")}, "\x00")))
	return hex.EncodeToString(h[:8])
}

func encodeCursor(c cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Query returns one page. Errors are *model.IPCError (invalid_request
// with a history.* key) or ctx.Err().
func (s *Store) Query(ctx context.Context, q Query) (model.HistoryPage, error) {
	page := model.HistoryPage{Records: []model.HistoryRecord{}}
	if s.err != nil {
		return page, bad(model.MsgHistoryDisabled, "disabled: "+s.err.Error())
	}
	switch {
	case q.From.IsZero():
		return page, bad(model.MsgHistoryBadRange, "from is required", "from", model.ValRequired)
	case q.To.IsZero():
		return page, bad(model.MsgHistoryBadRange, "to is required", "to", model.ValRequired)
	case !q.From.Before(q.To):
		return page, bad(model.MsgHistoryBadRange, "from must be before to", "to", model.ValGreaterThan, "from")
	case q.To.Sub(q.From) > model.HistoryMaxRange:
		return page, bad(model.MsgHistoryRangeTooLong, "the interval is longer than 31 days", "to")
	case q.PointKey != "" && q.ProfileID == "":
		return page, bad(model.MsgHistoryBadFilter, "pointKey requires profileId", "profileId", model.ValRequired)
	case q.Limit < 0:
		return page, bad(model.MsgHistoryBadFilter, "limit out of range", "limit", model.ValRange)
	}
	kinds := map[string]bool{}
	for _, k := range q.Kinds {
		switch k {
		case model.HistoryKindPoint:
			kinds[kindPoint] = true
		case model.HistoryKindProfile:
			kinds[kindProfile] = true
		case model.HistoryKindEvent:
			kinds[kindEvent] = true
		default:
			return page, bad(model.MsgHistoryBadFilter, "unknown kind "+strconv.Quote(k), "kinds", model.ValEnum, k)
		}
	}
	limit := q.Limit
	if limit == 0 {
		limit = model.HistoryDefaultLimit
	}
	if limit > model.HistoryMaxLimit {
		limit = model.HistoryMaxLimit
	}
	maxBytes := q.MaxBytes
	if maxBytes <= 0 {
		maxBytes = model.IPCMaxInteractiveReply - envelope
	}
	scanLines, scanTime := q.ScanLines, q.ScanTime
	if scanLines <= 0 {
		scanLines = model.HistoryScanLines
	}
	if scanTime <= 0 {
		scanTime = model.HistoryScanTime
	}
	h := q.hash()
	startDay, endDay := dayOf(q.From), dayOf(q.To)
	var off int64
	if q.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		var c cursor
		if err == nil {
			err = json.Unmarshal(b, &c)
		}
		if err != nil || c.H != h || c.O < 0 || !dayFile.MatchString(c.D+".jsonl") || c.D < startDay || c.D > endDay {
			return page, bad(model.MsgHistoryBadCursor, "invalid cursor", "cursor")
		}
		startDay, off = c.D, c.O
	}
	if err := ctx.Err(); err != nil {
		return model.HistoryPage{}, err
	}
	start := time.Now()
	size, scanned := 2, 0
	stop := func(reason, day string, o int64) (model.HistoryPage, error) {
		page.Truncated, page.TruncatedReason = true, reason
		page.NextCursor = encodeCursor(cursor{D: day, O: o, H: h})
		return page, nil
	}
	for day := startDay; day <= endDay; day = nextDay(day) {
		f, err := openRegular(filepath.Join(s.opt.Dir, day+".jsonl"))
		if err != nil {
			off = 0
			continue
		}
		if _, err := f.Seek(off, io.SeekStart); err != nil {
			f.Close()
			off = 0
			continue
		}
		r := bufio.NewReaderSize(f, 64<<10)
		pos := off
		off = 0
		for {
			b, err := r.ReadBytes('\n')
			if err != nil { // EOF; a torn last line is skipped
				break
			}
			at := pos
			pos += int64(len(b))
			scanned++
			if scanned%1000 == 0 {
				if err := ctx.Err(); err != nil {
					f.Close()
					return model.HistoryPage{}, err
				}
			}
			var d diskRec
			if json.Unmarshal(b, &d) == nil && d.V == 1 && match(q, kinds, d) {
				rec := d.record()
				rb, _ := json.Marshal(rec)
				if len(page.Records) == limit {
					f.Close()
					return stop(model.TruncatedLimit, day, at)
				}
				if size+len(rb)+1 > maxBytes {
					f.Close()
					return stop(model.TruncatedSize, day, at)
				}
				size += len(rb) + 1
				page.Records = append(page.Records, rec)
			}
			if scanned >= scanLines || time.Since(start) >= scanTime {
				f.Close()
				return stop(model.TruncatedScan, day, pos)
			}
		}
		f.Close()
	}
	return page, nil
}

func match(q Query, kinds map[string]bool, d diskRec) bool {
	if d.T.Before(q.From) || !d.T.Before(q.To) {
		return false
	}
	if len(kinds) > 0 && !kinds[d.K] {
		return false
	}
	if q.ProfileID != "" && d.P != q.ProfileID {
		return false
	}
	if q.PointKey != "" && (d.K == kindProfile || d.PK != q.PointKey) {
		return false
	}
	if q.Known != nil && !q.Known(d.P) {
		return false
	}
	return true
}

func nextDay(day string) string {
	t, err := time.ParseInLocation("2006-01-02", day, model.DBZone)
	if err != nil {
		return "9999-12-31~"
	}
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}

func openRegular(p string) (*os.File, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, os.ErrNotExist
	}
	return os.Open(p)
}
