//go:build windows

package wintray

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// tgService adds the Telegram contract of feat/svc-phase13 to the fake
// config service: settings.telegram in get_config (hasToken read-only),
// save_settings with botToken / clearBotToken (ipc.TelegramSettingsSaver),
// telegram_test (ipc.TelegramTester, at most once per 10 s like the
// service: rate_limited + retryAfterMs).
type tgService struct {
	*cfgService
	tmu      sync.Mutex
	tg       model.TelegramSettings
	saves    []model.SettingsSecrets
	saved    []model.Settings
	tests    [][2]string // botToken, chatId
	lastTest time.Time
	result   model.TelegramTestResult
}

func newTgService() *tgService {
	tg := model.DefaultTelegramSettings()
	tg.ChatID, tg.HasToken = "-100123", true
	tg.Status = &model.ViberStatus{Sent: 3, Queued: 1}
	return &tgService{cfgService: newCfgService(), tg: tg, result: model.TelegramTestResult{OK: true}}
}

func (s *tgService) GetConfig() (model.ConfigView, string, error) {
	cv, rev, err := s.cfgService.GetConfig()
	s.tmu.Lock()
	t := s.tg
	s.tmu.Unlock()
	cv.Settings.Telegram = &t
	return cv, rev, err
}

func (s *tgService) SaveSettingsAll(st model.Settings, sec model.SettingsSecrets, rev, account string) (string, error) {
	r, err := s.cfgService.SaveSettings(st, rev, account)
	if err != nil {
		return "", err
	}
	s.tmu.Lock()
	defer s.tmu.Unlock()
	s.saves = append(s.saves, sec)
	s.saved = append(s.saved, st)
	if st.Telegram != nil { // omitted = unchanged
		has := s.tg.HasToken
		s.tg = st.Telegram.Stored()
		s.tg.HasToken = (has || sec.BotToken != "") && !sec.ClearBotToken
	}
	return r, nil
}

func (s *tgService) TelegramTest(ctx context.Context, token, chat string) (model.TelegramTestResult, error) {
	s.tmu.Lock()
	defer s.tmu.Unlock()
	if !s.lastTest.IsZero() && time.Since(s.lastTest) < model.TelegramTestInterval {
		left := model.TelegramTestInterval - time.Since(s.lastTest)
		return model.TelegramTestResult{}, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgTelegramRateLimited,
			Message: "rate limited", RetryAfterMs: left.Milliseconds()}
	}
	s.lastTest = time.Now()
	s.tests = append(s.tests, [2]string{token, chat})
	return s.result, nil
}

func (s *tgService) nSaves() int { s.tmu.Lock(); defer s.tmu.Unlock(); return len(s.saves) }
func (s *tgService) nTests() int { s.tmu.Lock(); defer s.tmu.Unlock(); return len(s.tests) }

// TestCHK_TRAY_94_TelegramTab — the «Telegram» tab (capability telegram,
// real ipc.Server + session): filled from settings.telegram (token
// write-only, only «Токен сохранён»), save_settings sends the whole
// settings with telegram {enabled, chatId, events, schedule} and botToken
// only when typed, clearBotToken; «Тест» sends telegram_test with the typed
// token / chat id and shows the telegram_test_result text; a second test
// within 10 s shows the rate_limited text with the seconds left; the
// notify.telegram_* keys have Russian texts.
func TestCHK_TRAY_94_TelegramTab(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newTgService()
	svc.handler = svc
	app := startCfgApp(t, svc.cfgService, true, nil)
	if !app.Status().Has(model.CapTelegram) {
		t.Fatalf("capability telegram: %v", app.Status().Capabilities)
	}
	w := openAdmin(t, app)
	pumpUntil(t, "telegram tab", func() bool {
		w.update()
		for _, pg := range w.tabPages {
			if pg == pageTelegram {
				return true
			}
		}
		return false
	})
	if i18n.T(i18n.KeySetNavTelegram) != "Telegram" {
		t.Errorf("tab name %q", i18n.T(i18n.KeySetNavTelegram))
	}
	w.setPage(pageTelegram)
	if w.page != pageTelegram || !visible(w.ctl[idTgChat]) || visible(w.ctl[idSetName]) {
		t.Fatal("telegram page")
	}
	if w.text(idTgChat) != "-100123" || w.text(idTgToken) != "" || w.text(idTgTokenState) != i18n.T(i18n.KeyVbTokenSaved) {
		t.Fatalf("filled: %q %q %q", w.text(idTgChat), w.text(idTgToken), w.text(idTgTokenState))
	}
	if !strings.Contains(w.text(idTgStatus), "Отправлено: 3") {
		t.Errorf("status %q", w.text(idTgStatus))
	}
	// save: new token, chat, enabled, only error + warning events
	w.set(idTgToken, "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ_abc")
	w.set(idTgChat, "@ops_alerts")
	w.click(t, idTgEnabled)
	w.click(t, idTgEv1+2)
	w.click(t, idTgEv1+3)
	w.click(t, idSetSave)
	pumpUntil(t, "telegram saved", func() bool { return svc.nSaves() == 1 && !w.busy })
	svc.tmu.Lock()
	sec, st := svc.saves[0], svc.saved[0]
	svc.tmu.Unlock()
	if sec.BotToken != "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ_abc" || sec.ClearBotToken || st.Telegram == nil {
		t.Fatalf("save %+v %+v", sec, st.Telegram)
	}
	if tg := st.Telegram; !tg.Enabled || tg.ChatID != "@ops_alerts" || len(tg.Events) != 2 || tg.HasToken || tg.Status != nil {
		t.Errorf("sent telegram %+v", tg)
	}
	if st.Viber == nil || st.Email == nil {
		t.Error("save_settings must carry the whole settings")
	}
	if w.text(idTgToken) != "" || w.text(idTgTokenState) != i18n.T(i18n.KeyVbTokenSaved) {
		t.Errorf("after save: %q %q", w.text(idTgToken), w.text(idTgTokenState))
	}
	// clear the token
	w.click(t, idTgClearToken)
	w.click(t, idSetSave)
	pumpUntil(t, "token cleared", func() bool { return svc.nSaves() == 2 && !w.busy })
	svc.tmu.Lock()
	sec = svc.saves[1]
	svc.tmu.Unlock()
	if !sec.ClearBotToken || sec.BotToken != "" {
		t.Errorf("clear %+v", sec)
	}
	if w.text(idTgTokenState) != i18n.T(i18n.KeyVbTokenNone) {
		t.Errorf("state %q", w.text(idTgTokenState))
	}
	// test with a typed token and another chat id
	w.set(idTgToken, "654321:ZYXWVUTSRQPONMLKJIHGFEDCBA_xyz")
	w.set(idTgChat, "-1009")
	w.click(t, idTgTest)
	pumpUntil(t, "telegram test", func() bool { return w.tgTesting == nil && w.text(idTgResult) == i18n.T(i18n.KeyTgTestOK) })
	svc.tmu.Lock()
	got := svc.tests[0]
	svc.tmu.Unlock()
	if got[0] != "654321:ZYXWVUTSRQPONMLKJIHGFEDCBA_xyz" || got[1] != "-1009" {
		t.Errorf("telegram_test %v", got)
	}
	// second test within 10 s: rate_limited + retryAfterMs
	w.click(t, idTgTest)
	pumpUntil(t, "rate limited", func() bool { return w.tgTesting == nil && strings.Contains(w.text(idTgResult), "не чаще раза в 10 с") })
	if svc.nTests() != 1 {
		t.Errorf("tests %d", svc.nTests())
	}
	for _, k := range []string{model.MsgTelegramNoToken, model.MsgTelegramNoChat, model.MsgTelegramAuthFailed, model.MsgTelegramChatInvalid,
		model.MsgTelegramRateLimited, model.MsgTelegramNetwork, model.MsgTelegramFailed, model.MsgTelegramTimeout,
		model.MsgTelegramCanceled, model.MsgTelegramInvalid, model.MsgTelegramTLS} {
		if m := i18n.Message(k); m == "" || m == k || !strings.ContainsAny(m, "абвгдеёжзийклмнопрстуфхцчшщъыьэюя") {
			t.Errorf("%s: %q", k, m)
		}
	}
}
