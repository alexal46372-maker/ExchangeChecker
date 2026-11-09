//go:build windows

package wintray

import (
	"context"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Phase 13 item 7 (CHK-TRAY-95): full-screen flashing on an exchange
// error. The service (capability flash) pushes {type:flash, profileId,
// pointKey, eventId, level} on every check while the alert lasts and
// {type:flash_stop, profileId, pointKey} when it ends (recovery, mute,
// disable, delete). While any flash is active and «Мигание экрана» is on
// (HKCU, default on), a black topmost layered window over all monitors
// blinks; it is click-through and never activated (WS_EX_NOACTIVATE |
// WS_EX_TRANSPARENT | WS_EX_TOOLWINDOW), so it steals no focus.
// «Подтвердить» (ack_alert) does not stop it; «Закрыть» of the alert
// window sends dismiss_flash {eventId} for the active flashes.

// ScreenFlashValue is the HKCU REG_DWORD of «Мигание экрана» (1 = on,
// missing = on).
const ScreenFlashValue = "ScreenFlash"

// ScreenFlashStore keeps the per-user «Мигание экрана» switch.
type ScreenFlashStore interface {
	Load() (on bool, err error)
	Save(on bool) error
}

// RegistryScreenFlashStore stores the switch in HKCU\TrayKey\ScreenFlash.
type RegistryScreenFlashStore struct {
	Key string // "" = TrayKey
}

func (s RegistryScreenFlashStore) key() string {
	if s.Key == "" {
		return TrayKey
	}
	return s.Key
}

// Load implements ScreenFlashStore (missing = on).
func (s RegistryScreenFlashStore) Load() (bool, error) {
	v, ok, err := winapi.RegGetDWORDView(winapi.HKEY_CURRENT_USER, s.key(), ScreenFlashValue, 0)
	if err != nil || !ok {
		return true, err
	}
	return v != 0, nil
}

// Save implements ScreenFlashStore.
func (s RegistryScreenFlashStore) Save(on bool) error {
	var v uint32
	if on {
		v = 1
	}
	return winapi.RegSetDWORDView(winapi.HKEY_CURRENT_USER, s.key(), ScreenFlashValue, v, 0)
}

const (
	flashClass   = "ExchangeCheckerTrayFlash"
	flashTimerID = 11
	// FlashHalfMs: one blink phase (on / off).
	FlashHalfMs = 300
	// FlashAlpha: opacity of the black overlay in the "on" phase.
	FlashAlpha = 230
	// FlashExStyle: topmost, click-through, never activated, no taskbar.
	FlashExStyle = winapi.WS_EX_TOPMOST | winapi.WS_EX_TOOLWINDOW | winapi.WS_EX_NOACTIVATE |
		winapi.WS_EX_TRANSPARENT | winapi.WS_EX_LAYERED
)

type flashKey struct{ profile, point string }

type flashState struct {
	eventID uint64
	level   model.AlertLevel
}

// flashWindow is the overlay.
type flashWindow struct {
	hwnd   uintptr
	on     bool // the black phase is shown
	timer  bool
	phases int // phase changes (tests)
}

var flashBlack uintptr

// FlashActive reports the active flashes (tests).
func (a *App) FlashActive() int { return len(a.flashes) }

// Flash is the overlay window (nil before the first flash; tests).
func (a *App) Flash() *flashWindow { return a.flashWin }

// screenFlashOn reads «Мигание экрана» (nil store = on).
func (a *App) screenFlashOn() bool {
	if a.opt.ScreenFlashStore == nil {
		return true
	}
	on, err := a.opt.ScreenFlashStore.Load()
	if err != nil {
		return true
	}
	return on
}

// onFlashEvents applies flash / flash_stop pushes of a drain.
func (a *App) onFlashEvents(q []trayipc.Event) {
	changed := false
	for _, e := range q {
		switch {
		case e.Kind == trayipc.EventFlash && e.Message != nil:
			m := e.Message
			k := flashKey{m.ProfileID, m.PointKey}
			if m.Type == model.MsgFlashStop {
				if _, ok := a.flashes[k]; ok {
					delete(a.flashes, k)
					changed = true
				}
				continue
			}
			if a.flashes == nil {
				a.flashes = map[flashKey]flashState{}
			}
			lvl := model.LevelError
			if m.Level != nil {
				lvl = *m.Level
			}
			a.flashes[k] = flashState{eventID: m.EventID, level: lvl}
			changed = true
		case e.Kind == trayipc.EventState && e.Status.State != trayipc.StateConnected && len(a.flashes) > 0:
			// the service is gone: it resends on its first failed check
			a.flashes = nil
			changed = true
		}
	}
	if changed {
		a.updateFlash()
	}
}

// updateFlash starts / stops the overlay.
func (a *App) updateFlash() {
	if len(a.flashes) > 0 && a.screenFlashOn() {
		a.startFlash()
		return
	}
	a.stopFlash()
}

func (a *App) startFlash() {
	if a.flashWin == nil || !winapi.IsWindow(a.flashWin.hwnd) {
		fw := &flashWindow{}
		r := winapi.VirtualScreen()
		// pixel rectangle of all monitors as is: createWindow would treat
		// it as a 96-dpi client size, scale it and clamp it to the work
		// area with scroll bars (the overlay missed the taskbar strip)
		h, err := createWindowPx(flashClass, "", FlashExStyle, winapi.WS_POPUP, r.Left, r.Top, r.Right-r.Left, r.Bottom-r.Top, 0,
			func(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
				switch msg {
				case winapi.WM_ERASEBKGND:
					if flashBlack == 0 {
						flashBlack = winapi.CreateSolidBrush(0)
					}
					winapi.FillRect(wp, winapi.GetClientRect(hw), flashBlack)
					return 1, true
				case winapi.WM_TIMER:
					if wp == flashTimerID {
						a.flashTick()
						return 0, true
					}
				case winapi.WM_MOUSEACTIVATE:
					return winapi.MA_NOACTIVATE, true
				}
				return 0, false
			})
		if err != nil {
			a.logf("tray: flash window: %v", err)
			return
		}
		winapi.SetLayeredWindowAttributes(h, FlashAlpha)
		fw.hwnd = h
		a.flashWin = fw
	}
	fw := a.flashWin
	if fw.timer {
		return
	}
	fw.timer = winapi.SetTimer(fw.hwnd, flashTimerID, FlashHalfMs)
	fw.on = false
	a.flashTick() // the first black phase at once
}

func (a *App) stopFlash() {
	fw := a.flashWin
	if fw == nil {
		return
	}
	if fw.timer {
		winapi.KillTimer(fw.hwnd, flashTimerID)
		fw.timer = false
	}
	if fw.on {
		fw.on = false
		winapi.ShowWindow(fw.hwnd, winapi.SW_HIDE)
	}
}

// flashTick switches the blink phase (WM_TIMER; tests call it directly).
func (a *App) flashTick() {
	fw := a.flashWin
	if fw == nil {
		return
	}
	fw.on = !fw.on
	fw.phases++
	if !fw.on {
		winapi.ShowWindow(fw.hwnd, winapi.SW_HIDE)
		return
	}
	if a.opt.HiddenFlash {
		return
	}
	r := winapi.VirtualScreen() // monitors may change while it blinks
	winapi.SetWindowPos(fw.hwnd, winapi.HWND_TOPMOST, r.Left, r.Top, r.Right-r.Left, r.Bottom-r.Top,
		winapi.SWP_NOACTIVATE|winapi.SWP_SHOWWINDOW)
}

// dismissFlashes: «Закрыть» of the alert window — dismiss_flash {eventId}
// for every active flash (the service suppresses it until recovery); the
// overlay stops at once.
func (a *App) dismissFlashes() {
	if len(a.flashes) == 0 {
		return
	}
	var ids []uint64
	for _, f := range a.flashes {
		if f.eventID != 0 {
			ids = append(ids, f.eventID)
		}
	}
	a.flashes = nil
	a.stopFlash()
	s := a.session
	if s == nil || !s.Status().Has(model.CapFlash) {
		return
	}
	go func() {
		for _, id := range ids {
			ctx, cancel := context.WithTimeout(context.Background(), ackTimeout)
			_, _ = s.Do(ctx, model.IPCRequest{Cmd: model.CmdDismissFlash, EventID: id})
			cancel()
		}
	}()
}
