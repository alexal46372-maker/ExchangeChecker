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
	"fmt"
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

// Store keeps the settings and the token (config.Store).
type Store interface {
	LoadViber() (model.ViberSettings, error)
	SaveViber(model.ViberSettings) error
	LoadViberToken() (string, error)
	SaveViberToken(string) error
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
	// OnSave is called after save_viber (Event Log 105).
	OnSave func(account string, s model.ViberSettings, tokenChanged bool)
}

type item struct {
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
	wake       chan struct{}
	loadErr    error
}

// New loads the settings and the token (errors are logged; Viber stays
// off until a valid save).
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
	if s, err := o.Store.LoadViber(); err != nil {
		n.loadErr = err
		o.Log.Printf("viber: %v (Viber off)", err)
	} else {
		n.settings = s
	}
	if t, err := o.Store.LoadViberToken(); err != nil {
		o.Log.Printf("viber: token: %v", err)
	} else {
		n.token = t
	}
	return n
}

type nopLog struct{}

func (nopLog) Printf(string, ...interface{}) {}

func (n *Notifier) local() time.Time { return n.o.Now().In(n.o.Loc) }

func (n *Notifier) activeLocked() bool {
	return n.settings.Enabled && n.token != "" && len(n.settings.Receivers) > 0 && n.loadErr == nil
}

func (n *Notifier) kick() {
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

func (n *Notifier) enqueueLocked(text string, now time.Time) {
	for _, r := range n.settings.Receivers {
		if len(n.queue) >= MaxQueue {
			n.status.Dropped++
			continue
		}
		n.queue = append(n.queue, item{receiver: r, text: text, next: now})
	}
}

// Alert implements alert.Sink: overdue (level raised) and recovered (back
// to ok) per settings.events; outside the window nothing is queued.
func (n *Notifier) Alert(e model.AlertEvent) {
	if e.To == e.From {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.activeLocked() {
		return
	}
	if e.To == model.LevelOK && !n.settings.Events.Recovered || e.To != model.LevelOK && (e.To < e.From || !n.settings.Events.Overdue) {
		return
	}
	now := n.local()
	if !n.settings.InWindow(now) {
		n.status.Dropped++
		return
	}
	n.enqueueLocked(alert.Format(e, n.o.Lang), now)
	n.kick()
}

// Snapshot feeds the current status (problems of the summary/reminders).
func (n *Notifier) Snapshot(s model.StatusSnapshot) {
	n.mu.Lock()
	n.snap = &s
	n.mu.Unlock()
	n.kick()
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

func (n *Notifier) summaryText(s *model.StatusSnapshot, reminder bool) string {
	en := n.o.Lang == model.LangEN
	pr := problems(s)
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
		if p.Error != "" || p.ErrorKey != "" {
			if en {
				b.WriteString("no connection / query error")
			} else {
				b.WriteString("нет связи / ошибка запроса")
			}
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
	if n.summaryDay != day && ready(n.snap) {
		// one summary per window, also when the service starts mid-window
		n.summaryDay = day
		n.lastRemind = now
		n.enqueueLocked(n.summaryText(n.snap, false), now)
	} else if n.summaryDay == day && n.settings.ReminderMinutes > 0 && n.snap != nil && len(problems(n.snap)) > 0 &&
		now.Sub(n.lastRemind) >= time.Duration(n.settings.ReminderMinutes)*time.Minute {
		n.lastRemind = now
		n.enqueueLocked(n.summaryText(n.snap, true), now)
	}
	token, sender := n.token, n.settings.SenderName
	n.mu.Unlock()

	for ctx.Err() == nil {
		n.mu.Lock()
		if len(n.queue) == 0 || n.queue[0].next.After(now) || !n.settings.InWindow(n.local()) {
			n.mu.Unlock()
			return
		}
		it := n.queue[0]
		n.mu.Unlock()
		key, _, retry := n.send(ctx, token, sender, it.receiver, it.text)
		n.mu.Lock()
		if len(n.queue) == 0 || n.queue[0].receiver != it.receiver || n.queue[0].text != it.text {
			n.mu.Unlock() // settings changed meanwhile
			return
		}
		t := n.o.Now()
		if key == "" {
			n.status.Sent++
			n.status.LastOKAt = &t
			n.queue = n.queue[1:]
			n.mu.Unlock()
			continue
		}
		n.status.LastErrorAt, n.status.LastErrorKey = &t, key
		n.queue[0].attempts++
		if retry && n.queue[0].attempts < MaxAttempts {
			att := n.queue[0].attempts
			n.queue[0].next = now.Add(Backoff[att-1])
			n.mu.Unlock()
			n.o.Log.Printf("viber: %s, retry %d", key, att)
			return
		}
		n.status.Dropped++
		n.queue = n.queue[1:]
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

// GetViber is the get_viber reply (never the token).
func (n *Notifier) GetViber() (model.ViberView, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.viewLocked(), nil
}

func (n *Notifier) viewLocked() model.ViberView {
	st := n.status
	st.Queued = len(n.queue)
	s := n.settings
	s.Receivers = append([]string{}, s.Receivers...)
	s.Schedule.Days = append([]int{}, s.Schedule.Days...)
	return model.ViberView{Settings: s, HasToken: n.token != "", InWindow: s.InWindow(n.local()), Status: st}
}

func invalid(err error) error {
	ie := &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberInvalid, Message: err.Error()}
	if fe := model.AsFieldError(err); fe != nil {
		ie.Args = append([]string{fe.Field, fe.Key}, fe.Args...)
	}
	return ie
}

// SaveViber validates and saves the settings and, when given, the token
// (write-only; clear removes it). Enabled needs a token.
func (n *Notifier) SaveViber(s model.ViberSettings, token string, clear bool, account string) (model.ViberView, error) {
	if s.Receivers == nil {
		s.Receivers = []string{}
	}
	if err := s.Validate(); err != nil {
		return model.ViberView{}, invalid(err)
	}
	if token != "" && !model.ValidViberToken(token) {
		return model.ViberView{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberInvalid,
			Message: "viber: invalid token", Args: []string{"viberToken", model.ValRequired}}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	newTok := n.token
	if clear {
		newTok = ""
	}
	if token != "" {
		newTok = token
	}
	if s.Enabled && newTok == "" {
		return model.ViberView{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberNoToken,
			Message: "viber: enabled without a token", Args: []string{"viberToken", model.ValRequired}}
	}
	changed := newTok != n.token
	if changed {
		if err := n.o.Store.SaveViberToken(newTok); err != nil {
			return model.ViberView{}, &model.IPCError{Code: model.ErrCodeSaveFailed, Key: model.MsgMonitorConfigSaveFailed, Message: "viber: " + err.Error()}
		}
	}
	if err := n.o.Store.SaveViber(s); err != nil {
		if changed { // keep the files consistent with the memory state
			_ = n.o.Store.SaveViberToken(n.token)
		}
		return model.ViberView{}, &model.IPCError{Code: model.ErrCodeSaveFailed, Key: model.MsgMonitorConfigSaveFailed, Message: "viber: " + err.Error()}
	}
	old := n.settings
	n.settings, n.token, n.loadErr = s, newTok, nil
	if !n.activeLocked() || fmt.Sprint(old.Receivers) != fmt.Sprint(s.Receivers) {
		n.queue = nil
	}
	if n.o.OnSave != nil {
		n.o.OnSave(account, s, changed)
	}
	n.kick()
	return n.viewLocked(), nil
}

// ViberTest sends a test message to receiver (or every saved receiver)
// with token (or the saved one); ignores enabled and the schedule; one
// test per model.ViberTestInterval.
func (n *Notifier) ViberTest(ctx context.Context, receiver, token string) (model.ViberTestResult, error) {
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
	recv := append([]string{}, n.settings.Receivers...)
	sender := n.settings.SenderName
	n.mu.Unlock()
	if token != "" && !model.ValidViberToken(token) || receiver != "" && !(model.ViberSettings{Receivers: []string{receiver}}).ValidReceivers() {
		return model.ViberTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberInvalid, Message: "viber: invalid token or receiver"}
	}
	if token == "" {
		return model.ViberTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberNoToken, Message: "viber: no token"}
	}
	if receiver != "" {
		recv = []string{receiver}
	}
	if len(recv) == 0 {
		return model.ViberTestResult{}, &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgViberNoReceivers, Message: "viber: no receivers"}
	}
	n.mu.Lock()
	n.lastTest = now
	n.mu.Unlock()
	text := "ExchangeChecker: тестовое сообщение"
	if n.o.Lang == model.LangEN {
		text = "ExchangeChecker: test message"
	}
	res := model.ViberTestResult{Results: []model.ViberTestItem{}}
	for _, r := range recv {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		key, vs, _ := n.send(ctx, token, sender, r, text)
		res.Results = append(res.Results, model.ViberTestItem{Receiver: r, OK: key == "", ErrorKey: key, ViberStatus: vs})
	}
	return res, nil
}
