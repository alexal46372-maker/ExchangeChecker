//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Phase 10 D (CHK-TRAY-40/41, PLAN §12 p.121): «Простой» / «Экспертный»
// in the settings window and the red required fields with an asterisk.

// expertOnly are the controls hidden in «Простой» (UIFields: Simple false).
var expertOnly = []int32{idSetPort, idSetClearPw, idSetEncrypt, idSetPreset, idSetTLS10, idSetTrust,
	idSetWarn, idSetError, idSetConnTO, idSetQueryTO, idSetDiscover, idSetPage}

// reqFields maps the edit / combo boxes to their fields.
var reqFields = map[int32]traysettings.Field{
	idSetHost: traysettings.FieldHost, idSetDatabase: traysettings.FieldDatabase, idSetAuth: traysettings.FieldAuth,
	idSetLogin: traysettings.FieldLogin, idSetPassword: traysettings.FieldPassword,
}

// requiredBk is the background of an empty required edit (CHK-TRAY-51 / 55).
func requiredBk() uint32 { return trayui.ChromeRequired() }

// UIMode is the window mode (test hook).
func (w *settingsWindow) UIMode() traysettings.UIMode { return w.uiMode }

func (w *settingsWindow) applyUIMode() {
	simple := w.uiMode == traysettings.UISimple
	if simple && w.page != 0 {
		w.setPage(0) // calls applyUIMode again
		return
	}
	for _, id := range expertOnly {
		cmd := int32(winapi.SW_SHOW)
		if simple || w.page != 0 && id != idSetPage {
			cmd = winapi.SW_HIDE
		}
		if id == idSetClearPw && w.cfg.mode == traysettings.ReadOnly {
			cmd = winapi.SW_HIDE
		}
		if id == idSetDiscover && w.cfg.mode != traysettings.Admin {
			continue
		}
		if h := w.ctl[id]; h != 0 {
			w.show(h, cmd == winapi.SW_SHOW)
		}
		if h := w.labels[id]; h != 0 {
			w.show(h, cmd == winapi.SW_SHOW)
		}
	}
	if g := w.groups[i18n.KeySetGroupTLS]; g != 0 {
		cmd := int32(winapi.SW_SHOW)
		if simple || w.page != 0 {
			cmd = winapi.SW_HIDE
		}
		w.show(g, cmd == winapi.SW_SHOW)
	}
	w.expertShown = !simple && w.page == 0
	key := i18n.KeySetModeExpert
	if !simple {
		key = i18n.KeySetModeSimple
	}
	winapi.SetWindowText(w.ctl[idSetUIMode], w.t(key))
	if w.tips != nil {
		w.tips.set(w.ctl[idSetUIMode], w.t(i18n.KeyTipSetUIMode))
	}
	// the navigation marks the current page (phase 11, CHK-TRAY-47)
	w.markNav()
	w.relayoutAll() // Hidden controls take no room (no holes)
}

func (w *settingsWindow) toggleUIMode() {
	if w.uiMode == traysettings.UISimple {
		w.uiMode = traysettings.UIExpert
	} else {
		w.uiMode = traysettings.UISimple
	}
	if w.cfg.uiStore != nil {
		w.cfg.uiStore.Save(w.uiMode)
	}
	w.applyUIMode()
	w.update()
}

// updateRequired: asterisks on the required labels, red empty boxes.
func (w *settingsWindow) updateRequired() {
	req := map[traysettings.Field]bool{}
	miss := map[traysettings.Field]bool{}
	if w.ed.CanEdit() {
		stored := !w.ed.IsNew && w.ed.Base.HasPassword && !w.ed.Form.ClearPassword
		for _, f := range traysettings.RequiredFields(w.uiMode, w.ed.Form, stored) {
			req[f] = true
		}
		for _, f := range traysettings.Missing(w.uiMode, w.ed.Form, stored) {
			miss[f] = true
		}
	}
	for id, f := range reqFields {
		txt := w.labelText[id]
		if req[f] {
			txt += " *"
		}
		if h := w.labels[id]; h != 0 && winapi.GetWindowText(h) != txt {
			winapi.SetWindowText(h, txt)
		}
		if w.missing[id] != miss[f] {
			w.missing[id] = miss[f]
			winapi.InvalidateRect(w.ctl[id], true)
		}
	}
}

// Missing are the highlighted controls (test hook).
func (w *settingsWindow) Missing() map[int32]bool { return w.missing }

// requiredColor answers WM_CTLCOLOREDIT of an empty required box.
func (w *settingsWindow) requiredColor(dc, ctl uintptr) (uintptr, bool) {
	if w.brushOff {
		return 0, false
	}
	for id, on := range w.missing {
		if on && w.ctl[id] == ctl {
			if w.reqBrush == 0 {
				w.reqBrush = winapi.CreateSolidBrush(requiredBk())
			}
			winapi.SetBkColor(dc, requiredBk())
			return w.reqBrush, w.reqBrush != 0
		}
	}
	return 0, false
}

// navStyle: the navigation buttons are push-like check boxes; the one of
// the current page is pressed (BM_SETCHECK), all stay enabled (phase 11,
// CHK-TRAY-48: a grey disabled button read as «unavailable»).
const navStyle = 0x0002 /* BS_CHECKBOX */ | 0x1000 /* BS_PUSHLIKE */

// markNav presses the button of the current page.
func (w *settingsWindow) markNav() {
	for id, pg := range map[int32]int{idSetNavProfiles: 0, idSetPage: 1, idSetViber: pageViber} {
		st := uintptr(winapi.BST_UNCHECKED)
		if w.page == pg {
			st = winapi.BST_CHECKED
		}
		if h := w.ctl[id]; h != 0 {
			winapi.SendMessage(h, winapi.BM_SETCHECK, st, 0)
		}
	}
}

// navChecked reports a pressed navigation button.
func navChecked(h uintptr) bool {
	return winapi.SendMessage(h, winapi.BM_GETCHECK, 0, 0) == winapi.BST_CHECKED
}

// syncNavIcons gives every navigation icon the visibility of its button
// (before a layout).
func (w *settingsWindow) syncNavIcons() {
	for _, p := range w.navIco {
		p[1].Hidden = p[0].Hidden
	}
}
