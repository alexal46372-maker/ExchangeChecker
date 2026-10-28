//go:build windows

package wintray

import "github.com/alexal46372-maker/ExchangeChecker/internal/winapi"

// Phase 10 E (CHK-TRAY-42): a window larger than the work area of the
// monitor is clamped to it and scrolls its whole content (vertical and
// horizontal scroll bars of the window itself). The settings window has
// its own scrolled page host with a pinned button bar (settingsfit).

type scroller struct {
	hwnd   uintptr
	cw, ch int32 // content size, px
	x, y   int32
	rects  map[uintptr]winapi.RECT // unscrolled client rectangles
}

var scrollers = map[uintptr]*scroller{} // hmu

// outerSize is the outer size of a window with a client of cw×ch.
func outerSize(cw, ch int32, style, exStyle uint32) (int32, int32) {
	r := winapi.RECT{Right: cw, Bottom: ch}
	if !winapi.AdjustWindowRectEx(&r, style, exStyle) {
		r = winapi.RECT{Right: cw + 8, Bottom: ch + 32}
	}
	return r.Right - r.Left, r.Bottom - r.Top
}

// fitClient places a window with a client of cw×ch px in the work area;
// when it does not fit, it is clamped and gets scroll bars (scroll).
func fitClient(cw, ch int32, style, exStyle uint32) (x, y, w, h int32, st uint32, scroll bool) {
	work := workArea()
	ow, oh := outerSize(cw, ch, style, exStyle)
	st = style
	if ow > work.Right-work.Left || oh > work.Bottom-work.Top {
		st |= winapi.WS_VSCROLL | winapi.WS_HSCROLL
		ow, oh = outerSize(cw+winapi.GetSystemMetrics(winapi.SM_CXVSCROLL), ch+winapi.GetSystemMetrics(winapi.SM_CYHSCROLL), st, exStyle)
		scroll = true
	}
	x, y, w, h = fitRect(ow, oh, work)
	return x, y, w, h, st, scroll
}

// attachScroller makes hwnd scroll a content of cw×ch px.
func attachScroller(hwnd uintptr, cw, ch int32) {
	s := &scroller{hwnd: hwnd, cw: cw, ch: ch}
	hmu.Lock()
	scrollers[hwnd] = s
	hmu.Unlock()
	hostExtent[hwnd] = [2]int32{cw, ch}
	s.bars()
}

func (s *scroller) view() (int32, int32) {
	r := winapi.GetClientRect(s.hwnd)
	return r.Right - r.Left, r.Bottom - r.Top
}

func (s *scroller) bars() {
	vw, vh := s.view()
	winapi.SetScroll(s.hwnd, winapi.SB_VERT, s.ch, vh, s.y)
	winapi.SetScroll(s.hwnd, winapi.SB_HORZ, s.cw, vw, s.x)
}

// capture records the children at scroll position 0 (first scroll).
func (s *scroller) capture() {
	s.rects = map[uintptr]winapi.RECT{}
	ox, oy := winapi.ClientOrigin(s.hwnd)
	for _, c := range winapi.Children(s.hwnd) {
		r := winapi.GetWindowRect(c)
		s.rects[c] = winapi.RECT{Left: r.Left - ox + s.x, Top: r.Top - oy + s.y, Right: r.Right - ox + s.x, Bottom: r.Bottom - oy + s.y}
	}
}

func (s *scroller) scrollTo(x, y int32) {
	vw, vh := s.view()
	clamp := func(v, view, content int32) int32 {
		if v > content-view {
			v = content - view
		}
		if v < 0 {
			v = 0
		}
		return v
	}
	x, y = clamp(x, vw, s.cw), clamp(y, vh, s.ch)
	if x == s.x && y == s.y {
		return
	}
	if s.rects == nil {
		s.capture()
	}
	s.x, s.y = x, y
	// NoRedraw batch + one eraseRepaint — InvalidateRect alone left Win7
	// smear on every ScrollHost (status, wizard, alarm, discovery, …).
	withSetRedraw(s.hwnd, func() {
		for h, r := range s.rects {
			if winapi.IsWindow(h) {
				winapi.MoveWindowNoRedraw(h, r.Left-x, r.Top-y, r.Right-r.Left, r.Bottom-r.Top)
			}
		}
		s.bars()
	})
}

// handle: the window's own scroll bars and the wheel (a control's
// WM_HSCROLL, e.g. a trackbar, has lp != 0 and is not ours).
func (s *scroller) handle(msg uint32, wp, lp uintptr) bool {
	vw, vh := s.view()
	switch msg {
	case winapi.WM_VSCROLL, winapi.WM_HSCROLL:
		if lp != 0 {
			return false
		}
		vert := msg == winapi.WM_VSCROLL
		pos, page := s.x, vw
		if vert {
			pos, page = s.y, vh
		}
		switch int32(wp & 0xFFFF) {
		case winapi.SB_LINEUP:
			pos -= px(gridRow)
		case winapi.SB_LINEDOWN:
			pos += px(gridRow)
		case winapi.SB_PAGEUP:
			pos -= page
		case winapi.SB_PAGEDOWN:
			pos += page
		case winapi.SB_THUMBTRACK, winapi.SB_THUMBPOSITION:
			pos = int32(wp >> 16 & 0xFFFF)
		case winapi.SB_TOP:
			pos = 0
		case winapi.SB_BOTTOM:
			pos = 1 << 20
		}
		if vert {
			s.scrollTo(s.x, pos)
		} else {
			s.scrollTo(pos, s.y)
		}
		return true
	case winapi.WM_MOUSEWHEEL:
		s.scrollTo(s.x, s.y-int32(int16(wp>>16&0xFFFF))*px(gridRow)*3/120)
		return true
	}
	return false
}
