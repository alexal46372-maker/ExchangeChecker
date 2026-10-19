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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/hcwinnet"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

type memStore struct {
	mu  sync.Mutex
	v   model.ViberSettings
	tok string
	err error
}

func (m *memStore) LoadViber() (model.ViberSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.v, nil
}
func (m *memStore) SaveViber(v model.ViberSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.v = v
	return nil
}
func (m *memStore) LoadViberToken() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tok, nil
}
func (m *memStore) SaveViberToken(t string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tok = t
	return nil
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

func newN(t *testing.T, f *fakeViber, ck *clock, enabled bool) (*Notifier, *memStore) {
	st := &memStore{v: model.DefaultViberSettings()}
	if enabled {
		st.v.Enabled, st.v.Receivers, st.tok = true, []string{"r1", "r2"}, "tok-1"
	}
	n := New(Options{Store: st, Client: f.client(), URL: f.srv.URL + "/pa/send_message", Lang: model.LangRU, Now: ck.now, Loc: loc})
	return n, st
}

func snap(ready bool, problem bool) model.StatusSnapshot {
	p := model.ProfileSnapshot{ProfileID: "p1", Name: "Склад", Enabled: true, Level: model.LevelOK}
	if ready {
		p.CheckedAt = at(9, 0)
	}
	if problem {
		p.Level = model.LevelError
		p.Points = []model.PointStatus{{ExchangePoint: model.ExchangePoint{Name: "Точка А", Level: model.LevelError}}}
	}
	return model.StatusSnapshot{Profiles: []model.ProfileSnapshot{p}}
}

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
	if v, _ := n.GetViber(); v.Status.Dropped != 1 || v.Status.Sent != 4 || v.InWindow {
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

// TestCHK_SVC_06_EventsRetry — overdue / recovered per settings, retry
// with backoff on 5xx / Viber status 12, a permanent error (status 2)
// is not retried, at most MaxAttempts, queued items outside the window
// are dropped, TLS error = notify.viber_tls without retry.
func TestCHK_SVC_06_EventsRetry(t *testing.T) {
	f := newFake(t)
	ck := &clock{t: at(10, 0)}
	n, st := newN(t, f, ck, true)
	ctx := context.Background()
	n.summaryDay = "2026-10-19" // summary already sent
	st.v.Receivers = []string{"r1"}
	v := st.v
	if _, err := n.SaveViber(v, "", false, `HOST\a`); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.answers = [][2]int{{500, 0}, {200, 12}}
	f.mu.Unlock()
	n.Alert(model.AlertEvent{ProfileID: "p1", ProfileName: "Склад", PointName: "А", From: model.LevelOK, To: model.LevelWarning, At: ck.now()})
	n.Alert(model.AlertEvent{ProfileID: "p1", ProfileName: "Склад", From: model.LevelError, To: model.LevelWarning, At: ck.now()}) // improvement, not sent
	n.Step(ctx)
	if g, _ := n.GetViber(); g.Status.Queued != 1 || g.Status.LastErrorKey != model.MsgViberFailed {
		t.Fatalf("after 500 %+v", g.Status)
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
	if g, _ := n.GetViber(); g.Status.Sent != 1 || g.Status.Queued != 0 {
		t.Fatalf("status %+v", g.Status)
	}
	// recovered
	n.Alert(model.AlertEvent{ProfileID: "p1", ProfileName: "Склад", From: model.LevelWarning, To: model.LevelOK, At: ck.now()})
	// permanent: auth failed
	f.mu.Lock()
	f.answers = [][2]int{{200, 2}}
	f.mu.Unlock()
	n.Step(ctx)
	if g, _ := n.GetViber(); g.Status.Queued != 0 || g.Status.LastErrorKey != model.MsgViberAuthFailed || g.Status.Dropped != 1 {
		t.Fatalf("auth %+v", g.Status)
	}
	// events off
	v.Events.Overdue = false
	if _, err := n.SaveViber(v, "", false, ""); err != nil {
		t.Fatal(err)
	}
	n.Alert(model.AlertEvent{ProfileID: "p1", From: model.LevelOK, To: model.LevelError})
	if g, _ := n.GetViber(); g.Status.Queued != 0 {
		t.Fatal("overdue off but queued")
	}
	// queued, retrying, then the window closes: dropped, never sent
	v.Events.Overdue = true
	_, _ = n.SaveViber(v, "", false, "")
	f.mu.Lock()
	f.answers = [][2]int{{503, 0}, {503, 0}, {503, 0}, {503, 0}, {503, 0}}
	f.mu.Unlock()
	n.Alert(model.AlertEvent{ProfileID: "p1", From: model.LevelOK, To: model.LevelError})
	before := len(f.sent())
	n.Step(ctx)
	ck.set(at(17, 30))
	n.Step(ctx)
	if g, _ := n.GetViber(); len(f.sent()) != before+1 || g.Status.Queued != 0 || g.Status.Dropped != 2 {
		t.Fatalf("window close %d %+v", len(f.sent())-before, g.Status)
	}
	// max attempts
	ck.set(at(12, 0).AddDate(0, 0, 1))
	n.summaryDay = "2026-10-20"
	f.mu.Lock()
	f.answers = [][2]int{{503, 0}, {503, 0}, {503, 0}, {503, 0}, {503, 0}, {503, 0}}
	f.mu.Unlock()
	n.Alert(model.AlertEvent{ProfileID: "p1", From: model.LevelOK, To: model.LevelError})
	before = len(f.sent())
	for i := 0; i < 8; i++ {
		n.Step(ctx)
		ck.add(11 * time.Minute)
	}
	if g, _ := n.GetViber(); len(f.sent())-before != MaxAttempts || g.Status.Queued != 0 {
		t.Fatalf("attempts %d %+v", len(f.sent())-before, g.Status)
	}

	// TLS: untrusted certificate (Win7 without current roots)
	n2 := New(Options{Store: &memStore{v: st.v, tok: "t"}, Client: hcwinnet.NewClient(5*time.Second, nil), URL: f.srv.URL, Now: ck.now, Loc: loc})
	ck.set(at(10, 0))
	n2.summaryDay = "2026-10-19"
	n2.Alert(model.AlertEvent{ProfileID: "p1", From: model.LevelOK, To: model.LevelError})
	n2.Step(ctx)
	if g, _ := n2.GetViber(); g.Status.LastErrorKey != model.MsgViberTLS || g.Status.Queued != 0 {
		t.Fatalf("tls %+v", g.Status)
	}
}

// TestCHK_SVC_06_SaveAndTest — the token is write-only (hasToken only),
// enabled needs a token, clear removes it, validation errors carry
// notify.viber_invalid + field; viber_test uses the given token without
// saving it, reports per receiver, once per 10 s.
func TestCHK_SVC_06_SaveAndTest(t *testing.T) {
	f := newFake(t)
	ck := &clock{t: at(20, 0)} // outside the window: the test ignores it
	n, st := newN(t, f, ck, false)
	v := model.DefaultViberSettings()
	v.Enabled, v.Receivers = true, []string{"r1"}
	var ie *model.IPCError
	if _, err := n.SaveViber(v, "", false, ""); !errors.As(err, &ie) || ie.Key != model.MsgViberNoToken {
		t.Fatalf("enabled without token %v", err)
	}
	bad := v
	bad.Schedule.Days = []int{8}
	if _, err := n.SaveViber(bad, "t", false, ""); !errors.As(err, &ie) || ie.Key != model.MsgViberInvalid || ie.Args[0] != "schedule.days[0]" {
		t.Fatalf("bad days %v", err)
	}
	if _, err := n.SaveViber(v, "has space", false, ""); !errors.As(err, &ie) || ie.Key != model.MsgViberInvalid {
		t.Fatalf("bad token %v", err)
	}
	view, err := n.SaveViber(v, "tok-9", false, `HOST\a`)
	if err != nil || !view.HasToken || st.tok != "tok-9" {
		t.Fatalf("save %+v %v", view, err)
	}
	b, _ := json.Marshal(view)
	if strings.Contains(string(b), "tok-9") {
		t.Fatalf("token in reply %s", b)
	}
	v.Enabled = false
	if view, err := n.SaveViber(v, "", true, ""); err != nil || view.HasToken || st.tok != "" {
		t.Fatalf("clear %+v %v", view, err)
	}

	f.mu.Lock()
	f.answers = [][2]int{{200, 0}, {200, 6}}
	f.mu.Unlock()
	if _, err := n.ViberTest(context.Background(), "", ""); !errors.As(err, &ie) || ie.Key != model.MsgViberNoToken {
		t.Fatalf("no token %v", err)
	}
	v.Receivers = []string{"r1", "r2"}
	_, _ = n.SaveViber(v, "", false, "")
	res, err := n.ViberTest(context.Background(), "", "tmp-tok")
	if err != nil || len(res.Results) != 2 || !res.Results[0].OK || res.Results[1].OK ||
		res.Results[1].ErrorKey != model.MsgViberReceiverInvalid || *res.Results[1].ViberStatus != 6 || st.tok != "" {
		t.Fatalf("test %+v %v", res, err)
	}
	if f.tokens[0] != "tmp-tok" || !strings.Contains(f.sent()[0].Text, "тест") {
		t.Fatal("test message")
	}
	if _, err := n.ViberTest(context.Background(), "r1", "tmp-tok"); !errors.As(err, &ie) || ie.Code != model.ErrCodeRateLimited || ie.RetryAfterMs <= 0 {
		t.Fatalf("rate limit %v", err)
	}
	ck.add(model.ViberTestInterval)
	if res, err := n.ViberTest(context.Background(), "r9", "tmp-tok"); err != nil || len(res.Results) != 1 || res.Results[0].Receiver != "r9" {
		t.Fatalf("single %+v %v", res, err)
	}
}
