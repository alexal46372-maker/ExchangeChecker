package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

const tgTestToken = "123456789:AAFakeTokenForTests_0123456789abcdef"

type tgServer struct {
	mu    sync.Mutex
	texts []string // chat|text
	fail  int      // http status to answer with (0 = ok)
}

func (s *tgServer) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.URL.Path != "/bot"+tgTestToken+"/sendMessage" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
		return
	}
	if s.fail != 0 {
		w.WriteHeader(s.fail)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`)
		return
	}
	var b struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}
	if r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&b) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.texts = append(s.texts, b.ChatID+"|"+b.Text)
	_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
}

func (s *tgServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.texts)
}

// TestCHK_SVC_10_TelegramE2E — the real service: save_settings with
// settings.telegram + botToken (IPC) → settings.json + telegram.dpapi →
// views report hasToken, never the token → telegram_test reaches the Bot
// API (httptest) → restart → an alert is sent; export_config has no
// telegram; the token is never in settings.json, replies or the log.
func TestCHK_SVC_10_TelegramE2E(t *testing.T) {
	dir := t.TempDir()
	st := config.NewStore(dir)
	st.Protector = plainProtector{}
	p := model.NewProfile("db1")
	p.Name, p.Server, p.Database, p.AuthMode = "Склад", "srv", "db", model.AuthWindows
	p.Mapping = model.Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	if err := st.SaveProfiles([]model.Profile{p}); err != nil {
		t.Fatal(err)
	}
	tg := &tgServer{}
	hs := httptest.NewServer(http.HandlerFunc(tg.handler))
	defer hs.Close()
	mut := func(ac *appConfig) {
		ac.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Admin: true, Account: `TEST\admin`}, nil }
		ac.TelegramBaseURL = hs.URL
		ac.Protector = plainProtector{}
	}
	s := startSvcWith(t, dir, 0, nil, &fakeEvents{}, mut)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hello := s.waitPush(t, func(m model.IPCMessage) bool { return m.Type == model.MsgHello })
	if !strings.Contains(","+strings.Join(hello.Capabilities, ",")+",", ","+model.CapTelegram+",") {
		t.Fatalf("hello caps %v", hello.Capabilities)
	}
	cfg, err := s.c.Call(ctx, model.CmdGetConfig)
	if err != nil || cfg.Config == nil || cfg.Config.Settings.Telegram == nil || cfg.Config.Settings.Telegram.HasToken {
		t.Fatalf("get_config %+v %v", cfg, err)
	}
	set := cfg.Config.Settings
	set.Telegram = &model.TelegramSettings{Enabled: true, ChatID: "-1001234567890", Events: model.ViberEventKinds(),
		Schedule: model.ViberSchedule{Days: []int{1, 2, 3, 4, 5, 6, 7}, From: "00:00", To: "23:59"}}

	// enabled without a token: clear key
	m, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSaveSettings, Settings: &set, ExpectedRevision: cfg.Revision})
	if err == nil && m.Type == model.MsgOK || m.ErrorKey != model.MsgTelegramNoToken {
		t.Fatalf("save without token: %+v %v", m, err)
	}
	// bad token rejected
	m, _ = s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSaveSettings, Settings: &set, BotToken: "nope", ExpectedRevision: cfg.Revision})
	if m.ErrorKey != model.MsgTelegramInvalid {
		t.Fatalf("bad token: %+v", m)
	}
	m, err = s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSaveSettings, Settings: &set, BotToken: tgTestToken, ExpectedRevision: cfg.Revision})
	if err != nil || m.Type != model.MsgOK {
		t.Fatalf("save_settings %+v %v", m, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, config.SettingsFile))
	if strings.Contains(string(raw), tgTestToken) || strings.Contains(string(raw), "hasToken\": true") || !strings.Contains(string(raw), "-1001234567890") {
		t.Fatalf("settings.json: %s", raw)
	}
	if tok, err := st.LoadTelegramToken(); err != nil || tok != tgTestToken {
		t.Fatalf("telegram.dpapi: %v", err)
	}
	cfg, err = s.c.Call(ctx, model.CmdGetConfig)
	if err != nil || cfg.Config.Settings.Telegram == nil || !cfg.Config.Settings.Telegram.HasToken || !cfg.Config.Settings.Telegram.Enabled {
		t.Fatalf("view %+v %v", cfg.Config.Settings.Telegram, err)
	}
	if b, _ := json.Marshal(cfg); strings.Contains(string(b), tgTestToken) {
		t.Fatal("token in get_config")
	}

	// telegram_test → telegram_test_result {ok}
	test := func(s *svc, req model.IPCRequest) model.IPCMessage {
		t.Helper()
		req.Cmd = model.CmdTelegramTest
		var m model.IPCMessage
		for i := 0; i < 100; i++ {
			m, err = s.c.Do(ctx, req)
			if err == nil && m.Type == model.MsgTelegramTestResult {
				return m
			}
			if m.Code != model.ErrCodeRateLimited {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("telegram_test: %+v %v", m, err)
		return m
	}
	if m := test(s, model.IPCRequest{}); m.TelegramTest == nil || !m.TelegramTest.OK || tg.count() != 1 {
		t.Fatalf("telegram_test %+v (%d)", m.TelegramTest, tg.count())
	}
	// rate limit like viber_test
	if m, _ := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdTelegramTest}); m.Code != model.ErrCodeRateLimited {
		t.Fatalf("rate limit: %+v", m)
	}

	// export_config never has telegram
	ex, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdExportConfig, IncludeViberRecipients: true, IncludeEmailRecipients: true})
	if err != nil || ex.Export == nil || ex.Export.Settings == nil || ex.Export.Settings.Telegram != nil {
		t.Fatalf("export %+v %v", ex.Export, err)
	}
	if b, _ := json.Marshal(ex); strings.Contains(string(b), tgTestToken) || strings.Contains(string(b), "telegram") {
		t.Fatalf("export leaks telegram: %s", b)
	}
	s.stop()

	// restart: an alert (lag above the error threshold) is sent
	s = startSvcWith(t, dir, 600, nil, &fakeEvents{}, mut)
	deadline := time.Now().Add(8 * time.Second)
	for tg.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if tg.count() < 2 {
		t.Fatal("alert not sent after restart")
	}
	tg.mu.Lock()
	if !strings.HasPrefix(tg.texts[1], "-1001234567890|") {
		t.Errorf("alert %q", tg.texts[1])
	}
	tg.mu.Unlock()

	// a test with a wrong token override: errorKey, token redacted in the log
	bad := "987654321:BBWrongTokenForTests_0123456789abcd"
	time.Sleep(model.TelegramTestInterval)
	if m := test(s, model.IPCRequest{BotToken: bad}); m.TelegramTest == nil || m.TelegramTest.OK || m.TelegramTest.ErrorKey != model.MsgTelegramAuthFailed {
		t.Fatalf("bad token test %+v", m.TelegramTest)
	}
	// clearBotToken removes telegram.dpapi
	cfg, _ = s.c.Call(ctx, model.CmdGetConfig)
	set = cfg.Config.Settings
	set.Telegram.Enabled = false
	if m, err := s.c.Do(ctx, model.IPCRequest{Cmd: model.CmdSaveSettings, Settings: &set, ClearBotToken: true, ExpectedRevision: cfg.Revision}); err != nil || m.Type != model.MsgOK {
		t.Fatalf("clear %+v %v", m, err)
	}
	if st.HasTelegramToken() {
		t.Fatal("telegram.dpapi not removed")
	}
	s.stop()
	logs, _ := os.ReadFile(filepath.Join(dir, LogFile))
	if strings.Contains(string(logs), tgTestToken) || strings.Contains(string(logs), bad) || strings.Contains(string(logs), "AAFakeToken") {
		t.Fatalf("token in log:\n%s", logs)
	}
}
