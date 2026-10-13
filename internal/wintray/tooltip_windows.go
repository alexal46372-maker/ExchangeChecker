//go:build windows

package wintray

import "github.com/alexal46372-maker/ExchangeChecker/internal/winapi"

// tooltips is the TOOLTIPS_CLASS window of one tray window (phase 8,
// CHK-TRAY-15): one tool per control (TTF_IDISHWND | TTF_SUBCLASS), the
// texts come from the i18n catalog (RU / EN). texts keeps what was set
// for the tests.
type tooltips struct {
	hwnd, owner uintptr
	texts       map[uintptr]string
}

// tipWidth wraps long tooltips (pixels).
const tipWidth = 420

// newTooltips creates the tooltip window of owner; nil when comctl32
// cannot create it (the window works without tooltips).
func newTooltips(owner uintptr) *tooltips {
	winapi.InitTooltips()
	h, err := winapi.CreateWindowEx(winapi.WS_EX_TOPMOST, winapi.TOOLTIPS_CLASS, "",
		winapi.WS_POPUP|winapi.TTS_ALWAYSTIP|winapi.TTS_NOPREFIX, 0, 0, 0, 0, owner, 0, winapi.GetModuleHandle())
	if err != nil || h == 0 {
		return nil
	}
	winapi.SendMessage(h, winapi.TTM_SETMAXTIPWIDTH, 0, tipWidth)
	winapi.SendMessage(h, winapi.TTM_SETDELAYTIME, winapi.TTDT_AUTOPOP, 20000)
	return &tooltips{hwnd: h, owner: owner, texts: map[uintptr]string{}}
}

// set adds the tooltip of ctrl or replaces its text.
func (t *tooltips) set(ctrl uintptr, text string) {
	if t == nil || ctrl == 0 {
		return
	}
	if _, ok := t.texts[ctrl]; ok {
		winapi.SendToolInfo(t.hwnd, winapi.TTM_UPDATETIPTEXTW, t.owner, ctrl, text)
	} else if winapi.SendToolInfo(t.hwnd, winapi.TTM_ADDTOOLW, t.owner, ctrl, text) == 0 {
		return
	}
	t.texts[ctrl] = text
}

// count is TTM_GETTOOLCOUNT (tests).
func (t *tooltips) count() int {
	if t == nil {
		return 0
	}
	return int(winapi.SendMessage(t.hwnd, winapi.TTM_GETTOOLCOUNT, 0, 0))
}

// text is the tooltip set for ctrl ("" = none).
func (t *tooltips) text(ctrl uintptr) string {
	if t == nil {
		return ""
	}
	return t.texts[ctrl]
}
