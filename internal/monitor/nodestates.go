package monitor

import (
	"context"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/sqlcheck"
)

// Phase 11 (CHK-MON-18): the service's poll reader. Counters-mode
// mappings are read with sqlcheck.ReadNodeStates INSTEAD of ListPoints
// (both apply the counters to the profile's state, so they must never run
// together): it also returns the 1C message numbers (SentNo /
// ReceivedNo) for the stalled detector. Time-based mappings of the same
// profile are still read by ListPoints over a copy of the profile without
// the counters mappings.

// NewNodeStateReader is the default ReaderFactory of the monitor.
func NewNodeStateReader(p model.Profile, password string, opt sqlcheck.Options) (sqlcheck.ReadCloser, error) {
	st, err := sqlcheck.Open(p, password)
	if err != nil {
		return nil, err
	}
	return &nodeStateReader{ReadCloser: sqlcheck.ReaderFor(st, p, opt), st: st, p: p, opt: opt}, nil
}

type nodeStateReader struct {
	sqlcheck.ReadCloser // Test, Discover, ...; Close closes st
	st                  sqlcheck.Store
	p                   model.Profile
	opt                 sqlcheck.Options
}

// ListPoints implements sqlcheck.ExchangeReader.
func (r *nodeStateReader) ListPoints(ctx context.Context) ([]model.ExchangePoint, error) {
	rest, counters := splitCounters(r.p)
	if !counters {
		return r.ReadCloser.ListPoints(ctx)
	}
	var out []model.ExchangePoint
	if rest != nil {
		pts, err := sqlcheck.ReaderFor(r.st, *rest, r.opt).ListPoints(ctx) // not closed: shares st
		if err != nil {
			return nil, err
		}
		out = pts
	}
	states, err := sqlcheck.ReadNodeStates(ctx, r.st, r.p, r.opt)
	if err != nil {
		return nil, err
	}
	now := time.Now
	if r.opt.Now != nil {
		now = r.opt.Now
	}
	return append(out, NodeStatePoints(r.p.ID, states, r.opt.Settings, now())...), nil
}

// splitCounters: whether p has counters-mode mappings (not manual SQL),
// and p without them (nil when nothing else is left).
func splitCounters(p model.Profile) (*model.Profile, bool) {
	var keep []model.Mapping
	counters := false
	for _, m := range p.AllMappings() {
		manual := strings.TrimSpace(m.ManualSQL) != ""
		if m.Mode == model.MappingCounters && !manual {
			counters = true
			continue
		}
		keep = append(keep, m)
	}
	if !counters || len(keep) == 0 {
		return nil, counters
	}
	q := p
	q.Mapping, q.Mappings = keep[0], append([]model.Mapping(nil), keep[1:]...)
	return &q, true
}

// NodeStatePoints turns node states into the same points ListPoints
// returns for counters mappings (model.CounterPoints, plan tags) plus the
// message numbers.
func NodeStatePoints(profileID string, states []model.SQLNodeState, s model.Settings, now time.Time) []model.ExchangePoint {
	times := make([]model.CounterTimes, 0, len(states))
	for _, st := range states {
		times = append(times, model.CounterTimes{Key: st.Key, Name: st.Name, LastSend: st.LastSend, LastRecv: st.LastRecv,
			SendBaseline: st.SendBaseline, RecvBaseline: st.RecvBaseline})
	}
	pts := model.CounterPoints(profileID, times, s, now)
	for i := range pts {
		st := states[i]
		sent, recv := st.SentNo, st.ReceivedNo
		pts[i].SentNo, pts[i].ReceivedNo = &sent, &recv
		pts[i].PlanTable, pts[i].PlanNo, pts[i].PlanLabel = st.PlanTable, st.PlanNo, st.PlanLabel
	}
	return pts
}
