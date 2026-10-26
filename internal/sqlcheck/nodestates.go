package sqlcheck

import (
	"context"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// ReadNodeStates reads the message numbers of every counters-mode mapping
// of p (primary and additional; keys of additional mappings prefixed with
// their table, as in ListPoints) and applies them to the profile's counter
// state in opt.Counters (the same Load → apply → Save step as ListPoints,
// so it replaces, not accompanies, a ListPoints poll). One read-only
// SELECT … WITH (NOLOCK) per mapping, SQL Server 2008 compatible
// (CHK-SQL-14). No counters-mode mapping: empty slice.
func ReadNodeStates(ctx context.Context, st Store, p model.Profile, opt Options) ([]model.SQLNodeState, error) {
	b := &base{st: st, profile: p, opt: opt.withDefaults()}
	var obs []model.CounterObservation
	owner := map[string]model.Mapping{}
	nodeKeys := map[string]string{}
	for i, m := range p.AllMappings() {
		if m.Mode != model.MappingCounters || usesManualSQL(m) {
			continue
		}
		raw, err := st.Counters(ctx, m, b.opt.MaxPoints)
		if err != nil {
			return nil, err
		}
		for j, o := range CounterObservations(raw) {
			if i > 0 {
				o.Key = m.Table + "/" + o.Key
			}
			if _, dup := owner[o.Key]; dup {
				continue
			}
			owner[o.Key] = m
			nodeKeys[o.Key] = NodeKey(m, raw[j].Key, raw[j].ID, raw[j].Name)
			obs = append(obs, o)
		}
	}
	out := []model.SQLNodeState{}
	if len(obs) == 0 {
		return out, nil
	}
	times, _, err := b.applyCounters(obs, b.opt.Now())
	if err != nil {
		return nil, err
	}
	byKey := map[string]model.CounterObservation{}
	for _, o := range obs {
		byKey[o.Key] = o
	}
	for _, t := range times {
		s := model.NodeStateFrom(byKey[t.Key], t)
		s.Key = nodeKeys[t.Key] // the shared NodeKey, not the counter state key
		m := owner[t.Key]
		s.PlanTable, s.PlanNo, s.PlanLabel = m.PlanTable(), m.PlanNo(), m.PlanLabel
		out = append(out, s)
	}
	return out, nil
}
