//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// Settings section tabs: SysTabControl32 (General / Databases / Viber / Email).
// Simple/Expert is a window-common footer button (CHK-TRAY-61), not a tab.

const (
	wcTabControl = winapi.WC_TABCONTROL
	tcsHotTrack  = winapi.TCS_HOTTRACK
)

// SettingsTabPanelClass is one child panel per settings tab. SW_HIDE on
// the panel hides every descendant (orphan STATICs included) — the
// pageCtl walk alone left ghosts on live Win7 after PR #8.
const SettingsTabPanelClass = "ExchangeCheckerSettingsTab"

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

// pagePanelProc forwards control notifications to the settings window
// (controls are children of the tab panel, not of the scrolled host).
func (w *settingsWindow) pagePanelProc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case winapi.WM_ERASEBKGND:
		return contentErase(wp, hwnd), true
	case winapi.WM_PAINT:
		// Installer-style: always FillRect on paint so uncover (fErase=0)
		// and scroll leave no ghost under TRANSPARENT STATICs (all tabs).
		contentPaint(hwnd)
		return 0, true
	case winapi.WM_CTLCOLOREDIT, winapi.WM_CTLCOLORSTATIC, winapi.WM_CTLCOLORLISTBOX, winapi.WM_CTLCOLORBTN:
		// content page brush (installer ctlColor); required edit via hostProc
		return w.hostProc(w.host, msg, wp, lp)
	case winapi.WM_COMMAND, winapi.WM_NOTIFY:
		// via hostProc: BN_SETFOCUS / EN_SETFOCUS still scrollIntoView
		return w.hostProc(w.host, msg, wp, lp)
	}
	return 0, false
}

// ensurePagePanels creates one WS_EX_CONTROLPARENT child per tab under
// the scrolled host. No WS_CLIPCHILDREN: contentErase/contentPaint must
// fill under TRANSPARENT STATICs (CHK-TRAY-71; installer has none). Idempotent.
func (w *settingsWindow) ensurePagePanels() {
	if w.host == 0 {
		return
	}
	for i := range w.pagePanel {
		if w.pagePanel[i] != 0 {
			continue
		}
		h, err := createWindowPx(SettingsTabPanelClass, "", winapi.WS_EX_CONTROLPARENT,
			winapi.WS_CHILD|winapi.WS_CLIPSIBLINGS, 0, 0, 10, 10, w.host, w.pagePanelProc)
		if err != nil {
			continue
		}
		w.pagePanel[i] = h
		winapi.ShowWindow(h, winapi.SW_HIDE)
	}
}

// hideInactiveTabControls SW_HIDEs every inactive pagePanel (entire
// child tree) and marks the ui page roots Hidden. Page-action buttons
// (Test/…) are NOT pageCtl leftovers — showPageActions owns them
// (CHK-TRAY-67). Call before and after applyUIMode / update.
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
		})
	}
	for i, pan := range w.pagePanel {
		if pan == 0 {
			continue
		}
		if i == w.page {
			winapi.ShowWindow(pan, winapi.SW_SHOWNA)
			continue
		}
		hideWindowTree(pan)
	}
	for i, hs := range w.pageCtl {
		if i == w.page {
			continue
		}
		for _, h := range hs {
			if w.isPageAction(h) {
				continue // showPageActions owns visibility
			}
			if c := w.ctlOf[h]; c != nil {
				c.Hidden = true
			}
			if !w.underPagePanel(h) {
				hideWindowTree(h)
			}
		}
	}
	w.showPageActions(w.page)
}

// isPageAction reports whether h is a footer Databases action button.
func (w *settingsWindow) isPageAction(h uintptr) bool {
	if h == 0 {
		return false
	}
	for _, a := range w.pageActions {
		if a == h {
			return true
		}
	}
	for _, id := range pageActionIDs {
		if w.ctl[id] == h {
			return true
		}
	}
	return false
}

// showPageActions shows Test/Cancel/Alarm/Poll only on Databases (page 0).
// Must run after every tab / Simple↔Expert switch (CHK-TRAY-67).
func (w *settingsWindow) showPageActions(page int) {
	on := page == 0
	for _, id := range pageActionIDs {
		h := w.ctl[id]
		if h == 0 {
			continue
		}
		if on {
			winapi.ShowWindow(h, winapi.SW_SHOWNA)
		} else {
			hideWindowTree(h)
		}
	}
	// AlarmTest additionally needs capability — update() sets EnableWindow;
	// if capability missing keep hidden even on Databases.
	if on {
		if h := w.ctl[idSetAlarmTest]; h != 0 {
			sess := w.cfg.session
			hasAlarm := sess != nil && (sess.Supported(model.CmdAlarmTest) || sess.Status().Has(model.CapAlarmTest))
			if !hasAlarm {
				hideWindowTree(h)
			}
		}
	}
}

// underPagePanel reports whether h is a descendant of any pagePanel.
func (w *settingsWindow) underPagePanel(h uintptr) bool {
	if h == 0 {
		return false
	}
	for _, pan := range w.pagePanel {
		if pan == 0 {
			continue
		}
		if h == pan {
			return true
		}
		for _, ch := range winapi.Children(pan) {
			if ch == h || underHWND(ch, h) {
				return true
			}
		}
	}
	return false
}

func underHWND(root, target uintptr) bool {
	if root == 0 || target == 0 {
		return false
	}
	for _, ch := range winapi.Children(root) {
		if ch == target || underHWND(ch, target) {
			return true
		}
	}
	return false
}

// pageOfHWND returns the tab index that owns h, or -1.
func (w *settingsWindow) pageOfHWND(h uintptr) int {
	for i, pan := range w.pagePanel {
		if pan != 0 && (h == pan || underHWND(pan, h)) {
			return i
		}
	}
	for i, hs := range w.pageCtl {
		for _, x := range hs {
			if x == h {
				return i
			}
		}
	}
	return -1
}

// rebuildTabs fills SysTabControl32 (CHK-TRAY-61/75): General first,
// Databases second (always); General ALWAYS (also in Simple); Viber/Email
// expert-only when the capability is available.
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
	// CHK-TRAY-75: General → Databases → Viber → e-mail (page indices unchanged).
	add(i18n.KeySetNavGeneral, 1)
	add(i18n.KeySetNavProfiles, 0)
	if w.uiMode != traysettings.UISimple && w.viberAvailable() {
		add(i18n.KeySetNavViber, pageViber)
	}
	if w.uiMode != traysettings.UISimple && w.emailAvailable() {
		add(i18n.KeySetNavEmail, pageEmail)
	}
	// if current page vanished (Simple hid Viber), fall back to General
	ok := false
	for _, p := range w.tabPages {
		if p == w.page {
			ok = true
			break
		}
	}
	if !ok {
		w.page = 1
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
// (test hook). Also fails on GetWindowRect overlap of visible controls
// from different tabs, and on IsWindowVisible ghosts under inactive
// panels (catches orphans the pageCtl walk missed).
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
		if pan := w.pagePanel[i]; pan != 0 {
			if !wantOn {
				if winapi.WindowStyle(pan)&winapi.WS_VISIBLE != 0 {
					return "inactive pagePanel still WS_VISIBLE"
				}
				if winapi.IsWindowVisible(pan) {
					return "inactive pagePanel IsWindowVisible"
				}
				for _, ch := range winapi.Children(pan) {
					if winapi.IsWindowVisible(ch) {
						return "ghost: IsWindowVisible under inactive panel"
					}
					if winapi.WindowStyle(ch)&winapi.WS_VISIBLE != 0 {
						return "ghost: WS_VISIBLE child under inactive panel"
					}
				}
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
	visPages := 0
	for i := range w.pageCtl {
		if w.pageCtlVisible(i) {
			visPages++
		}
	}
	if visPages > 1 {
		return "overlap: more than one pageCtl tree WS_VISIBLE"
	}
	if why := w.assertNoCrossTabOverlap(); why != "" {
		return why
	}
	return ""
}

func rectsOverlap(a, b winapi.RECT) bool {
	return a.Left < b.Right && b.Left < a.Right && a.Top < b.Bottom && b.Top < a.Bottom
}

// assertNoCrossTabOverlap fails when two IsWindowVisible controls from
// different tabs have overlapping screen rects (Alex ghost repro).
func (w *settingsWindow) assertNoCrossTabOverlap() string {
	type item struct {
		page int
		r    winapi.RECT
	}
	var vis []item
	for i, hs := range w.pageCtl {
		for _, h := range hs {
			if h == 0 || !winapi.IsWindowVisible(h) {
				continue
			}
			vis = append(vis, item{i, winapi.GetWindowRect(h)})
		}
	}
	// also any visible direct child of host that belongs to a page
	for _, h := range winapi.Children(w.host) {
		if !winapi.IsWindowVisible(h) {
			continue
		}
		pg := w.pageOfHWND(h)
		if pg < 0 {
			continue
		}
		vis = append(vis, item{pg, winapi.GetWindowRect(h)})
		for _, ch := range winapi.Children(h) {
			if winapi.IsWindowVisible(ch) {
				vis = append(vis, item{pg, winapi.GetWindowRect(ch)})
			}
		}
	}
	for a := 0; a < len(vis); a++ {
		for b := a + 1; b < len(vis); b++ {
			if vis[a].page == vis[b].page {
				continue
			}
			if rectsOverlap(vis[a].r, vis[b].r) {
				return "overlap: visible GetWindowRect across tabs"
			}
		}
	}
	return ""
}
