package traysettings

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_94_TelegramEditor — the page model: requests carry the
// whole settings, botToken only when typed, clearBotToken; invalid chat id
// / token focus their fields; telegram_test needs a token and a chat id;
// rate_limited shows the seconds left.
func TestCHK_TRAY_94_TelegramEditor(t *testing.T) {
	e := NewEditor(Admin)
	s := model.DefaultSettings()
	s.Telegram.ChatID = "-100"
	e.LoadConfig(model.ConfigView{Settings: s}, "r1")
	if !e.CanEditTelegram() || e.TelegramUnsaved() || e.Telegram.ChatID != "-100" {
		t.Fatalf("load %+v", e.Telegram)
	}
	if _, pr := e.TelegramTestRequest(); pr == nil || pr.Field != FieldTgToken {
		t.Fatalf("no token: %+v", pr)
	}
	e.Telegram.Token = "bad"
	if _, pr := e.TelegramRequest(); pr == nil || pr.Field != FieldTgToken {
		t.Fatalf("bad token: %+v", pr)
	}
	e.Telegram.Token = "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ_abc"
	e.Telegram.ChatID = "not a chat"
	if _, pr := e.TelegramRequest(); pr == nil || pr.Field != FieldTgChat {
		t.Fatalf("bad chat: %+v", pr)
	}
	e.Telegram.ChatID, e.Telegram.Enabled = "@ops_chan", true
	req, pr := e.TelegramRequest()
	if pr != nil || req.BotToken != e.Telegram.Token || req.ClearBotToken || req.ExpectedRevision != "r1" ||
		req.Settings.Telegram.ChatID != "@ops_chan" || !req.Settings.Telegram.Enabled || req.Settings.Viber == nil {
		t.Fatalf("request %+v %v", req, pr)
	}
	e.TelegramDone(model.IPCMessage{Revision: "r2"}, req)
	if !e.Settings.Telegram.HasToken || e.Revision != "r2" || e.TelegramUnsaved() {
		t.Fatal("done")
	}
	tr, pr := e.TelegramTestRequest()
	if pr != nil || tr.Cmd != model.CmdTelegramTest || tr.BotToken != "" || tr.ChatID != "" {
		t.Fatalf("test of saved values %+v %v", tr, pr)
	}
	if txt := e.TelegramTestText(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeRateLimited, RetryAfterMs: 4200}); txt != "Тест Telegram — не чаще раза в 10 с; повторите через 5 с." {
		t.Errorf("rate %q", txt)
	}
	if txt := e.TelegramTestText(model.IPCMessage{Type: model.MsgTelegramTestResult, TelegramTest: &model.TelegramTestResult{ErrorKey: model.MsgTelegramChatInvalid}}); txt != "Тест Telegram: не доставлено. Чат Telegram не найден или бот не добавлен в чат." {
		t.Errorf("fail %q", txt)
	}
	e.Telegram.ClearToken = true
	req, _ = e.TelegramRequest()
	if !req.ClearBotToken || req.BotToken != "" {
		t.Errorf("clear %+v", req)
	}
}
