package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/hcwinnet"
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
	got := make(chan struct{}, 16)
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
		got <- struct{}{}
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
	if k := model.ViberEventOf(ev); k != model.ViberEventError {
		t.Fatalf("event kind %q, want %q", k, model.ViberEventError)
	}
	// Saturday: outside the window → dropped synchronously, nothing queued
	tn.Alert(ev)
	if st := tn.Status(); st.Dropped != 1 || st.Queued != 0 {
		t.Fatalf("outside window: %+v", st)
	}
	// Monday 12:00 → sent
	now = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	tn.Alert(ev)
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("in window not sent")
	}
	deadline := time.After(5 * time.Second)
	for tn.Status().Sent != 1 {
		select {
		case <-deadline:
			t.Fatalf("sent not counted: %+v", tn.Status())
		case <-time.After(5 * time.Millisecond):
		}
	}
	// event filter: unselected kind → not queued (synchronous decision)
	set.Telegram.Events = []string{}
	tn.Snapshot(model.StatusSnapshot{Settings: &set})
	tn.Alert(ev)
	if st := tn.Status(); st.Queued != 0 || st.Sent != 1 {
		t.Fatalf("unselected event queued: %+v", st)
	}
	select {
	case <-got:
		t.Fatal("unselected event sent")
	default:
	}

	// redaction: unreachable server
	dead := NewTelegram(TelegramOptions{BaseURL: "http://127.0.0.1:1", Log: lg, Client: hcwinnet.NewClient(time.Second, nil)})
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

// TestCHK_SVC_10_TelegramUsesHcwinnet — the production client is the
// shared hcwinnet transport (TLS >= 1.2), like Viber; no own net/http client.
func TestCHK_SVC_10_TelegramUsesHcwinnet(t *testing.T) {
	tn := NewTelegram(TelegramOptions{})
	defer tn.Close()
	c, ok := tn.o.Client.(*hcwinnet.Client)
	if !ok || c.MinTLSVersion() < tls.VersionTLS12 {
		t.Fatalf("client %T", tn.o.Client)
	}
	src, err := os.ReadFile("telegram.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "http.Client") || strings.Contains(string(src), ".Do(") {
		t.Fatal("telegram.go must send through hcwinnet")
	}
}
