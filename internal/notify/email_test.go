package notify

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type memEmailStore struct{ pw string }

func (m *memEmailStore) LoadEmailPassword() (string, error) { return m.pw, nil }

func TestCHK_SVC_08_EmailAlertAsync(t *testing.T) {
	var sends int32
	store := &memEmailStore{pw: "secret"}
	n := NewEmail(EmailOptions{
		Store: store, Lang: model.LangEN, Now: func() time.Time { return time.Date(2026, 10, 27, 12, 0, 0, 0, time.Local) },
		SendHook: func(ctx context.Context, host string, port int, tlsMode, from, password, to, subject, body string) error {
			atomic.AddInt32(&sends, 1)
			if password != "secret" || to != "a@b.co" || host != "smtp.example.com" {
				t.Errorf("send args %s %s %s", host, to, password)
			}
			return nil
		},
	})
	defer n.Close()

	st := model.DefaultEmailSettings()
	st.Enabled = true
	st.Host, st.Port, st.From = "smtp.example.com", 587, "bot@example.com"
	st.TLSMode = model.EmailTLSStartTLS
	st.Recipients = []model.EmailRecipient{{ID: "e1", Addr: "a@b.co", Name: "Ann"}}
	n.Snapshot(model.StatusSnapshot{Settings: &model.Settings{Email: &st}})

	n.Alert(model.AlertEvent{ProfileID: "p1", ProfileName: "P", PointName: "pt", From: model.LevelOK, To: model.LevelError})
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&sends) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt32(&sends) != 1 {
		t.Fatalf("sends=%d", sends)
	}
}

func TestCHK_IPC_16_EmailTestHook(t *testing.T) {
	store := &memEmailStore{pw: "secret"}
	var gotTo string
	n := NewEmail(EmailOptions{
		Store: store, Now: time.Now,
		SendHook: func(ctx context.Context, host string, port int, tlsMode, from, password, to, subject, body string) error {
			gotTo = to
			return nil
		},
	})
	defer n.Close()
	st := model.DefaultEmailSettings()
	st.Host, st.Port, st.From = "smtp.example.com", 587, "bot@example.com"
	st.TLSMode = model.EmailTLSStartTLS
	st.Recipients = []model.EmailRecipient{{ID: "e1", Addr: "a@b.co", Name: "Ann"}}
	n.Snapshot(model.StatusSnapshot{Settings: &model.Settings{Email: &st}})

	res, err := n.EmailTest(context.Background(), "e1", "", "")
	if err != nil || !res.OK || len(res.Results) != 1 || res.Results[0].Addr != "a@b.co" || gotTo != "a@b.co" {
		t.Fatalf("res=%#v err=%v to=%q", res, err, gotTo)
	}

	// one-off addr
	_, err = n.EmailTest(context.Background(), "", "one@off.co", "")
	ie, ok := err.(*model.IPCError)
	if !ok || ie.Code != model.ErrCodeRateLimited {
		t.Fatalf("want rate limit, got %v", err)
	}

	// force clock past interval
	n.mu.Lock()
	n.lastTest = time.Time{}
	n.mu.Unlock()
	res, err = n.EmailTest(context.Background(), "", "one@off.co", "")
	if err != nil || !res.OK || gotTo != "one@off.co" {
		t.Fatalf("one-off %#v err=%v to=%q", res, err, gotTo)
	}

	n.mu.Lock()
	n.lastTest = time.Time{}
	n.mu.Unlock()
	_, err = n.EmailTest(context.Background(), "", "", "")
	// all recipients ok
	if err != nil {
		t.Fatal(err)
	}

	n.mu.Lock()
	n.lastTest = time.Time{}
	n.password = ""
	n.mu.Unlock()
	_, err = n.EmailTest(context.Background(), "e1", "", "")
	if !errors.As(err, new(*model.IPCError)) {
		t.Fatalf("no password: %v", err)
	}
}
