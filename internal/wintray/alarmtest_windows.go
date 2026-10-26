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

// Alarm-test dialog (CHK-TRAY-53/54): admin-only; does not write events/problems.
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
	owner   *settingsWindow
	hwnd    uintptr
	ctl     map[int32]uintptr
	lang    string
	mu      sync.Mutex
	pend    *model.AlarmTestResult
	pendErr string
	retryS  int
	busy    bool
}

func (w *settingsWindow) openAlarmTest() {
	if w.cfg.mode != traysettings.Admin {
		return
	}
	sess := w.cfg.session
	if sess == nil || !(sess.Supported(model.CmdAlarmTest) || sess.Status().Has(model.CapAlarmTest)) {
		return
	}
	aw, err := createAlarmTest(w, w.cfg.lang)
	if err != nil {
		return
	}
	winapi.ShowWindow(aw.hwnd, winapi.SW_SHOWNORMAL)
	winapi.SetForegroundWindow(aw.hwnd)
}

// createAlarmTest builds the dialog (owner may be nil for screenshots).
func createAlarmTest(owner *settingsWindow, lang string) (*alarmTestWindow, error) {
	aw := &alarmTestWindow{owner: owner, lang: lang, ctl: map[int32]uintptr{}}
	var parent uintptr
	if owner != nil {
		parent = owner.hwnd
	}
	hwnd, err := createWindow(alarmClass, i18n.T(i18n.KeyAlarmTestTitle, lang), 0,
		winapi.WS_OVERLAPPED|winapi.WS_CAPTION|winapi.WS_SYSMENU, 0, 0, 440, 340, parent, aw.wndProc)
	if err != nil {
		return nil, err
	}
	aw.hwnd = hwnd
	aw.build()
	return aw, nil
}

func (aw *alarmTestWindow) t(k string) string { return i18n.T(k, aw.lang) }

func (aw *alarmTestWindow) build() {
	add := func(class, text string, ex, style uint32, id int32, kind ui.Kind, wd, hg int32) *ui.Ctl {
		h, _ := winapi.CreateWindowEx(ex, class, text, winapi.WS_CHILD|winapi.WS_VISIBLE|style,
			0, 0, 0, 0, aw.hwnd, uintptr(id), winapi.GetModuleHandle())
		winapi.SendMessage(h, winapi.WM_SETFONT, uiFont(), 1)
		aw.ctl[id] = h
		c := &ui.Ctl{H: h, Kind: kind, Text: text, W: wd, Hgt: hg}
		return c
	}
	badge := add("STATIC", "["+aw.t(i18n.KeyAlertTestBadge)+"]", 0, winapi.SS_LEFT, idAlarmBadge, ui.Label, 0, 0)
	warn := add("BUTTON", aw.t(i18n.KeyAlarmTestWarning), 0, winapi.WS_TABSTOP|winapi.BS_AUTORADIOBUTTON, idAlarmWarn, ui.Check, 0, 0)
	errB := add("BUTTON", aw.t(i18n.KeyAlarmTestError), 0, winapi.WS_TABSTOP|winapi.BS_AUTORADIOBUTTON, idAlarmErr, ui.Check, 0, 0)
	winapi.SendMessage(aw.ctl[idAlarmWarn], winapi.BM_SETCHECK, winapi.BST_CHECKED, 0)
	viber := add("BUTTON", aw.t(i18n.KeyAlarmTestViber), 0, winapi.WS_TABSTOP|winapi.BS_AUTOCHECKBOX, idAlarmViber, ui.Check, 0, 0)
	run := add("BUTTON", aw.t(i18n.KeyAlarmTestRun), 0, winapi.WS_TABSTOP, idAlarmRun, ui.Button, 160, 0)
	closeB := add("BUTTON", aw.t(i18n.KeyAlarmTestClose), 0, winapi.WS_TABSTOP, idAlarmClose, ui.Button, 100, 0)
	res := add("EDIT", "", winapi.WS_EX_CLIENTEDGE,
		winapi.ES_MULTILINE|winapi.ES_READONLY|winapi.ES_AUTOVSCROLL|winapi.WS_VSCROLL,
		idAlarmResult, ui.Box, 0, 120)
	res.Grow, res.W, res.Hgt = true, 360, 120
	badge.W = 360
	// content without Bottom pad — gap to bar ≤ 16
	content := &ui.Pad{Left: ui.P12, Top: ui.P12, Right: ui.P12, Child: &ui.Col{Gap: ui.P8, Items: []ui.Node{
		badge,
		&ui.Row{Gap: ui.P16, Items: []ui.Node{warn, errB}},
		viber,
		res,
	}}}
	bar := &ui.Pad{All: ui.P12, Child: &ui.Row{Grow: true, Gap: ui.P8, Items: []ui.Node{
		&ui.Ctl{Kind: ui.Box, Grow: true, Hgt: 28},
		run, closeB,
	}}}
	fitForm(aw.hwnd, content, bar, 360, false)
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
	if aw.owner == nil || aw.owner.cfg.session == nil {
		return
	}
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

func (aw *alarmTestWindow) setResult(text string) {
	if h := aw.ctl[idAlarmResult]; h != 0 {
		winapi.SetWindowText(h, text)
	}
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
