//go:build windows

package wintray

import "github.com/alexal46372-maker/ExchangeChecker/internal/winapi"

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
	cw, ch := px(setContentW), px(setContentH+setBarH)
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
		if h := w.ctl[b.id]; h != 0 {
			left = append(left, btn{h, px(b.width)})
		}
	}
	for _, b := range w.barRight {
		if h := w.ctl[b.id]; h != 0 {
			right = append(right, btn{h, px(b.width)})
		}
	}
	rightW := int32(0)
	for _, b := range right {
		rightW += b.width + gap
	}
	leftW := m
	for _, b := range left {
		leftW += b.width + gap
	}
	rows := int32(1)
	if leftW > cw-m-rightW {
		rows = 2
	}
	barH := px(setBarH) + (rows-1)*(bh+gap)
	if barH > ch/2 {
		barH = ch / 2
	}
	top := ch - barH
	winapi.MoveWindow(w.ctl[idSetStatus], m, top+gap, cw-2*m, px(40))
	by := top + gap + px(40) + gap
	x := m
	for _, b := range left {
		winapi.MoveWindow(b.h, x, by, b.width, bh)
		x += b.width + gap
	}
	if rows == 2 {
		by += bh + gap
	}
	x = cw - m
	for i := len(right) - 1; i >= 0; i-- {
		x -= right[i].width
		winapi.MoveWindow(right[i].h, x, by, right[i].width, bh)
		x -= gap
	}
	winapi.MoveWindow(w.host, 0, 0, cw, top)
	w.contentW, w.contentH = px(setContentW), px(setContentH)
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
