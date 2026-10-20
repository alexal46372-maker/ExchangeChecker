//go:build windows

package wintray

import (
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
	lang                     string
	onAck, onClose           func()
	onMute                   func()
	style                    uint32 // window style (tests: only Minimize / Close)
	form                     *ui.Form
}

// layout places the controls (phase 11, CHK-TRAY-45): icon | text and
// status, the hint, «Звук» on the left, «Подтвердить» / «Закрыть» right.
// The text and the status are dynamic: fixed boxes; the buttons are as
// wide as their longest text in this language.
func (aw *alertWindow) layout() {
	mute := uiCtl(aw.mute, ui.Button, longest(i18n.T(i18n.KeyAlertMute, aw.lang), i18n.T(i18n.KeyAlertUnmute, aw.lang)))
	root := &ui.Pad{All: ui.P16, Child: &ui.Col{Gap: ui.P12, Items: []ui.Node{
		&ui.Row{Align: ui.Top, Gap: ui.P16, Grow: true, Items: []ui.Node{
			&ui.Ctl{H: aw.icon, Kind: ui.Box, W: 32, Hgt: 32},
			&ui.Col{Gap: ui.P8, Items: []ui.Node{
				&ui.Ctl{H: aw.text, Kind: ui.Box, W: 448, Hgt: 160, Grow: true},
				&ui.Ctl{H: aw.status, Kind: ui.Box, W: 448, Hgt: 20, Grow: true},
			}},
		}},
		uiCtl(aw.hint, ui.Label, ""),
		&ui.Row{Grow: true, Items: []ui.Node{mute, &ui.Row{Right: true, Items: []ui.Node{
			uiCtl(aw.ack, ui.Button, ""), uiCtl(aw.close, ui.Button, "")}}}},
	}}}
	aw.form = newForm(root, 0)
	fitToContent(aw.hwnd, aw.form.W, aw.form.H)
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

func newAlertWindow(owner uintptr, lang string, onAck, onClose func()) (*alertWindow, error) {
	const w, h = 560, 330
	x := (winapi.GetSystemMetrics(winapi.SM_CXSCREEN) - w) / 2
	y := (winapi.GetSystemMetrics(winapi.SM_CYSCREEN) - h) / 3
	aw := &alertWindow{lang: lang, onAck: onAck, onClose: onClose, level: -1}
	handler := func(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
		switch msg {
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
	hwnd, err := createWindow(alertClass, i18n.T(i18n.KeyAlertTitleWarning, lang), winapi.WS_EX_DLGMODALFRAME|winapi.WS_EX_TOPMOST,
		aw.style, x, y, w, h, owner, handler)
	if err != nil {
		return nil, err
	}
	aw.hwnd = hwnd
	// phase 11 (CHK-TRAY-45): laid out by the ui framework
	aw.icon = child(hwnd, "STATIC", "", winapi.SS_ICON, idAlertIcon, 0, 0, 0, 0)
	aw.text = child(hwnd, "STATIC", "", winapi.SS_LEFT, idAlertText, 0, 0, 0, 0)
	aw.status = child(hwnd, "STATIC", "", winapi.SS_LEFT, idAlertStatus, 0, 0, 0, 0)
	aw.hint = child(hwnd, "STATIC", i18n.T(i18n.KeyAlertCloseHint, lang), winapi.SS_LEFT, idAlertHint, 0, 0, 0, 0)
	aw.mute = child(hwnd, "BUTTON", i18n.T(i18n.KeyAlertMute, lang), winapi.WS_TABSTOP, idAlertMute, 0, 0, 0, 0)
	aw.ack = child(hwnd, "BUTTON", i18n.T(i18n.KeyAlertAck, lang), winapi.BS_DEFPUSHBUTTON|winapi.WS_TABSTOP, idAlertAck, 0, 0, 0, 0)
	aw.close = child(hwnd, "BUTTON", i18n.T(i18n.KeyAlertClose, lang), winapi.WS_TABSTOP, idAlertClose, 0, 0, 0, 0)
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
	winapi.SetWindowText(aw.hwnd, trayalert.Title(v, aw.lang))
	winapi.SetWindowText(aw.text, trayalert.Text(v, aw.lang))
	winapi.SetWindowText(aw.status, v.Message)
	winapi.EnableWindow(aw.ack, v.CanAck)
	aw.muted = v.Muted
	winapi.SetWindowText(aw.mute, aw.muteText())
	switch {
	case v.Visible && (!aw.shown || v.Raise):
		// first show, or a new alert / escalation while already open:
		// to the front again (foreground, else the taskbar flashes)
		aw.shown = true
		aw.raised++
		if show {
			aw.bringToFront()
		}
	case !v.Visible && aw.shown:
		aw.shown = false
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

// setLang switches the texts of the window to lang (the view texts follow
// with the next apply).
func (aw *alertWindow) setLang(lang string) {
	aw.lang = lang
	winapi.SetWindowText(aw.hint, i18n.T(i18n.KeyAlertCloseHint, lang))
	winapi.SetWindowText(aw.ack, i18n.T(i18n.KeyAlertAck, lang))
	winapi.SetWindowText(aw.close, i18n.T(i18n.KeyAlertClose, lang))
	winapi.SetWindowText(aw.mute, aw.muteText())
	aw.setTips()
	aw.layout() // texts of the other language
}

// setTips sets the button tooltips in the current language.
func (aw *alertWindow) setTips() {
	aw.tips.set(aw.ack, i18n.T(i18n.KeyTipAlertAck, aw.lang))
	aw.tips.set(aw.close, i18n.T(i18n.KeyTipAlertClose, aw.lang))
	aw.tips.set(aw.mute, i18n.T(i18n.KeyTipAlertMute, aw.lang))
}

// muteText: the button names the action («Звук выкл» while it sounds).
func (aw *alertWindow) muteText() string {
	if aw.muted {
		return i18n.T(i18n.KeyAlertUnmute, aw.lang)
	}
	return i18n.T(i18n.KeyAlertMute, aw.lang)
}
