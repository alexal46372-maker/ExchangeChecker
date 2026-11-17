//go:build windows

package wintray

import (
	"context"
	"strconv"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
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
// Only errors flash: pushes with level warning are ignored (a warning push
// for a flashing alert ends its flash). The overlay is kept below the
// alert window and the panel by the clock (both stay readable).
// «Подтвердить» (ack_alert) stops the local flashing of the acknowledged
// alerts; the service keeps pushing flash on every failed check, and the
// next push turns it on again. Phase 16: the alert window has no
// «Закрыть». Phase 17 (CHK-TRAY-109): minimizing the alert window pauses
// the flash at once (dismiss_flash {eventId} for every active flash), any
// restore resumes it (resume_flash {eventId}; the service pushes flash
// again while the alert is active and not confirmed); a confirmed alert is
// never resumed; a new alert restores the window. Only with the service
// capability resume_flash — an older service keeps flashing while
// minimized. Turning «Мигание экрана» off stops it at once.

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
	changed, restore := false, false
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
			if lvl != model.LevelError { // only errors flash the screen
				if _, ok := a.flashes[k]; ok {
					delete(a.flashes, k)
					changed = true
				}
				continue
			}
			a.resumedPush(m.EventID)
			if a.flashMin {
				if a.flashPausedID(m.EventID) {
					continue // paused: a push that crossed the dismiss
				}
				// a new alert while minimized: the window comes back and
				// the flash resumes (CHK-TRAY-109)
				a.flashes[k] = flashState{eventID: m.EventID, level: lvl}
				changed = true
				restore = true
				continue
			}
			a.flashes[k] = flashState{eventID: m.EventID, level: lvl}
			changed = true
		case e.Kind == trayipc.EventState && e.Status.State != trayipc.StateConnected && len(a.flashes) > 0:
			// the service is gone: it resends on its first failed check
			a.flashes = nil
			a.flashResuming = nil
			changed = true
		}
	}
	if restore {
		a.restoreAlertWindow()
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
	winapi.SetWindowPos(fw.hwnd, a.flashInsertAfter(), r.Left, r.Top, r.Right-r.Left, r.Bottom-r.Top,
		winapi.SWP_NOACTIVATE|winapi.SWP_SHOWWINDOW)
}

// flashInsertAfter is the z-order place of the overlay: right below the
// lower of the shown alert window and panel (both topmost), so they stay
// above it; HWND_TOPMOST when neither is shown.
func (a *App) flashInsertAfter() uintptr {
	after := winapi.HWND_TOPMOST
	for _, h := range a.flashKeepAbove() {
		if after == winapi.HWND_TOPMOST || winapi.IsAbove(after, h) {
			after = h
		}
	}
	return after
}

// flashKeepAbove: the shown windows the overlay must not cover.
func (a *App) flashKeepAbove() []uintptr {
	var out []uintptr
	if a.awin != nil && winapi.IsWindow(a.awin.hwnd) && winapi.IsWindowVisible(a.awin.hwnd) {
		out = append(out, a.awin.hwnd)
	}
	if a.panel != nil && winapi.IsWindow(a.panel.hwnd) && winapi.IsWindowVisible(a.panel.hwnd) {
		out = append(out, a.panel.hwnd)
	}
	return out
}

// pauseFlashes: «Подтвердить» — the local flashing of the acknowledged
// alerts stops (by eventId and by profile / point); the next flash push
// of the service (the alert is still failing) turns it on again.
func (a *App) pauseFlashes(reqs []trayalert.AckRequest) {
	a.forgetConfirmed(reqs) // CHK-TRAY-109: never resumed
	if len(a.flashes) == 0 {
		return
	}
	n := len(a.flashes)
	for _, r := range reqs {
		for k, f := range a.flashes {
			if r.Req.EventID != 0 && f.eventID == r.Req.EventID {
				delete(a.flashes, k)
			}
		}
		for _, id := range r.IDs {
			delete(a.flashes, flashKey{id.ProfileID, id.Key})
		}
	}
	if len(a.flashes) != n {
		a.updateFlash()
	}
}

// Phase 17 (CHK-TRAY-109): the flash follows the alert window state.

// FlashPaused reports whether the flash is paused by a minimized alert
// window and how many flashes wait for the restore (tests).
func (a *App) FlashPaused() (bool, int) { return a.flashMin, len(a.flashPaused) }

// FlashRequests are the dismiss_flash / resume_flash requests sent, as
// "dismiss:<id>" / "resume:<id>", in the order they went out (tests).
func (a *App) FlashRequests() []string {
	a.flashMu.Lock()
	defer a.flashMu.Unlock()
	return append([]string(nil), a.flashSent...)
}

// FlashSending reports whether a dismiss_flash / resume_flash worker is
// still running (tests).
func (a *App) FlashSending() bool {
	a.flashMu.Lock()
	defer a.flashMu.Unlock()
	return len(a.flashBusy) != 0
}

// canPauseFlash: the service announces flash and resume_flash.
func (a *App) canPauseFlash() bool {
	s := a.session
	if s == nil {
		return false
	}
	st := s.Status()
	return st.State == trayipc.StateConnected && st.Has(model.CapFlash) && st.Has(model.CapResumeFlash)
}

func (a *App) flashPausedID(id uint64) bool {
	if id == 0 {
		return false
	}
	for _, f := range a.flashPaused {
		if f.eventID == id {
			return true
		}
	}
	return false
}

// alertMinimized: the alert window was minimized — the screen flash stops
// at once and dismiss_flash goes to the service for every active flash.
// Without resume_flash (older service) nothing changes.
func (a *App) alertMinimized() {
	if a.flashMin || !a.canPauseFlash() {
		return
	}
	a.flashMin = true
	if a.flashPaused == nil {
		a.flashPaused = map[flashKey]flashState{}
	}
	var ids []uint64
	for k, f := range a.flashes {
		a.flashPaused[k] = f
		if f.eventID != 0 {
			ids = append(ids, f.eventID)
		}
	}
	// restored a moment ago and the resume_flash not answered by a flash
	// push yet: paused again (the dismiss follows it in order)
	a.flashMu.Lock()
	for k, f := range a.flashResuming {
		if _, ok := a.flashPaused[k]; !ok && !a.flashNoResume[f.eventID] {
			a.flashPaused[k] = f
			ids = append(ids, f.eventID)
		}
	}
	a.flashNoResume = nil
	a.flashMu.Unlock()
	a.flashResuming = nil
	a.flashes = nil
	a.updateFlash()
	a.sendFlashRequests(model.CmdDismissFlash, "dismiss:", ids)
}

// alertRestored: the alert window is back — resume_flash for every paused
// flash that was not confirmed meanwhile; the service pushes flash again
// (the alert is still active and not confirmed) and the overlay restarts
// on that push. A paused flash without eventId restarts locally.
func (a *App) alertRestored() {
	if !a.flashMin {
		return
	}
	a.flashMin = false
	paused := a.flashPaused
	a.flashPaused = nil
	var ids []uint64
	local := false
	for k, f := range paused {
		switch {
		case f.eventID == 0:
			if a.flashes == nil {
				a.flashes = map[flashKey]flashState{}
			}
			a.flashes[k] = f
			local = true
		case a.flashAcked[f.eventID]:
		default:
			ids = append(ids, f.eventID)
			if a.flashResuming == nil {
				a.flashResuming = map[flashKey]flashState{}
			}
			a.flashResuming[k] = f
		}
	}
	if local {
		a.updateFlash()
	}
	a.sendFlashRequests(model.CmdResumeFlash, "resume:", ids)
}

// resumedPush: the flash push of a resumed event arrived — it is no longer
// waiting for its resume_flash.
func (a *App) resumedPush(id uint64) {
	if id == 0 {
		return
	}
	for k, f := range a.flashResuming {
		if f.eventID == id {
			delete(a.flashResuming, k)
		}
	}
}

// restoreAlertWindow: a new alert while the window is minimized restores
// it (and with it the flash, alertRestored via WM_SIZE).
func (a *App) restoreAlertWindow() {
	if a.awin != nil && winapi.IsWindow(a.awin.hwnd) && winapi.IsIconic(a.awin.hwnd) {
		winapi.ShowWindow(a.awin.hwnd, winapi.SW_RESTORE)
	}
	a.alertRestored() // no-op when WM_SIZE already resumed
}

// forgetConfirmed: «Подтвердить» — the confirmed alerts leave the paused
// set and are never resumed.
func (a *App) forgetConfirmed(reqs []trayalert.AckRequest) {
	for _, r := range reqs {
		if id := r.Req.EventID; id != 0 {
			if a.flashAcked == nil {
				a.flashAcked = map[uint64]bool{}
			}
			a.flashAcked[id] = true
		}
		for k, f := range a.flashPaused {
			if r.Req.EventID != 0 && f.eventID == r.Req.EventID {
				delete(a.flashPaused, k)
			}
		}
		for k, f := range a.flashResuming {
			if r.Req.EventID != 0 && f.eventID == r.Req.EventID {
				delete(a.flashResuming, k)
			}
		}
		for _, id := range r.IDs {
			if f, ok := a.flashPaused[flashKey{id.ProfileID, id.Key}]; ok {
				if f.eventID != 0 {
					if a.flashAcked == nil {
						a.flashAcked = map[uint64]bool{}
					}
					a.flashAcked[f.eventID] = true
				}
				delete(a.flashPaused, flashKey{id.ProfileID, id.Key})
			}
			delete(a.flashResuming, flashKey{id.ProfileID, id.Key})
		}
	}
}

// sendFlashRequests asks for the state cmd (dismiss_flash = paused,
// resume_flash = flashing) of every event in ids, off the UI thread.
// Per eventId the requests are serialized by one worker goroutine and
// coalesced latest-wins: while a request of an event is in flight, a
// newer wanted state replaces the pending one (intermediate states are
// skipped) and goes out only after the reply, so a resume_flash never
// overtakes a dismiss_flash still under way (the service would mark the
// restored flash dismissed again); a pending state equal to the one just
// delivered by the same worker is dropped (minimize→restore→minimize
// during a slow dismiss sends one dismiss_flash). Order is never changed;
// the worker ends when nothing is pending. CHK-TRAY-109.
func (a *App) sendFlashRequests(cmd, tag string, ids []uint64) {
	s := a.session
	if s == nil || len(ids) == 0 {
		return
	}
	a.flashMu.Lock()
	defer a.flashMu.Unlock()
	if a.flashWant == nil {
		a.flashWant = map[uint64]flashReq{}
		a.flashLast = map[uint64]string{}
		a.flashBusy = map[uint64]bool{}
	}
	for _, id := range ids {
		a.flashWant[id] = flashReq{cmd, tag}
		if !a.flashBusy[id] {
			a.flashBusy[id] = true
			go a.flashWorker(s, id)
		}
	}
}

// flashReq is the latest wanted state of one event (sendFlashRequests).
type flashReq struct{ cmd, tag string }

// flashWorker delivers the wanted states of one eventId one at a time.
func (a *App) flashWorker(s *trayipc.Session, id uint64) {
	for {
		a.flashMu.Lock()
		r, ok := a.flashWant[id]
		delete(a.flashWant, id)
		if !ok {
			delete(a.flashBusy, id)
			delete(a.flashLast, id) // only within one burst: no stale state later
			a.flashMu.Unlock()
			return
		}
		if a.flashLast[id] == r.cmd {
			a.flashMu.Unlock()
			continue // already the service's state
		}
		a.flashSent = append(a.flashSent, r.tag+strconv.FormatUint(id, 10))
		a.flashMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), ackTimeout)
		m, err := s.Do(ctx, model.IPCRequest{Cmd: r.cmd, EventID: id})
		cancel()
		a.flashMu.Lock()
		if err == nil && m.Type == model.MsgOK {
			a.flashLast[id] = r.cmd
		} else {
			delete(a.flashLast, id) // unknown: the next wanted state goes out
		}
		if r.cmd == model.CmdResumeFlash {
			// not resumed (confirmed / cleared / failed): no flash push will
			// come, a later minimize must not pause it again
			if err != nil || m.Resumed == nil || !*m.Resumed {
				if a.flashNoResume == nil {
					a.flashNoResume = map[uint64]bool{}
				}
				a.flashNoResume[id] = true
			} else {
				delete(a.flashNoResume, id)
			}
		}
		a.flashMu.Unlock()
	}
}
