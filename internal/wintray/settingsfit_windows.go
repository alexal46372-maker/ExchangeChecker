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
		winapi.WS_CHILD|winapi.WS_VISIBLE|winapi.WS_VSCROLL|winapi.WS_HSCROLL|winapi.WS_CLIPCHILDREN, 0, 0, 10, 10, w.hwnd, w.hostProc)
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
	case winapi.WM_CTLCOLOREDIT, winapi.WM_NOTIFY:
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
	for h, r := range w.hostRect {
		winapi.MoveWindow(h, r.Left-x, r.Top-y, r.Right-r.Left, r.Bottom-r.Top)
	}
	winapi.SetScroll(w.host, winapi.SB_VERT, w.contentH, w.viewH, y)
	winapi.SetScroll(w.host, winapi.SB_HORZ, w.contentW, w.viewW, x)
	winapi.InvalidateRect(w.host, true)
}

// ScrollPos is the page scroll position in pixels (test hook).
func (w *settingsWindow) ScrollPos() (int32, int32) { return w.scrollX, w.scrollY }

// scrollIntoView scrolls a page control into the view.
func (w *settingsWindow) scrollIntoView(h uintptr) {
	r, ok := w.hostRect[h]
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
	type btn struct {
		h     uintptr
		width int32
	}
	var left, right []btn
	for _, b := range w.barLeft {
		if h := w.ctl[b.id]; h != 0 && winapi.WindowStyle(h)&winapi.WS_VISIBLE != 0 {
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
	top := ch - barH
	winapi.MoveWindow(w.ctl[idSetStatus], m, top+gap, cw-2*m, px(40))
	by := top + gap + px(40) + gap
	for _, s := range spots {
		winapi.MoveWindow(s.b.h, s.x, by+s.row*(bh+gap), s.b.width, bh)
	}
	x = cw - m
	for i := len(right) - 1; i >= 0; i-- {
		x -= right[i].width
		winapi.MoveWindow(right[i].h, x, by+row*(bh+gap), right[i].width, bh)
		x -= gap
	}
	winapi.MoveWindow(w.host, 0, 0, cw, top)
	w.contentW, w.contentH = px(setContentW), px(setContentH)
	if w.pageW > 0 { // laid out by the framework (phase 11)
		w.contentW, w.contentH = w.pageW, w.pageH
	}
	// the client of the host without its scroll bars
	w.viewW, w.viewH = cw, top
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
	w.scrollSet = false
	w.scrollTo(0, 0)
}

// barButton is a button of the bottom row and its grid width.
type barButton struct {
	id    int32
	width int32
}

// layoutPages lays out the page host (phase 11, CHK-TRAY-45): the profile
// list column on the left and the pages, all in one area as large as the
// largest page, on the right. Visibility stays with setPage /
// applyUIMode; this only places (host coordinates, hostRect for the
// scrolling) and sizes the window to the content.
func (w *settingsWindow) layoutPages(m *ui.Metrics, left ui.Node, pages []ui.Node) {
	var pw, ph int32
	for _, n := range pages {
		x, y := ui.Layout(n, m, 0, 0, 0)
		pw, ph = max32(pw, x), max32(ph, y)
	}
	area := &ui.Ctl{Kind: ui.Box, W: unpx(pw), Hgt: unpx(ph)}
	root := &ui.Pad{Left: ui.P12, Top: ui.P4, Right: ui.P16, Bottom: ui.P8,
		Child: &ui.Row{Align: ui.Top, Gap: ui.P12, Items: []ui.Node{&ui.Pad{Top: 6, Child: left}, area}}}
	w.left, w.leftM, w.leftX, w.leftY = left, m, m.Px(ui.P12), m.Px(ui.P4)+m.Px(6)
	w.pageW, w.pageH = ui.Layout(root, m, 0, 0, 0)
	ui.Walk(root, w.placeCtl)
	for _, n := range pages {
		ui.Layout(n, m, area.Rect.X, area.Rect.Y, area.Rect.W)
		ui.Walk(n, w.placeCtl)
	}
	style := winapi.WindowStyle(w.hwnd)
	x, y, ww, wh := settingsRectPx(style&^(winapi.WS_VSCROLL|winapi.WS_HSCROLL), w.pageW, w.pageH+px(setBarH))
	winapi.SetWindowPos(w.hwnd, 0, x, y, ww, wh, winapi.SWP_NOZORDER|winapi.SWP_NOACTIVATE)
}

// placeCtl moves a laid out page control (host coordinates; combo boxes
// keep their drop-down height) and records it for the scrolling.
func (w *settingsWindow) placeCtl(c *ui.Ctl) {
	if c.H == 0 || !c.Placed {
		return
	}
	r := c.Rect
	h := r.H
	if c.Kind == ui.Combo {
		h += px(comboDrop)
	}
	w.hostRect[c.H] = winapi.RECT{Left: r.X, Top: r.Y, Right: r.X + r.W, Bottom: r.Y + h}
	winapi.MoveWindow(c.H, r.X, r.Y, r.W, h)
}

// relayoutLeft lays the left column out again without its hidden
// controls (phase 11, CHK-TRAY-47: no gaps for the navigation buttons
// hidden in «Простой» / without Viber, nor for the profile list on the
// other pages).
func (w *settingsWindow) relayoutLeft() {
	if w.left == nil {
		return
	}
	ui.Walk(w.left, func(c *ui.Ctl) {
		if c.H != 0 {
			c.Hidden = winapi.WindowStyle(c.H)&winapi.WS_VISIBLE == 0
		}
	})
	ui.Layout(w.left, w.leftM, w.leftX, w.leftY, 0)
	ui.Walk(w.left, w.placeCtl)
	w.scrollSet = false
	w.scrollTo(w.scrollX, w.scrollY)
}
