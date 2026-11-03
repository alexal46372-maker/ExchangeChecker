package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/hcwinnet"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 13 (CHK-SVC-10 / CHK-IPC-17): Telegram notifications via the Bot
// API sendMessage through internal/hcwinnet — the same HTTPS transport as
// Viber (Go crypto/tls, TLS >= 1.2 independent of Schannel, system roots,
// HTTPS_PROXY), timeouts, response size limit. Alerts are queued
// asynchronously like Viber/email. The bot token is part of the URL path:
// it is never logged and is redacted from every error text.

// TelegramTokenStore loads the bot token (config.Store).
type TelegramTokenStore interface {
	LoadTelegramToken() (string, error)
}

// TelegramAPIBase is the production Bot API base URL.
const TelegramAPIBase = "https://api.telegram.org"

// TelegramOptions of NewTelegram; zero = production defaults.
type TelegramOptions struct {
	Store TelegramTokenStore
	// BaseURL overrides TelegramAPIBase (tests: httptest server).
	BaseURL string
	Client  Poster // nil = hcwinnet.NewClient(20 s, system roots)
	Lang    string
	Log     Logger
	Now     func() time.Time
	Loc     *time.Location
}

type tgItem struct {
	text     string
	attempts int
	next     time.Time
}

// TelegramNotifier implements alert.Sink + ipc.TelegramTester.
type TelegramNotifier struct {
	o        TelegramOptions
	mu       sync.Mutex
	settings model.TelegramSettings
	token    string
	queue    []tgItem
	status   model.ViberStatus
	lastTest time.Time
	wake     chan struct{}
	stop     chan struct{}
	wg       sync.WaitGroup
}

// NewTelegram builds a TelegramNotifier. Call Close to stop the worker.
func NewTelegram(o TelegramOptions) *TelegramNotifier {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Loc == nil {
		o.Loc = time.Local
	}
	if o.Log == nil {
		o.Log = nopLog{}
	}
	if o.BaseURL == "" {
		o.BaseURL = TelegramAPIBase
	}
	if o.Client == nil {
		o.Client = hcwinnet.NewClient(20*time.Second, nil)
	}
	n := &TelegramNotifier{o: o, settings: model.DefaultTelegramSettings(),
		wake: make(chan struct{}, 1), stop: make(chan struct{})}
	n.ReloadToken()
	n.wg.Add(1)
	go n.loop()
	return n
}

// Close stops the worker.
func (n *TelegramNotifier) Close() {
	select {
	case <-n.stop:
	default:
		close(n.stop)
	}
	n.wg.Wait()
}

// ReloadToken rereads telegram.dpapi.
func (n *TelegramNotifier) ReloadToken() {
	var t string
	if n.o.Store != nil {
		var err error
		t, err = n.o.Store.LoadTelegramToken()
		if err != nil {
			n.o.Log.Printf("telegram: token: %s (telegram off)", redactToken(err.Error(), t))
			t = ""
		}
	}
	n.mu.Lock()
	n.token = t
	n.mu.Unlock()
}

// Status is settings.telegram.status (views).
func (n *TelegramNotifier) Status() *model.ViberStatus {
	n.mu.Lock()
	defer n.mu.Unlock()
	s := n.status
	s.Queued = len(n.queue)
	return &s
}

// Snapshot feeds settings.telegram from the monitor.
func (n *TelegramNotifier) Snapshot(s model.StatusSnapshot) {
	n.mu.Lock()
	if s.Settings != nil && s.Settings.Telegram != nil {
		t := s.Settings.Telegram.Stored()
		t.Events = append([]string{}, t.Events...)
		t.Schedule.Days = append([]int{}, t.Schedule.Days...)
		if t.ChatID != n.settings.ChatID || !t.Enabled {
			n.queue = nil
		}
		n.settings = t
	}
	n.mu.Unlock()
	n.kick()
}

// Alert enqueues a message when enabled, the event matches and the
// schedule window is open.
func (n *TelegramNotifier) Alert(e model.AlertEvent) {
	kind := model.ViberEventOf(e)
	if kind == "" {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.settings.Enabled || !n.settings.HasEvent(kind) || n.token == "" || n.settings.ChatID == "" {
		return
	}
	now := n.o.Now().In(n.o.Loc)
	if !n.settings.InWindow(now) {
		n.status.Dropped++
		return
	}
	if len(n.queue) >= MaxQueue {
		n.status.Dropped++
		return
	}
	n.queue = append(n.queue, tgItem{text: EventText(e, n.o.Lang), next: now})
	n.kick()
}

func (n *TelegramNotifier) kick() {
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

func (n *TelegramNotifier) loop() {
	defer n.wg.Done()
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-n.stop:
			return
		case <-n.wake:
			n.drain()
		case <-t.C:
			n.drain()
		}
	}
}

func (n *TelegramNotifier) drain() {
	for {
		n.mu.Lock()
		if len(n.queue) == 0 || !n.settings.Enabled || n.token == "" {
			n.mu.Unlock()
			return
		}
		now := n.o.Now()
		idx := -1
		for i, it := range n.queue {
			if !it.next.After(now) {
				idx = i
				break
			}
		}
		if idx < 0 {
			n.mu.Unlock()
			return
		}
		it := n.queue[idx]
		token, chat := n.token, n.settings.ChatID
		n.queue = append(n.queue[:idx], n.queue[idx+1:]...)
		n.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := n.send(ctx, token, chat, it.text)
		cancel()

		n.mu.Lock()
		if err != nil {
			n.status.LastErrorKey = tgErrKey(err)
			nowt := n.o.Now()
			n.status.LastErrorAt = &nowt
			it.attempts++
			if it.attempts < MaxAttempts && n.status.LastErrorKey != model.MsgTelegramAuthFailed {
				i := it.attempts - 1
				if i >= len(Backoff) {
					i = len(Backoff) - 1
				}
				it.next = n.o.Now().Add(Backoff[i])
				n.queue = append(n.queue, it)
			} else {
				n.o.Log.Printf("telegram: give up after %d attempts: %s", it.attempts, n.status.LastErrorKey)
			}
		} else {
			n.status.Sent++
			nowt := n.o.Now()
			n.status.LastOKAt = &nowt
			n.status.LastErrorKey = ""
		}
		n.mu.Unlock()
	}
}

// tgError carries a message key and a token-free detail.
type tgError struct{ key, detail string }

func (e *tgError) Error() string { return e.key + ": " + e.detail }

func tgErrKey(err error) string {
	var te *tgError
	if errors.As(err, &te) {
		return te.key
	}
	return model.MsgTelegramFailed
}

// redactToken removes the token (and any "bot<digits>:<secret>" form)
// from s.
func redactToken(s, token string) string {
	if token != "" {
		s = strings.ReplaceAll(s, token, "***")
		s = strings.ReplaceAll(s, url.PathEscape(token), "***")
	}
	return tgTokenInText.ReplaceAllString(s, "bot***")
}

// send posts one sendMessage. Errors never contain the token.
func (n *TelegramNotifier) send(ctx context.Context, token, chatID, text string) error {
	body, _ := json.Marshal(map[string]string{"chat_id": chatID, "text": text})
	u := strings.TrimRight(n.o.BaseURL, "/") + "/bot" + token + "/sendMessage"
	code, raw, err := n.o.Client.PostJSON(ctx, u, nil, body, 64<<10)
	if err != nil {
		key := model.MsgTelegramNetwork
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			key = model.MsgTelegramTimeout
		case errors.Is(ctx.Err(), context.Canceled):
			key = model.MsgTelegramCanceled
		case errors.Is(err, hcwinnet.ErrTLS):
			key = model.MsgTelegramTLS
		}
		return &tgError{key, redactToken(err.Error(), token)}
	}
	var r struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(raw, &r)
	if code == http.StatusOK && r.OK {
		return nil
	}
	if r.ErrorCode != 0 {
		code = r.ErrorCode
	}
	detail := redactToken(fmt.Sprintf("http %d: %s", code, r.Description), token)
	switch code {
	case http.StatusUnauthorized, http.StatusNotFound:
		return &tgError{model.MsgTelegramAuthFailed, detail}
	case http.StatusBadRequest, http.StatusForbidden:
		return &tgError{model.MsgTelegramChatInvalid, detail}
	case http.StatusTooManyRequests:
		return &tgError{model.MsgTelegramRateLimited, detail}
	}
	return &tgError{model.MsgTelegramFailed, detail}
}

// TelegramTest implements ipc.TelegramTester (CHK-IPC-17): rate-limited
// like viber_test; ignores enabled/schedule. botToken / chatID override
// the stored values for this one test (never stored, never logged).
func (n *TelegramNotifier) TelegramTest(ctx context.Context, botToken, chatID string) (model.TelegramTestResult, error) {
	if botToken != "" && !model.ValidTelegramToken(botToken) {
		return model.TelegramTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgTelegramInvalid, Message: "telegram: invalid botToken"}
	}
	chatID = strings.TrimSpace(chatID)
	if chatID != "" && !model.ValidTelegramChatID(chatID) {
		return model.TelegramTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgTelegramInvalid, Message: "telegram: invalid chatId"}
	}
	n.mu.Lock()
	now := n.o.Now()
	if !n.lastTest.IsZero() && now.Sub(n.lastTest) < model.TelegramTestInterval {
		wait := model.TelegramTestInterval - now.Sub(n.lastTest)
		n.mu.Unlock()
		return model.TelegramTestResult{}, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgTelegramRateLimited,
			Message: "telegram: one test per 10 s", RetryAfterMs: int64(wait / time.Millisecond)}
	}
	token := botToken
	if token == "" {
		token = n.token
	}
	if chatID == "" {
		chatID = n.settings.ChatID
	}
	n.mu.Unlock()
	if token == "" {
		return model.TelegramTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgTelegramNoToken, Message: "telegram: no bot token"}
	}
	if chatID == "" {
		return model.TelegramTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgTelegramNoChat, Message: "telegram: no chatId"}
	}
	n.mu.Lock()
	n.lastTest = now
	n.mu.Unlock()
	text := "ExchangeChecker: тестовое сообщение"
	if n.o.Lang == model.LangEN {
		text = "ExchangeChecker: test message"
	}
	if err := n.send(ctx, token, chatID, text); err != nil {
		n.o.Log.Printf("telegram: test failed: %s", redactToken(err.Error(), token))
		return model.TelegramTestResult{OK: false, ErrorKey: tgErrKey(err)}, nil
	}
	return model.TelegramTestResult{OK: true}, nil
}

var tgTokenInText = regexp.MustCompile(`bot[0-9]{3,20}:[A-Za-z0-9_-]+`)
