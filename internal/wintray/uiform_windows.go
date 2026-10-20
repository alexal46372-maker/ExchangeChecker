//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// Phase 11 (CHK-TRAY-45): the Windows side of the layout framework. The
// controls are created at 0, 0; a ui tree places them (font metrics of
// the window font, units scaled by px) and the window takes the size of
// its content (clamped to the work area, scroll bars when larger).

// uiMetrics measures in the window font of the current DPI.
func uiMetrics() *ui.Metrics {
	font := uiFont()
	asc, lh := winapi.FontMetrics(font)
	if lh <= 0 {
		lh, asc = px(15), px(12)
	}
	return &ui.Metrics{Px: px, Ascent: asc, LineH: lh,
		Text: func(s string) (int32, int32) {
			if s == "" {
				return 0, lh
			}
			return winapi.TextSize(font, s, 0)
		}}
}

// comboDrop is the list height added to a combo box on MoveWindow (its
// window height includes the drop-down list).
const comboDrop = 200

// uiCtl wraps a created control; text "" = its window text.
func uiCtl(h uintptr, kind ui.Kind, text string) *ui.Ctl {
	if text == "" && h != 0 && kind != ui.Edit && kind != ui.Box {
		text = winapi.GetWindowText(h)
	}
	return &ui.Ctl{H: h, Kind: kind, Text: text}
}

// applyCtl moves a placed control and shows it; an unplaced one is
// hidden.
func applyCtl(c *ui.Ctl) {
	if c.H == 0 {
		return
	}
	if !c.Placed {
		winapi.ShowWindow(c.H, winapi.SW_HIDE)
		return
	}
	h := c.Rect.H
	if c.Kind == ui.Combo {
		h += px(comboDrop)
	}
	winapi.MoveWindow(c.H, c.Rect.X, c.Rect.Y, c.Rect.W, h)
	winapi.ShowWindow(c.H, winapi.SW_SHOWNA)
}

// newForm lays root out in hwnd (origin 0, 0, at least minW units wide).
func newForm(root ui.Node, minW int32) *ui.Form {
	f := &ui.Form{Root: root, M: uiMetrics(), MinW: px(minW), Apply: applyCtl}
	f.Relayout()
	return f
}

// fitToContent sizes the top-level hwnd to a client of cw × ch pixels
// (centred, clamped to the work area; scroll bars when clamped).
func fitToContent(hwnd uintptr, cw, ch int32) {
	style := winapi.WindowStyle(hwnd)
	ex := winapi.WindowExStyle(hwnd)
	x, y, w, h, st, scroll := fitClient(cw, ch, style&^(winapi.WS_VSCROLL|winapi.WS_HSCROLL), ex)
	if st != style {
		winapi.SetWindowLong(hwnd, winapi.GWL_STYLE, st)
	}
	winapi.SetWindowPos(hwnd, 0, x, y, w, h, winapi.SWP_NOZORDER|winapi.SWP_NOACTIVATE|winapi.SWP_FRAMECHANGED)
	hmu.Lock()
	sc := scrollers[hwnd]
	hmu.Unlock()
	switch {
	case scroll && sc == nil:
		attachScroller(hwnd, cw, ch)
	case sc != nil:
		sc.cw, sc.ch, sc.rects = cw, ch, nil
		sc.x, sc.y = 0, 0
		hostExtent[hwnd] = [2]int32{cw, ch}
		sc.bars()
	}
}
