package main

import (
	"context"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// summarySource implements ipc.SummarySource (phase 11, CHK-SVC-07) from
// the snapshot (current profiles, level, freshness; deleted profiles are
// absent) and the history (buckets, lastOkAt).
type summarySource struct {
	historySource
	now func() time.Time
}

func (s summarySource) Summary(ctx context.Context, hours, buckets int) (model.Summary, error) {
	_, _, d, err := model.SummaryParams(hours, buckets)
	if err != nil {
		return model.Summary{}, err
	}
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	snap := s.mon.Snapshot()
	// the last bucket contains now; buckets are aligned to their length
	to := now().Truncate(d).Add(d)
	from := to.Add(-time.Duration(hours) * time.Hour)
	ids := map[string]bool{}
	for _, p := range snap.Profiles {
		ids[p.ProfileID] = true
	}
	series, err := s.h.Summary(ctx, from, buckets, d, ids)
	if err != nil {
		return model.Summary{}, err
	}
	out := model.Summary{From: from.In(model.DBZone), To: to.In(model.DBZone), BucketMinutes: int(d / time.Minute),
		Profiles: make([]model.SummaryProfile, 0, len(snap.Profiles))}
	for _, p := range snap.Profiles {
		sp := model.SummaryProfile{ProfileID: p.ProfileID, Name: p.Name, Level: p.Level, Buckets: series[p.ProfileID].Buckets,
			LastOKAt: series[p.ProfileID].LastOKAt}
		if p.Polled && p.Error == "" {
			lag := -1
			for _, pt := range p.Points {
				if pt.LagMinutes > lag {
					lag = pt.LagMinutes
				}
			}
			if lag >= 0 {
				sp.FreshnessMin = &lag
			}
			if p.Level == model.LevelOK && !p.CheckedAt.IsZero() && (sp.LastOKAt == nil || p.CheckedAt.After(*sp.LastOKAt)) {
				t := p.CheckedAt.In(model.DBZone)
				sp.LastOKAt = &t
			}
		}
		out.Profiles = append(out.Profiles, sp)
	}
	return out, nil
}
