package ipc

import (
	"context"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// AlarmTester runs the optional Viber message of alarm_test (phase 11,
// CHK-IPC-11). The server itself builds and pushes the test alert.
type AlarmTester interface {
	AlarmTest(ctx context.Context, e model.AlertEvent, viber bool, account string) (model.AlarmTestResult, error)
}

// TestAlert builds the test alert of level (pushed only, never stored).
func TestAlert(level model.AlertLevel, now time.Time) model.AlertEvent {
	return model.AlertEvent{ProfileID: model.AlarmTestProfileID, ProfileName: "Тест / Test", From: model.LevelOK, To: level,
		At: now.In(model.DBZone), Kind: model.AlertKindAlert, Test: true}
}

// startAlarmTest: admin only (every request), level check, one per
// service per model.AlarmTestInterval, then the push and the tester
// asynchronously (cancel {ref}). false: the client is too slow.
func (s *Server) startAlarmTest(c *conn, req model.IPCRequest) bool {
	cl, err := s.identify(c)
	if err != nil || !cl.Admin {
		why := "not an elevated administrator"
		if err != nil {
			why = err.Error()
		}
		s.Log.Printf("ipc: %s refused for %q: %s", req.Cmd, cl.Account, why)
		return c.send(errorReply(req.ID, ErrAccessDenied, ""))
	}
	level, ok := model.AlarmLevelOf(req.Level)
	if !ok {
		return c.send(errorReply(req.ID, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgAlarmTestLevel,
			Message: "ipc: alarm_test level must be warning or error", Args: []string{"level", model.ValEnum}}, ""))
	}
	s.mu.Lock()
	s.initLocked()
	now := s.now()
	if !s.alarmTestAt.IsZero() && now.Sub(s.alarmTestAt) < model.AlarmTestInterval {
		wait := model.AlarmTestInterval - now.Sub(s.alarmTestAt)
		s.mu.Unlock()
		ms := int64((wait + time.Millisecond - 1) / time.Millisecond)
		return c.send(errorReply(req.ID, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgMonitorRateLimited,
			Message: "ipc: one alarm_test per 10 s", RetryAfterMs: ms}, ""))
	}
	s.alarmTestAt = now
	s.mu.Unlock()
	e := TestAlert(level, now)
	s.Log.Printf("ipc: alarm_test %s by %q (viber %t)", req.Level, cl.Account, req.Viber)
	s.broadcastTo(model.IPCMessage{Type: model.MsgAlert, Alert: &e}, model.ClientCapAlarmTest)
	s.startAsync(c, req, func(ctx context.Context) (model.IPCMessage, error) {
		res, err := s.AlarmTest.AlarmTest(ctx, e, req.Viber, cl.Account)
		if err != nil {
			return model.IPCMessage{}, err
		}
		res.OK = true
		return model.IPCMessage{Type: model.MsgAlarmTest, AlarmTest: &res}, nil
	})
	return true
}
