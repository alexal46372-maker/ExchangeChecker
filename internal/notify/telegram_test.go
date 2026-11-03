package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

const tgTok = "123456789:AAFakeTokenForTests_0123456789abcdef"

type tgStore struct{}

func (tgStore) LoadTelegramToken() (string, error) { return tgTok, nil }

type capLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *capLog) Printf(f string, a ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.b.WriteString(strings.TrimSpace(fmt.Sprintf(f, a...)) + "\n")
}

// TestCHK_SVC_10_TelegramScheduleAndRedaction — alerts outside the
// schedule window are dropped (not sent); network/HTTP errors never
// contain the token.
func TestCHK_SVC_10_TelegramScheduleAndRedaction(t *testing.T) {
	var mu sync.Mutex
	n := 0
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer hs.Close()
	// Saturday 12:00 local
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	lg := &capLog{}
	tn := NewTelegram(TelegramOptions{Store: tgStore{}, BaseURL: hs.URL, Log: lg, Now: func() time.Time { return now }, Loc: time.UTC})
	defer tn.Close()
	set := model.Settings{Telegram: &model.TelegramSettings{Enabled: true, ChatID: "42", Events: model.ViberEventKinds(),
		Schedule: model.ViberSchedule{Days: []int{1, 2, 3, 4, 5}, From: "08:30", To: "17:00"}}}
	tn.Snapshot(model.StatusSnapshot{Settings: &set})
	ev := model.AlertEvent{Kind: model.AlertKindAlert, From: model.LevelOK, To: model.LevelError, PointName: "Магазин 1"}
	if model.ViberEventOf(ev) == "" {
		t.Skip("event kind mapping changed")
	}
	tn.Alert(ev)
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	sent := n
	mu.Unlock()
	if sent != 0 || tn.Status().Dropped != 1 {
		t.Fatalf("outside window: sent %d status %+v", sent, tn.Status())
	}
	// Monday 12:00 → sent
	now = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	tn.Alert(ev)
	deadline := time.Now().Add(3 * time.Second)
	for tn.Status().Sent == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if tn.Status().Sent != 1 {
		t.Fatalf("in window not sent: %+v", tn.Status())
	}
	// event filter
	set.Telegram.Events = []string{}
	tn.Snapshot(model.StatusSnapshot{Settings: &set})
	tn.Alert(ev)
	time.Sleep(100 * time.Millisecond)
	if tn.Status().Sent != 1 {
		t.Fatal("unselected event sent")
	}

	// redaction: unreachable server
	dead := NewTelegram(TelegramOptions{BaseURL: "http://127.0.0.1:1", Log: lg, Client: &http.Client{Timeout: time.Second}})
	defer dead.Close()
	err := dead.send(context.Background(), tgTok, "42", "x")
	if err == nil || strings.Contains(err.Error(), tgTok) || tgErrKey(err) != model.MsgTelegramNetwork {
		t.Fatalf("network error %v", err)
	}
	res, err := dead.TelegramTest(context.Background(), tgTok, "42")
	if err != nil || res.OK || res.ErrorKey != model.MsgTelegramNetwork {
		t.Fatalf("test %+v %v", res, err)
	}
	if s := redactToken("Post https://x/bot"+tgTok+"/sendMessage", ""); strings.Contains(s, "AAFake") {
		t.Fatalf("redact %q", s)
	}
	lg.mu.Lock()
	out := lg.b.String()
	lg.mu.Unlock()
	if strings.Contains(out, tgTok) || strings.Contains(out, "AAFake") {
		t.Fatalf("token in log: %s", out)
	}
}

// TestCHK_IPC_17_TelegramTestValidation — no token / no chat / bad
// overrides give clear keys; rate limit 10 s.
func TestCHK_IPC_17_TelegramTestValidation(t *testing.T) {
	tn := NewTelegram(TelegramOptions{BaseURL: "http://127.0.0.1:1"})
	defer tn.Close()
	ctx := context.Background()
	key := func(err error) string {
		if ie, ok := err.(*model.IPCError); ok {
			return ie.Key
		}
		return ""
	}
	if _, err := tn.TelegramTest(ctx, "", "42"); key(err) != model.MsgTelegramNoToken {
		t.Fatalf("no token: %v", err)
	}
	if _, err := tn.TelegramTest(ctx, tgTok, ""); key(err) != model.MsgTelegramNoChat {
		t.Fatalf("no chat: %v", err)
	}
	if _, err := tn.TelegramTest(ctx, "bad", "42"); key(err) != model.MsgTelegramInvalid {
		t.Fatalf("bad token: %v", err)
	}
	if _, err := tn.TelegramTest(ctx, tgTok, "chat with space"); key(err) != model.MsgTelegramInvalid {
		t.Fatalf("bad chat: %v", err)
	}
	_, _ = tn.TelegramTest(ctx, tgTok, "42")
	if _, err := tn.TelegramTest(ctx, tgTok, "42"); key(err) != model.MsgTelegramRateLimited {
		t.Fatalf("rate limit: %v", err)
	}
}
