//go:build windows

package wintray

import (
	"context"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Phase 9b: service clock, events_since catch-up, history window.

// eventsTimeout bounds one events_since request; catchUpPages bounds the
// pages of one catch-up (the service ring holds 1000 events, 500 a page).
const (
	eventsTimeout = 10 * time.Second
	catchUpPages  = 10
)

// serverNow is the service clock (serverNow of the last snapshot,
// CHK-TRAY-23); the local clock for an older service.
func (a *App) serverNow() time.Time { return a.clock.Now(time.Now()) }

// afterDrain is the phase 9b part of drain: clock, connection
// transitions (catch-up), check_now wait, windows.
func (a *App) afterDrain(q []trayipc.Event) {
	a.clock.Update(a.status.Snapshot, time.Now())
	for _, e := range q {
		if e.Kind != trayipc.EventState {
			continue
		}
		c := e.Status.State == trayipc.StateConnected
		if c && !a.wasConnected {
			a.onConnected()
		}
		a.wasConnected = c
	}
	a.checkStatus(a.status.Snapshot)
	a.updateCheckUI()
}

// onConnected: catch up on the transitions missed while disconnected
// (events_since, CHK-TRAY-25). The first connection only learns the
// position (runId "" -> resetRequired with lastId): get_status already
// carries the active alerts, old events are not replayed.
func (a *App) onConnected() {
	if !a.session.Supported(model.CmdEventsSince) {
		return
	}
	req, ok := a.tracker.Request()
	if !ok {
		req = model.IPCRequest{Cmd: model.CmdEventsSince}
	}
	a.catchUps++
	a.catchUp(req, 0)
}

func (a *App) catchUp(req model.IPCRequest, page int) {
	s := a.session
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), eventsTimeout)
		m, err := s.Do(ctx, req)
		cancel()
		a.runOnUI(func() {
			if err != nil || m.Type != model.MsgEvents {
				if err == nil {
					err = errHistory{m.Code + " " + m.ErrorKey}
				}
				a.logf("tray: events_since: %v", err)
				return
			}
			hadRun := a.tracker.RunID != ""
			fresh, more := a.tracker.Apply(m)
			if m.ResetRequired && hadRun {
				// service restarted or the position left the ring: the
				// active alerts come from get_status, nothing is replayed
				a.debounce.Reset()
				a.logf("tray: events_since: reset required (run %s)", m.RunID)
			}
			a.caughtUp = append(a.caughtUp, fresh...)
			a.feedBalloons(fresh)
			if more && page+1 < catchUpPages {
				if next, ok := a.tracker.Request(); ok {
					a.catchUp(next, page+1)
				}
			}
		})
	}()
}

// canHistory: the service announced history (capability; the button and
// the window are unavailable otherwise).
func (a *App) canHistory() bool { return a.connected() && a.session.Supported(model.CmdHistory) }

// OpenHistory opens the history window of a point row.
func (a *App) OpenHistory(row trayui.StatusRow) *histWindow {
	if row.PointKey == "" || !a.canHistory() {
		if sw := a.statusWin; sw != nil && winapi.IsWindow(sw.hwnd) {
			key := i18n.KeyStHistoryNoPoint
			if !a.canHistory() {
				key = i18n.KeyStHistoryOff
			}
			sw.setInfo(i18n.T(key, a.opt.Lang))
		}
		return nil
	}
	if hw := a.histWin; hw != nil && hw.alive() {
		winapi.DestroyWindow(hw.hwnd) // one history window: the new point replaces it
	}
	hw, err := newHistWindow(a, row, !a.opt.HiddenStatus)
	if err != nil {
		return nil
	}
	a.histWin = hw
	return hw
}
