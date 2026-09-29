package sqlcheck

import (
	"encoding/hex"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// counterKey identifies a point across polls: hex _IDRRef, else code, else name.
func counterKey(c RawCounter) string {
	switch {
	case len(c.Key) > 0:
		return hex.EncodeToString(c.Key)
	case strings.TrimSpace(c.ID) != "":
		return "code:" + strings.TrimSpace(c.ID)
	}
	return "name:" + strings.TrimSpace(c.Name)
}

// CounterObservations converts a counters read into ApplyCounters input.
func CounterObservations(raw []RawCounter) []model.CounterObservation {
	out := make([]model.CounterObservation, 0, len(raw))
	for _, c := range raw {
		out = append(out, model.CounterObservation{
			Key: counterKey(c), Name: pointName(RawPoint{Name: c.Name, ID: c.ID}),
			SentNo: c.SentNo, ReceivedNo: c.ReceivedNo,
		})
	}
	return out
}

// applyCounters is the counters-mode step of ListPoints: apply the
// observed counters (read with NOLOCK) to the remembered state, save the
// state, evaluate points (same order as the returned times).
func (b *base) applyCounters(obs []model.CounterObservation, now time.Time) ([]model.CounterTimes, []model.ExchangePoint, error) {
	prev, err := b.opt.Counters.Load(b.profile.ID)
	if err != nil {
		return nil, nil, &Error{Kind: KindConfig, Op: "counters state", Err: err}
	}
	next, times := model.ApplyCounters(prev, obs, now)
	if err := b.opt.Counters.Save(b.profile.ID, next); err != nil {
		return nil, nil, &Error{Kind: KindConfig, Op: "counters state", Err: err}
	}
	return times, model.CounterPoints(b.profile.ID, times, b.opt.Settings, now), nil
}

// counterSampleRows: names only (counters carry no timestamps).
func counterSampleRows(raw []RawCounter) []model.SampleRow {
	out := make([]model.SampleRow, 0, len(raw))
	for _, c := range raw {
		out = append(out, model.SampleRow{Point: pointName(RawPoint{Name: c.Name, ID: c.ID})})
	}
	return out
}
