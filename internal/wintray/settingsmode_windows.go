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
	idSetWarn, idSetError, idSetConnTO, idSetQueryTO, idSetDiscover}

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
		on := !simple && w.page == 0
		if id == idSetClearPw && w.cfg.mode == traysettings.ReadOnly {
			on = false
		}
		if id == idSetDiscover && w.cfg.mode != traysettings.Admin {
			continue
		}
		if h := w.ctl[id]; h != 0 {
			w.show(h, on)
			if !on {
				hideWindowTree(h)
			}
		}
		if h := w.labels[id]; h != 0 {
			w.show(h, on)
			if !on {
				hideWindowTree(h)
			}
		}
	}
	if g := w.groups[i18n.KeySetGroupTLS]; g != 0 {
		on := !simple && w.page == 0
		w.show(g, on)
		if !on {
			hideWindowTree(g)
		}
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
	// tabs: hide General (+ Viber via capability) in Simple
	w.rebuildTabs()
	w.hideInactiveTabControls()
	w.relayoutAll() // Hidden controls take no room (no holes)
	// Belt: Simple↔Expert can leave expert HWNDs WS_VISIBLE at a stale Y
	// inside panel[0] (Conn/Login overlap photo A). Re-hide after place.
	if simple {
		for _, id := range expertOnly {
			if id == idSetDiscover && w.cfg.mode != traysettings.Admin {
				continue
			}
			if h := w.ctl[id]; h != 0 {
				hideWindowTree(h)
			}
			if h := w.labels[id]; h != 0 {
				hideWindowTree(h)
			}
		}
		if g := w.groups[i18n.KeySetGroupTLS]; g != 0 {
			hideWindowTree(g)
		}
	}
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
	w.hideInactiveTabControls() // update must not leave other tabs Visible
	if w.host != 0 {
		winapi.InvalidateRect(w.host, true)
		winapi.RedrawWindow(w.host, winapi.RDW_INVALIDATE|winapi.RDW_ERASE|winapi.RDW_ALLCHILDREN|winapi.RDW_UPDATENOW)
	}
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

// markNav keeps the tab selection in sync with w.page (tabs replaced
// the push-like nav buttons).
func (w *settingsWindow) markNav() { w.syncTabSelection() }

// navTabSelected reports whether the section tabs currently select page.
func (w *settingsWindow) navTabSelected(page int) bool {
	h := w.ctl[idSetTabs]
	if h == 0 {
		return w.page == page
	}
	return w.tabPageAt(winapi.TabGetCurSel(h)) == page
}

// syncNavIcons gives every navigation icon the visibility of its button
// (before a layout).
func (w *settingsWindow) syncNavIcons() {
	for _, p := range w.navIco {
		p[1].Hidden = p[0].Hidden
	}
}
