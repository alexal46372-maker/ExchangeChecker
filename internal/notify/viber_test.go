package notify

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/hcwinnet"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type memStore struct {
	mu  sync.Mutex
	tok string
}

func (m *memStore) LoadViberToken() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tok, nil
}

// fakeViber is a fake chatapi.viber.com (TLS, own CA).
type fakeViber struct {
	srv    *httptest.Server
	mu     sync.Mutex
	got    []viberMsg
	tokens []string
	// answers: HTTP status / Viber status, consumed in order; then 200/0
	answers [][2]int
}

func newFake(t *testing.T) *fakeViber {
	f := &fakeViber{}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m viberMsg
		_ = json.Unmarshal(b, &m)
		f.mu.Lock()
		ans := [2]int{200, 0}
		if len(f.answers) > 0 {
			ans, f.answers = f.answers[0], f.answers[1:]
		}
		if r.URL.Path == "/pa/send_message" && r.Method == http.MethodPost {
			f.got = append(f.got, m)
			f.tokens = append(f.tokens, r.Header.Get("X-Viber-Auth-Token"))
		}
		f.mu.Unlock()
		w.WriteHeader(ans[0])
		_, _ = w.Write([]byte(`{"status":` + itoa(ans[1]) + `,"status_message":"x"}`))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func (f *fakeViber) sent() []viberMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]viberMsg{}, f.got...)
}

func (f *fakeViber) client() *hcwinnet.Client {
	p := x509.NewCertPool()
	p.AddCert(f.srv.Certificate())
	return hcwinnet.NewClient(5*time.Second, &tls.Config{RootCAs: p})
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }
func (c *clock) set(t time.Time)     { c.mu.Lock(); c.t = t; c.mu.Unlock() }

var loc = time.FixedZone("UTC+3", 3*3600)

// Monday 2026-10-19 local
func at(hh, mm int) time.Time { return time.Date(2026, 10, 19, hh, mm, 0, 0, loc) }

func viberOn() model.ViberSettings {
	v := model.DefaultViberSettings()
	v.Enabled, v.Recipients = true, []model.ViberRecipient{{ID: "r1", Name: "Иван"}, {ID: "r2", Name: "Пётр"}}
	return v
}

func newN(t *testing.T, f *fakeViber, ck *clock, enabled bool) (*Notifier, *memStore) {
	st := &memStore{}
	if enabled {
		st.tok = "tok-1"
	}
	n := New(Options{Store: st, Client: f.client(), URL: f.srv.URL + "/pa/send_message", Lang: model.LangRU, Now: ck.now, Loc: loc})
	return n, st
}

// cur: the settings.viber of the next snapshots
var cur = viberOn()

func snapV(v model.ViberSettings, ready, problem bool) model.StatusSnapshot {
	p := model.ProfileSnapshot{ProfileID: "p1", Name: "Склад", Enabled: true, Level: model.LevelOK}
	if ready {
		p.CheckedAt = at(9, 0)
	}
	if problem {
		p.Level = model.LevelError
		p.Points = []model.PointStatus{{ExchangePoint: model.ExchangePoint{Name: "Точка А", Level: model.LevelError}}}
	}
	st := model.DefaultSettings()
	st.Viber = &v
	return model.StatusSnapshot{Profiles: []model.ProfileSnapshot{p}, Settings: &st}
}

func snap(ready, problem bool) model.StatusSnapshot { return snapV(cur, ready, problem) }

// TestCHK_SVC_06_SummaryOncePerWindow — the summary is sent once per
// window (also on a start in the middle of the window), only after every
// enabled profile was polled; reminders every reminderMinutes while
// problems remain; nothing outside the window.
func TestCHK_SVC_06_SummaryOncePerWindow(t *testing.T) {
	f := newFake(t)
	ck := &clock{t: at(7, 0)}
	n, _ := newN(t, f, ck, true)
	ctx := context.Background()
	n.Snapshot(snap(true, true))
	n.Step(ctx) // 07:00: before the window
	if len(f.sent()) != 0 {
		t.Fatal("sent before the window")
	}
	// mid-window start: not ready yet -> no summary
	ck.set(at(11, 0))
	n.started = ck.now() // the service started now (mid-window)
	n.Snapshot(snap(false, true))
	n.Step(ctx)
	if len(f.sent()) != 0 {
		t.Fatal("summary before the first poll")
	}
	n.Snapshot(snap(true, true))
	n.Step(ctx)
	got := f.sent()
	if len(got) != 2 || got[0].Receiver != "r1" || got[1].Receiver != "r2" || !strings.Contains(got[0].Text, "сводка") ||
		!strings.Contains(got[0].Text, "Точка А") || got[0].Type != "text" || got[0].MinAPIVersion != 1 || got[0].Sender.Name != "ExchangeChecker" {
		t.Fatalf("summary %+v", got)
	}
	if f.tokens[0] != "tok-1" {
		t.Fatal("token header")
	}
	ck.add(30 * time.Minute)
	n.Step(ctx)
	if len(f.sent()) != 2 {
		t.Fatal("second summary in the same window")
	}
	ck.add(30 * time.Minute) // 12:00: reminder after 60 min
	n.Step(ctx)
	if got := f.sent(); len(got) != 4 || !strings.Contains(got[3].Text, "напоминание") {
		t.Fatalf("reminder %+v", got)
	}
	n.Snapshot(snap(true, false))
	ck.add(2 * time.Hour)
	n.Step(ctx)
	if len(f.sent()) != 4 {
		t.Fatal("reminder without problems")
	}
	ck.set(at(17, 0)) // window end (exclusive)
	n.Snapshot(snap(true, true))
	n.Alert(model.AlertEvent{ProfileID: "p1", ProfileName: "Склад", From: model.LevelOK, To: model.LevelError, At: at(17, 0)})
	n.Step(ctx)
	ck.set(at(23, 0))
	n.Step(ctx)
	if len(f.sent()) != 4 {
		t.Fatal("sent outside the window")
	}
	if v := n.Status(); v.Dropped != 1 || v.Sent != 4 {
		t.Fatalf("status %+v", v)
	}
	// next working day 08:30: a new summary
	ck.set(at(8, 30).AddDate(0, 0, 1))
	n.Step(ctx)
	if got := f.sent(); len(got) != 6 || !strings.Contains(got[5].Text, "сводка") {
		t.Fatalf("next day %+v", got)
	}
	// Saturday: no window
	ck.set(at(10, 0).AddDate(0, 0, 5))
	n.Step(ctx)
	if len(f.sent()) != 6 {
		t.Fatal("sent on Saturday")
	}
}

// TestCHK_SVC_06_EventsRetry — error / warning / no_connection /
// recovered per settings.viber.events, retry with backoff on 5xx / Viber
// status 12, a permanent error (status 2) is not retried, at most
// MaxAttempts, queued items outside the window are dropped, TLS error =
// notify.viber_tls without retry.
func TestCHK_SVC_06_EventsRetry(t *testing.T) {
	f := newFake(t)
	ck := &clock{t: at(10, 0)}
	n, _ := newN(t, f, ck, true)
	ctx := context.Background()
	v := viberOn()
	v.Recipients = v.Recipients[:1]
	n.Snapshot(snapV(v, true, false))
	n.summaryDay = "2026-10-19" // summary already sent
	f.mu.Lock()
	f.answers = [][2]int{{500, 0}, {200, 12}}
	f.mu.Unlock()
	n.Alert(model.AlertEvent{ProfileID: "p1", ProfileName: "Склад", PointName: "А", From: model.LevelOK, To: model.LevelWarning, At: ck.now()})
	n.Alert(model.AlertEvent{ProfileID: "p1", ProfileName: "Склад", From: model.LevelError, To: model.LevelWarning, At: ck.now()}) // improvement, not sent
	n.Step(ctx)
	if g := n.Status(); g.Queued != 1 || g.LastErrorKey != model.MsgViberFailed {
		t.Fatalf("after 500 %+v", g)
	}
	ck.add(4 * time.Second)
	n.Step(ctx) // backoff 5 s not elapsed
	if len(f.sent()) != 1 {
		t.Fatal("retried before the backoff")
	}
	ck.add(time.Second)
	n.Step(ctx) // status 12: rate limited, retry after 30 s
	ck.add(30 * time.Second)
	n.Step(ctx)
	got := f.sent()
	if len(got) != 3 || got[2].Text != got[0].Text {
		t.Fatalf("retries %+v", got)
	}
	if g := n.Status(); g.Sent != 1 || g.Queued != 0 || g.LastOKAt == nil {
		t.Fatalf("status %+v", g)
	}
	// recovered; permanent error: auth failed
	n.Alert(model.AlertEvent{ProfileID: "p1", ProfileName: "Склад", From: model.LevelWarning, To: model.LevelOK, At: ck.now()})
	f.mu.Lock()
	f.answers = [][2]int{{200, 2}}
	f.mu.Unlock()
	n.Step(ctx)
	if g := n.Status(); g.Queued != 0 || g.LastErrorKey != model.MsgViberAuthFailed || g.Dropped != 1 {
		t.Fatalf("auth %+v", g)
	}
	// event kinds: only no_connection selected
	v.Events = []string{model.ViberEventNoConnection}
	n.Snapshot(snapV(v, true, false))
	n.Alert(model.AlertEvent{ProfileID: "p1", PointName: "А", From: model.LevelOK, To: model.LevelError})
	n.Alert(model.AlertEvent{ProfileID: "p1", PointName: "А", From: model.LevelOK, To: model.LevelWarning})
	n.Alert(model.AlertEvent{ProfileID: "p1", From: model.LevelError, To: model.LevelOK})
	if g := n.Status(); g.Queued != 0 {
		t.Fatalf("unselected kinds queued %+v", g)
	}
	n.Alert(model.AlertEvent{ProfileID: "p1", ProfileName: "Склад", From: model.LevelOK, To: model.LevelError, Error: "login failed", ErrorKey: "sql.login_failed"})
	if g := n.Status(); g.Queued != 1 {
		t.Fatalf("no_connection not queued %+v", g)
	}
	n.Step(ctx)
	// queued, retrying, then the window closes: dropped, never sent
	v.Events = model.ViberEventKinds()
	n.Snapshot(snapV(v, true, false))
	f.mu.Lock()
	f.answers = [][2]int{{503, 0}, {503, 0}, {503, 0}, {503, 0}, {503, 0}}
	f.mu.Unlock()
	n.Alert(model.AlertEvent{ProfileID: "p1", PointName: "А", From: model.LevelOK, To: model.LevelError})
	before, dropped := len(f.sent()), n.Status().Dropped
	n.Step(ctx)
	ck.set(at(17, 30))
	n.Step(ctx)
	if g := n.Status(); len(f.sent()) != before+1 || g.Queued != 0 || g.Dropped != dropped+1 {
		t.Fatalf("window close %d %+v", len(f.sent())-before, g)
	}
	// max attempts
	ck.set(at(12, 0).AddDate(0, 0, 1))
	n.summaryDay = "2026-10-20"
	f.mu.Lock()
	f.answers = [][2]int{{503, 0}, {503, 0}, {503, 0}, {503, 0}, {503, 0}, {503, 0}}
	f.mu.Unlock()
	n.Alert(model.AlertEvent{ProfileID: "p1", PointName: "А", From: model.LevelOK, To: model.LevelError})
	before = len(f.sent())
	for i := 0; i < 8; i++ {
		n.Step(ctx)
		ck.add(11 * time.Minute)
	}
	if g := n.Status(); len(f.sent())-before != MaxAttempts || g.Queued != 0 {
		t.Fatalf("attempts %d %+v", len(f.sent())-before, g)
	}
	// disabled in a new snapshot: nothing queued
	off := v
	off.Enabled = false
	n.Snapshot(snapV(off, true, false))
	n.Alert(model.AlertEvent{ProfileID: "p1", PointName: "А", From: model.LevelOK, To: model.LevelError})
	if g := n.Status(); g.Queued != 0 {
		t.Fatal("disabled but queued")
	}

	// TLS: untrusted certificate (Win7 without current roots)
	n2 := New(Options{Store: &memStore{tok: "t"}, Client: hcwinnet.NewClient(5*time.Second, nil), URL: f.srv.URL, Now: ck.now, Loc: loc})
	ck.set(at(10, 0))
	n2.Snapshot(snapV(v, true, false))
	n2.summaryDay = "2026-10-19"
	n2.Alert(model.AlertEvent{ProfileID: "p1", PointName: "А", From: model.LevelOK, To: model.LevelError})
	n2.Step(ctx)
	if g := n2.Status(); g.LastErrorKey != model.MsgViberTLS || g.Queued != 0 {
		t.Fatalf("tls %+v", g)
	}
}

// TestCHK_SVC_06_ViberTest — viber_test uses the given token without
// saving it (or the saved one, reloaded by ReloadToken), reports per
// recipient (ok = all ok), once per 10 s, ignores enabled / schedule.
func TestCHK_SVC_06_ViberTest(t *testing.T) {
	f := newFake(t)
	ck := &clock{t: at(20, 0)} // outside the window: the test ignores it
	n, st := newN(t, f, ck, false)
	v := viberOn()
	v.Enabled = false
	n.Snapshot(snapV(v, true, false))
	var ie *model.IPCError
	if _, err := n.ViberTest(context.Background(), "", ""); !errors.As(err, &ie) || ie.Key != model.MsgViberNoToken {
		t.Fatalf("no token %v", err)
	}
	if _, err := n.ViberTest(context.Background(), "has space", "tok"); !errors.As(err, &ie) || ie.Key != model.MsgViberInvalid {
		t.Fatalf("bad id %v", err)
	}
	f.mu.Lock()
	f.answers = [][2]int{{200, 0}, {200, 6}}
	f.mu.Unlock()
	res, err := n.ViberTest(context.Background(), "", "tmp-tok")
	b, _ := json.Marshal(res)
	if err != nil || string(b) != `{"ok":false,"results":[{"id":"r1","ok":true},{"id":"r2","ok":false,"errorKey":"notify.viber_receiver_invalid"}]}` || st.tok != "" {
		t.Fatalf("test %s %v", b, err)
	}
	if f.tokens[0] != "tmp-tok" || !strings.Contains(f.sent()[0].Text, "тест") || f.sent()[0].Sender.Name != model.ViberSenderName {
		t.Fatal("test message")
	}
	if _, err := n.ViberTest(context.Background(), "r1", "tmp-tok"); !errors.As(err, &ie) || ie.Code != model.ErrCodeRateLimited || ie.RetryAfterMs <= 0 {
		t.Fatalf("rate limit %v", err)
	}
	ck.add(model.ViberTestInterval)
	st.mu.Lock()
	st.tok = "saved-tok"
	st.mu.Unlock()
	n.ReloadToken()
	if res, err := n.ViberTest(context.Background(), "r9", ""); err != nil || !res.OK || len(res.Results) != 1 || res.Results[0].ID != "r9" || f.tokens[2] != "saved-tok" {
		t.Fatalf("single %+v %v", res, err)
	}
}

// TestCHK_SVC_06_SummaryStateAndReadyCap — the date of the last summary
// survives a restart (viber_state.json, atomic): a restart inside the
// window does not resend; a missing / unreadable file = not sent yet; a
// profile still not polled after ReadyWait is listed as
// notify.not_checked.
func TestCHK_SVC_06_SummaryStateAndReadyCap(t *testing.T) {
	f := newFake(t)
	dir := t.TempDir()
	ck := &clock{t: at(10, 0)}
	mk := func() *Notifier {
		n := New(Options{Store: &memStore{tok: "t"}, Client: f.client(), URL: f.srv.URL + "/pa/send_message", Lang: model.LangRU,
			Now: ck.now, Loc: loc, StateDir: dir})
		return n
	}
	ctx := context.Background()
	n := mk()
	n.Snapshot(snap(true, false))
	n.Step(ctx)
	if len(f.sent()) != 2 {
		t.Fatalf("summary %d", len(f.sent()))
	}
	b, err := os.ReadFile(filepath.Join(dir, StateFile))
	if err != nil || string(b) != `{"version":1,"lastSummaryDate":"2026-10-19"}` {
		t.Fatalf("state %s %v", b, err)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(m) != 0 {
		t.Fatalf("temp files %v", m)
	}
	// restart inside the window: no second summary
	ck.add(time.Hour)
	n2 := mk()
	n2.Snapshot(snap(true, false))
	n2.Step(ctx)
	if len(f.sent()) != 2 {
		t.Fatal("summary resent after a restart")
	}
	// unreadable state: treated as not sent yet
	if err := os.WriteFile(filepath.Join(dir, StateFile), []byte("{garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	n3 := mk()
	n3.Snapshot(snap(true, false))
	n3.Step(ctx)
	if len(f.sent()) != 4 {
		t.Fatal("no summary with an unreadable state file")
	}
	// next day: a profile never polled; summary after ReadyWait
	os.Remove(filepath.Join(dir, StateFile))
	ck.set(at(9, 0).AddDate(0, 0, 1))
	n4 := mk()
	s := snap(true, false)
	s.Profiles = append(s.Profiles, model.ProfileSnapshot{ProfileID: "p2", Name: "Офис", Enabled: true},
		model.ProfileSnapshot{ProfileID: "p3", Name: "Выкл", Enabled: false})
	n4.Snapshot(s)
	n4.Step(ctx)
	ck.add(ReadyWait - time.Second)
	n4.Step(ctx)
	if len(f.sent()) != 4 {
		t.Fatal("summary before ReadyWait without all polls")
	}
	ck.add(time.Second)
	n4.Step(ctx)
	got := f.sent()
	if len(got) != 6 || !strings.Contains(got[4].Text, "- Офис: ещё не проверена") || strings.Contains(got[4].Text, "Выкл") {
		t.Fatalf("capped summary %+v", got[len(got)-1])
	}
}

// TestCHK_SVC_06_NoHeadOfLineBlocking — a message in backoff does not
// hold back the messages after it.
func TestCHK_SVC_06_NoHeadOfLineBlocking(t *testing.T) {
	f := newFake(t)
	ck := &clock{t: at(10, 0)}
	n, _ := newN(t, f, ck, true)
	v := viberOn()
	v.Recipients = v.Recipients[:1]
	n.Snapshot(snapV(v, true, false))
	n.summaryDay = "2026-10-19"
	f.mu.Lock()
	f.answers = [][2]int{{500, 0}}
	f.mu.Unlock()
	n.Alert(model.AlertEvent{ProfileID: "p1", ProfileName: "A", PointName: "x", From: model.LevelOK, To: model.LevelError})
	n.Alert(model.AlertEvent{ProfileID: "p1", ProfileName: "B", PointName: "y", From: model.LevelOK, To: model.LevelWarning})
	n.Step(context.Background())
	got := f.sent()
	if g := n.Status(); len(got) != 2 || g.Sent != 1 || g.Queued != 1 || !strings.Contains(got[1].Text, "«B»") {
		t.Fatalf("HOL %+v %+v", got, g)
	}
	n.Alert(model.AlertEvent{ProfileID: "p1", ProfileName: "C", PointName: "z", From: model.LevelOK, To: model.LevelError})
	ck.add(time.Second)
	n.Step(context.Background()) // A still in backoff, C goes out
	if got := f.sent(); len(got) != 3 || !strings.Contains(got[2].Text, "«C»") {
		t.Fatalf("after backoff head %+v", got)
	}
	ck.add(5 * time.Second)
	n.Step(context.Background())
	if g := n.Status(); len(f.sent()) != 4 || g.Queued != 0 || g.Sent != 3 {
		t.Fatalf("retry %+v", g)
	}
}

// TestCHK_SVC_06_NoRawErrorText — events, summaries and reminders carry
// the localized text of the error key, never the raw SQL error.
func TestCHK_SVC_06_NoRawErrorText(t *testing.T) {
	f := newFake(t)
	ck := &clock{t: at(10, 0)}
	n, _ := newN(t, f, ck, true)
	raw := "Login failed for user 'sa'. RAW-SQL-SECRET"
	s := snap(true, false)
	s.Profiles[0].Level, s.Profiles[0].Error, s.Profiles[0].ErrorKey = model.LevelError, raw, model.MsgSQLTLSCertificate
	s.Profiles = append(s.Profiles, model.ProfileSnapshot{ProfileID: "p2", Name: "Офис", Enabled: true, CheckedAt: at(9, 0),
		Level: model.LevelError, Error: raw})
	n.Snapshot(s)
	n.Alert(model.AlertEvent{ProfileID: "p1", ProfileName: "Склад", From: model.LevelOK, To: model.LevelError, Error: raw, ErrorKey: model.MsgSQLTLSCertificate})
	n.Alert(model.AlertEvent{ProfileID: "p2", ProfileName: "Офис", From: model.LevelOK, To: model.LevelError, Error: raw})
	n.Step(context.Background())
	ck.add(time.Hour)
	n.Step(context.Background()) // reminder
	got := f.sent()
	if len(got) != 8 {
		t.Fatalf("sent %d", len(got))
	}
	known := model.SQLMessage(model.MsgSQLTLSCertificate, model.LangRU)
	unknown := model.MonitorMessage(model.MsgViberErrorUnknown, model.LangRU)
	for _, m := range got {
		if strings.Contains(m.Text, "RAW-SQL") || strings.Contains(m.Text, "Login failed") {
			t.Fatalf("raw error text sent: %q", m.Text)
		}
	}
	if known == "" || unknown == "" || !strings.Contains(got[0].Text, known) || !strings.Contains(got[2].Text, unknown) ||
		!strings.Contains(got[4].Text, known) || !strings.Contains(got[4].Text, unknown) || !strings.Contains(got[6].Text, known) {
		t.Fatalf("texts %q", []string{got[0].Text, got[2].Text, got[4].Text, got[6].Text})
	}
	if en := EventText(model.AlertEvent{ProfileName: "X", From: model.LevelOK, To: model.LevelError, Error: raw}, model.LangEN); strings.Contains(en, "RAW") ||
		!strings.Contains(en, "connection or query error") {
		t.Fatalf("en %q", en)
	}
}
