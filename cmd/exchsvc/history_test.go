package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/history"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/monitor"
)

// TestCHK_SVC_03_HistoryDisabledSource — when the history folder cannot be
// secured (DACL failure) the store is disabled, the service still serves
// the history command (capability announced) and every request, also for
// an unknown profile, answers invalid_request + history.disabled.
func TestCHK_SVC_03_HistoryDisabledSource(t *testing.T) {
	h := history.Open(history.Options{Dir: t.TempDir(), Prepare: func(string) error { return errors.New("acl failed") }})
	defer h.Close()
	if h.Err() == nil {
		t.Fatal("store not disabled")
	}
	src := historySource{h: h, mon: monitor.New(monitor.Options{})}
	now := time.Now()
	from, to := now.Add(-time.Hour), now
	for _, id := range []string{"", "nope"} {
		_, err := src.History(context.Background(), model.IPCRequest{Cmd: model.CmdHistory, ProfileID: id, From: &from, To: &to})
		var ie *model.IPCError
		if !errors.As(err, &ie) || ie.Code != model.ErrCodeInvalidRequest || ie.Key != model.MsgHistoryDisabled {
			t.Errorf("profile %q: %v", id, err)
		}
	}
}
