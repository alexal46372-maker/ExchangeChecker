package traysettings

import (
	"fmt"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 13 item 10 (CHK-TRAY-94): the «Telegram» page of the settings
// window, like «Viber», shown only with capability telegram.
// settings.telegram {enabled, chatId, events, schedule} comes with
// get_config / get_status (hasToken and status are read-only); saving is
// save_settings with the whole settings and botToken / clearBotToken
// (write-only). telegram_test {botToken?, chatId?} (admin, asynchronous,
// at most once per 10 s) answers telegram_test_result {telegramTest:{ok,
// errorKey?}}.

// Telegram fields.
const (
	FieldTgToken  Field = "telegram.token"
	FieldTgChat   Field = "telegram.chatId"
	FieldTgDays   Field = "telegram.schedule.days"
	FieldTgFrom   Field = "telegram.schedule.from"
	FieldTgTo     Field = "telegram.schedule.to"
	FieldTgEvents Field = "telegram.events"
	FieldTgTest   Field = "telegram.test"
)

// TelegramForm is the editable state of the page.
type TelegramForm struct {
	Enabled  bool
	ChatID   string
	Days     [7]bool // Monday .. Sunday
	From, To string
	Events   [4]bool // model.ViberEventKinds order
	// Token typed in the write-only box ("" = keep the saved one);
	// ClearToken removes the saved token.
	Token      string
	ClearToken bool
}

// FromTelegram fills the page from settings.telegram.
func FromTelegram(t model.TelegramSettings) TelegramForm {
	f := TelegramForm{Enabled: t.Enabled, ChatID: t.ChatID, From: t.Schedule.From, To: t.Schedule.To}
	for _, d := range t.Schedule.Days {
		if d >= 1 && d <= 7 {
			f.Days[d-1] = true
		}
	}
	for i, k := range model.ViberEventKinds() {
		f.Events[i] = t.HasEvent(k)
	}
	return f
}

// Apply returns base with the page applied and validated (model rules).
func (f TelegramForm) Apply(base model.TelegramSettings) (model.TelegramSettings, *model.FieldError) {
	t := base.Stored()
	t.Enabled = f.Enabled
	t.ChatID = strings.TrimSpace(f.ChatID)
	t.Schedule.Days = []int{}
	for i, on := range f.Days {
		if on {
			t.Schedule.Days = append(t.Schedule.Days, i+1)
		}
	}
	t.Schedule.From, t.Schedule.To = strings.TrimSpace(f.From), strings.TrimSpace(f.To)
	t.Events = []string{}
	for i, k := range model.ViberEventKinds() {
		if f.Events[i] {
			t.Events = append(t.Events, k)
		}
	}
	if f.Token != "" && !model.ValidTelegramToken(f.Token) {
		return t, fieldErr("botToken", model.ValRequired)
	}
	if err := t.Validate(); err != nil {
		if fe := model.AsFieldError(err); fe != nil {
			return t, fe
		}
		return t, &model.FieldError{Key: model.ValRequired, Msg: err.Error()}
	}
	return t, nil
}

// TelegramFocus is the control of a telegram field path.
func TelegramFocus(path string) (Field, bool) {
	switch {
	case path == "botToken" || path == "clearBotToken" || path == "telegram.token":
		return FieldTgToken, true
	case path == "telegram.chatId" || path == "chatId" || path == "telegram.enabled":
		return FieldTgChat, true
	case strings.HasPrefix(path, "telegram.schedule.days"):
		return FieldTgDays, true
	case path == "telegram.schedule.from":
		return FieldTgFrom, true
	case path == "telegram.schedule.to":
		return FieldTgTo, true
	case strings.HasPrefix(path, "telegram.events"):
		return FieldTgEvents, true
	}
	return FieldNone, false
}

// telegramKeyFocus: errors of a whole block focus their field.
var telegramKeyFocus = map[string]Field{
	model.MsgTelegramNoToken: FieldTgToken, model.MsgTelegramAuthFailed: FieldTgToken,
	model.MsgTelegramNoChat: FieldTgChat, model.MsgTelegramChatInvalid: FieldTgChat,
}

// IsTelegram tells whether f is on the «Telegram» page.
func IsTelegram(f Field) bool { return strings.HasPrefix(string(f), "telegram.") }

func telegramFieldLabel(path string) (string, bool) {
	f, ok := TelegramFocus(path)
	if !ok {
		return "", false
	}
	key := map[Field]string{
		FieldTgToken: i18n.KeyTgToken, FieldTgChat: i18n.KeyTgChat, FieldTgDays: i18n.KeyVbDays,
		FieldTgFrom: i18n.KeyVbFrom, FieldTgTo: i18n.KeyVbTo, FieldTgEvents: i18n.KeyVbEvents,
	}[f]
	return strings.TrimSuffix(strings.TrimSpace(i18n.T(key)), ":"), true
}

func (e *Editor) loadTelegram() {
	e.HasTelegram = e.Settings.Telegram != nil
	if e.HasTelegram {
		e.Telegram = FromTelegram(*e.Settings.Telegram)
	}
}

// CanEditTelegram tells whether the page is editable.
func (e *Editor) CanEditTelegram() bool { return e.Mode == Admin && e.HasSettings && e.HasTelegram }

// TelegramUnsaved tells whether the page has unsaved edits.
func (e *Editor) TelegramUnsaved() bool {
	return e.CanEditTelegram() && e.Telegram != FromTelegram(*e.Settings.Telegram)
}

// TelegramRequest is save_settings of the page: the whole saved settings
// with the new telegram block, botToken only when typed, clearBotToken,
// expectedRevision.
func (e *Editor) TelegramRequest() (model.IPCRequest, *Problem) {
	if !e.CanEditTelegram() || e.Revision == "" {
		return model.IPCRequest{}, &Problem{Text: e.t(i18n.KeySetNoRevision), Reload: true}
	}
	e.settingsCtx = true
	t, fe := e.Telegram.Apply(*e.Settings.Telegram)
	if fe != nil {
		return model.IPCRequest{}, e.fieldProblem(fe)
	}
	s := e.Settings
	s.Telegram = &t
	req := model.IPCRequest{Cmd: model.CmdSaveSettings, Settings: &s, ExpectedRevision: e.Revision}
	if e.Telegram.ClearToken {
		req.ClearBotToken = true
	} else if e.Telegram.Token != "" {
		req.BotToken = e.Telegram.Token
	}
	if pr := e.checkSize(req); pr != nil {
		return model.IPCRequest{}, pr
	}
	return req, nil
}

// TelegramDone applies an ok reply of the page's save_settings.
func (e *Editor) TelegramDone(m model.IPCMessage, req model.IPCRequest) string {
	if m.Revision != "" {
		e.Revision = m.Revision
	}
	s := *req.Settings
	t := *s.Telegram
	old := e.Settings.Telegram
	t.HasToken = old != nil && old.HasToken
	switch {
	case req.ClearBotToken:
		t.HasToken = false
	case req.BotToken != "":
		t.HasToken = true
	}
	if old != nil {
		t.Status = old.Status
	}
	s.Telegram = &t
	e.LoadSettings(s)
	return e.t(i18n.KeyTgSaved)
}

// TelegramTestRequest is telegram_test with the typed token / chat id
// (sent once, not saved; "" = the saved values).
func (e *Editor) TelegramTestRequest() (model.IPCRequest, *Problem) {
	f := e.Telegram
	if f.Token != "" && !model.ValidTelegramToken(f.Token) {
		e.settingsCtx = true
		return model.IPCRequest{}, e.fieldProblem(fieldErr("botToken", model.ValRequired))
	}
	if f.Token == "" && (e.Settings.Telegram == nil || !e.Settings.Telegram.HasToken) {
		return model.IPCRequest{}, &Problem{Text: i18n.Message(model.MsgTelegramNoToken), Field: FieldTgToken}
	}
	chat := strings.TrimSpace(f.ChatID)
	if chat == "" {
		return model.IPCRequest{}, &Problem{Text: i18n.Message(model.MsgTelegramNoChat), Field: FieldTgChat}
	}
	if !model.ValidTelegramChatID(chat) {
		e.settingsCtx = true
		return model.IPCRequest{}, e.fieldProblem(fieldErr("telegram.chatId", model.ValRequired))
	}
	req := model.IPCRequest{Cmd: model.CmdTelegramTest, BotToken: f.Token}
	if e.Settings.Telegram == nil || chat != e.Settings.Telegram.ChatID {
		req.ChatID = chat
	}
	return req, nil
}

// TelegramTestText renders telegram_test_result / its error reply.
func (e *Editor) TelegramTestText(m model.IPCMessage) string {
	if m.Type == model.MsgError {
		if m.Code == model.ErrCodeRateLimited {
			return fmt.Sprintf(e.t(i18n.KeyTgTestRateLimited), RetryAfterSeconds(m.RetryAfterMs))
		}
		if m.ErrorKey != "" {
			return e.t(i18n.KeyTgTestFailed) + " " + i18n.Message(m.ErrorKey)
		}
		return e.t(i18n.KeyTgTestFailed) + " " + i18n.Message(model.MsgTelegramFailed)
	}
	r := m.TelegramTest
	if r == nil {
		return e.t(i18n.KeyVbTestNoResult)
	}
	if r.OK {
		return e.t(i18n.KeyTgTestOK)
	}
	key := r.ErrorKey
	if key == "" {
		key = model.MsgTelegramFailed
	}
	return e.t(i18n.KeyTgTestFailed) + " " + i18n.Message(key)
}

// TelegramStatusText renders settings.telegram.status.
func TelegramStatusText(t *model.TelegramSettings) string {
	if t == nil || t.Status == nil {
		return ""
	}
	return ViberStatusText(&model.ViberSettings{Status: t.Status})
}

// TelegramTokenStateText: «Токен сохранён» / «Токен не задан» (+ the
// pending change).
func (e *Editor) TelegramTokenStateText() string {
	switch {
	case e.Telegram.ClearToken:
		return e.t(i18n.KeyVbTokenClear)
	case e.Telegram.Token != "":
		return e.t(i18n.KeyVbTokenNew)
	case e.Settings.Telegram != nil && e.Settings.Telegram.HasToken:
		return e.t(i18n.KeyVbTokenSaved)
	}
	return e.t(i18n.KeyVbTokenNone)
}
