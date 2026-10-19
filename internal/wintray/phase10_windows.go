//go:build windows

package wintray

import (
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Phase 10 C (CHK-TRAY-37..39): the blinking icon, the problem panel by
// the clock and the «Как закрепить значок» note.

const (
	blinkTimerID = 10
	// blinkHalfMs: ~1 Hz (on 500 ms, off 500 ms)
	blinkHalfMs = 500

	panelClass  = "ExchangeCheckerTrayPanel"
	idPanelText = 400
	panelWidth  = 380
	panelLineH  = 16
)

// SetStatusExt applies the optional phase10-svc status fields (raw
// status JSON parsed with trayalert.ParseStatusExt; missing = derived
// from the snapshot as before).
func (a *App) SetStatusExt(x trayalert.StatusExt) {
	a.statusExt = x
	a.updateProblems()
}

// OnAlertRepeat applies an optional alert_repeat event (capability
// alert_repeat): acknowledged alarms of the profile are shown again by the
// next status.
func (a *App) OnAlertRepeat(r trayalert.AlertRepeat) {
	a.alerts.Repeat(r.ProfileID)
	a.applyAlert(a.alerts.Update(a.status, a.session.Supported(model.CmdAckAlert)))
}

// Problems are the current problems (UI thread).
func (a *App) Problems() []trayalert.Problem { return a.problems }

// Blink is the current blink color.
func (a *App) Blink() trayalert.Blink { return a.blink }

// shownIcon is the icon of this blink phase.
func (a *App) shownIcon() trayui.Icon {
	switch {
	case a.blink == trayalert.BlinkNone:
		return a.ind.Icon
	case a.blinkOff:
		return trayui.IconBlank
	case a.blink == trayalert.BlinkRed:
		return trayui.IconError
	}
	return trayui.IconWarning
}

// ShownIcon is the icon currently in the tray (test hook).
func (a *App) ShownIcon() trayui.Icon { return a.shownIcon() }

// Blinks counts blink phase changes (test hook).
func (a *App) Blinks() int { return a.blinks }

// updateProblems recomputes the list; blinking and the panel follow at
// once (the panel disappears immediately on recovery, decision 5).
func (a *App) updateProblems() {
	ps := trayalert.Problems(a.status, a.statusExt, a.opt.Lang)
	if a.status.State == trayipc.StateConnecting && len(a.problems) > 0 && a.problems[0].Kind == trayalert.ProblemNoConnection {
		ps = a.problems // a reconnect attempt: no flicker of the panel
	}
	a.problems = ps
	b := trayalert.BlinkFor(a.problems)
	if b != a.blink {
		a.blink = b
		a.blinkOff = false
		if b == trayalert.BlinkNone {
			a.stopBlink()
		} else {
			a.startBlink()
		}
		if a.nidAdded {
			a.modifyIcon()
		}
	}
	a.updatePanel()
}

func (a *App) startBlink() {
	if a.blinkTimer || a.opt.NoBlink {
		return
	}
	a.blinkTimer = winapi.SetTimer(a.hwnd, blinkTimerID, blinkHalfMs)
}

func (a *App) stopBlink() {
	if a.blinkTimer {
		winapi.KillTimer(a.hwnd, blinkTimerID)
		a.blinkTimer = false
	}
	a.blinkOff = false
}

// blinkTick switches the blink phase (WM_TIMER; tests call it directly).
func (a *App) blinkTick() {
	if a.blink == trayalert.BlinkNone {
		a.stopBlink()
		return
	}
	a.blinkOff = !a.blinkOff
	a.blinks++
	if a.nidAdded {
		a.modifyIcon()
	}
}

// panelWindow is the topmost problem panel by the clock: no caption, no
// close button, never activated; it hides itself when the list is empty.
type panelWindow struct {
	hwnd, text uintptr
	lines      []string
	shown      bool
}

// Panel is the panel (nil before the first problem; test hook).
func (a *App) Panel() *panelWindow { return a.panel }

func (a *App) updatePanel() {
	lines := trayalert.PanelLines(a.problems, a.opt.Lang)
	if len(lines) == 0 {
		if a.panel != nil && a.panel.shown {
			a.panel.shown = false
			winapi.ShowWindow(a.panel.hwnd, winapi.SW_HIDE)
		}
		return
	}
	if a.panel == nil || !winapi.IsWindow(a.panel.hwnd) {
		p := &panelWindow{}
		h, err := createWindow(panelClass, i18n.T(i18n.KeyPanelTitle, a.opt.Lang),
			winapi.WS_EX_TOPMOST|winapi.WS_EX_TOOLWINDOW|winapi.WS_EX_NOACTIVATE,
			winapi.WS_POPUP|winapi.WS_BORDER, 0, 0, panelWidth, 40, a.hwnd,
			func(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
				switch msg {
				case winapi.WM_LBUTTONUP:
					a.OpenStatus()
					return 0, true
				case winapi.WM_COMMAND: // STN_CLICKED of the text
					if wp&0xFFFF == idPanelText {
						a.OpenStatus()
						return 0, true
					}
				case winapi.WM_CLOSE: // no close: it disappears on recovery
					return 0, true
				}
				return 0, false
			})
		if err != nil {
			return
		}
		p.hwnd = h
		p.text = child(h, "STATIC", "", winapi.SS_LEFT|ssNotify, idPanelText, 8, 6, panelWidth-18, 20)
		a.panel = p
	}
	p := a.panel
	p.lines = lines
	winapi.SetWindowText(p.text, strings.Join(lines, "\r\n"))
	hgt := int32(len(lines)*panelLineH + 14)
	winapi.MoveWindow(p.text, 8, 6, panelWidth-18, hgt-12)
	x, y := a.panelPos(panelWidth, hgt)
	winapi.SetWindowPos(p.hwnd, winapi.HWND_TOPMOST, x, y, panelWidth, hgt, winapi.SWP_NOACTIVATE)
	if !p.shown {
		p.shown = true
		if !a.opt.HiddenPanel && !a.opt.HiddenAlerts {
			winapi.ShowWindow(p.hwnd, winapi.SW_SHOWNOACTIVATE)
		}
	}
}

// panelPos: above (or below, for a top taskbar) the tray icon when its
// rectangle is known (Windows 7+), else the bottom-right of the work area.
func (a *App) panelPos(w, h int32) (int32, int32) {
	work, ok := winapi.WorkArea()
	if !ok || work.Right <= work.Left {
		work = winapi.RECT{Right: winapi.GetSystemMetrics(winapi.SM_CXSCREEN), Bottom: winapi.GetSystemMetrics(winapi.SM_CYSCREEN)}
	}
	x, y := work.Right-w-8, work.Bottom-h-8
	if a.nidAdded {
		if r, ok := winapi.NotifyIconRect(a.hwnd, trayID); ok {
			x = r.Left + (r.Right-r.Left)/2 - w/2
			if r.Top >= work.Bottom-4 || r.Top > (work.Top+work.Bottom)/2 {
				y = work.Bottom - h - 8
			} else {
				y = work.Top + 8
			}
		}
	}
	if x+w > work.Right {
		x = work.Right - w
	}
	if x < work.Left {
		x = work.Left
	}
	return x, y
}

// PinHelpText is the «Как закрепить значок» note.
func PinHelpText(lang string) string { return i18n.T(i18n.KeyPinHelpText, lang) }

// ShowPinHelp shows the note (Options.Notify in tests).
func (a *App) ShowPinHelp() {
	txt := PinHelpText(a.opt.Lang)
	if a.opt.Notify != nil {
		a.opt.Notify(txt, false)
		return
	}
	winapi.MessageBox(a.hwnd, txt, i18n.T(i18n.KeyPinHelpTitle, a.opt.Lang), winapi.MB_ICONINFORMATION)
}
