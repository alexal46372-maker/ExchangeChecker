package history

import (
	"bufio"
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Series is the per-profile bucket series of Summary (phase 11,
// CHK-SVC-07).
type Series struct {
	Buckets  []model.SummaryBucket
	LastOKAt *time.Time
}

// Summary scans [from, to) for the profiles in ids: n buckets of length
// d starting at from (every bucket present, Level / LagMaxMin nil when
// nothing was polled), and the last ok profile poll. A disabled history
// gives empty buckets.
func (s *Store) Summary(ctx context.Context, from time.Time, n int, d time.Duration, ids map[string]bool) (map[string]*Series, error) {
	out := map[string]*Series{}
	for id := range ids {
		bs := make([]model.SummaryBucket, n)
		for i := range bs {
			bs[i].T = from.Add(time.Duration(i) * d).In(model.DBZone)
		}
		out[id] = &Series{Buckets: bs}
	}
	if s.err != nil {
		return out, nil
	}
	to := from.Add(time.Duration(n) * d)
	scanned := 0
	for day := dayOf(from); day <= dayOf(to); day = nextDay(day) {
		f, err := openRegular(filepath.Join(s.opt.Dir, day+".jsonl"))
		if err != nil {
			continue
		}
		r := bufio.NewReaderSize(f, 64<<10)
		for {
			b, err := r.ReadBytes('\n')
			if err != nil {
				break
			}
			if scanned++; scanned%1000 == 0 && ctx.Err() != nil {
				f.Close()
				return nil, ctx.Err()
			}
			var rec diskRec
			if json.Unmarshal(b, &rec) != nil || rec.V != 1 || (rec.K != kindProfile && rec.K != kindPoint) {
				continue
			}
			sr := out[rec.P]
			if sr == nil || rec.T.Before(from) || !rec.T.Before(to) {
				continue
			}
			bk := &sr.Buckets[int(rec.T.Sub(from)/d)]
			if rec.L != nil && rec.K == kindProfile {
				l := *rec.L
				if bk.Level != nil {
					l = model.Worst(l, *bk.Level)
				}
				bk.Level = &l
				if *rec.L == model.LevelOK && (sr.LastOKAt == nil || rec.T.After(*sr.LastOKAt)) {
					t := rec.T
					sr.LastOKAt = &t
				}
			}
			if rec.K == kindPoint && rec.Lag != nil && (bk.LagMaxMin == nil || *rec.Lag > *bk.LagMaxMin) {
				v := *rec.Lag
				bk.LagMaxMin = &v
			}
		}
		f.Close()
	}
	return out, nil
}
