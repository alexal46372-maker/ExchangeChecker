//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// Phase 11 (CHK-TRAY-47, PLAN §12 p.142): a window is never wider than
// the work area (its wide boxes shrink), the button bar stays pinned to
// the bottom and visible, and only the content scrolls, vertically, in a
// scroll host child when the window is taller than the work area.

// ScrollHostClass is the scrolled content host of fitForm.
const ScrollHostClass = "ExchangeCheckerScrollHost"

// pinnedHosts: the scroll host of a window (created on demand).
var pinnedHosts = map[uintptr]uintptr{}

// fitForm lays content above bar out in hwnd and sizes the window
// (centred; keep: keep the position, e.g. a relayout after a language
// change). It returns the client size.
func fitForm(hwnd uintptr, content, bar ui.Node, minW int32, keep bool) (int32, int32) {
	syncCues(content)
	m := uiMetrics()
	style := winapi.WindowStyle(hwnd) &^ (winapi.WS_VSCROLL | winapi.WS_HSCROLL)
	ex := winapi.WindowExStyle(hwnd)
	work := workArea()
	frameW, frameH := outerSize(0, 0, style, ex)
	maxW, maxH := work.Right-work.Left-frameW, work.Bottom-work.Top-frameH
	vs := winapi.GetSystemMetrics(winapi.SM_CXVSCROLL)
	root := &ui.Col{Gap: -1, Items: []ui.Node{content, bar}}
	var cw, ch int32
	// phase 11 (CHK-TRAY-49): a window that already has a scroll host
	// keeps room for its vertical scroll bar (no horizontal scrolling when
	// a relayout makes the content just fit)
	hadHost := pinnedHosts[hwnd] != 0
	for i := 0; i < 8; i++ {
		cw, ch = ui.Layout(root, m, 0, 0, px(minW))
		limit := maxW
		if ch > maxH || hadHost {
			limit -= vs
		}
		over := cw - limit
		if over <= 0 || !ui.Shrink(root, unpx(over)+1) {
			break
		}
	}
	// drop a scroll bar of an earlier fit / of createWindow
	if winapi.WindowStyle(hwnd)&(winapi.WS_VSCROLL|winapi.WS_HSCROLL) != 0 {
		winapi.SetWindowLong(hwnd, winapi.GWL_STYLE, style)
	}
	hmu.Lock()
	delete(scrollers, hwnd)
	hmu.Unlock()
	delete(hostExtent, hwnd)
	host := pinnedHosts[hwnd]
	if ch <= maxH && host == 0 {
		ui.Walk(root, applyCtl)
		placeWindow(hwnd, cw, ch, style, ex, keep)
		return cw, ch
	}
	if cw > maxW {
		cw = maxW
	}
	_, bh := ui.Layout(bar, m, 0, 0, cw)
	viewH := min32(ch, maxH) - bh
	scroll := ch > maxH || hadHost
	inner := cw
	if scroll {
		inner = min32(cw+vs, maxW) - vs
		cw = inner + vs
	}
	if host == 0 {
		h, err := createWindowPx(ScrollHostClass, "", winapi.WS_EX_CONTROLPARENT,
			winapi.WS_CHILD|winapi.WS_VISIBLE|winapi.WS_VSCROLL|winapi.WS_CLIPCHILDREN|winapi.WS_CLIPSIBLINGS, 0, 0, cw, viewH, hwnd,
			func(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
				switch msg {
				case winapi.WM_ERASEBKGND:
					// page brush, not panel — matches content STATICs (CHK-TRAY-68)
					return contentErase(wp, hw), true
				case winapi.WM_CTLCOLOREDIT, winapi.WM_CTLCOLORSTATIC, winapi.WM_CTLCOLORBTN, winapi.WM_CTLCOLORLISTBOX:
					if r, ok := contentCtlColor(msg, wp); ok {
						return r, true
					}
				case winapi.WM_COMMAND, winapi.WM_NOTIFY, winapi.WM_DRAWITEM:
					return winapi.SendMessage(hwnd, msg, wp, lp), true
				case winapi.WM_HSCROLL:
					if lp != 0 { // a trackbar
						return winapi.SendMessage(hwnd, msg, wp, lp), true
					}
				case winapi.WM_DESTROY:
					delete(pinnedHosts, hwnd)
				}
				return 0, false
			})
		if err != nil {
			ui.Walk(root, applyCtl)
			placeWindow(hwnd, cw, ch, style, ex, keep)
			return cw, ch
		}
		host = h
		pinnedHosts[hwnd] = host
		winapi.SendMessage(host, winapi.WM_SETFONT, uiFont(), 1)
	}
	ui.Walk(content, func(c *ui.Ctl) {
		if c.H != 0 {
			winapi.SetParent(c.H, host)
		}
	})
	_, contentH := ui.Layout(content, m, 0, 0, inner)
	ui.Walk(content, applyCtl)
	ui.Layout(bar, m, 0, viewH, cw)
	ui.Walk(bar, applyCtl)
	winapi.MoveWindow(host, 0, 0, cw, viewH)
	hmu.Lock()
	sc := scrollers[host]
	hmu.Unlock()
	if sc == nil {
		attachScroller(host, inner, contentH)
	} else {
		sc.cw, sc.ch, sc.rects, sc.x, sc.y = inner, contentH, nil, 0, 0
		hostExtent[host] = [2]int32{inner, contentH}
		sc.bars()
	}
	placeWindow(hwnd, cw, viewH+bh, style, ex, keep)
	return cw, viewH + bh
}

// placeWindow sizes hwnd to a client of cw × ch: centred in the work
// area, or at its position (keep) moved back inside the work area.
func placeWindow(hwnd uintptr, cw, ch int32, style, ex uint32, keep bool) {
	ow, oh := outerSize(cw, ch, style, ex)
	work := workArea()
	x, y, w, h := fitRect(ow, oh, work)
	if keep {
		r := winapi.GetWindowRect(hwnd)
		x, y = r.Left, r.Top
		if x+w > work.Right {
			x = work.Right - w
		}
		if y+h > work.Bottom {
			y = work.Bottom - h
		}
		x, y = max32(x, work.Left), max32(y, work.Top)
	}
	winapi.SetWindowPos(hwnd, 0, x, y, w, h, winapi.SWP_NOZORDER|winapi.SWP_NOACTIVATE|winapi.SWP_FRAMECHANGED)
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

// syncCues copies the cue banner of every edit box into its ui.Ctl, so
// that the layout keeps the box at least as wide as the cue (phase 11,
// CHK-TRAY-48).
func syncCues(n ui.Node) {
	ui.Walk(n, func(c *ui.Ctl) {
		if c.H != 0 && (c.Kind == ui.Edit || c.Kind == ui.Box) && winapi.GetClassName(c.H) == "Edit" {
			c.Cue = winapi.CueBanner(c.H)
		}
	})
}
