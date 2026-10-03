package trayalert

import (
	"errors"
	"sort"

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
	settings  *model.Settings

	shown    map[ID]model.AlertEvent // unacknowledged active alerts shown
	suppress map[ID]model.AlertLevel // acknowledged by us, until the snapshot agrees

	visible, sound, pending bool
	msg                     string
}

// New returns a controller for the UI language lang.
func New(lang string) *Controller {
	return &Controller{lang: i18n.Normalize(lang), shown: map[ID]model.AlertEvent{}, suppress: map[ID]model.AlertLevel{}}
}

// Update applies a session status. ackable = the session supports
// ack_alert (trayipc.Session.Supported). Without a live snapshot nothing
// changes (the last alerts stay; no new ones can be known).
func (c *Controller) Update(st trayipc.Status, ackable bool) View {
	c.connected = st.State == trayipc.StateConnected
	c.ackable = ackable
	snap := st.Snapshot
	if !c.connected || snap == nil {
		return c.View()
	}
	if snap.Settings != nil {
		s := *snap.Settings
		c.settings = &s
	}
	cur := map[ID]model.AlertEvent{}
	for _, a := range snap.Alerts {
		if a.To <= model.LevelOK || a.Acknowledged {
			continue
		}
		cur[idOf(a)] = a
	}
	// our own acks hide an alert until the snapshot shows it acknowledged
	// or gone; a level change ends the suppression (new status)
	for id, lvl := range c.suppress {
		if a, ok := cur[id]; !ok || a.To != lvl {
			delete(c.suppress, id)
		} else {
			delete(cur, id)
		}
	}
	changed := false
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
		c.visible, c.sound, c.msg = true, true, ""
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
	whole := map[string]bool{}
	for id := range c.shown {
		if id.Key == "" {
			whole[id.ProfileID] = true
		}
	}
	byProfile := map[string]*AckRequest{}
	var reqs []AckRequest
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
					c.suppress[id] = a.To
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
	muted := c.settings != nil && !c.settings.Beep.BeepEnabled()
	v := View{Visible: c.visible && len(c.shown) > 0, Sound: c.sound && len(c.shown) > 0 && !muted,
		Alerts: c.sorted(), CanAck: c.canAck(), Pending: c.pending, Message: c.msg}
	for _, a := range v.Alerts {
		if a.To > v.Level {
			v.Level = a.To
		}
	}
	v.Tone = ToneFor(v.Level, c.settings)
	return v
}
