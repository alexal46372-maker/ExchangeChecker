package ipc

import (
	"context"
	"encoding/json"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// SummarySource builds the summary (phase 11, CHK-SVC-07).
type SummarySource interface {
	Summary(ctx context.Context, hours, buckets int) (model.Summary, error)
}

// startSummary validates, then runs the summary asynchronously (cancel
// {ref}; shares the history concurrency limits) and fits it into
// model.IPCMaxInteractiveReply. false: the client is too slow.
func (s *Server) startSummary(c *conn, req model.IPCRequest) bool {
	hours, buckets, _, err := model.SummaryParams(req.Hours, req.Buckets)
	if err != nil {
		return c.send(errorReply(req.ID, err, ""))
	}
	s.mu.Lock()
	busy := s.histN >= model.HistoryMaxGlobal || c.histN >= model.HistoryMaxPerConn
	if !busy {
		s.histN++
		c.histN++
	}
	s.mu.Unlock()
	if busy {
		return c.send(errorReply(req.ID, &model.IPCError{Code: model.ErrCodeBusy, Key: model.MsgHistoryBusy,
			Message: "ipc: a history / summary request is already running"}, ""))
	}
	src := s.Summary
	if !s.startAsync(c, req, func(ctx context.Context) (model.IPCMessage, error) {
		defer func() {
			s.mu.Lock()
			s.histN--
			c.histN--
			s.mu.Unlock()
		}()
		sum, err := src.Summary(ctx, hours, buckets)
		if err != nil {
			return model.IPCMessage{}, err
		}
		fitSummary(&sum, req.ID, model.IPCMaxInteractiveReply)
		return model.IPCMessage{Type: model.MsgSummary, Summary: &sum}, nil
	}) {
		s.mu.Lock()
		s.histN--
		c.histN--
		s.mu.Unlock()
	}
	return true
}

// fitSummary drops whole profiles from the end until the reply frame
// fits max (Truncated); buckets of the kept profiles are never cut.
func fitSummary(sum *model.Summary, id uint64, max int) {
	if sum.Profiles == nil {
		sum.Profiles = []model.SummaryProfile{}
	}
	size := func() int {
		b, _ := json.Marshal(model.IPCMessage{ID: id, Type: model.MsgSummary, Summary: sum})
		return len(b) + 1
	}
	if size() <= max {
		return
	}
	sum.Truncated = true
	// binary search on the number of kept profiles
	all := sum.Profiles
	lo, hi := 0, len(all)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		sum.Profiles = all[:mid]
		if size() <= max {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	sum.Profiles = all[:lo]
}
