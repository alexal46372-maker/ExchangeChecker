package model

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// Phase 13 (EC Service, CHK-CFG-10 / CHK-IPC-17 / CHK-SVC-10): Telegram
// notifications, modeled on Viber. settings.telegram {enabled, chatId,
// events, schedule, hasToken}; the bot token is in telegram.dpapi (DPAPI
// LocalMachine, DACL SY+BA), write-only via save_settings botToken /
// clearBotToken, never in replies, logs or export_config.

const (
	CmdTelegramTest = "telegram_test"
	CapTelegram     = "telegram"
	// MsgTelegramTestResult is the async reply of telegram_test.
	MsgTelegramTestResult = "telegram_test_result"

	TelegramTestInterval = 10 * time.Second
	TelegramMaxTokenLen  = 128
	TelegramMaxChatIDLen = 64
)

const (
	MsgTelegramNoToken     = "notify.telegram_no_token"
	MsgTelegramNoChat      = "notify.telegram_no_chat"
	MsgTelegramAuthFailed  = "notify.telegram_auth_failed"
	MsgTelegramChatInvalid = "notify.telegram_chat_invalid"
	MsgTelegramRateLimited = "notify.telegram_rate_limited"
	MsgTelegramNetwork     = "notify.telegram_network"
	MsgTelegramFailed      = "notify.telegram_failed"
	MsgTelegramTimeout     = "notify.telegram_timeout"
	MsgTelegramCanceled    = "notify.telegram_canceled"
	MsgTelegramInvalid     = "notify.telegram_invalid"
)

// TelegramSettings is settings.telegram.
type TelegramSettings struct {
	Enabled bool `json:"enabled"`
	// ChatID: numeric chat id ("123", "-100123…") or "@channel".
	ChatID string `json:"chatId"`
	// HasToken is read-only (views): telegram.dpapi holds a bot token.
	HasToken bool          `json:"hasToken"`
	Events   []string      `json:"events"`
	Schedule ViberSchedule `json:"schedule"`
	Status   *ViberStatus  `json:"status,omitempty"`
}

// DefaultTelegramSettings: off, Mon–Fri 08:30–17:00, all events.
func DefaultTelegramSettings() TelegramSettings {
	return TelegramSettings{Events: ViberEventKinds(),
		Schedule: ViberSchedule{Days: []int{1, 2, 3, 4, 5}, From: "08:30", To: "17:00"}}
}

// Stored drops the read-only fields (settings.json).
func (t TelegramSettings) Stored() TelegramSettings {
	t.HasToken, t.Status = false, nil
	t.ChatID = strings.TrimSpace(t.ChatID)
	if t.Events == nil {
		t.Events = []string{}
	}
	if t.Schedule.Days == nil {
		t.Schedule.Days = []int{}
	}
	return t
}

// MarshalJSON keeps hasToken always present.
func (t TelegramSettings) MarshalJSON() ([]byte, error) {
	type wire TelegramSettings
	return json.Marshal(wire(t))
}

// HasEvent reports whether kind is selected.
func (t TelegramSettings) HasEvent(kind string) bool {
	for _, k := range t.Events {
		if k == kind {
			return true
		}
	}
	return false
}

// InWindow: same schedule rules as Viber.
func (t TelegramSettings) InWindow(at time.Time) bool {
	return (ViberSettings{Schedule: t.Schedule}).InWindow(at)
}

var tgTokenRe = regexp.MustCompile(`^[0-9]{3,20}:[A-Za-z0-9_-]{20,100}$`)
var tgChatRe = regexp.MustCompile(`^(-?[0-9]{1,20}|@[A-Za-z][A-Za-z0-9_]{3,63})$`)

// ValidTelegramToken: "<digits>:<secret>" (Bot API format).
func ValidTelegramToken(s string) bool {
	return len(s) <= TelegramMaxTokenLen && tgTokenRe.MatchString(s)
}

// ValidTelegramChatID: numeric id (may be negative) or @channelname.
func ValidTelegramChatID(s string) bool {
	return len(s) <= TelegramMaxChatIDLen && tgChatRe.MatchString(s)
}

// Validate checks settings.telegram (paths "telegram.*").
func (t TelegramSettings) Validate() error {
	const p = "telegram."
	id := strings.TrimSpace(t.ChatID)
	if id != "" && !ValidTelegramChatID(id) {
		return fieldErr(p+"chatId", ValRequired, "telegram: chatId must be a number or @channel")
	}
	if t.Enabled && id == "" {
		return fieldErr(p+"chatId", ValRequired, "telegram: enabled without chatId")
	}
	vs := ViberSettings{Schedule: t.Schedule, Events: t.Events, Recipients: []ViberRecipient{{ID: "x"}}}
	if err := vs.Validate(); err != nil {
		if fe := AsFieldError(err); fe != nil {
			fe.Field = strings.Replace(fe.Field, "viber.", p, 1)
			fe.Msg = strings.Replace(fe.Msg, "viber:", "telegram:", 1)
			return fe
		}
		return err
	}
	return nil
}

// TelegramTestResult is the telegram_test reply {ok, errorKey?}.
type TelegramTestResult struct {
	OK       bool   `json:"ok"`
	ErrorKey string `json:"errorKey,omitempty"`
}

// SettingsSecrets are the write-only secrets of save_settings.
type SettingsSecrets struct {
	ViberToken    string
	ClearViber    bool
	SmtpPassword  string
	ClearSmtp     bool
	BotToken      string
	ClearBotToken bool
}
