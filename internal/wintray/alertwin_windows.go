//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Alert window control IDs.
const (
	alertClass    = "ExchangeCheckerTrayAlert"
	idAlertIcon   = 300
	idAlertText   = 301
	idAlertStatus = 302
	idAlertHint   = 303
	idAlertAck    = 310
	idAlertClose  = winapi.IDCANCEL
)

// alertWindow is the topmost alert window (PLAN §6.3, §12 p.50). It is
// created once and hidden / shown; «Закрыть» and the close box only
// call onClose (mute until the next change), «Подтвердить» calls onAck.
type alertWindow struct {
	hwnd                     uintptr
	icon, text, status, hint uintptr
	ack, close               uintptr
	level                    model.AlertLevel
	shown                    bool
	raised                   int // bring-to-front requests (tests)
	lang                     string
	onAck, onClose           func()
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
	hwnd, err := createWindow(alertClass, i18n.T(i18n.KeyAlertTitleWarning, lang), winapi.WS_EX_DLGMODALFRAME|winapi.WS_EX_TOPMOST,
		winapi.WS_OVERLAPPED|winapi.WS_CAPTION|winapi.WS_SYSMENU, x, y, w, h, owner, handler)
	if err != nil {
		return nil, err
	}
	aw.hwnd = hwnd
	aw.icon = child(hwnd, "STATIC", "", winapi.SS_ICON, idAlertIcon, 16, 16, 32, 32)
	aw.text = child(hwnd, "STATIC", "", winapi.SS_LEFT, idAlertText, 64, 16, w-96, h-150)
	aw.status = child(hwnd, "STATIC", "", winapi.SS_LEFT, idAlertStatus, 64, h-128, w-96, 20)
	aw.hint = child(hwnd, "STATIC", i18n.T(i18n.KeyAlertCloseHint, lang), winapi.SS_LEFT, idAlertHint, 16, h-104, w-48, 32)
	aw.ack = child(hwnd, "BUTTON", i18n.T(i18n.KeyAlertAck, lang), winapi.BS_DEFPUSHBUTTON|winapi.WS_TABSTOP, idAlertAck, w-264, h-70, 120, 28)
	aw.close = child(hwnd, "BUTTON", i18n.T(i18n.KeyAlertClose, lang), winapi.WS_TABSTOP, idAlertClose, w-136, h-70, 104, 28)
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
}
