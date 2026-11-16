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
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
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
	ps := trayalert.Problems(a.status, a.statusExt)
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
// Phase 16 A (CHK-TRAY-92): the frame of the panel by the clock blinks
// with the icon in the same tick — its colour ↔ black.
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
	if a.panel != nil && a.panel.shown && winapi.IsWindow(a.panel.hwnd) {
		a.panel.frameRepaints++
		eraseRepaint(a.panel.hwnd) // the frame in the new phase, the text over it
	}
}

// panelWindow is the topmost problem panel by the clock: no caption, no
// close button, never activated; it hides itself when the list is empty.
// Phase 16 A: a speech bubble (window region: rounded body + tail to the
// tray icon) with a thin frame in the colour of the worst problem.
type panelWindow struct {
	hwnd, text uintptr
	lines      []string
	shown      bool
	cloud      trayui.Cloud // the placed bubble (window region)
	// frameRepaints: blink-phase repaints of the frame (tests)
	frameRepaints int
}

// Panel is the panel (nil before the first problem; test hook).
func (a *App) Panel() *panelWindow { return a.panel }

// PanelFrameColor is the current frame colour (COLORREF; tests): the worst
// problem (red error / yellow warning), black in the «off» blink phase.
func (a *App) PanelFrameColor() uint32 {
	return trayui.PanelFrameColor(a.blink == trayalert.BlinkRed, a.blinkOff && a.blink != trayalert.BlinkNone)
}

func (a *App) updatePanel() {
	lines := trayalert.PanelLines(a.problems)
	if len(lines) == 0 {
		if a.panel != nil && a.panel.shown {
			a.panel.shown = false
			winapi.ShowWindow(a.panel.hwnd, winapi.SW_HIDE)
		}
		return
	}
	if a.panel == nil || !winapi.IsWindow(a.panel.hwnd) {
		p := &panelWindow{}
		h, err := createWindow(panelClass, i18n.T(i18n.KeyPanelTitle),
			winapi.WS_EX_TOPMOST|winapi.WS_EX_TOOLWINDOW|winapi.WS_EX_NOACTIVATE,
			winapi.WS_POPUP, 0, 0, panelWidth, 40, a.hwnd,
			func(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
				switch msg {
				case winapi.WM_ERASEBKGND: // CHK-TRAY-92: the bubble and its frame
					themeErase(wp, hw)
					paintPanelFrame(wp, p.cloud, a.PanelFrameColor())
					return 1, true
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
		p.text = child(h, "STATIC", "", winapi.SS_LEFT|ssNotify, idPanelText, 0, 0, 0, 0)
		a.panel = p
	}
	p := a.panel
	p.lines = lines
	winapi.SetWindowText(p.text, strings.Join(lines, "\r\n"))
	// phase 11 (CHK-TRAY-45): the text measured by the framework, at
	// least panelWidth wide, one line per problem
	text := uiCtl(p.text, ui.Label, strings.Join(lines, "\n"))
	text.W = panelWidth - 18
	b := int32(trayui.PanelBorder) // logical; newForm scales the pad
	f := newForm(&ui.Pad{Left: ui.P8 + b, Right: ui.P8 + b, Top: 6 + b, Bottom: 6 + b, Child: text}, 0)
	// phase 16 A: the body is the laid-out form; the bubble adds the tail
	c := a.panelCloud(f.W, f.H)
	if text.Placed && (c.Body.L != 0 || c.Body.T != 0) { // tail above / left: shift the text into the body
		winapi.SetWindowPos(p.text, 0, text.Rect.X+c.Body.L, text.Rect.Y+c.Body.T, text.Rect.W, text.Rect.H, winapi.SWP_NOZORDER|winapi.SWP_NOACTIVATE)
	}
	winapi.SetWindowPos(p.hwnd, winapi.HWND_TOPMOST, c.Win.L, c.Win.T, c.Win.W(), c.Win.H(), winapi.SWP_NOACTIVATE)
	if c != p.cloud {
		p.cloud = c
		winapi.SetWindowRgn(p.hwnd, cloudRegion(c), true) // the system owns the region
	}
	winapi.InvalidateRect(p.hwnd, true) // the frame follows the new size
	if !p.shown {
		p.shown = true
		if !a.opt.HiddenPanel && !a.opt.HiddenAlerts {
			winapi.ShowWindow(p.hwnd, winapi.SW_SHOWNOACTIVATE)
		}
	}
}

// Thin frame of the panel by the clock (phase 13 item 4, phase 16 A,
// CHK-TRAY-92): trayui.PanelBorder logical pixels (DPI-scaled) along the
// inside of the bubble region; the panel still has no close button.
const panelBorder = trayui.PanelBorder

// PanelFrameRGB is the error frame colour, PanelFrameYellow the warning
// one (COLORREF 0x00BBGGRR); the frame blinks to black with the icon.
const (
	PanelFrameRGB    = trayui.PanelFrameRed
	PanelFrameYellow = trayui.PanelFrameYellow
)

var panelFrameBrushes = map[uint32]uintptr{}

func panelBorderPx() int32 { return px(panelBorder) }

// panelMetrics are the DPI-scaled bubble sizes.
func panelMetrics() trayui.CloudMetrics {
	return trayui.CloudMetrics{Tail: px(trayui.PanelTail), TailHalf: px(trayui.PanelTailHalf),
		Radius: px(trayui.PanelRadius), Gap: px(trayui.PanelGap)}
}

// cloudRegion builds the bubble region in window coordinates: the rounded
// body (CreateRoundRectRgn, right / bottom exclusive, so +1) OR the tail
// triangle (CreatePolygonRgn).
func cloudRegion(c trayui.Cloud) uintptr {
	d := 2 * c.Radius
	body := winapi.CreateRoundRectRgn(c.Body.L, c.Body.T, c.Body.R+1, c.Body.B+1, d, d)
	pts := make([]winapi.POINT, 3)
	for i, q := range c.Tail {
		pts[i] = winapi.POINT{X: q.X, Y: q.Y}
	}
	tail := winapi.CreatePolygonRgn(pts)
	if body == 0 || tail == 0 {
		if tail != 0 {
			winapi.DeleteObject(tail)
		}
		return body
	}
	winapi.CombineRgn(body, body, tail, winapi.RGN_OR)
	winapi.DeleteObject(tail)
	return body
}

// paintPanelFrame draws the frame along the inside of the bubble region
// (FrameRgn, panelBorderPx wide) in colour col.
func paintPanelFrame(dc uintptr, c trayui.Cloud, col uint32) {
	br := panelFrameBrushes[col]
	if br == 0 {
		br = winapi.CreateSolidBrush(col)
		panelFrameBrushes[col] = br
	}
	rgn := cloudRegion(c)
	if rgn == 0 {
		return
	}
	b := panelBorderPx()
	winapi.FrameRgn(dc, rgn, br, b, b)
	winapi.DeleteObject(rgn)
}

// panelCloud places the bubble of a w × h body by the tray icon (its
// rectangle when known, Windows 7+), else at the bottom-right of the work
// area (phase 16 A: the tail touches the icon, nothing is cut off).
func (a *App) panelCloud(w, h int32) trayui.Cloud {
	work, ok := winapi.WorkArea()
	if !ok || work.Right <= work.Left {
		work = winapi.RECT{Right: winapi.GetSystemMetrics(winapi.SM_CXSCREEN), Bottom: winapi.GetSystemMetrics(winapi.SM_CYSCREEN)}
	}
	var icon winapi.RECT
	known := false
	if a.nidAdded {
		icon, known = winapi.NotifyIconRect(a.hwnd, trayID)
	}
	return trayui.PanelCloud(w, h, trayui.CloudRect{L: icon.Left, T: icon.Top, R: icon.Right, B: icon.Bottom}, known,
		trayui.CloudRect{L: work.Left, T: work.Top, R: work.Right, B: work.Bottom}, panelMetrics())
}

// panelPos: above (or below, for a top taskbar) the tray icon when its
// rectangle is known (Windows 7+), else the bottom-right of the work area
// (the volume window; the panel itself is placed by panelCloud).
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
func PinHelpText() string { return i18n.T(i18n.KeyPinHelpText) }

// ShowPinHelp shows the note (Options.Notify in tests).
func (a *App) ShowPinHelp() {
	txt := PinHelpText()
	if a.opt.Notify != nil {
		a.opt.Notify(txt, false)
		return
	}
	winapi.MessageBox(a.hwnd, txt, i18n.T(i18n.KeyPinHelpTitle), winapi.MB_ICONINFORMATION)
}
