package trayalert

import (
	"errors"
	"sort"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

// ID identifies an alert: the profile and the point key ("" = an alert of
// the database itself, e.g. connection error).
type ID struct {
	ProfileID string
	Key       string
}

func idOf(e model.AlertEvent) ID { return ID{e.ProfileID, e.Key} }

// View is what the UI shows.
type View struct {
	Visible bool
	Sound   bool
	Tone    Tone
	// Level is the worst shown level (window style, tone).
	Level  model.AlertLevel
	Alerts []model.AlertEvent
	// CanAck: «Подтвердить» is enabled (service supports ack_alert, is
	// connected, nothing in flight).
	CanAck  bool
	Pending bool
	// Message is the info / error line under the list ("" = none).
	Message string
	// Raise: this update brought a new alert or a worse level — the UI
	// brings the window to the front even when it is already visible
	// (one-shot, only in the View returned by Update).
	Raise bool
	// Muted: «Звук выкл» of the window for the current alarm (phase 10).
	Muted bool
}

// AckRequest is one ack_alert request and the alerts it covers.
type AckRequest struct {
	Req model.IPCRequest
	IDs []ID
}

// AckResult is the reply to an AckRequest (Err from trayipc.Session.Do).
type AckResult struct {
	Req AckRequest
	Msg model.IPCMessage
	Err error
}

// Controller is the alert state machine. Not safe for concurrent use (the
// UI thread owns it).
type Controller struct {
	lang      string
	connected bool
	ackable   bool // service supports ack_alert
	ackByEvent bool // capability ack_event: ack by eventId
	settings  *model.Settings

	shown map[ID]model.AlertEvent // unacknowledged active alerts shown
	// acked: acknowledged alerts (by us or by the service), hidden until a
	// later check of the profile still sees them (PLAN §12 p.117,
	// CHK-TRAY-36) or the level changes
	acked   map[ID]ackMark
	checked map[string]time.Time // profile → checkedAt of the last snapshot
	// muted: «Звук выкл» of the current alarm (until the next Raise)
	muted bool

	visible, sound, pending bool
	reshown                 bool
	repeat                  map[string]bool // alert_repeat since the last Update
	// Policy decides when an acknowledged alert is shown again.
	Policy AckPolicy
	msg    string
}

// SetLang switches the UI language of the texts (tray menu choice).
func (c *Controller) SetLang(lang string) { c.lang = i18n.Normalize(lang) }

// New returns a controller for the UI language lang.
func New(lang string) *Controller {
	return &Controller{Policy: CheckedAtPolicy{}, lang: i18n.Normalize(lang), shown: map[ID]model.AlertEvent{},
		acked: map[ID]ackMark{}, checked: map[string]time.Time{}}
}

// Update applies a session status. ackable = the session supports
// ack_alert (trayipc.Session.Supported). Without a live snapshot nothing
// changes (the last alerts stay; no new ones can be known).
func (c *Controller) Update(st trayipc.Status, ackable bool) View {
	c.connected = st.State == trayipc.StateConnected
	c.ackable = ackable
	c.ackByEvent = false
	for _, cap := range st.Capabilities {
		if cap == model.CapAckEvent {
			c.ackByEvent = true
			break
		}
	}
	snap := st.Snapshot
	if !c.connected || snap == nil {
		return c.View()
	}
	if snap.Settings != nil {
		s := *snap.Settings
		c.settings = &s
	}
	for _, p := range snap.Profiles {
		c.checked[p.ProfileID] = p.CheckedAt
	}
	cur := map[ID]model.AlertEvent{}
	for _, a := range snap.Alerts {
		if a.To <= model.LevelOK || a.Test {
			continue // test alarms are push-only, not in status problems
		}
		id := idOf(a)
		m, ours := c.acked[id]
		if ours && m.level != a.To {
			ours = false // a level change is a new alarm
			delete(c.acked, id)
		}
		if a.Acknowledged && !ours {
			m, ours = ackMark{level: a.To}, true
			if a.AckedAt != nil {
				m.at = *a.AckedAt
			}
			c.acked[id] = m
		}
		if ours {
			if !c.repeat[a.ProfileID] && !c.Policy.Reshow(a, m.at, c.checked[a.ProfileID]) {
				continue
			}
			// a later check still sees it: shown again, and the mark is
			// dropped so a new «Подтвердить» is needed
			delete(c.acked, id)
			if old, ok := c.shown[id]; !ok || old.To != a.To {
				c.reshown = true
			}
			a.Acknowledged = false
		}
		cur[id] = a
	}
	for id := range c.acked {
		found := false
		for _, a := range snap.Alerts {
			if idOf(a) == id && a.To > model.LevelOK {
				found = true
				break
			}
		}
		if !found {
			delete(c.acked, id)
		}
	}
	changed := c.reshown
	c.reshown, c.repeat = false, nil
	for id, a := range cur {
		if old, ok := c.shown[id]; !ok || a.To > old.To {
			changed = true
		}
	}
	c.shown = cur
	switch {
	case len(cur) == 0:
		c.visible, c.sound, c.msg = false, false, ""
	case changed:
		c.visible, c.sound, c.msg, c.muted = true, true, "", false
	}
	v := c.View()
	v.Raise = changed && len(cur) > 0
	return v
}

// Close is the user closing the window: hide and mute until the next
// change; the alerts stay unacknowledged (PLAN §12 p.50).
func (c *Controller) Close() View {
	c.visible, c.sound = false, false
	return c.View()
}

// BeginAck returns the ack_alert requests for the shown alerts (nil when
// acknowledging is not possible now) and marks them in flight. A profile
// with an alert of the database itself is acknowledged as a whole (no
// pointKey: the service acknowledges all its alerts); other alerts per
// point.
func (c *Controller) BeginAck() []AckRequest {
	if !c.canAck() || len(c.shown) == 0 {
		return nil
	}
	var reqs []AckRequest
	if c.ackByEvent {
		for _, a := range c.sorted() {
			if a.Test {
				continue
			}
			eid := a.EventID
			if eid == 0 {
				eid = a.ID
			}
			if eid == 0 {
				continue
			}
			reqs = append(reqs, AckRequest{Req: model.IPCRequest{Cmd: model.CmdAckAlert, EventID: eid}, IDs: []ID{idOf(a)}})
		}
	} else {
		whole := map[string]bool{}
		for id := range c.shown {
			if id.Key == "" {
				whole[id.ProfileID] = true
			}
		}
		byProfile := map[string]*AckRequest{}
		var points []AckRequest
		for _, a := range c.sorted() {
			id := idOf(a)
			if whole[id.ProfileID] {
				r := byProfile[id.ProfileID]
				if r == nil {
					r = &AckRequest{Req: model.IPCRequest{Cmd: model.CmdAckAlert, ProfileID: id.ProfileID}}
					byProfile[id.ProfileID] = r
				}
				r.IDs = append(r.IDs, id)
				continue
			}
			points = append(points, AckRequest{Req: model.IPCRequest{Cmd: model.CmdAckAlert, ProfileID: id.ProfileID, PointKey: id.Key}, IDs: []ID{id}})
		}
		ps := make([]string, 0, len(byProfile))
		for p := range byProfile {
			ps = append(ps, p)
		}
		sort.Strings(ps)
		for _, p := range ps {
			reqs = append(reqs, *byProfile[p])
		}
		reqs = append(reqs, points...)
	}
	if len(reqs) == 0 {
		return nil
	}
	c.pending = true
	c.msg = i18n.T(i18n.KeyAlertAckPending, c.lang)
	return reqs
}

// AckDone applies the replies; refresh reports that the status should be
// re-read (get_status) because the service changed or already had
// changed the alerts.
func (c *Controller) AckDone(results []AckResult) (v View, refresh bool) {
	c.pending = false
	c.msg = ""
	var fail string
	gone := false
	for _, r := range results {
		resolved := false
		switch {
		case r.Err == nil && r.Msg.Type == model.MsgOK:
			resolved = true
		case r.Err == nil && r.Msg.Type == model.MsgError &&
			(r.Msg.Code == model.ErrCodeNotFound || r.Msg.Code == model.ErrCodeNoActiveAlert):
			resolved, gone = true, true
		case errors.Is(r.Err, trayipc.ErrUnsupported):
			c.ackable = false
			fail = i18n.T(i18n.KeyAlertAckUnsupported, c.lang)
		case r.Err != nil:
			fail = failText(c.lang, errText(r.Err, c.lang))
		case r.Msg.Type == model.MsgError:
			txt := r.Msg.Error
			if r.Msg.ErrorKey != "" {
				txt = model.FormatMessage(r.Msg.ErrorKey, r.Msg.ErrorArgs, c.lang)
				if txt == "" {
					txt = i18n.Message(r.Msg.ErrorKey, c.lang)
				}
			}
			fail = failText(c.lang, txt)
		default:
			fail = failText(c.lang, r.Msg.Type)
		}
		if resolved {
			refresh = true
			for _, id := range r.Req.IDs {
				if a, ok := c.shown[id]; ok {
					c.acked[id] = ackMark{level: a.To, at: c.checked[id.ProfileID]}
					delete(c.shown, id)
				}
			}
		}
	}
	switch {
	case fail != "":
		c.msg = fail
	case gone && len(c.shown) > 0:
		c.msg = i18n.T(i18n.KeyAlertAckGone, c.lang)
	}
	if len(c.shown) == 0 {
		c.visible, c.sound = false, false
		c.msg = ""
	}
	return c.View(), refresh
}

func failText(lang, s string) string {
	return sprintf(i18n.T(i18n.KeyAlertAckFailed, lang), s)
}

func errText(err error, lang string) string {
	switch {
	case errors.Is(err, trayipc.ErrNotConnected), errors.Is(err, trayipc.ErrServiceNotRunning):
		return i18n.T(i18n.KeySvcUnavailable, lang)
	}
	return err.Error()
}

func (c *Controller) canAck() bool { return c.connected && c.ackable && !c.pending }

func (c *Controller) sorted() []model.AlertEvent {
	out := make([]model.AlertEvent, 0, len(c.shown))
	for _, a := range c.shown {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.To != b.To {
			return a.To > b.To
		}
		if a.ProfileName != b.ProfileName {
			return a.ProfileName < b.ProfileName
		}
		if a.ProfileID != b.ProfileID {
			return a.ProfileID < b.ProfileID
		}
		if (a.Key == "") != (b.Key == "") {
			return a.Key == ""
		}
		return a.PointName < b.PointName || (a.PointName == b.PointName && a.Key < b.Key)
	})
	return out
}

// View is the current view. beep.enabled = false (PLAN §12 p.65) mutes the
// sound only; the window is shown as usual.
func (c *Controller) View() View {
	muted := c.muted || (c.settings != nil && !c.settings.Beep.BeepEnabled())
	v := View{Visible: c.visible && len(c.shown) > 0, Sound: c.sound && len(c.shown) > 0 && !muted,
		Alerts: c.sorted(), CanAck: c.canAck(), Pending: c.pending, Message: c.msg, Muted: c.muted}
	for _, a := range v.Alerts {
		if a.To > v.Level {
			v.Level = a.To
		}
	}
	v.Tone = ToneFor(v.Level, c.settings)
	return v
}

type ackMark struct {
	level model.AlertLevel
	// at: the profile checkedAt when acknowledged (ours) or AckedAt
	at time.Time
}

// AckPolicy decides when an acknowledged alert is shown again. Today:
// CheckedAtPolicy (a later check of the profile, from status); with the
// service alert_repeat event (phase10-svc) RepeatEvent can drive it
// instead — one place to switch (Кончита's decision 4).
type AckPolicy interface {
	// Reshow: a is active and acknowledged at ackAt; checkedAt is the
	// profile's last check from the current snapshot.
	Reshow(a model.AlertEvent, ackAt, checkedAt time.Time) bool
}

// CheckedAtPolicy reshows when the profile was checked after the ack.
// Unknown times (an older service) keep the old behaviour: hidden until
// the level changes.
type CheckedAtPolicy struct{}

// Reshow implements AckPolicy.
func (CheckedAtPolicy) Reshow(a model.AlertEvent, ackAt, checkedAt time.Time) bool {
	return !ackAt.IsZero() && !checkedAt.IsZero() && checkedAt.After(ackAt)
}

// Repeat is the alert_repeat event (phase10-svc, capability
// alert_repeat, optional): the check checkSeq of profileID still sees the
// alarm. The next Update shows its acknowledged alerts again. The service also sends it for acknowledged alarms; the
// tray decides here.
func (c *Controller) Repeat(profileID string) {
	if c.repeat == nil {
		c.repeat = map[string]bool{}
	}
	c.repeat[profileID] = true
}

// ToggleMute is «Звук вкл/выкл» of the window: mutes or unmutes the
// current alarm only (the next new alarm or escalation sounds again).
func (c *Controller) ToggleMute() View {
	c.muted = !c.muted
	return c.View()
}

// Muted reports the window's mute toggle.
func (c *Controller) Muted() bool { return c.muted }

// PushTest shows a test alarm (alarm_test push, CHK-TRAY-53). Not acked.
func (c *Controller) PushTest(a model.AlertEvent) View {
	if !a.Test {
		return c.View()
	}
	id := idOf(a)
	if id.ProfileID == "" {
		id.ProfileID = model.AlarmTestProfileID
		a.ProfileID = id.ProfileID
	}
	c.shown[id] = a
	c.visible, c.sound, c.msg, c.muted = true, true, "", false
	v := c.View()
	v.Raise = true
	return v
}
