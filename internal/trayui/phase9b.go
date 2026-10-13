package trayui

import (
	"fmt"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// ---- service clock (CHK-TRAY-23) ----

// ServerClock is the service clock seen by the tray: «N мин назад» and
// the history window count from serverNow of the last snapshot, so a
// skewed local clock does not shift them. Offset = serverNow - local time
// when the snapshot arrived (the IPC delay, well under a second, is
// ignored).
type ServerClock struct {
	Offset time.Duration
	Known  bool
	seq    uint64
}

// Update takes serverNow of a new snapshot (seq changes); a snapshot
// without serverNow (older service) keeps the local clock.
func (c *ServerClock) Update(snap *model.StatusSnapshot, local time.Time) {
	if snap == nil || snap.ServerNow.IsZero() {
		if snap != nil {
			c.Known, c.Offset = false, 0
		}
		return
	}
	if c.Known && snap.Seq == c.seq {
		return
	}
	c.seq, c.Known, c.Offset = snap.Seq, true, snap.ServerNow.Sub(local)
}

// Now is the service time for a local time.
func (c ServerClock) Now(local time.Time) time.Time { return local.Add(c.Offset) }

// ---- check now (CHK-TRAY-22) ----

// CheckWait bounds how long «Проверить сейчас» stays unavailable waiting
// for the status with lastCheckSeq >= checkSeq (a service that never
// reports it does not lock the button forever).
const CheckWait = 2 * time.Minute

// CheckState is the «Проверить сейчас» state: Busy from the request until
// the status that contains the result (check_now) or until the reply
// (poll_now fallback).
type CheckState struct {
	Busy     bool
	Seq      uint64 // check_now checkSeq
	WaitSeq  bool
	Deadline time.Time
}

// Start marks a request as sent.
func (c *CheckState) Start(now time.Time) { *c = CheckState{Busy: true, Deadline: now.Add(CheckWait)} }

// CheckReply is the outcome of a check_now / poll_now reply.
type CheckReply struct {
	Text    string
	Warn    bool
	WaitSeq bool // the button waits for the status (check_now accepted / coalesced)
	Seq     uint64
}

// ParseCheckReply renders a check_now (useCheckNow) or poll_now reply.
func ParseCheckReply(m model.IPCMessage, err error, useCheckNow bool, lang string) CheckReply {
	if err == nil && m.Type == model.MsgError {
		if m.Code == model.ErrCodeRateLimited && m.RetryAfterMs > 0 {
			sec := int((m.RetryAfterMs + 999) / 1000)
			return CheckReply{Text: fmt.Sprintf(i18n.T(i18n.KeyCheckRateLimited, lang), sec), Warn: true}
		}
		text := m.Error
		if m.ErrorKey != "" {
			text = i18n.Message(m.ErrorKey, lang)
		}
		return CheckReply{Text: fmt.Sprintf(i18n.T(i18n.KeyStCheckFailed, lang), text), Warn: true}
	}
	if err != nil {
		return CheckReply{Text: fmt.Sprintf(i18n.T(i18n.KeyStCheckFailed, lang), err), Warn: true}
	}
	if useCheckNow && m.CheckSeq != nil {
		key := i18n.KeyCheckRunning
		if m.Result == model.ResultCheckCoalesced {
			key = i18n.KeyCheckCoalesced
		}
		return CheckReply{Text: i18n.T(key, lang), WaitSeq: true, Seq: *m.CheckSeq}
	}
	return CheckReply{Text: i18n.T(i18n.KeyStCheckQueued, lang)}
}

// Reply applies a reply: poll_now / errors end Busy, check_now waits.
func (c *CheckState) Reply(r CheckReply) {
	if !r.WaitSeq {
		c.Busy = false
		return
	}
	c.WaitSeq, c.Seq = true, r.Seq
}

// Status applies a snapshot (or a tick with snap nil); done = the result
// of the check is in this snapshot (lastCheckSeq >= checkSeq only: this
// is normative; checkSeq is a counter of its own, unrelated to seq),
// timedOut = CheckWait passed (a coalesced check may come with a later poll).
func (c *CheckState) Status(snap *model.StatusSnapshot, now time.Time) (done, timedOut bool) {
	if !c.Busy {
		return false, false
	}
	if c.WaitSeq && snap != nil && snap.LastCheckSeq >= c.Seq {
		c.Busy = false
		return true, false
	}
	if !c.Deadline.IsZero() && !now.Before(c.Deadline) {
		c.Busy = false
		return false, true
	}
	return false, false
}

// ---- balloon debounce (CHK-TRAY-24) ----

// DebounceWindow returns the hold time of a recovery balloon: twice the
// poll interval, at least 2 and at most 10 minutes. A point that flaps
// «просрочка → восстановление → просрочка» changes at most once per poll,
// so two polls without a new overdue confirm the recovery; the bounds keep
// the delay noticeable but short for the default 60 s interval (2 min).
func DebounceWindow(pollIntervalSec int) time.Duration {
	d := 2 * time.Duration(pollIntervalSec) * time.Second
	if d < 2*time.Minute {
		d = 2 * time.Minute
	}
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	return d
}

type pendingRec struct {
	b    Balloon
	due  time.Time
	from model.AlertLevel // level before the recovery
}

// Debouncer holds recovery balloons for Window: an overdue of the same
// point within the window cancels the pending recovery and is not shown
// again unless it is worse than the overdue already shown. Overdue
// balloons of a quiet point are shown at once.
type Debouncer struct {
	Window  time.Duration
	pending map[string]pendingRec
	shown   map[string]model.AlertLevel // last overdue level shown, until a recovery is shown
}

func debKey(ev model.AlertEvent) string {
	return ev.ProfileID + "\x00" + ev.Key + "\x00" + ev.PointName
}

// Feed takes one alert push; it returns the balloons to show now.
func (d *Debouncer) Feed(ev model.AlertEvent, now time.Time, lang string) []Balloon {
	b, ok := BalloonFor(ev, lang)
	if !ok {
		return nil
	}
	if d.pending == nil {
		d.pending, d.shown = map[string]pendingRec{}, map[string]model.AlertLevel{}
	}
	k := debKey(ev)
	if b.Recovered {
		d.pending[k] = pendingRec{b: b, due: now.Add(d.Window), from: ev.From}
		return nil
	}
	if p, ok := d.pending[k]; ok {
		delete(d.pending, k) // flap: the recovery is never shown
		prev := p.from
		if s, ok := d.shown[k]; ok && s > prev {
			prev = s
		}
		if b.Level <= prev {
			d.shown[k] = prev
			return nil
		}
	} else if prev, ok := d.shown[k]; ok && b.Level <= prev {
		return nil
	}
	d.shown[k] = b.Level
	return []Balloon{b}
}

// Due returns the recoveries whose window passed.
func (d *Debouncer) Due(now time.Time) []Balloon {
	var out []Balloon
	for k, p := range d.pending {
		if !now.Before(p.due) {
			out = append(out, p.b)
			delete(d.pending, k)
			delete(d.shown, k)
		}
	}
	return out
}

// NextDue is the earliest pending deadline (ok = false when none).
func (d *Debouncer) NextDue() (time.Time, bool) {
	var t time.Time
	for _, p := range d.pending {
		if t.IsZero() || p.due.Before(t) {
			t = p.due
		}
	}
	return t, !t.IsZero()
}

// ---- events_since catch-up (CHK-TRAY-25) ----

// maxSeen bounds the dedup set (the service ring holds 1000 events).
const maxSeen = 2000

// EventTracker remembers the (runId, eventId) of the alert pushes and
// builds the events_since request after a reconnection.
type EventTracker struct {
	RunID  string
	LastID uint64
	seen   map[string]bool
	order  []string
}

func evKey(run string, id uint64) string { return fmt.Sprintf("%s/%d", run, id) }

// Observe records a pushed event; false = already seen (duplicate).
func (t *EventTracker) Observe(ev model.AlertEvent) bool {
	id := ev.EventID
	if id == 0 {
		id = ev.ID
	}
	if ev.RunID == "" || id == 0 {
		return true // older service: no ids, no dedup
	}
	if t.seen == nil {
		t.seen = map[string]bool{}
	}
	k := evKey(ev.RunID, id)
	if t.seen[k] {
		return false
	}
	t.seen[k] = true
	t.order = append(t.order, k)
	if len(t.order) > maxSeen {
		delete(t.seen, t.order[0])
		t.order = t.order[1:]
	}
	if ev.RunID != t.RunID {
		t.RunID, t.LastID = ev.RunID, 0
	}
	if id > t.LastID {
		t.LastID = id
	}
	return true
}

// Request is the events_since request (ok = false before any run is
// known: get_status already carries the active alerts).
func (t *EventTracker) Request() (model.IPCRequest, bool) {
	if t.RunID == "" {
		return model.IPCRequest{}, false
	}
	return model.IPCRequest{Cmd: model.CmdEventsSince, RunID: t.RunID, SinceID: t.LastID}, true
}

// Apply takes an events reply: the new events (not seen before) in order,
// and more = another request is needed (truncated). resetRequired (new
// run or position lost) moves to the new run without replaying anything.
func (t *EventTracker) Apply(m model.IPCMessage) (fresh []model.AlertEvent, more bool) {
	if m.ResetRequired {
		t.RunID = m.RunID
		if m.LastID != nil {
			t.LastID = *m.LastID
		}
		return nil, false
	}
	for _, ev := range m.Events {
		if ev.RunID == "" {
			ev.RunID = m.RunID
		}
		if t.Observe(ev) {
			fresh = append(fresh, ev)
		}
	}
	if m.RunID != "" && m.RunID != t.RunID {
		t.RunID, t.LastID = m.RunID, 0
	}
	if m.LastID != nil && *m.LastID > t.LastID {
		t.LastID = *m.LastID
	}
	return fresh, m.Truncated && len(m.Events) > 0
}

// SetRun is the runId of a hello heartbeat: another run (service
// restart) keeps the old position so events_since answers resetRequired.
func (t *EventTracker) SetRun(run string) {
	if t.RunID == "" && run != "" {
		t.RunID = run
	}
}
