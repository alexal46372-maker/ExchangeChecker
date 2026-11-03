//go:build windows

package wintray

import (
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// Alert window control IDs.
const (
	alertClass    = "ExchangeCheckerTrayAlert"
	idAlertIcon   = 300
	idAlertText   = 301
	idAlertStatus = 302
	idAlertHint   = 303
	idAlertTitle  = 304
	idAlertAck    = 310
	idAlertMute   = 311
	idAlertClose  = winapi.IDCANCEL
)

// alertWindow is the topmost exchange error window (PLAN §6.3, §12 p.50,
// reworked in phase 10: §12 p.117, CHK-TRAY-03/36). It is created once
// and hidden / shown; only the Minimize and Close boxes (no maximize, no
// hooks, no BlockInput). «Закрыть» and the close box call onClose (mute
// until the next change), «Подтвердить» calls onAck (hidden until the
// next check that still sees the error), «Звук вкл/выкл» calls onMute.
type alertWindow struct {
	hwnd                     uintptr
	icon, text, status, hint uintptr
	ack, close, mute         uintptr
	muted                    bool
	tips                     *tooltips
	level                    model.AlertLevel
	shown                    bool
	raised                   int // bring-to-front requests (tests)
	onAck, onClose           func()
	onMute                   func()
	style                    uint32 // window style (tests: only Minimize / Close)
	laid                     bool   // laid out once: relayouts keep the position
	textStr, statusStr       string // the shown texts (their size, phase 11)
	// title: the blinking title strip at the top (phase 13, item 8,
	// CHK-TRAY-93): bright red / black, white bold text, every
	// alertTitleBlinkMs while the window is shown.
	title      uintptr
	titleRed   bool
	titleBlink bool // the timer runs
	titleFlips int  // phase changes (tests)
}

// Title strip blink (CHK-TRAY-93).
const (
	alertTitleTimer   = 7
	alertTitleBlinkMs = 250
	// AlertTitleRed / AlertTitleBlack / AlertTitleText: COLORREF 0x00BBGGRR.
	AlertTitleRed   = 0x000000FF
	AlertTitleBlack = 0x00000000
	AlertTitleText  = 0x00FFFFFF
)

var alertTitleBrushes [2]uintptr
var alertTitleFont uintptr

// titleBrush is the brush of the current phase (red / black).
func (aw *alertWindow) titleBrush() uintptr {
	i, c := 0, uint32(AlertTitleBlack)
	if aw.titleRed {
		i, c = 1, AlertTitleRed
	}
	if alertTitleBrushes[i] == 0 {
		alertTitleBrushes[i] = winapi.CreateSolidBrush(c)
	}
	return alertTitleBrushes[i]
}

// TitleColor is the current strip color (COLORREF; tests).
func (aw *alertWindow) TitleColor() uint32 {
	if aw.titleRed {
		return AlertTitleRed
	}
	return AlertTitleBlack
}

// titleTick switches the strip phase (WM_TIMER; tests call it directly).
func (aw *alertWindow) titleTick() {
	aw.titleRed = !aw.titleRed
	aw.titleFlips++
	winapi.InvalidateRect(aw.title, true)
}

func (aw *alertWindow) startTitleBlink() {
	if aw.titleBlink {
		return
	}
	aw.titleRed = true
	aw.titleBlink = winapi.SetTimer(aw.hwnd, alertTitleTimer, alertTitleBlinkMs)
	winapi.InvalidateRect(aw.title, true)
}

func (aw *alertWindow) stopTitleBlink() {
	if aw.titleBlink {
		winapi.KillTimer(aw.hwnd, alertTitleTimer)
		aw.titleBlink = false
	}
}

// layout places the controls (phase 11, CHK-TRAY-45): icon | text and
// status, the hint, «Звук» on the left, «Подтвердить» / «Закрыть» right.
// The text and the status are dynamic: fixed boxes; the buttons are as
// wide as their longest text.
func (aw *alertWindow) layout() {
	mute := uiCtl(aw.mute, ui.Button, longest(i18n.T(i18n.KeyAlertMute), i18n.T(i18n.KeyAlertUnmute)))
	title := &ui.Ctl{H: aw.title, Kind: ui.Box, W: 528, Hgt: 36, Grow: true}
	root := &ui.Pad{All: ui.P16, Child: &ui.Col{Gap: ui.P12, Items: []ui.Node{
		title,
		&ui.Row{Align: ui.Top, Gap: ui.P16, Grow: true, Items: []ui.Node{
			&ui.Ctl{H: aw.icon, Kind: ui.Box, W: 32, Hgt: 32},
			&ui.Col{Gap: ui.P8, Items: []ui.Node{
				// phase 11: as tall as the alert text (no hole above the
				// status line); the status only when there is one
				&ui.Ctl{H: aw.text, Kind: ui.Label, W: 448, Text: aw.textStr, Lines: strings.Count(aw.textStr, "\n") + 1, Grow: true},
				&ui.Ctl{H: aw.status, Kind: ui.Label, W: 448, Text: aw.statusStr, Lines: 1, Grow: true, Hidden: aw.statusStr == ""},
			}},
		}},
		uiCtl(aw.hint, ui.Label, ""),
		&ui.Row{Grow: true, Items: []ui.Node{mute, &ui.Row{Right: true, Items: []ui.Node{
			uiCtl(aw.ack, ui.Button, ""), uiCtl(aw.close, ui.Button, "")}}}},
	}}}
	// a relayout (new text) keeps the position
	fitForm(aw.hwnd, &ui.Pad{All: ui.P16, Child: &ui.Col{Gap: ui.P12, Items: root.Child.(*ui.Col).Items[:3]}},
		&ui.Pad{Left: ui.P16, Right: ui.P16, Bottom: ui.P16, Child: root.Child.(*ui.Col).Items[3]}, 0, aw.laid)
	aw.laid = true
}

// longest is the longest of the texts (a control showing either).
func longest(s ...string) string {
	l := ""
	for _, v := range s {
		if len([]rune(v)) > len([]rune(l)) {
			l = v
		}
	}
	return l
}

func newAlertWindow(owner uintptr, onAck, onClose func()) (*alertWindow, error) {
	const w, h = 560, 330
	x := (winapi.GetSystemMetrics(winapi.SM_CXSCREEN) - w) / 2
	y := (winapi.GetSystemMetrics(winapi.SM_CYSCREEN) - h) / 3
	aw := &alertWindow{onAck: onAck, onClose: onClose, level: -1}
	handler := func(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
		switch msg {
		case winapi.WM_TIMER:
			if wp == alertTitleTimer {
				aw.titleTick()
				return 0, true
			}
		case winapi.WM_CTLCOLORSTATIC:
			if lp != 0 && lp == aw.title {
				winapi.SetTextColor(wp, AlertTitleText)
				winapi.SetBkColor(wp, aw.TitleColor())
				return aw.titleBrush(), true
			}
		case winapi.WM_COMMAND:
			switch wp & 0xFFFF {
			case idAlertAck:
				if aw.onAck != nil {
					aw.onAck()
				}
				return 0, true
			case idAlertMute:
				if aw.onMute != nil {
					aw.onMute()
				}
				return 0, true
			case idAlertClose:
				if aw.onClose != nil {
					aw.onClose()
				}
				return 0, true
			}
		case winapi.WM_CLOSE:
			// never destroyed by the user: closing = mute (PLAN §12 p.50)
			if aw.onClose != nil {
				aw.onClose()
			}
			return 0, true
		}
		return 0, false
	}
	aw.style = winapi.WS_OVERLAPPED | winapi.WS_CAPTION | winapi.WS_SYSMENU | winapi.WS_MINIMIZEBOX
	hwnd, err := createWindow(alertClass, i18n.T(i18n.KeyAlertTitleWarning), winapi.WS_EX_DLGMODALFRAME|winapi.WS_EX_TOPMOST,
		aw.style, x, y, w, h, owner, handler)
	if err != nil {
		return nil, err
	}
	aw.hwnd = hwnd
	// phase 11 (CHK-TRAY-45): laid out by the ui framework
	aw.title = child(hwnd, "STATIC", "", winapi.SS_CENTER|winapi.SS_CENTERIMAGE, idAlertTitle, 0, 0, 0, 0)
	if alertTitleFont == 0 {
		alertTitleFont = winapi.CreateFontWeight(uiFontFace, -px(18), winapi.FW_BOLD)
	}
	winapi.SendMessage(aw.title, winapi.WM_SETFONT, alertTitleFont, 1)
	aw.icon = child(hwnd, "STATIC", "", winapi.SS_ICON, idAlertIcon, 0, 0, 0, 0)
	aw.text = child(hwnd, "STATIC", "", winapi.SS_LEFT, idAlertText, 0, 0, 0, 0)
	aw.status = child(hwnd, "STATIC", "", winapi.SS_LEFT, idAlertStatus, 0, 0, 0, 0)
	aw.hint = child(hwnd, "STATIC", i18n.T(i18n.KeyAlertCloseHint), winapi.SS_LEFT, idAlertHint, 0, 0, 0, 0)
	aw.mute = child(hwnd, "BUTTON", i18n.T(i18n.KeyAlertMute), winapi.WS_TABSTOP, idAlertMute, 0, 0, 0, 0)
	aw.ack = child(hwnd, "BUTTON", i18n.T(i18n.KeyAlertAck), winapi.BS_DEFPUSHBUTTON|winapi.WS_TABSTOP, idAlertAck, 0, 0, 0, 0)
	aw.close = child(hwnd, "BUTTON", i18n.T(i18n.KeyAlertClose), winapi.WS_TABSTOP, idAlertClose, 0, 0, 0, 0)
	aw.layout()
	aw.tips = newTooltips(hwnd) // CHK-TRAY-15
	aw.setTips()
	return aw, nil
}

// apply shows the view; show == false keeps the window hidden (tests).
func (aw *alertWindow) apply(v trayalert.View, show bool) {
	if v.Level != aw.level {
		aw.level = v.Level
		icon := uintptr(winapi.IDI_WARNING)
		if v.Level >= model.LevelError {
			icon = winapi.IDI_ERROR
		}
		winapi.SendMessage(aw.icon, winapi.STM_SETICON, winapi.LoadSystemIcon(icon), 0)
	}
	winapi.SetWindowText(aw.hwnd, trayalert.Title(v))
	winapi.SetWindowText(aw.title, trayalert.Title(v))
	text := trayalert.Text(v)
	winapi.SetWindowText(aw.text, text)
	winapi.SetWindowText(aw.status, v.Message)
	if t := strings.ReplaceAll(text, "\r\n", "\n"); t != aw.textStr || v.Message != aw.statusStr {
		aw.textStr, aw.statusStr = t, v.Message
		aw.layout()
	}
	winapi.EnableWindow(aw.ack, v.CanAck)
	aw.muted = v.Muted
	winapi.SetWindowText(aw.mute, aw.muteText())
	switch {
	case v.Visible && (!aw.shown || v.Raise):
		// first show, or a new alert / escalation while already open:
		// to the front again (foreground, else the taskbar flashes)
		aw.shown = true
		aw.raised++
		aw.startTitleBlink()
		if show {
			aw.bringToFront()
		}
	case !v.Visible && aw.shown:
		aw.shown = false
		aw.stopTitleBlink()
		winapi.ShowWindow(aw.hwnd, winapi.SW_HIDE)
	}
}

// bringToFront: topmost + foreground with the Win7-compatible APIs; when
// the foreground lock refuses, the taskbar button flashes.
func (aw *alertWindow) bringToFront() {
	if winapi.IsIconic(aw.hwnd) {
		winapi.ShowWindow(aw.hwnd, winapi.SW_RESTORE)
	}
	winapi.ShowWindow(aw.hwnd, winapi.SW_SHOWNORMAL)
	winapi.SetWindowPos(aw.hwnd, winapi.HWND_TOPMOST, 0, 0, 0, 0, winapi.SWP_NOMOVE|winapi.SWP_NOSIZE|winapi.SWP_SHOWWINDOW)
	winapi.SetForegroundWindow(aw.hwnd)
	if winapi.GetForegroundWindow() != aw.hwnd {
		winapi.FlashWindow(aw.hwnd)
	}
}

// setTips sets the button tooltips .
func (aw *alertWindow) setTips() {
	aw.tips.set(aw.ack, i18n.T(i18n.KeyTipAlertAck))
	aw.tips.set(aw.close, i18n.T(i18n.KeyTipAlertClose))
	aw.tips.set(aw.mute, i18n.T(i18n.KeyTipAlertMute))
}

// muteText: the button names the action («Звук выкл» while it sounds).
func (aw *alertWindow) muteText() string {
	if aw.muted {
		return i18n.T(i18n.KeyAlertUnmute)
	}
	return i18n.T(i18n.KeyAlertMute)
}
