package ipc

import (
	"context"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TelegramTester sends telegram_test messages (phase 13, CHK-IPC-17;
// notify.TelegramNotifier). The bot token is write-only.
type TelegramTester interface {
	TelegramTest(ctx context.Context, botToken, chatID string) (model.TelegramTestResult, error)
}

// TelegramSettingsSaver: save_settings with viberToken / smtpPassword /
// botToken (phase 13, CHK-CFG-10). Preferred over EmailSettingsSaver.
type TelegramSettingsSaver interface {
	SaveSettingsAll(st model.Settings, sec model.SettingsSecrets, expectedRevision, account string) (string, error)
}

func telegramCommand(cmd string) bool { return cmd == model.CmdTelegramTest }

// startTelegram: telegram_test, admin-only, asynchronous (cancel {ref}).
func (s *Server) startTelegram(c *conn, req model.IPCRequest) bool {
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
		res, err := s.Telegram.TelegramTest(ctx, req.BotToken, req.ChatID)
		if err != nil {
			return model.IPCMessage{}, err
		}
		return model.IPCMessage{Type: model.MsgTelegramTestResult, TelegramTest: &res}, nil
	})
	return true
}
