//go:build windows

package wintray

import (
	"fmt"

	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Volume slider in the tray right menu (phase 13 item 11, CHK-TRAY-96):
// the item is owner-draw (speaker icon, track with thumb, «N %»), the
// WH_MSGFILTER hook of trackMenu drags it — the master volume changes and
// the speaker follows at once while the menu stays open, the release plays
// the short preview cue. Every change repaints the item at once straight
// into the shown menu window (paintVolumeNow): RedrawWindow of the #32768
// window from inside the menu loop does not make the menu send
// WM_DRAWITEM again, so the speaker and the track used to stay stale
// until the menu repainted by itself (CI run 41587440218). Enter on the
// item (keyboard) still opens the trackbar window by the clock
// (CHK-TRAY-35).

// menuVolume is the state of the slider while trackMenu runs (UI thread).
var menuVolume struct {
	app   *App
	index int // top-level index of the Volume item, -1 = none
	drag  bool
}

// volumeItemSize of the owner-draw item (96 dpi units, scaled by px).
const (
	volumeItemW = 220
	volumeItemH = 26
	volumeIconW = 16
)

// volumeTrack is the track [left, right) inside the item rectangle r.
func volumeTrack(r winapi.RECT) (int32, int32) {
	return r.Left + px(8+volumeIconW+10), r.Right - px(52)
}

// speakerIcon returns the cached speaker icon of level l.
func (b *bitmaps) speakerIcon(l trayui.SpeakerLevel) uintptr {
	if b.speakers == nil {
		b.speakers = map[trayui.SpeakerLevel]uintptr{}
	}
	if h, ok := b.speakers[l]; ok {
		return h
	}
	n := int(px(volumeIconW))
	h, err := winapi.CreateIconFromPixels(n, n, trayui.SpeakerPixels(l, n))
	if err != nil {
		return 0
	}
	b.speakers[l] = h
	return h
}

// measureVolumeItem answers WM_MEASUREITEM of the slider.
func measureVolumeItem(lp uintptr) bool {
	m := winapi.ReadMeasureItem(lp)
	if m.CtlType != winapi.ODT_MENU || m.ItemID != uint32(trayui.CmdVolume) {
		return false
	}
	m.ItemWidth, m.ItemHeight = uint32(px(volumeItemW)), uint32(px(volumeItemH))
	winapi.WriteMeasureItem(lp, m)
	return true
}

// drawVolumeItem paints the slider at volume v into r of dc.
func (a *App) drawVolumeItem(dc uintptr, r winapi.RECT, v int, selected bool) {
	bg := winapi.GetSysColor(winapi.COLOR_MENU)
	br := winapi.CreateSolidBrush(bg)
	winapi.FillRect(dc, r, br)
	winapi.DeleteObject(br)
	n := px(volumeIconW)
	cy := (r.Top + r.Bottom) / 2
	if h := a.squares.speakerIcon(trayui.VolumeLevel(v)); h != 0 {
		winapi.DrawIconEx(dc, r.Left+px(8), cy-n/2, h, n, n)
	}
	l, rt := volumeTrack(r)
	fill := l + int32(int64(rt-l-1)*int64(v)/100)
	groove := winapi.CreateSolidBrush(winapi.GetSysColor(winapi.COLOR_BTNSHADOW))
	winapi.FillRect(dc, winapi.RECT{Left: l, Top: cy - px(2), Right: rt, Bottom: cy + px(2)}, groove)
	winapi.DeleteObject(groove)
	hl := winapi.CreateSolidBrush(winapi.GetSysColor(winapi.COLOR_HIGHLIGHT))
	winapi.FillRect(dc, winapi.RECT{Left: l, Top: cy - px(2), Right: fill + 1, Bottom: cy + px(2)}, hl)
	winapi.DeleteObject(hl)
	thumb := winapi.CreateSolidBrush(winapi.GetSysColor(winapi.COLOR_MENUTEXT))
	winapi.FillRect(dc, winapi.RECT{Left: fill - px(3), Top: cy - px(7), Right: fill + px(3), Bottom: cy + px(7)}, thumb)
	if selected { // keyboard/hover focus: a frame line under the item
		winapi.FillRect(dc, winapi.RECT{Left: r.Left + px(2), Top: r.Bottom - px(1), Right: r.Right - px(2), Bottom: r.Bottom}, thumb)
	}
	winapi.DeleteObject(thumb)
	winapi.SetBkMode(dc, winapi.TRANSPARENT)
	winapi.SetTextColor(dc, winapi.GetSysColor(winapi.COLOR_MENUTEXT))
	winapi.DrawText(dc, volumeText(v), winapi.RECT{Left: rt + px(4), Top: r.Top, Right: r.Right - px(6), Bottom: r.Bottom}, winapi.DT_RIGHT|winapi.DT_VCENTER|winapi.DT_SINGLELINE)
}

// volumeText is the value shown right of the track.
func volumeText(v int) string { return fmt.Sprintf("%d %%", v) }

// onDrawItem handles WM_DRAWITEM of the slider.
func (a *App) onDrawItem(lp uintptr) bool {
	d := winapi.ReadDrawItem(lp)
	if d.CtlType != winapi.ODT_MENU || d.ItemID != uint32(trayui.CmdVolume) {
		return false
	}
	a.drawVolume(d)
	return true
}

// drawVolume paints the slider for d at the current master volume and
// records where and what was painted (CHK-TRAY-96 tests).
func (a *App) drawVolume(d winapi.DRAWITEMSTRUCT) {
	a.volumeDraws++
	a.volumeDrawnOn = winapi.WindowFromDC(d.HDC)
	a.volumeDrawnValue = a.soundPrefs.Master
	a.drawVolumeItem(d.HDC, d.RcItem, a.soundPrefs.Master, d.ItemState&winapi.ODS_SELECTED != 0)
}

// volumeMenuWindow is the shown #32768 window of the tracked menu: the
// window of the hook message, else the one under the cursor (0 = none).
func volumeMenuWindow(hw uintptr) uintptr {
	for _, w := range []uintptr{hw, winapi.WindowFromPoint(winapi.GetCursorPos())} {
		if w != 0 && winapi.GetClassName(w) == "#32768" && winapi.SendMessage(w, winapi.MN_GETHMENU, 0, 0) == menuToggle.menu {
			return w
		}
	}
	return 0
}

// paintVolumeNow repaints the slider item (screen rectangle r) into the
// shown menu window mw at once, the same drawing as WM_DRAWITEM; false
// when there is no DC.
func (a *App) paintVolumeNow(mw uintptr, r winapi.RECT) bool {
	dc := winapi.GetDC(mw)
	if dc == 0 {
		return false
	}
	defer winapi.ReleaseDC(mw, dc)
	ox, oy := winapi.ClientOrigin(mw)
	a.drawVolume(winapi.DRAWITEMSTRUCT{
		CtlType: winapi.ODT_MENU, ItemID: uint32(trayui.CmdVolume),
		ItemAction: winapi.ODA_DRAWENTIRE, ItemState: winapi.ODS_SELECTED, HDC: dc,
		RcItem: winapi.RECT{Left: r.Left - ox, Top: r.Top - oy, Right: r.Right - ox, Bottom: r.Bottom - oy},
	})
	return true
}

// volumeIndex is the top-level index of the slider in items (-1 = none).
func volumeIndex(items []trayui.Item) int {
	for i, it := range items {
		if it.Volume {
			return i
		}
	}
	return -1
}

// volumeAtX sets the master volume from the screen x on the item
// rectangle r; true when it changed.
func (a *App) volumeAtX(x int32, r winapi.RECT) bool {
	l, rt := volumeTrack(r)
	v := trayui.VolumeFromX(int(x), int(l), int(rt))
	if v == a.soundPrefs.Master {
		return false
	}
	if err := a.SetMasterVolume(v); err != nil {
		a.logf("tray: sound: save master volume: %v", err)
	}
	return true
}

// volumeHook handles a mouse message of the menu loop on the slider;
// true = swallowed.
func volumeHook(m winapi.MSG) bool {
	a, i := menuVolume.app, menuVolume.index
	if a == nil || i < 0 {
		return false
	}
	switch m.Message {
	case winapi.WM_LBUTTONDOWN:
		if winapi.MenuItemFromPoint(0, menuToggle.menu, winapi.GetCursorPos()) != i {
			return false
		}
		menuVolume.drag = true
	case winapi.WM_MOUSEMOVE, winapi.WM_LBUTTONUP:
		if !menuVolume.drag {
			return m.Message == winapi.WM_LBUTTONUP && winapi.MenuItemFromPoint(0, menuToggle.menu, winapi.GetCursorPos()) == i
		}
	default:
		return false
	}
	if r, ok := winapi.GetMenuItemRect(0, menuToggle.menu, i); ok && a.volumeAtX(winapi.GetCursorPos().X, r) {
		// live: the speaker and the track follow every move, not the release
		if mw := volumeMenuWindow(m.Hwnd); mw == 0 || !a.paintVolumeNow(mw, r) {
			a.logf("tray: volume slider: no menu window to repaint")
		}
	}
	if m.Message == winapi.WM_LBUTTONUP {
		menuVolume.drag = false
		a.volumePreviewCue()
	}
	return true
}
