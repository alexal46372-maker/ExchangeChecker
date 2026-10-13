package main

import (
	"context"

	"github.com/alexal46372-maker/ExchangeChecker/internal/history"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/monitor"
)

// historySource serves the IPC history command (phase 9, CHK-SVC-03):
// only profiles of the current configuration are returned (records of a
// deleted profile stay on disk until the rotation removes them).
type historySource struct {
	h   *history.Store
	mon *monitor.Monitor
}

func (s historySource) History(ctx context.Context, req model.IPCRequest) (model.HistoryPage, error) {
	known := map[string]bool{}
	for _, p := range s.mon.Snapshot().Profiles {
		known[p.ProfileID] = true
	}
	if req.ProfileID != "" && !known[req.ProfileID] && s.h.Err() == nil {
		return model.HistoryPage{}, monitor.ErrUnknownProfile
	}
	q := history.Query{ProfileID: req.ProfileID, PointKey: req.PointKey, Kinds: req.Kinds, Limit: req.Limit,
		Cursor: req.Cursor, Known: func(id string) bool { return known[id] }}
	if req.From != nil {
		q.From = *req.From
	}
	if req.To != nil {
		q.To = *req.To
	}
	return s.h.Query(ctx, q)
}
