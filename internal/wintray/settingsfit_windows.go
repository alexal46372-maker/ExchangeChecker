//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// Phase 10 E (CHK-TRAY-42, PLAN §12 p.122): the settings window never
// exceeds the work area of the monitor. The pages live in a scrolled
// host child (vertical and, if needed, horizontal scroll bars); the
// status box and the button row («Сохранить», «Закрыть», …) are children
// of the window itself, pinned to its bottom edge, so they stay visible.

// SettingsPageClass is the scrolled page host.
const SettingsPageClass = "ExchangeCheckerSettingsPage"

// Page content and bar of the 96-dpi grid.
const (
	setContentW = 774
	setContentH = 622 // the pages end above the old status box
	setBarH     = 84  // status box (40) + button row (28) + gaps
)

// testWorkArea overrides the work area (layout tests: 1024×768 with a
// 40 px taskbar).
var testWorkArea *winapi.RECT

func workArea() winapi.RECT {
	if testWorkArea != nil {
		return *testWorkArea
	}
	work, ok := winapi.WorkArea()
	if !ok || work.Right <= work.Left || work.Bottom <= work.Top {
		work = winapi.RECT{Right: winapi.GetSystemMetrics(winapi.SM_CXSCREEN), Bottom: winapi.GetSystemMetrics(winapi.SM_CYSCREEN)}
	}
	return work
}

// hostExtent: the scrolled content size (px) of each page host (the
// layout test checks the controls against it).
var hostExtent = map[uintptr][2]int32{}

// settingsRect is the outer rectangle of the settings window: the full
// grid if it fits, else clamped to the work area (with room for the
// vertical scroll bar when the height is clamped).
func settingsRect(style uint32) (x, y, w, h int32) {
	return settingsRectPx(style, px(setContentW), px(setContentH+setBarH))
}

// settingsRectPx is settingsRect for a client of cw×ch pixels (the laid
// out pages plus the bar, phase 11).
func settingsRectPx(style uint32, cw, ch int32) (x, y, w, h int32) {
	work := workArea()
	outer := func(cw, ch int32) (int32, int32) {
		r := winapi.RECT{Right: cw, Bottom: ch}
		if !winapi.AdjustWindowRectEx(&r, style, 0) {
			r = winapi.RECT{Right: cw + 8, Bottom: ch + 32}
		}
		return r.Right - r.Left, r.Bottom - r.Top
	}
	ow, oh := outer(cw, ch)
	if oh > work.Bottom-work.Top {
		ow, oh = outer(cw+winapi.GetSystemMetrics(winapi.SM_CXVSCROLL), ch)
	}
	return fitRect(ow, oh, work)
}

// createHost creates the page host filling the client above the bar.
func (w *settingsWindow) createHost() error {
	w.hostRect = map[uintptr]winapi.RECT{}
	h, err := createWindowPx(SettingsPageClass, "", winapi.WS_EX_CONTROLPARENT,
		winapi.WS_CHILD|winapi.WS_VISIBLE|winapi.WS_VSCROLL|winapi.WS_HSCROLL|winapi.WS_CLIPCHILDREN|winapi.WS_CLIPSIBLINGS, 0, 0, 10, 10, w.hwnd, w.hostProc)
	if err != nil {
		return err
	}
	w.host = h
	return nil
}

// hostProc forwards the notifications of the page controls to the
// window and scrolls.
func (w *settingsWindow) hostProc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case winapi.WM_ERASEBKGND:
		return contentErase(wp, hwnd), true
	case winapi.WM_PAINT:
		contentPaint(hwnd)
		return 0, true
	case winapi.WM_COMMAND:
		// focus (also by Tab): scroll the control into the view; the
		// buttons have BS_NOTIFY for BN_SETFOCUS, so their BN_DBLCLK is
		// a second BN_CLICKED (as without BS_NOTIFY)
		code := uint32(wp >> 16 & 0xFFFF)
		switch {
		case lp != 0 && w.notifyBtn[lp] && code == winapi.BN_SETFOCUS:
			w.scrollIntoView(lp)
			return 0, true
		case lp != 0 && w.notifyBtn[lp] && code == winapi.BN_DBLCLK:
			wp &^= 0xFFFF << 16 // BN_CLICKED
		case lp != 0 && w.notifyBtn[lp] && code != winapi.BN_CLICKED:
			return 0, true // BN_KILLFOCUS…: only BS_NOTIFY sends them, the handlers expect clicks
		case lp != 0 && !w.notifyBtn[lp] && (code == winapi.EN_SETFOCUS || code == winapi.CBN_SETFOCUS || code == winapi.LBN_SETFOCUS):
			w.scrollIntoView(lp)
		}
		return w.wndProc(w.hwnd, msg, wp, lp)
	case winapi.WM_CTLCOLOREDIT:
		// required-field brush first; else content page brush (not panel)
		if br, ok := w.requiredColor(wp, lp); ok {
			return br, true
		}
		if r, ok := contentCtlColor(msg, wp); ok {
			return r, true
		}
		return w.wndProc(w.hwnd, msg, wp, lp)
	case winapi.WM_CTLCOLORSTATIC, winapi.WM_CTLCOLORLISTBOX, winapi.WM_CTLCOLORBTN:
		if r, ok := contentCtlColor(msg, wp); ok {
			return r, true
		}
	case winapi.WM_NOTIFY:
		return w.wndProc(w.hwnd, msg, wp, lp)
	case winapi.WM_VSCROLL, winapi.WM_HSCROLL:
		vert := msg == winapi.WM_VSCROLL
		pos, page, max := w.scrollX, w.viewW, w.contentW
		if vert {
			pos, page, max = w.scrollY, w.viewH, w.contentH
		}
		line := px(gridRow)
		switch int32(wp & 0xFFFF) {
		case winapi.SB_LINEUP:
			pos -= line
		case winapi.SB_LINEDOWN:
			pos += line
		case winapi.SB_PAGEUP:
			pos -= page
		case winapi.SB_PAGEDOWN:
			pos += page
		case winapi.SB_THUMBTRACK, winapi.SB_THUMBPOSITION:
			pos = int32(wp >> 16 & 0xFFFF)
		case winapi.SB_TOP:
			pos = 0
		case winapi.SB_BOTTOM:
			pos = max
		}
		if vert {
			w.scrollTo(w.scrollX, pos)
		} else {
			w.scrollTo(pos, w.scrollY)
		}
		return 0, true
	case winapi.WM_MOUSEWHEEL:
		delta := int32(int16(wp >> 16 & 0xFFFF))
		w.scrollTo(w.scrollX, w.scrollY-delta*px(gridRow)*3/120)
		return 0, true
	}
	return 0, false
}

// scrollTo moves the page controls to the scroll position (clamped).
func (w *settingsWindow) scrollTo(x, y int32) {
	clamp := func(v, view, content int32) int32 {
		if v > content-view {
			v = content - view
		}
		if v < 0 {
			v = 0
		}
		return v
	}
	x, y = clamp(x, w.viewW, w.contentW), clamp(y, w.viewH, w.contentH)
	if x == w.scrollX && y == w.scrollY && w.scrollSet {
		return
	}
	w.scrollX, w.scrollY, w.scrollSet = x, y, true
	if w.page >= 0 && w.page < len(w.scrollPos) {
		w.scrollPos[w.page].x, w.scrollPos[w.page].y = x, y
	}
	// Move children from unscrolled hostRect without per-child repaint
	// (Win7 intermediate paints trail — CHK-TRAY-68). Never touch the frame
	// tab strip; never ScrollWindow. eraseRepaint(host) then active pagePanel
	// (CHK-TRAY-71: panel must fill under STATICs — no WS_CLIPCHILDREN).
	withSetRedraw(w.host, func() {
		for h, r := range w.hostRect {
			if h == 0 || !winapi.IsWindow(h) {
				continue
			}
			if w.ctl[idSetTabs] != 0 && h == w.ctl[idSetTabs] {
				continue
			}
			winapi.MoveWindowNoRedraw(h, r.Left-x, r.Top-y, r.Right-r.Left, r.Bottom-r.Top)
		}
		// CHK-TRAY-78: left DB chrome is under pagePanel[0]; counteract scroll
		// so list/Add stay fixed while the form moves with the panel.
		w.repositionPinnedInPanelNoRedraw()
		// bRedraw=FALSE inside batch — SetScrollInfo(TRUE) partial-invalidates on Win7.
		winapi.SetScrollNoRedraw(w.host, winapi.SB_VERT, w.contentH, w.viewH, y)
		winapi.SetScrollNoRedraw(w.host, winapi.SB_HORZ, w.contentW, w.viewW, x)
	})
	// Host CLIPCHILDREN does not erase under pagePanel — must erase panel too.
	if w.page >= 0 && w.page < len(w.pagePanel) {
		if pan := w.pagePanel[w.page]; pan != 0 {
			eraseRepaint(pan)
		}
	}
	// Refresh scrollbar thumb after erase (was NoRedraw in batch).
	winapi.SetScroll(w.host, winapi.SB_VERT, w.contentH, w.viewH, y)
	winapi.SetScroll(w.host, winapi.SB_HORZ, w.contentW, w.viewW, x)
}

// ScrollPos is the page scroll position in pixels (test hook).
func (w *settingsWindow) ScrollPos() (int32, int32) { return w.scrollX, w.scrollY }

// scrollIntoView scrolls a page control into the view.
func (w *settingsWindow) scrollIntoView(h uintptr) {
	r, ok := w.hostRect[h]
	if !ok {
		// panel child: layout Rect is still in host/content coordinates
		if c := w.ctlOf[h]; c != nil && c.Placed {
			r = winapi.RECT{Left: c.Rect.X, Top: c.Rect.Y, Right: c.Rect.X + c.Rect.W, Bottom: c.Rect.Y + c.Rect.H}
			ok = true
		}
	}
	if !ok {
		return
	}
	x, y := w.scrollX, w.scrollY
	if r.Bottom-y > w.viewH {
		y = r.Bottom - w.viewH
	}
	if r.Top < y {
		y = r.Top
	}
	if r.Right-x > w.viewW {
		x = r.Right - w.viewW
	}
	if r.Left < x {
		x = r.Left
	}
	w.scrollTo(x, y)
}

// layoutFit places the bar at the bottom of the client area (two button
// rows if one does not fit) and sizes the host and its scroll bars.
func (w *settingsWindow) layoutFit() {
	rc := winapi.GetClientRect(w.hwnd)
	cw, ch := rc.Right-rc.Left, rc.Bottom-rc.Top
	m, gap, bh := px(gridMargin), px(gridGap), px(gridButtonH)
	// Fixed tab chrome (frame child) — never inside the scroll host.
	tabH := px(settingsTabChromeH)
	if w.ctl[idSetTabs] != 0 {
		// brand accent (3px) + pad above tabs; no pad under tabs (CHK-TRAY-76)
		winapi.MoveWindow(w.ctl[idSetTabs], m, 3+gap/2, cw-2*m, tabH)
		winapi.ShowWindow(w.ctl[idSetTabs], winapi.SW_SHOWNA)
		delete(w.hostRect, w.ctl[idSetTabs])
	}
	w.tabChromeH = 3 + gap/2 + tabH // flush host under SysTabControl (CHK-TRAY-76)
	type btn struct {
		h     uintptr
		width int32
	}
	var left, right []btn
	isPageAct := func(id int32) bool {
		for _, a := range pageActionIDs {
			if a == id {
				return true
			}
		}
		return false
	}
	for _, b := range w.barLeft {
		h := w.ctl[b.id]
		if h == 0 {
			continue
		}
		// Always reserve layout slots for page-actions on Databases so a
		// later showPageActions can paint them (CHK-TRAY-67).
		vis := winapi.WindowStyle(h)&winapi.WS_VISIBLE != 0
		if vis || (w.page == 0 && isPageAct(b.id)) {
			left = append(left, btn{h, px(b.width)})
		}
	}
	for _, b := range w.barRight {
		if h := w.ctl[b.id]; h != 0 && winapi.WindowStyle(h)&winapi.WS_VISIBLE != 0 {
			right = append(right, btn{h, px(b.width)})
		}
	}
	rightW := int32(0)
	for _, b := range right {
		rightW += b.width + gap
	}
	// left buttons flow over as many rows as needed (phase 11: widths
	// follow the texts); the right buttons end the last row, or get a row
	// of their own when they do not fit beside the left ones
	type spot struct {
		b      btn
		x, row int32
	}
	var spots []spot
	x, row := m, int32(0)
	for _, b := range left {
		if x > m && x+b.width > cw-m {
			x, row = m, row+1
		}
		spots = append(spots, spot{b, x, row})
		x += b.width + gap
	}
	if len(right) > 0 && x > m && x > cw-m-rightW {
		row++
	}
	rows := row + 1
	barH := px(setBarH) + (rows-1)*(bh+gap)
	if barH > ch/2 {
		barH = ch / 2
	}
	footer := ch - barH
	w.footerTop = footer
	winapi.MoveWindow(w.ctl[idSetStatus], m, footer+gap, cw-2*m, px(40))
	by := footer + gap + px(40) + gap
	for _, s := range spots {
		winapi.MoveWindow(s.b.h, s.x, by+s.row*(bh+gap), s.b.width, bh)
	}
	// Right buttons: wrap to extra rows when they do not fit on one line
	// (144 dpi EN Export/Import/Mode/Save/Close overflowed left of client).
	rx, rrow := cw-m, row
	for i := len(right) - 1; i >= 0; i-- {
		if rx < m+right[i].width && rx < cw-m {
			rrow++
			rx = cw - m
		}
		rx -= right[i].width
		if rx < m {
			rx = m
		}
		winapi.MoveWindow(right[i].h, rx, by+rrow*(bh+gap), right[i].width, bh)
		rx -= gap
	}
	if rrow > row {
		rows = rrow + 1
		barH = px(setBarH) + (rows-1)*(bh+gap)
		if barH > ch/2 {
			barH = ch / 2
		}
		footer = ch - barH
		w.footerTop = footer
		winapi.MoveWindow(w.ctl[idSetStatus], m, footer+gap, cw-2*m, px(40))
		by = footer + gap + px(40) + gap
		// re-place left spots for the new footer top
		for _, s := range spots {
			winapi.MoveWindow(s.b.h, s.x, by+s.row*(bh+gap), s.b.width, bh)
		}
		// re-place right on the updated by
		rx, rrow = cw-m, row
		for i := len(right) - 1; i >= 0; i-- {
			if rx < m+right[i].width && rx < cw-m {
				rrow++
				rx = cw - m
			}
			rx -= right[i].width
			if rx < m {
				rx = m
			}
			winapi.MoveWindow(right[i].h, rx, by+rrow*(bh+gap), right[i].width, bh)
			rx -= gap
		}
	}
	// Middle scroll host only (between fixed tabs and fixed footer).
	hostTop := w.tabChromeH
	hostH := footer - hostTop
	if hostH < 40 {
		hostH = 40
	}
	winapi.MoveWindow(w.host, 0, hostTop, cw, hostH)
	w.contentW, w.contentH = px(setContentW), px(setContentH)
	if w.pageW > 0 { // laid out by the framework (phase 11)
		w.contentW, w.contentH = w.pageW, w.pageH
	}
	// the client of the host without its scroll bars
	w.viewW, w.viewH = cw, hostH
	if w.contentH > w.viewH {
		w.viewW -= winapi.GetSystemMetrics(winapi.SM_CXVSCROLL)
	}
	if w.contentW > w.viewW {
		w.viewH -= winapi.GetSystemMetrics(winapi.SM_CYHSCROLL)
		if w.contentH > w.viewH && w.viewW == cw {
			w.viewW -= winapi.GetSystemMetrics(winapi.SM_CXVSCROLL)
		}
	}
	hostExtent[w.host] = [2]int32{w.contentW, w.contentH}
	// Drop stale hostRect entries (e.g. pre-chrome leftovers) then re-sync
	// panels against the final host size so scroll 0 ⇒ child y ≥ 0.
	for h := range w.hostRect {
		if h == w.host {
			delete(w.hostRect, h)
			continue
		}
		isPan := false
		for _, pan := range w.pagePanel {
			if pan == h {
				isPan = true
				break
			}
		}
		if !isPan {
			delete(w.hostRect, h)
		}
	}
	w.scrollSet = false
	sx, sy := int32(0), int32(0)
	if w.page >= 0 && w.page < len(w.scrollPos) {
		sx, sy = w.scrollPos[w.page].x, w.scrollPos[w.page].y
	}
	// syncPagePanels writes hostRect[pan]=slot and scrollTo's
	w.scrollX, w.scrollY = sx, sy
	w.syncPagePanels()
}

// barButton is a button of the bottom row and its grid width.
type barButton struct {
	id    int32
	width int32
}

// layoutPages lays out the page host: SysTabControl32 on top, then the
// page stack only (CHK-TRAY-60). Bases chrome (list/Add/…) lives inside
// pagePanel[0]; Export/Import/UIMode are window-common bar buttons
// (CHK-TRAY-61). Pages share one slot (Col Gap:-1). SW_HIDE(panel) stops ghosts.
func (w *settingsWindow) layoutPages(m *ui.Metrics, pages []ui.Node) {
	// Tabs are frame chrome (layoutFit); host content is pages only.
	body := &ui.Pad{Top: 2, Child: &ui.Col{Gap: -1, Items: pages}} // CHK-TRAY-76: minimal gap under tabs
	w.root = &ui.Pad{Left: ui.P12, Top: 0, Right: ui.P12, Bottom: ui.P8, Child: body}
	w.rootM = m
	syncCues(w.root)
	w.ctlOf = map[uintptr]*ui.Ctl{}
	ui.Walk(w.root, func(c *ui.Ctl) {
		if c.H != 0 {
			w.ctlOf[c.H] = c
			c.Hidden = winapi.WindowStyle(c.H)&winapi.WS_VISIBLE == 0
		}
	})
	w.syncNavIcons()
	style := winapi.WindowStyle(w.hwnd) &^ (winapi.WS_VSCROLL | winapi.WS_HSCROLL)
	work := workArea()
	fw, _ := outerSize(0, 0, style, 0)
	maxW := work.Right - work.Left - fw - winapi.GetSystemMetrics(winapi.SM_CXVSCROLL)
	// the window is as wide as the widest page (each shrunk to the work
	// area), so that no other page scrolls horizontally: lay everything
	// out visible once
	hidden := map[*ui.Ctl]bool{}
	ui.Walk(w.root, func(c *ui.Ctl) { hidden[c], c.Hidden = c.Hidden, false })
	var allW int32
	for i := 0; i < 8; i++ {
		allW, _ = ui.Layout(w.root, m, 0, 0, 0)
		if over := allW - maxW; over <= 0 || !ui.Shrink(w.root, unpx(over)+1) && !w.stackPairs() {
			break
		}
	}
	for c, h := range hidden {
		c.Hidden = h
	}
	for i := 0; i < 8; i++ {
		w.pageW, w.pageH = ui.Layout(w.root, m, 0, 0, 0)
		if over := w.pageW - maxW; over <= 0 || !ui.Shrink(w.root, unpx(over)+1) && !w.stackPairs() {
			break
		}
	}
	// CHK-TRAY-77: stretch every tab to the host width when content is narrower
	// (do not force minW above natural width — that caused HSCROLL at 144 dpi).
	wid := min32(max32(w.pageW, allW), maxW)
	if w.pageW < wid {
		w.pageW, w.pageH = ui.Layout(w.root, m, 0, 0, wid)
	}
	w.fitWrap()
	ui.Walk(w.root, w.placeCtl)
	w.syncPagePanels()
	x, y, ww, wh := settingsRectPx(style, wid, w.pageH+px(setBarH)+px(settingsTabChromeH)+px(gridGap))
	winapi.SetWindowPos(w.hwnd, 0, x, y, ww, wh, winapi.SWP_NOZORDER|winapi.SWP_NOACTIVATE)
}

// show shows or hides a control: a laid out one through ui.Ctl.Hidden
// (relayoutAll applies it), the others (bar, before the layout) at once.
// Hiding also calls ShowWindow(SW_HIDE) immediately so a section switch
// cannot leave the previous panel painted until the next placeCtl pass.
func (w *settingsWindow) show(h uintptr, on bool) {
	if h == 0 {
		return
	}
	if c := w.ctlOf[h]; c != nil {
		c.Hidden = !on
		if !on {
			hideWindowTree(h)
		}
		return
	}
	if on {
		winapi.ShowWindow(h, winapi.SW_SHOW)
		return
	}
	hideWindowTree(h)
}

// relayoutAll lays the host content out again (no holes for hidden
// controls), shows / hides the controls and fits the bar and scrolling.
func (w *settingsWindow) relayoutAll() {
	if w.root == nil {
		return
	}
	syncCues(w.root)
	w.syncNavIcons()
	// another page may be wider: shrink it to the window (no horizontal
	// scrolling, phase 11)
	avail := winapi.GetClientRect(w.hwnd).Right - winapi.GetSystemMetrics(winapi.SM_CXVSCROLL)
	for i := 0; i < 8; i++ {
		w.pageW, w.pageH = ui.Layout(w.root, w.rootM, 0, 0, 0)
		if over := w.pageW - avail; over <= 0 || !ui.Shrink(w.root, unpx(over)+1) && !w.stackPairs() {
			break
		}
	}
	// CHK-TRAY-77: fill host width when content is narrower than the host.
	if w.pageW < avail {
		w.pageW, w.pageH = ui.Layout(w.root, w.rootM, 0, 0, avail)
	}
	w.fitWrap()
	ui.Walk(w.root, w.placeCtl)
	w.syncPagePanels()
	w.hideInactiveTabControls()
	w.layoutFit()
	if w.host != 0 {
		eraseRepaint(w.host) // eraseRedrawFlags incl. RDW_ERASENOW (CHK-TRAY-71)
	}
}

// placeCtl moves a laid out control. Host children (tabs)
// use host coordinates and hostRect scrolling. Controls under a
// pagePanel are positioned in panel-local coords by syncPagePanels
// (panel HWND scrolls as one unit — Conchita child-panel architecture).
func (w *settingsWindow) placeCtl(c *ui.Ctl) {
	if c.H == 0 {
		return
	}
	if !c.Placed || c.Hidden {
		hideWindowTree(c.H)
		return
	}
	r := c.Rect
	h := r.H
	if c.Kind == ui.Combo {
		h += px(comboDrop)
	}
	if w.underPagePanel(c.H) {
		// content coords kept on Ctl.Rect; panel-local MoveWindow in syncPagePanels
		delete(w.hostRect, c.H)
		return
	}
	w.hostRect[c.H] = winapi.RECT{Left: r.X, Top: r.Y, Right: r.X + r.W, Bottom: r.Y + h}
	winapi.MoveWindow(c.H, r.X, r.Y, r.W, h)
	winapi.ShowWindow(c.H, winapi.SW_SHOWNA)
}

// syncPagePanels sizes every tab panel to the shared page slot and
// MoveWindows its children into panel-local coordinates. Inactive
// panels stay SW_HIDE'd (entire tree invisible).
func (w *settingsWindow) syncPagePanels() {
	if w.host == 0 {
		return
	}
	// slot = union of placed controls on the active page (Gap:-1 shared origin)
	var slot winapi.RECT
	any := false
	if n := w.pages[w.page]; n != nil {
		ui.Walk(n, func(c *ui.Ctl) {
			if c == nil || c.H == 0 || !c.Placed || c.Hidden {
				return
			}
			// slot uses layout height only — comboDrop is a MoveWindow quirk of
			// CBS_DROPDOWNLIST, not painted panel content (would blow hostExtent)
			h := c.Rect.H
			l, t, r, b := c.Rect.X, c.Rect.Y, c.Rect.X+c.Rect.W, c.Rect.Y+h
			if !any {
				slot = winapi.RECT{Left: l, Top: t, Right: r, Bottom: b}
				any = true
				return
			}
			if l < slot.Left {
				slot.Left = l
			}
			if t < slot.Top {
				slot.Top = t
			}
			if r > slot.Right {
				slot.Right = r
			}
			if b > slot.Bottom {
				slot.Bottom = b
			}
		})
	}
	if !any {
		// keep previous slot if any; else a tiny placeholder
		if w.pageSlot.Right > w.pageSlot.Left {
			slot = w.pageSlot
		} else {
			slot = winapi.RECT{Right: 10, Bottom: 10}
		}
	}
	w.pageSlot = slot
	pw, ph := slot.Right-slot.Left, slot.Bottom-slot.Top
	if pw < 1 {
		pw = 1
	}
	if ph < 1 {
		ph = 1
	}
	for i, pan := range w.pagePanel {
		if pan == 0 {
			continue
		}
		w.hostRect[pan] = slot
		// Same-tab ghost fix (Alex photo A): after Simple↔Expert the layout
		// Rects for Conn/Login are exclusive, but HWNDs left WS_VISIBLE at
		// stale Y painted over «База данных» / doubled «Пароль». Hide every
		// direct child first, then MoveWindow+Show only Placed && !Hidden.
		// Walk Children(pan) so ctlOf misses cannot leave a visible ghost.
		for _, h := range winapi.Children(pan) {
			winapi.ShowWindow(h, winapi.SW_HIDE)
			delete(w.hostRect, h)
		}
		for _, h := range w.pageCtl[i] {
			// bar Test/… are in pageCtl[0] but parented to the frame, not the panel
			if !w.underPagePanel(h) {
				continue
			}
			c := w.ctlOf[h]
			if c == nil || c.H == 0 {
				hideWindowTree(h)
				continue
			}
			delete(w.hostRect, h)
			if !c.Placed || c.Hidden {
				hideWindowTree(h)
				continue
			}
			hgt := c.Rect.H
			if c.Kind == ui.Combo {
				hgt += px(comboDrop)
			}
			lx, ly := c.Rect.X-slot.Left, c.Rect.Y-slot.Top
			// CHK-TRAY-78: pin left chrome against panel scroll (screen Y fixed).
			if w.pinnedHost[h] && i == 0 {
				lx += w.scrollX
				ly += w.scrollY
			}
			winapi.MoveWindow(h, lx, ly, c.Rect.W, hgt)
			if i == w.page {
				winapi.ShowWindow(h, winapi.SW_SHOWNA)
			}
		}
		if i == w.page {
			winapi.ShowWindow(pan, winapi.SW_SHOWNA)
			eraseRepaint(pan) // eraseRedrawFlags incl. RDW_ERASENOW (CHK-TRAY-71)
		} else {
			hideWindowTree(pan)
		}
	}
	// re-apply scroll so panels sit at slot - scroll
	w.scrollSet = false
	w.scrollTo(w.scrollX, w.scrollY)
}

// fitWrap sets the height of the wrapped text boxes to their text at
// the width of the last layout (an empty box takes almost no room) and
// lays the tree out again when a height changed.
func (w *settingsWindow) fitWrap() {
	changed := false
	for _, c := range w.wrap {
		if c.H == 0 || c.Rect.W <= 0 {
			continue
		}
		hg := int32(1)
		if text := winapi.GetWindowText(c.H); text != "" {
			_, th := winapi.TextSize(winapi.SendMessage(c.H, 0x0031 /* WM_GETFONT */, 0, 0), text, c.Rect.W)
			hg = unpx(th) + 1
		}
		if hg != c.Hgt {
			c.Hgt, changed = hg, true
		}
	}
	if changed {
		w.pageW, w.pageH = ui.Layout(w.root, w.rootM, 0, 0, 0)
	}
}

// stackPairs puts the two number boxes of a thresholds / timeouts row on
// rows of their own (a window still too wide after Shrink); it reports
// whether anything changed.
func (w *settingsWindow) stackPairs() bool {
	if w.checksG == nil || w.stackedRows == nil {
		return false
	}
	w.checksG.Rows, w.stackedRows = w.stackedRows, nil
	if w.tlsG != nil && w.tlsStacked != nil {
		last := len(w.tlsG.Rows) - 1
		w.tlsG.Rows[last].Field, w.tlsStacked = w.tlsStacked, nil
	}
	return true
}
