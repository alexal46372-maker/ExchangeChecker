//go:build windows

package wintray

import (
	"context"
	"fmt"
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
	if a.opt.BalloonStore != nil {
		a.balloonsOn, _ = a.opt.BalloonStore.Load()
	}
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
	if !a.balloonsOn {
		return
	}
	var bs []trayui.Balloon
	for _, e := range q {
		if e.Kind != trayipc.EventAlert || e.Alert == nil {
			continue
		}
		if b, ok := trayui.BalloonFor(*e.Alert, a.opt.Lang); ok {
			bs = append(bs, b)
		}
	}
	b, ok := trayui.CombineBalloons(bs, a.opt.Lang)
	if !ok {
		return
	}
	a.balloons = append(a.balloons, b)
	a.showBalloon(b)
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
		a.statusWin = sw
	}
	a.statusWin.update(a.status, a.connected())
	if !a.opt.HiddenStatus {
		a.statusWin.show()
	}
	return a.statusWin
}

// CheckNow sends poll_now for all databases (CHK-TRAY-20) off the UI
// thread; the result goes to the status window (or a tray message).
func (a *App) CheckNow() {
	s, lang := a.session, a.opt.Lang
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), pollNowTimeout)
		m, err := s.Do(ctx, model.IPCRequest{Cmd: model.CmdPollNow})
		cancel()
		text, warn := CheckNowText(m, err, lang)
		a.runOnUI(func() {
			a.checkResults = append(a.checkResults, text)
			if sw := a.statusWin; sw != nil && winapi.IsWindow(sw.hwnd) && winapi.IsWindowVisible(sw.hwnd) {
				sw.setInfo(text)
				return
			}
			if warn {
				a.notify(text, true)
			}
		})
	}()
}

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
