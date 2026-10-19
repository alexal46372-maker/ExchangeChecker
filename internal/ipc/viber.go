package ipc

import (
	"context"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// ViberTester sends viber_test messages (phase 10, CHK-SVC-06;
// notify.Notifier). The token is write-only: it is never in a reply or a
// log line.
type ViberTester interface {
	ViberTest(ctx context.Context, recipientID, token string) (model.ViberTestResult, error)
}

// ViberSettingsSaver: save_settings with viberToken / clearViberToken
// (phase 10, CHK-SVC-06; monitor.Monitor). A ConfigManager without it
// refuses a request carrying a token.
type ViberSettingsSaver interface {
	SaveSettingsViber(st model.Settings, token string, clearToken bool, expectedRevision, account string) (string, error)
}

func viberCommand(cmd string) bool { return cmd == model.CmdViberTest }

// startViber: viber_test, admin-only on every request, asynchronous
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
	s.startAsync(c, req, func(ctx context.Context) (model.IPCMessage, error) {
		res, err := s.Viber.ViberTest(ctx, req.RecipientID, req.ViberToken)
		if err != nil {
			return model.IPCMessage{}, err
		}
		return model.IPCMessage{Type: model.MsgViberTest, ViberTest: &res}, nil
	})
	return true
}
