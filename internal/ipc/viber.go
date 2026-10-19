package ipc

import (
	"context"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// ViberManager is the Viber notifier of the service (phase 10,
// CHK-SVC-06; notify.Notifier). The token is write-only: it is never in
// a reply or a log line.
type ViberManager interface {
	GetViber() (model.ViberView, error)
	SaveViber(s model.ViberSettings, token string, clear bool, account string) (model.ViberView, error)
	ViberTest(ctx context.Context, receiver, token string) (model.ViberTestResult, error)
}

func viberCommand(cmd string) bool {
	return cmd == model.CmdGetViber || cmd == model.CmdSaveViber || cmd == model.CmdViberTest
}

// startViber: admin-only on every request; viber_test asynchronously
// (cancel {ref}). false: the client is too slow.
func (s *Server) startViber(c *conn, req model.IPCRequest) bool {
	cl, err := s.identify(c)
	if err != nil || !cl.Admin {
		why := "not an elevated administrator"
		if err != nil {
			why = err.Error()
		}
		s.Log.Printf("ipc: %s refused for %q: %s", req.Cmd, cl.Account, why)
		return c.send(errorReply(req.ID, ErrAccessDenied, ""))
	}
	switch req.Cmd {
	case model.CmdGetViber:
		v, err := s.Viber.GetViber()
		if err != nil {
			return c.send(errorReply(req.ID, err, model.ErrCodeSaveFailed))
		}
		return c.send(model.IPCMessage{ID: req.ID, Type: model.MsgViber, Viber: &v})
	case model.CmdSaveViber:
		if req.Viber == nil {
			return c.send(errorReply(req.ID, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberInvalid,
				Message: "ipc: save_viber without viber", Args: []string{"viber", model.ValRequired}}, ""))
		}
		v, err := s.Viber.SaveViber(*req.Viber, req.ViberToken, req.ClearViberToken, cl.Account)
		if err != nil {
			return c.send(errorReply(req.ID, err, model.ErrCodeSaveFailed))
		}
		s.Log.Printf("ipc: save_viber by %q (enabled %t, %d receivers)", cl.Account, v.Settings.Enabled, len(v.Settings.Receivers))
		return c.send(model.IPCMessage{ID: req.ID, Type: model.MsgViber, Result: model.ResultSaved, Viber: &v})
	}
	s.startAsync(c, req, func(ctx context.Context) (model.IPCMessage, error) {
		res, err := s.Viber.ViberTest(ctx, req.Receiver, req.ViberToken)
		if err != nil {
			return model.IPCMessage{}, err
		}
		return model.IPCMessage{Type: model.MsgViberTest, ViberTest: &res}, nil
	})
	return true
}
