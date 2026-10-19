package history

import (
	"sort"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// aggregator builds the history buckets (IPC history with bucket,
// phase 9 CHK-SVC-03): min / max / avg of latency, query time and lag per
// profile / point and bucket. Event records are not aggregated.
type aggregator struct {
	name   string
	d      time.Duration
	names  map[string]string
	m      map[bkey]*bacc
	order  []bkey
	size   int
	reason string
}

type bkey struct {
	start int64
	p, pk string
}

type bacc struct {
	b             model.HistoryBucket
	lat, qry, lag accum
}

type accum struct {
	n             int
	min, max, sum int64
}

func (a *accum) add(v int64) {
	if a.n == 0 || v < a.min {
		a.min = v
	}
	if a.n == 0 || v > a.max {
		a.max = v
	}
	a.n++
	a.sum += v
}

func (a accum) agg() *model.HistoryAgg {
	if a.n == 0 {
		return nil
	}
	return &model.HistoryAgg{Min: a.min, Max: a.max, Avg: float64(a.sum) / float64(a.n), Count: a.n}
}

// bucketSize: upper estimate of one serialized bucket (bytes).
const bucketSize = 480

func newAggregator(name string, d time.Duration, names map[string]string) *aggregator {
	if d <= 0 {
		return nil
	}
	return &aggregator{name: name, d: d, names: names, m: map[bkey]*bacc{}, size: 64}
}

// add aggregates one disk record; false = the page is full (reason set,
// the record is not included).
func (a *aggregator) add(d diskRec, limit, maxBytes int) bool {
	if d.K != kindPoint && d.K != kindProfile {
		return true
	}
	start := d.T.In(model.DBZone).Truncate(a.d)
	k := bkey{start: start.UnixNano(), p: d.P}
	if d.K == kindPoint {
		k.pk = d.PK
	}
	b := a.m[k]
	if b == nil {
		if len(a.order) >= limit {
			a.reason = model.TruncatedLimit
			return false
		}
		if a.size+bucketSize+len(d.P)+len(d.PK)+len(d.N)+len(a.names[d.P]) > maxBytes {
			a.reason = model.TruncatedSize
			return false
		}
		a.size += bucketSize + len(d.P) + len(d.PK) + len(d.N) + len(a.names[d.P])
		b = &bacc{b: model.HistoryBucket{Start: start, Kind: kindNames[d.K], ProfileID: d.P, ProfileName: a.names[d.P],
			PointKey: k.pk}}
		if d.K == kindPoint {
			b.b.PointName = d.N
		}
		a.m[k] = b
		a.order = append(a.order, k)
	}
	b.b.Records++
	if d.L != nil && (b.b.WorstLevel == nil || *d.L > *b.b.WorstLevel) {
		l := *d.L
		b.b.WorstLevel = &l
	}
	switch d.St {
	case model.HistoryStatusError, model.HistoryStatusTimeout, model.HistoryStatusHung:
		b.b.Errors++
	}
	if d.MS != nil {
		b.lat.add(*d.MS)
	}
	if d.QM != nil {
		b.qry.add(*d.QM)
	}
	if d.Lag != nil {
		b.lag.add(int64(*d.Lag))
	}
	return true
}

func (a *aggregator) result() []model.HistoryBucket {
	out := make([]model.HistoryBucket, 0, len(a.order))
	for _, k := range a.order {
		b := a.m[k]
		b.b.LatencyMs, b.b.QueryMs, b.b.LagMin = b.lat.agg(), b.qry.agg(), b.lag.agg()
		out = append(out, b.b)
	}
	sort.SliceStable(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if !x.Start.Equal(y.Start) {
			return x.Start.Before(y.Start)
		}
		if x.ProfileID != y.ProfileID {
			return x.ProfileID < y.ProfileID
		}
		return x.PointKey < y.PointKey
	})
	return out
}
