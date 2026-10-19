// Package notify sends the Viber notifications of the service (phase 10,
// CHK-SVC-06): alert transitions, one summary per schedule window and
// reminders, only inside the window (local time of the service machine),
// with a bounded queue, retries and backoff. HTTPS through
// internal/hcwinnet (PLAN §12 п. 131).
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/hcwinnet"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// ViberURL is the Viber REST endpoint.
const ViberURL = "https://chatapi.viber.com/pa/send_message"

const (
	// MaxQueue is the queue limit (messages x receivers); more is dropped.
	MaxQueue = 200
	// MaxAttempts per message and receiver.
	MaxAttempts = 5
	maxResp     = 64 << 10
	tick        = 5 * time.Second
)

// Backoff between attempts (index = attempts already made - 1).
var Backoff = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}

// Store keeps the token (config.Store); the settings come with the
// snapshots (settings.viber).
type Store interface {
	LoadViberToken() (string, error)
}

// Poster is the HTTPS client (hcwinnet.Client).
type Poster interface {
	PostJSON(ctx context.Context, url string, headers map[string]string, body []byte, maxResp int64) (int, []byte, error)
}

// Logger as alert.Logger.
type Logger = alert.Logger

// Options of New; zero = production defaults.
type Options struct {
	Store  Store
	Client Poster // nil = hcwinnet.NewClient(15 s, system roots)
	URL    string // "" = ViberURL
	Lang   string
	Log    Logger
	Now    func() time.Time
	// Loc is the schedule time zone (nil = time.Local: the machine time).
	Loc *time.Location
	// StateDir holds viber_state.json (the date of the last summary); ""
	// = not persisted (tests).
	StateDir string
}

// StateFile keeps the date of the last summary across restarts.
const StateFile = "viber_state.json"

// ReadyWait caps the wait for the first poll of every enabled profile
// before the summary (then listed as notify.not_checked).
const ReadyWait = 10 * time.Minute

type stateDoc struct {
	Version         int    `json:"version"`
	LastSummaryDate string `json:"lastSummaryDate"`
}

type item struct {
	id       uint64
	receiver string
	text     string
	attempts int
	next     time.Time
}

// Notifier is an alert.Sink fed with the snapshots of the monitor.
type Notifier struct {
	o Options

	mu         sync.Mutex
	settings   model.ViberSettings
	token      string
	queue      []item
	status     model.ViberStatus
	snap       *model.StatusSnapshot
	summaryDay string    // local date of the window whose summary was queued
	lastRemind time.Time // last summary / reminder
	lastTest   time.Time
	started    time.Time
	seq        uint64
	wake       chan struct{}
}

// New loads the token (an error is logged: Viber stays off). The
// settings arrive with the first snapshot (Viber off until then).
func New(o Options) *Notifier {
	if o.Client == nil {
		o.Client = hcwinnet.NewClient(15*time.Second, nil)
	}
	if o.URL == "" {
		o.URL = ViberURL
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Loc == nil {
		o.Loc = time.Local
	}
	if o.Log == nil {
		o.Log = nopLog{}
	}
	n := &Notifier{o: o, settings: model.DefaultViberSettings(), wake: make(chan struct{}, 1)}
	n.settings.Enabled = false
	n.started = o.Now()
	n.summaryDay = n.loadState()
	n.ReloadToken()
	return n
}

// loadState reads viber_state.json; missing / unreadable = no summary
// sent yet.
func (n *Notifier) loadState() string {
	if n.o.StateDir == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(n.o.StateDir, StateFile))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			n.o.Log.Printf("viber: %s: %v (no summary sent yet)", StateFile, err)
		}
		return ""
	}
	var d stateDoc
	if err := json.Unmarshal(b, &d); err != nil {
		n.o.Log.Printf("viber: %s: %v (no summary sent yet)", StateFile, err)
		return ""
	}
	if _, err := time.Parse("2006-01-02", d.LastSummaryDate); err != nil {
		return ""
	}
	return d.LastSummaryDate
}

// saveState writes viber_state.json atomically (temp file + rename).
func (n *Notifier) saveState(day string) {
	if n.o.StateDir == "" {
		return
	}
	b, _ := json.Marshal(stateDoc{Version: 1, LastSummaryDate: day})
	err := func() error {
		f, err := os.CreateTemp(n.o.StateDir, StateFile+".*.tmp")
		if err != nil {
			return err
		}
		tmp := f.Name()
		_, werr := f.Write(b)
		if werr == nil {
			werr = f.Sync()
		}
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr == nil {
			werr = os.Rename(tmp, filepath.Join(n.o.StateDir, StateFile))
		}
		if werr != nil {
			_ = os.Remove(tmp)
		}
		return werr
	}()
	if err != nil {
		n.o.Log.Printf("viber: %s: %v", StateFile, err)
	}
}

// ReloadToken rereads viber.dpapi (after save_settings changed it).
func (n *Notifier) ReloadToken() {
	t, err := n.o.Store.LoadViberToken()
	if err != nil {
		n.o.Log.Printf("viber: token: %v (Viber off)", err)
		t = ""
	}
	n.mu.Lock()
	if t != n.token {
		n.queue = nil
	}
	n.token = t
	n.mu.Unlock()
	n.kick()
}

// Status is settings.viber.status.
func (n *Notifier) Status() *model.ViberStatus {
	n.mu.Lock()
	defer n.mu.Unlock()
	st := n.status
	st.Queued = len(n.queue)
	return &st
}

type nopLog struct{}

func (nopLog) Printf(string, ...interface{}) {}

func (n *Notifier) local() time.Time { return n.o.Now().In(n.o.Loc) }

func (n *Notifier) activeLocked() bool {
	return n.settings.Enabled && n.token != "" && len(n.settings.Recipients) > 0
}

func (n *Notifier) kick() {
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

func (n *Notifier) enqueueLocked(text string, now time.Time) {
	for _, r := range n.settings.Recipients {
		if len(n.queue) >= MaxQueue {
			n.status.Dropped++
			continue
		}
		n.seq++
		n.queue = append(n.queue, item{id: n.seq, receiver: r.ID, text: text, next: now})
	}
}

// Alert implements alert.Sink: model.ViberEventOf (error, warning,
// no_connection, recovered) per settings.viber.events; outside the
// window nothing is queued.
func (n *Notifier) Alert(e model.AlertEvent) {
	kind := model.ViberEventOf(e)
	if kind == "" {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.activeLocked() || !n.settings.HasEvent(kind) {
		return
	}
	now := n.local()
	if !n.settings.InWindow(now) {
		n.status.Dropped++
		return
	}
	n.enqueueLocked(EventText(e, n.o.Lang), now)
	n.kick()
}

// Snapshot feeds the current status (problems of the summary /
// reminders) and settings.viber.
func (n *Notifier) Snapshot(s model.StatusSnapshot) {
	n.mu.Lock()
	n.snap = &s
	if s.Settings != nil && s.Settings.Viber != nil {
		v := s.Settings.Viber.Stored()
		v.Recipients = append([]model.ViberRecipient{}, v.Recipients...)
		v.Events = append([]string{}, v.Events...)
		v.Schedule.Days = append([]int{}, v.Schedule.Days...)
		if !sameRecipients(n.settings.Recipients, v.Recipients) || !v.Enabled {
			n.queue = nil
		}
		n.settings = v
	}
	n.mu.Unlock()
	n.kick()
}

func sameRecipients(a, b []model.ViberRecipient) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}

// ready: every enabled profile was polled at least once.
func ready(s *model.StatusSnapshot) bool {
	if s == nil {
		return false
	}
	for _, p := range s.Profiles {
		if p.Enabled && p.CheckedAt.IsZero() {
			return false
		}
	}
	return true
}

func problems(s *model.StatusSnapshot) []model.ProfileSnapshot {
	var out []model.ProfileSnapshot
	for _, p := range s.Profiles {
		if p.Enabled && p.Level != model.LevelOK {
			out = append(out, p)
		}
	}
	return out
}

// ErrorText is the localized text of an error key (sql.* / monitor.*),
// notify.error_unknown otherwise: the raw (SQL) error text is never sent.
func ErrorText(key, lang string) string {
	if t := model.SQLMessage(key, lang); t != "" {
		return t
	}
	if t := model.MonitorMessage(key, lang); t != "" {
		return t
	}
	return model.MonitorMessage(model.MsgViberErrorUnknown, lang)
}

// EventText renders an alert for Viber: alert.Format without the raw
// error, the error as its localized key.
func EventText(e model.AlertEvent, lang string) string {
	raw := e.Error != "" || e.ErrorKey != ""
	key := e.ErrorKey
	e.Error, e.ErrorKey = "", ""
	t := alert.Format(e, lang)
	if raw {
		if lang == model.LangEN {
			t += "; error: "
		} else {
			t += "; ошибка: "
		}
		t += ErrorText(key, lang)
	}
	return t
}

func (n *Notifier) summaryText(s *model.StatusSnapshot, reminder bool) string {
	en := n.o.Lang == model.LangEN
	pr := problems(s)
	if !reminder {
		for _, p := range s.Profiles {
			if p.Enabled && p.CheckedAt.IsZero() {
				pr = append(pr, p)
			}
		}
	}
	var b strings.Builder
	switch {
	case reminder && en:
		b.WriteString("ExchangeChecker reminder: unresolved problems")
	case reminder:
		b.WriteString("ExchangeChecker, напоминание: нерешённые проблемы")
	case en:
		b.WriteString("ExchangeChecker summary")
	default:
		b.WriteString("ExchangeChecker, сводка")
	}
	if len(pr) == 0 {
		if en {
			b.WriteString(": all profiles ok")
		} else {
			b.WriteString(": все профили в норме")
		}
		return b.String()
	}
	for _, p := range pr {
		b.WriteString("\n- " + p.Name + ": ")
		if p.CheckedAt.IsZero() {
			b.WriteString(model.MonitorMessage(model.MsgViberNotChecked, n.o.Lang))
			continue
		}
		if p.Error != "" || p.ErrorKey != "" {
			b.WriteString(ErrorText(p.ErrorKey, n.o.Lang))
			continue
		}
		var bad []string
		for _, pt := range p.Points {
			if pt.Level != model.LevelOK {
				bad = append(bad, pt.Name)
			}
		}
		if len(bad) > 10 {
			bad = append(bad[:10], "...")
		}
		b.WriteString(strings.Join(bad, ", "))
	}
	return b.String()
}

// Step runs one scheduler round at now (exported for tests).
func (n *Notifier) Step(ctx context.Context) {
	n.mu.Lock()
	now := n.local()
	if !n.activeLocked() {
		n.queue = nil
		n.mu.Unlock()
		return
	}
	if !n.settings.InWindow(now) {
		// nothing is sent outside the window: what is left is dropped
		n.status.Dropped += uint64(len(n.queue))
		n.queue = nil
		n.mu.Unlock()
		return
	}
	day := now.Format("2006-01-02")
	if n.summaryDay != day && n.snap != nil && (ready(n.snap) || n.o.Now().Sub(n.started) >= ReadyWait) {
		// one summary per window, also when the service starts mid-window
		// (persisted: a restart inside the window does not resend it);
		// profiles still not polled after ReadyWait are listed as such
		n.summaryDay = day
		n.saveState(day)
		n.lastRemind = now
		n.enqueueLocked(n.summaryText(n.snap, false), now)
	} else if n.summaryDay == day && n.settings.ReminderMinutes > 0 && n.snap != nil && len(problems(n.snap)) > 0 &&
		now.Sub(n.lastRemind) >= time.Duration(n.settings.ReminderMinutes)*time.Minute {
		n.lastRemind = now
		n.enqueueLocked(n.summaryText(n.snap, true), now)
	}
	token, sender := n.token, model.ViberSenderName
	n.mu.Unlock()

	// every ready message once per round (no head-of-line blocking by a
	// message in backoff); a network / rate-limit error ends the round
	tried := map[uint64]bool{}
	for ctx.Err() == nil {
		n.mu.Lock()
		if !n.settings.InWindow(n.local()) {
			n.mu.Unlock()
			return
		}
		var it item
		found := false
		for _, q := range n.queue {
			if !tried[q.id] && !q.next.After(now) {
				it, found = q, true
				break
			}
		}
		n.mu.Unlock()
		if !found {
			return
		}
		tried[it.id] = true
		key, _, retry := n.send(ctx, token, sender, it.receiver, it.text)
		n.mu.Lock()
		idx := -1
		for i, q := range n.queue {
			if q.id == it.id {
				idx = i
			}
		}
		if idx < 0 {
			n.mu.Unlock() // dropped meanwhile (settings / token changed)
			continue
		}
		t := n.o.Now()
		if key == "" {
			n.status.Sent++
			n.status.LastOKAt = &t
			n.queue = append(n.queue[:idx:idx], n.queue[idx+1:]...)
			n.mu.Unlock()
			continue
		}
		n.status.LastErrorAt, n.status.LastErrorKey = &t, key
		n.queue[idx].attempts++
		if att := n.queue[idx].attempts; retry && att < MaxAttempts {
			n.queue[idx].next = now.Add(Backoff[att-1])
			n.mu.Unlock()
			n.o.Log.Printf("viber: %s, retry %d", key, att)
			if key == model.MsgViberNetwork || key == model.MsgViberRateLimited {
				return
			}
			continue
		}
		n.status.Dropped++
		n.queue = append(n.queue[:idx:idx], n.queue[idx+1:]...)
		n.mu.Unlock()
		n.o.Log.Printf("viber: %s, message dropped", key)
	}
}

// Run schedules until ctx ends.
func (n *Notifier) Run(ctx context.Context) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		n.Step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-n.wake:
		}
	}
}

type viberSender struct {
	Name string `json:"name"`
}

type viberMsg struct {
	Receiver      string      `json:"receiver"`
	MinAPIVersion int         `json:"min_api_version"`
	Sender        viberSender `json:"sender"`
	Type          string      `json:"type"`
	Text          string      `json:"text"`
}

type viberResp struct {
	Status        *int   `json:"status"`
	StatusMessage string `json:"status_message"`
}

// send posts one message; key "" = ok; viberStatus nil when no Viber
// answer; retry = transient failure.
func (n *Notifier) send(ctx context.Context, token, sender, receiver, text string) (key string, viberStatus *int, retry bool) {
	body, _ := json.Marshal(viberMsg{Receiver: receiver, MinAPIVersion: 1, Sender: viberSender{Name: sender}, Type: "text", Text: text})
	st, b, err := n.o.Client.PostJSON(ctx, n.o.URL, map[string]string{"X-Viber-Auth-Token": token}, body, maxResp)
	switch {
	case errors.Is(err, hcwinnet.ErrTLS):
		return model.MsgViberTLS, nil, false
	case err != nil:
		return model.MsgViberNetwork, nil, true
	case st == 429:
		return model.MsgViberRateLimited, nil, true
	case st >= 500:
		return model.MsgViberFailed, nil, true
	case st != 200:
		return model.MsgViberFailed, nil, false
	}
	var r viberResp
	if json.Unmarshal(b, &r) != nil || r.Status == nil {
		return model.MsgViberFailed, nil, true
	}
	switch *r.Status {
	case 0:
		return "", r.Status, false
	case 2:
		return model.MsgViberAuthFailed, r.Status, false
	case 5, 6:
		return model.MsgViberReceiverInvalid, r.Status, false
	case 12:
		return model.MsgViberRateLimited, r.Status, true
	}
	return model.MsgViberFailed, r.Status, false
}

// ViberTest sends a test message to recipientID (or every saved
// recipient) with token (or the saved one); ignores enabled and the
// schedule; one test per model.ViberTestInterval.
func (n *Notifier) ViberTest(ctx context.Context, recipientID, token string) (model.ViberTestResult, error) {
	n.mu.Lock()
	now := n.o.Now()
	if !n.lastTest.IsZero() && now.Sub(n.lastTest) < model.ViberTestInterval {
		wait := model.ViberTestInterval - now.Sub(n.lastTest)
		n.mu.Unlock()
		return model.ViberTestResult{}, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgViberRateLimited,
			Message: "viber: one test per 10 s", RetryAfterMs: int64(wait / time.Millisecond)}
	}
	if token == "" {
		token = n.token
	}
	var recv []string
	for _, r := range n.settings.Recipients {
		recv = append(recv, r.ID)
	}
	n.mu.Unlock()
	if token != "" && !model.ValidViberToken(token) || recipientID != "" && !model.ValidViberID(recipientID) {
		return model.ViberTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberInvalid, Message: "viber: invalid token or recipient id"}
	}
	if token == "" {
		return model.ViberTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberNoToken, Message: "viber: no token"}
	}
	if recipientID != "" {
		recv = []string{recipientID}
	}
	if len(recv) == 0 {
		return model.ViberTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberNoRecipients, Message: "viber: no recipients"}
	}
	n.mu.Lock()
	n.lastTest = now
	n.mu.Unlock()
	text := "ExchangeChecker: тестовое сообщение"
	if n.o.Lang == model.LangEN {
		text = "ExchangeChecker: test message"
	}
	res := model.ViberTestResult{OK: true, Results: []model.ViberTestItem{}}
	for _, r := range recv {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		key, _, _ := n.send(ctx, token, model.ViberSenderName, r, text)
		res.Results = append(res.Results, model.ViberTestItem{ID: r, OK: key == "", ErrorKey: key})
		res.OK = res.OK && key == ""
	}
	return res, nil
}
