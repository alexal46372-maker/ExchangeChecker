package ipc

import (
	"context"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// EmailTester sends email_test messages (phase 13b, CHK-IPC-16;
// notify.EmailNotifier). The SMTP password is write-only: never in a
// reply or a log line.
type EmailTester interface {
	EmailTest(ctx context.Context, recipientID, addr, smtpPassword string) (model.EmailTestResult, error)
}

// EmailSettingsSaver: save_settings with viberToken / smtpPassword
// (phase 13b, CHK-CFG-09). Preferred over ViberSettingsSaver when present.
type EmailSettingsSaver interface {
	SaveSettingsEmail(st model.Settings, viberToken string, clearViber bool, smtpPassword string, clearSmtp bool, expectedRevision, account string) (string, error)
}

func emailCommand(cmd string) bool { return cmd == model.CmdEmailTest }

// startEmail: email_test, admin-only, asynchronous (cancel {ref}).
func (s *Server) startEmail(c *conn, req model.IPCRequest) bool {
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
		res, err := s.Email.EmailTest(ctx, req.RecipientID, req.Addr, req.SmtpPassword)
		if err != nil {
			return model.IPCMessage{}, err
		}
		return model.IPCMessage{Type: model.MsgEmailTest, EmailTest: &res}, nil
	})
	return true
}
