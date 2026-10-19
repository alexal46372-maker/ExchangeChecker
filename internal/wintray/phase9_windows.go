//go:build windows

package wintray

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// ssNotify (SS_NOTIFY) lets a STATIC receive mouse messages (tooltips).
const ssNotify = 0x0100

// BalloonsValue is the HKCU REG_DWORD of the balloon switch (1 = on,
// missing = on; phase 9a, CHK-TRAY-18). A per-user tray preference like
// the language: settings.json belongs to the service and is admin-only.
const BalloonsValue = "Balloons"

// BalloonStore keeps the per-user balloon switch.
type BalloonStore interface {
	Load() (on bool, err error)
	Save(on bool) error
}

// RegistryBalloonStore stores the switch in HKCU\Key\Balloons.
type RegistryBalloonStore struct {
	Key string // "" = LangKey
}

func (s RegistryBalloonStore) key() string {
	if s.Key == "" {
		return LangKey
	}
	return s.Key
}

// Load implements BalloonStore (missing = on).
func (s RegistryBalloonStore) Load() (bool, error) {
	v, ok, err := winapi.RegGetDWORDView(winapi.HKEY_CURRENT_USER, s.key(), BalloonsValue, 0)
	if err != nil || !ok {
		return true, err
	}
	return v != 0, nil
}

// Save implements BalloonStore.
func (s RegistryBalloonStore) Save(on bool) error {
	var v uint32
	if on {
		v = 1
	}
	return winapi.RegSetDWORDView(winapi.HKEY_CURRENT_USER, s.key(), BalloonsValue, v, 0)
}

// pollNowTimeout bounds one poll_now request.
const pollNowTimeout = 10 * time.Second

func (a *App) loadBalloonPref() {
	a.balloonsOn = true
	if a.opt.BalloonStore == nil {
		return
	}
	on, err := a.opt.BalloonStore.Load()
	if err != nil {
		// a missing key / value is not an error (Load returns on, nil);
		// anything else is logged and the default (on) is kept
		a.logf("tray: balloon switch: read HKCU %s\\%s: %v (using on)", LangKey, BalloonsValue, err)
		on = true
	}
	a.balloonsOn = on
}

// logf writes a line to the tray log (Options.Log; nil = package log).
func (a *App) logf(format string, args ...interface{}) {
	if a.opt.Log != nil {
		a.opt.Log(fmt.Sprintf(format, args...))
		return
	}
	log.Printf(format, args...)
}

// BalloonsEnabled / SetBalloons: the switch of the right menu.
func (a *App) BalloonsEnabled() bool { return a.balloonsOn }
func (a *App) SetBalloons(on bool) error {
	a.balloonsOn = on
	if a.opt.BalloonStore != nil {
		return a.opt.BalloonStore.Save(on)
	}
	return nil
}

// Balloons are the balloons shown so far (test hook).
func (a *App) Balloons() []trayui.Balloon { return a.balloons }

// balloonEvents shows one balloon for the point alert pushes of a drain.
func (a *App) balloonEvents(q []trayipc.Event) {
	var evs []model.AlertEvent
	for _, e := range q {
		if e.Kind != trayipc.EventAlert || e.Alert == nil {
			continue
		}
		if e.Alert.Kind == model.AlertKindRepeat {
			// never through Observe: repeat ids are not in the events_since
			// ring, a LastID on them would make the service answer
			// resetRequired
			continue
		}
		if !a.tracker.Observe(*e.Alert) { // (runId, eventId) seen: duplicate push
			continue
		}
		evs = append(evs, *e.Alert)
	}
	a.feedBalloons(evs)
}

// feedBalloons passes alert events through the debouncer (CHK-TRAY-24)
// and shows one balloon for what is due now.
func (a *App) feedBalloons(evs []model.AlertEvent) {
	now := time.Now()
	a.debounce.Window = a.debounceWindow()
	var bs []trayui.Balloon
	for _, ev := range evs {
		if ev.Kind == model.AlertKindRepeat { // a repeat is not a transition
			continue
		}
		bs = append(bs, a.debounce.Feed(ev, now, a.opt.Lang)...)
	}
	bs = append(bs, a.debounce.Due(now)...)
	a.showBalloons(bs)
	a.scheduleDue()
}

func (a *App) showBalloons(bs []trayui.Balloon) {
	if !a.balloonsOn {
		return
	}
	b, ok := trayui.CombineBalloons(bs, a.opt.Lang)
	if !ok {
		return
	}
	a.balloons = append(a.balloons, b)
	a.showBalloon(b)
}

// debounceWindow: Options.BalloonDebounce (tests; < 0 = none), else twice
// the poll interval of the heartbeat (trayui.DebounceWindow).
func (a *App) debounceWindow() time.Duration {
	switch {
	case a.opt.BalloonDebounce < 0:
		return 0
	case a.opt.BalloonDebounce > 0:
		return a.opt.BalloonDebounce
	}
	poll := 60
	if hb := a.status.Heartbeat; hb != nil && hb.PollIntervalSec > 0 {
		poll = hb.PollIntervalSec
	}
	return trayui.DebounceWindow(poll)
}

// scheduleDue arms one timer for the earliest held recovery.
func (a *App) scheduleDue() {
	t, ok := a.debounce.NextDue()
	if !ok {
		return
	}
	if a.dueTimer != nil {
		a.dueTimer.Stop()
	}
	a.dueTimer = time.AfterFunc(time.Until(t)+10*time.Millisecond, func() {
		a.runOnUI(func() {
			a.showBalloons(a.debounce.Due(time.Now()))
			a.scheduleDue()
		})
	})
}

func (a *App) showBalloon(b trayui.Balloon) {
	if !a.nidAdded {
		return
	}
	n := a.nid()
	n.Flags |= winapi.NIF_INFO
	copy(n.InfoTitle[:len(n.InfoTitle)-1], truncU16(b.Title, len(n.InfoTitle)-1))
	copy(n.Info[:len(n.Info)-1], truncU16(b.Text, len(n.Info)-1))
	switch b.Level {
	case model.LevelError:
		n.InfoFlags = winapi.NIIF_ERROR
	case model.LevelWarning:
		n.InfoFlags = winapi.NIIF_WARNING
	default:
		n.InfoFlags = winapi.NIIF_INFO
	}
	n.InfoFlags |= winapi.NIIF_NOSOUND
	winapi.ShellNotifyIcon(winapi.NIM_MODIFY, &n)
}

func (a *App) connected() bool { return a.status.State == trayipc.StateConnected }

// OpenStatus shows the «Состояние» window (created once, then hidden /
// shown) and returns it.
func (a *App) OpenStatus() *statusWindow {
	if a.statusWin == nil || !winapi.IsWindow(a.statusWin.hwnd) {
		sw, err := newStatusWindow(a.opt.Lang, false, a.CheckNow)
		if err != nil {
			return nil
		}
		sw.now = a.serverNow
		sw.canHistory = a.canHistory
		sw.onHistory = func(r trayui.StatusRow) { a.OpenHistory(r) }
		a.statusWin = sw
	}
	a.statusWin.update(a.status, a.connected() && !a.check.Busy)
	if !a.opt.HiddenStatus {
		a.statusWin.show()
	}
	return a.statusWin
}

// CheckNow asks the service for an immediate poll of all databases
// (CHK-TRAY-20 / CHK-TRAY-22): check_now when announced (the button and
// the menu item stay unavailable until the status that contains the
// result: lastCheckSeq >= checkSeq, at most trayui.CheckWait), else
// poll_now (unavailable until the reply).
func (a *App) CheckNow() {
	if a.check.Busy {
		return
	}
	s, lang := a.session, a.opt.Lang
	useCheck := s.Supported(model.CmdCheckNow)
	a.check.Start(time.Now())
	a.updateCheckUI()
	cmd := model.CmdPollNow
	if useCheck {
		cmd = model.CmdCheckNow
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), pollNowTimeout)
		m, err := s.Do(ctx, model.IPCRequest{Cmd: cmd})
		cancel()
		r := trayui.ParseCheckReply(m, err, useCheck, lang)
		a.runOnUI(func() {
			a.check.Reply(r)
			a.checkResult(r.Text, r.Warn)
			if a.check.Busy {
				// the result may already be in the current snapshot
				a.checkStatus(a.status.Snapshot)
				time.AfterFunc(trayui.CheckWait+50*time.Millisecond, func() {
					a.runOnUI(func() { a.checkStatus(nil) })
				})
			}
			a.updateCheckUI()
		})
	}()
}

// checkStatus ends the wait of check_now on the status with the result.
func (a *App) checkStatus(snap *model.StatusSnapshot) {
	done, timedOut := a.check.Status(snap, time.Now())
	switch {
	case done:
		a.checkResult(i18n.T(i18n.KeyCheckDone, a.opt.Lang), false)
	case timedOut:
		a.checkResult(i18n.T(i18n.KeyCheckTimeout, a.opt.Lang), false)
	default:
		return
	}
	a.updateCheckUI()
}

func (a *App) checkResult(text string, warn bool) {
	a.checkResults = append(a.checkResults, text)
	if sw := a.statusWin; sw != nil && winapi.IsWindow(sw.hwnd) {
		sw.setInfo(text)
		if winapi.IsWindowVisible(sw.hwnd) {
			return
		}
	}
	if warn {
		a.notify(text, true)
	}
}

func (a *App) updateCheckUI() {
	if sw := a.statusWin; sw != nil && winapi.IsWindow(sw.hwnd) {
		winapi.EnableWindow(sw.check, a.connected() && !a.check.Busy)
	}
}

// CheckBusy: «Проверить сейчас» is waiting (tests).
func (a *App) CheckBusy() bool { return a.check.Busy }

// CheckNowText is the outcome of a poll_now reply.
func CheckNowText(m model.IPCMessage, err error, lang string) (string, bool) {
	if err == nil && m.Type == model.MsgError {
		err = fmt.Errorf("%s", m.Error)
		if m.ErrorKey != "" {
			err = fmt.Errorf("%s", i18n.Message(m.ErrorKey, lang))
		}
	}
	if err != nil {
		return fmt.Sprintf(i18n.T(i18n.KeyStCheckFailed, lang), err), true
	}
	return i18n.T(i18n.KeyStCheckQueued, lang), false
}

// truncU16 is s as UTF-16 cut to max units without splitting a surrogate
// pair ("…" marks the cut).
func truncU16(s string, max int) []uint16 {
	u, _ := utf16From(s)
	if len(u) <= max {
		return u
	}
	u = u[:max-1]
	if c := u[len(u)-1]; c >= 0xD800 && c < 0xDC00 {
		u = u[:len(u)-1]
	}
	return append(u, 0x2026)
}
