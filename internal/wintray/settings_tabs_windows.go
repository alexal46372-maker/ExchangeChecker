//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// Settings section tabs: SysTabControl32 for the three pages (Databases /
// General / Viber). Simple/Expert stays a separate button (not a tab).

const (
	wcTabControl = winapi.WC_TABCONTROL
	tcsHotTrack  = winapi.TCS_HOTTRACK
)

// hideWindowTree hides hwnd and every descendant (combo/edit children
// included) so an inactive tab cannot stay painted over the active one.
func hideWindowTree(h uintptr) {
	if h == 0 {
		return
	}
	winapi.ShowWindow(h, winapi.SW_HIDE)
	for _, ch := range winapi.Children(h) {
		hideWindowTree(ch)
	}
}

// hideInactiveTabControls hard-hides every control of every page that is
// not the current tab: ui.SetHidden on the page root, SW_HIDE on each
// pageCtl HWND and its children. Call before and after applyUIMode —
// mode flips only toggled expertOnly IDs and left other page trees
// Visible (Alex Simple→Expert overlap).
func (w *settingsWindow) hideInactiveTabControls() {
	for i, n := range w.pages {
		if i == w.page || n == nil {
			continue
		}
		ui.SetHidden(n, true)
		ui.Walk(n, func(c *ui.Ctl) {
			if c == nil || c.H == 0 {
				return
			}
			c.Hidden = true
			hideWindowTree(c.H)
		})
	}
	for i, hs := range w.pageCtl {
		if i == w.page {
			continue
		}
		for _, h := range hs {
			if c := w.ctlOf[h]; c != nil {
				c.Hidden = true
			}
			hideWindowTree(h)
		}
	}
}

// rebuildTabs fills SysTabControl32: Databases always; General in Expert;
// Viber in Expert when the capability is available (same rules as the
// former nav buttons / updateViber).
func (w *settingsWindow) rebuildTabs() {
	h := w.ctl[idSetTabs]
	if h == 0 {
		return
	}
	w.tabSync = true
	defer func() { w.tabSync = false }()
	winapi.TabDeleteAllItems(h)
	w.tabPages = w.tabPages[:0]
	add := func(key string, page int) {
		if winapi.TabInsertItem(h, w.t(key)) < 0 {
			return
		}
		w.tabPages = append(w.tabPages, page)
	}
	add(i18n.KeySetNavProfiles, 0)
	if w.uiMode != traysettings.UISimple {
		add(i18n.KeySetNavGeneral, 1)
		if w.viberAvailable() {
			add(i18n.KeySetNavViber, pageViber)
		}
	}
	// if current page vanished (Simple / no Viber), fall back to Databases
	ok := false
	for _, p := range w.tabPages {
		if p == w.page {
			ok = true
			break
		}
	}
	if !ok {
		w.page = 0
	}
	w.syncTabSelection()
}

// syncTabSelection selects the tab that matches w.page (no setPage).
func (w *settingsWindow) syncTabSelection() {
	h := w.ctl[idSetTabs]
	if h == 0 {
		return
	}
	w.tabSync = true
	defer func() { w.tabSync = false }()
	for i, p := range w.tabPages {
		if p == w.page {
			winapi.TabSetCurSel(h, i)
			return
		}
	}
	if len(w.tabPages) > 0 {
		winapi.TabSetCurSel(h, 0)
	}
}

// tabPageAt returns the page index for tab i, or -1.
func (w *settingsWindow) tabPageAt(i int) int {
	if i < 0 || i >= len(w.tabPages) {
		return -1
	}
	return w.tabPages[i]
}

// onTabNotify handles TCN_SELCHANGE from the section tabs.
func (w *settingsWindow) onTabNotify(lp uintptr) bool {
	if lp == 0 || w.ctl[idSetTabs] == 0 || w.tabSync {
		return false
	}
	hdr := winapi.ReadNMHDR(lp)
	if hdr.From != w.ctl[idSetTabs] || hdr.Code != winapi.TCN_SELCHANGE {
		return false
	}
	i := winapi.TabGetCurSel(w.ctl[idSetTabs])
	p := w.tabPageAt(i)
	if p < 0 || p == w.page {
		return true
	}
	w.setPage(p)
	return true
}

// pageCtlVisible reports whether any HWND of page i has WS_VISIBLE.
func (w *settingsWindow) pageCtlVisible(i int) bool {
	if i < 0 || i >= len(w.pageCtl) {
		return false
	}
	for _, h := range w.pageCtl[i] {
		if h != 0 && winapi.WindowStyle(h)&winapi.WS_VISIBLE != 0 {
			return true
		}
	}
	return false
}

// assertPageClear checks inactive pages are Hidden and not WS_VISIBLE
// (test hook). Returns a short reason or "".
func (w *settingsWindow) assertPageClear() string {
	for i := range w.pageCtl {
		wantOn := i == w.page
		if n := w.pages[i]; n != nil {
			bad := false
			ui.Walk(n, func(c *ui.Ctl) {
				if c == nil || c.H == 0 {
					return
				}
				if !wantOn && !c.Hidden {
					bad = true
				}
				if !wantOn && winapi.WindowStyle(c.H)&winapi.WS_VISIBLE != 0 {
					bad = true
				}
			})
			if bad {
				return "pages root/controls not cleared"
			}
		}
		for _, h := range w.pageCtl[i] {
			if h == 0 {
				continue
			}
			vis := winapi.WindowStyle(h)&winapi.WS_VISIBLE != 0
			if !wantOn && vis {
				return "inactive pageCtl still WS_VISIBLE"
			}
			if c := w.ctlOf[h]; c != nil && !wantOn && !c.Hidden {
				return "inactive pageCtl Ctl.Hidden=false"
			}
		}
	}
	// overlap: two page trees must not both have visible controls
	visPages := 0
	for i := range w.pageCtl {
		if w.pageCtlVisible(i) {
			visPages++
		}
	}
	if visPages > 1 {
		return "overlap: more than one pageCtl tree WS_VISIBLE"
	}
	return ""
}
