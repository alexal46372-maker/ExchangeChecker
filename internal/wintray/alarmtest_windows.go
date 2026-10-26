//go:build windows

package wintray

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// Alarm-test dialog (CHK-TRAY-53): admin-only; does not write events/problems.
const (
	alarmClass    = "ExchangeCheckerTrayAlarmTest"
	idAlarmWarn   = 901
	idAlarmErr    = 902
	idAlarmViber  = 903
	idAlarmRun    = 904
	idAlarmClose  = 905
	idAlarmResult = 906
	idAlarmBadge  = 907
	alarmRetryTmr = 54
	msgAlarmApply = winapi.WM_APP + 21
)

type alarmTestWindow struct {
	owner  *settingsWindow
	hwnd   uintptr
	ctl    map[int32]uintptr
	lang   string
	mu     sync.Mutex
	pend   *model.AlarmTestResult
	pendErr string
	retryS int
	busy   bool
}

func (w *settingsWindow) openAlarmTest() {
	if w.cfg.mode != traysettings.Admin {
		return
	}
	sess := w.cfg.session
	if sess == nil || !(sess.Supported(model.CmdAlarmTest) || sess.Status().Has(model.CapAlarmTest)) {
		return
	}
	aw := &alarmTestWindow{owner: w, lang: w.cfg.lang, ctl: map[int32]uintptr{}}
	hwnd, err := createWindow(alarmClass, i18n.T(i18n.KeyAlarmTestTitle, w.cfg.lang), 0,
		winapi.WS_OVERLAPPED|winapi.WS_CAPTION|winapi.WS_SYSMENU, 0, 0, 440, 340, w.hwnd, aw.wndProc)
	if err != nil {
		return
	}
	aw.hwnd = hwnd
	aw.build()
	winapi.ShowWindow(hwnd, winapi.SW_SHOWNORMAL)
	winapi.SetForegroundWindow(hwnd)
}

func (aw *alarmTestWindow) t(k string) string { return i18n.T(k, aw.lang) }

func (aw *alarmTestWindow) build() {
	add := func(class, text string, style uint32, id int32, x, y, wd, hg int32) uintptr {
		h, _ := winapi.CreateWindowEx(0, class, text, winapi.WS_CHILD|winapi.WS_VISIBLE|style,
			x, y, wd, hg, aw.hwnd, uintptr(id), winapi.GetModuleHandle())
		winapi.SendMessage(h, winapi.WM_SETFONT, uiFont(), 1)
		aw.ctl[id] = h
		return h
	}
	add("STATIC", "["+aw.t(i18n.KeyAlertTestBadge)+"]", winapi.SS_LEFT, idAlarmBadge, 12, 12, 80, 24)
	add("BUTTON", aw.t(i18n.KeyAlarmTestWarning), winapi.WS_TABSTOP|winapi.BS_AUTORADIOBUTTON, idAlarmWarn, 12, 48, 180, 24)
	add("BUTTON", aw.t(i18n.KeyAlarmTestError), winapi.WS_TABSTOP|winapi.BS_AUTORADIOBUTTON, idAlarmErr, 200, 48, 180, 24)
	winapi.SendMessage(aw.ctl[idAlarmWarn], winapi.BM_SETCHECK, winapi.BST_CHECKED, 0)
	add("BUTTON", aw.t(i18n.KeyAlarmTestViber), winapi.WS_TABSTOP|winapi.BS_AUTOCHECKBOX, idAlarmViber, 12, 84, 280, 24)
	add("BUTTON", aw.t(i18n.KeyAlarmTestRun), winapi.WS_TABSTOP, idAlarmRun, 12, 120, 160, 28)
	add("BUTTON", aw.t(i18n.KeyAlarmTestClose), winapi.WS_TABSTOP, idAlarmClose, 300, 120, 100, 28)
	h, _ := winapi.CreateWindowEx(winapi.WS_EX_CLIENTEDGE, "EDIT", "",
		winapi.WS_CHILD|winapi.WS_VISIBLE|winapi.ES_MULTILINE|winapi.ES_READONLY|winapi.ES_AUTOVSCROLL|winapi.WS_VSCROLL,
		12, 160, 400, 120, aw.hwnd, uintptr(idAlarmResult), winapi.GetModuleHandle())
	winapi.SendMessage(h, winapi.WM_SETFONT, uiFont(), 1)
	aw.ctl[idAlarmResult] = h
	_ = ui.P8
}

func (aw *alarmTestWindow) wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case winapi.WM_COMMAND:
		id := int32(wp & 0xFFFF)
		switch id {
		case idAlarmClose:
			winapi.PostMessage(hwnd, winapi.WM_CLOSE, 0, 0)
			return 0, true
		case idAlarmRun:
			aw.run()
			return 0, true
		}
	case msgAlarmApply:
		aw.onApply()
		return 0, true
	case winapi.WM_TIMER:
		if wp == alarmRetryTmr {
			aw.tickRetry()
			return 0, true
		}
	case winapi.WM_CLOSE:
		destroyWindow(hwnd)
		return 0, true
	case winapi.WM_DESTROY:
		aw.hwnd = 0
		return 0, false
	}
	return 0, false
}

func (aw *alarmTestWindow) level() string {
	if winapi.SendMessage(aw.ctl[idAlarmErr], winapi.BM_GETCHECK, 0, 0) == winapi.BST_CHECKED {
		return model.AlarmLevelError
	}
	return model.AlarmLevelWarning
}

func (aw *alarmTestWindow) viber() bool {
	return winapi.SendMessage(aw.ctl[idAlarmViber], winapi.BM_GETCHECK, 0, 0) == winapi.BST_CHECKED
}

func (aw *alarmTestWindow) run() {
	aw.mu.Lock()
	if aw.busy || aw.retryS > 0 {
		aw.mu.Unlock()
		return
	}
	aw.busy = true
	aw.mu.Unlock()
	winapi.EnableWindow(aw.ctl[idAlarmRun], false)
	sess := aw.owner.cfg.session
	level, viber := aw.level(), aw.viber()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		m, err := sess.Do(ctx, model.IPCRequest{Cmd: model.CmdAlarmTest, Level: level, Viber: viber})
		aw.mu.Lock()
		aw.busy, aw.pend, aw.pendErr = false, nil, ""
		if err != nil {
			aw.pendErr = err.Error()
		} else if m.Type == model.MsgError {
			if m.Code == model.ErrCodeAccessDenied {
				aw.pendErr = aw.t(i18n.KeyAlarmTestAccess)
			} else if m.Code == model.ErrCodeRateLimited {
				sec := int((m.RetryAfterMs + 999) / 1000)
				if sec < 1 {
					sec = 1
				}
				aw.retryS = sec
				aw.pendErr = fmt.Sprintf(aw.t(i18n.KeyAlarmTestRetry), sec)
			} else {
				aw.pendErr = m.Error
				if aw.pendErr == "" {
					aw.pendErr = m.Code
				}
			}
		} else if m.AlarmTest != nil {
			cp := *m.AlarmTest
			aw.pend = &cp
		}
		aw.mu.Unlock()
		if winapi.IsWindow(aw.hwnd) {
			winapi.PostMessage(aw.hwnd, msgAlarmApply, 0, 0)
		}
	}()
}

func (aw *alarmTestWindow) onApply() {
	aw.mu.Lock()
	res, errText, retry := aw.pend, aw.pendErr, aw.retryS
	aw.pend = nil
	aw.mu.Unlock()
	if retry > 0 {
		winapi.SetWindowText(aw.ctl[idAlarmResult], errText)
		aw.setRunLabel(retry)
		winapi.EnableWindow(aw.ctl[idAlarmRun], false)
		winapi.SetTimer(aw.hwnd, alarmRetryTmr, 1000)
		return
	}
	if res == nil {
		winapi.SetWindowText(aw.ctl[idAlarmResult], errText)
		winapi.EnableWindow(aw.ctl[idAlarmRun], true)
		winapi.SetWindowText(aw.ctl[idAlarmRun], aw.t(i18n.KeyAlarmTestRun))
		return
	}
	winapi.SetWindowText(aw.ctl[idAlarmResult], formatAlarmViber(res, aw.lang))
	winapi.EnableWindow(aw.ctl[idAlarmRun], true)
	winapi.SetWindowText(aw.ctl[idAlarmRun], aw.t(i18n.KeyAlarmTestRun))
}

func (aw *alarmTestWindow) tickRetry() {
	aw.mu.Lock()
	if aw.retryS > 0 {
		aw.retryS--
	}
	left := aw.retryS
	aw.mu.Unlock()
	if left <= 0 {
		winapi.KillTimer(aw.hwnd, alarmRetryTmr)
		winapi.EnableWindow(aw.ctl[idAlarmRun], true)
		winapi.SetWindowText(aw.ctl[idAlarmRun], aw.t(i18n.KeyAlarmTestRun))
		return
	}
	aw.setRunLabel(left)
}

func (aw *alarmTestWindow) setRunLabel(sec int) {
	winapi.SetWindowText(aw.ctl[idAlarmRun], fmt.Sprintf(aw.t(i18n.KeyAlarmTestRetry), sec))
}

func formatAlarmViber(r *model.AlarmTestResult, lang string) string {
	if r == nil {
		return ""
	}
	if r.Viber == nil || len(r.Viber.Results) == 0 {
		return i18n.T(i18n.KeyAlarmTestNoViber, lang)
	}
	_ = r.Viber.ErrorKey
	var lines []string
	for _, it := range r.Viber.Results {
		if it.OK {
			lines = append(lines, it.ID+": OK")
			continue
		}
		msg := it.ErrorKey
		if msg != "" {
			msg = i18n.Message(msg, lang)
		}
		if msg == "" {
			msg = "error"
		}
		lines = append(lines, it.ID+": "+msg)
	}
	if len(lines) == 0 {
		return i18n.T(i18n.KeyAlarmTestNoViber, lang)
	}
	return strings.Join(lines, "\r\n")
}
