package config

import (
	"os"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_CFG_10_TelegramTokenAndSettings — telegram.dpapi round trip and
// removal; settings.json keeps settings.telegram without hasToken/status.
func TestCHK_CFG_10_TelegramTokenAndSettings(t *testing.T) {
	s := NewStore(t.TempDir())
	s.Protector = fakeProtector{}
	if s.HasTelegramToken() {
		t.Fatal("token before save")
	}
	tok := "123456789:AAFakeTokenForTests_0123456789abcdef"
	if err := s.SaveTelegramToken(tok); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadTelegramToken(); err != nil || got != tok || !s.HasTelegramToken() {
		t.Fatalf("load %v", err)
	}
	if err := s.SaveTelegramToken(""); err != nil || s.HasTelegramToken() {
		t.Fatalf("remove %v", err)
	}
	st := model.DefaultSettings()
	st.Telegram.Enabled, st.Telegram.ChatID, st.Telegram.HasToken = true, "@ops_chan", true
	st.Telegram.Status = &model.ViberStatus{Sent: 3}
	if err := s.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(s.Path(SettingsFile))
	if strings.Contains(string(raw), `"hasToken": true`) || strings.Contains(string(raw), `"status"`) || !strings.Contains(string(raw), "@ops_chan") {
		t.Fatalf("settings.json %s", raw)
	}
	got, err := s.LoadSettings()
	if err != nil || got.Telegram == nil || got.Telegram.ChatID != "@ops_chan" || !got.Telegram.Enabled {
		t.Fatalf("reload %+v %v", got.Telegram, err)
	}
	bad := model.DefaultSettings()
	bad.Telegram.ChatID = "not a chat"
	if bad.Validate() == nil {
		t.Fatal("bad chatId accepted")
	}
	bad.Telegram.ChatID, bad.Telegram.Enabled = "", true
	if bad.Validate() == nil {
		t.Fatal("enabled without chatId accepted")
	}
	if model.ValidTelegramToken("nope") || !model.ValidTelegramToken(tok) {
		t.Fatal("token format")
	}
}
