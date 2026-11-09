//go:build windows

package wintray

import (
	"strings"
	"syscall"

	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// squareSize of the color square in menus (pixels).
const squareSize = 16

// bitmaps caches the colored squares (MENUITEMINFO.hbmpItem, 32-bit
// premultiplied DIB — PLAN §12: no owner-draw).
type bitmaps struct {
	m        map[trayui.Color]uintptr
	checks   map[checkKey]uintptr
	speakers map[speakerKey]uintptr // icons of the volume slider by level and size (CHK-TRAY-96)
}

func (b *bitmaps) get(c trayui.Color) uintptr {
	if c == trayui.ColorNone {
		return 0
	}
	if b.m == nil {
		b.m = map[trayui.Color]uintptr{}
	}
	if h, ok := b.m[c]; ok {
		return h
	}
	h, err := winapi.CreateDIB32(squareSize, squareSize, trayui.SquarePixels(c, squareSize))
	if err != nil {
		return 0
	}
	b.m[c] = h
	return h
}

func (b *bitmaps) free() {
	for _, h := range b.m {
		winapi.DeleteObject(h)
	}
	for _, h := range b.checks {
		winapi.DeleteObject(h)
	}
	for _, h := range b.speakers {
		winapi.DestroyIcon(h)
	}
	b.m, b.checks, b.speakers = nil, nil, nil
}

// menuText escapes '&' (menu mnemonic prefix).
func menuText(s string) string { return strings.ReplaceAll(s, "&", "&&") }

// buildMenu creates a popup menu (with submenus) from items. The caller
// destroys it with winapi.DestroyMenu (submenus go with it).
func buildMenu(items []trayui.Item, bm *bitmaps) (uintptr, error) {
	hm, err := winapi.CreatePopupMenu()
	if err != nil {
		return 0, err
	}
	for i, it := range items {
		var mii winapi.MENUITEMINFOW
		if it.Separator {
			mii.Mask = winapi.MIIM_FTYPE
			mii.Type = winapi.MFT_SEPARATOR
		} else {
			mii.Mask = winapi.MIIM_FTYPE | winapi.MIIM_STRING | winapi.MIIM_ID | winapi.MIIM_STATE
			mii.Type = winapi.MFT_STRING
			mii.ID = uint32(it.ID)
			mii.TypeData = winapi.UTF16Ptr(menuText(it.Text))
			if it.Disabled {
				mii.State = winapi.MFS_DISABLED
			}
			if it.Volume { // owner-draw slider (CHK-TRAY-96)
				mii.Type = winapi.MFT_OWNERDRAW
			}
			if it.Checked && !it.Check {
				mii.State |= winapi.MFS_CHECKED
			}
			h := bm.get(it.Color)
			if it.Check {
				h = bm.check(it.Color, it.Checked)
			}
			if h != 0 {
				mii.Mask |= winapi.MIIM_BITMAP
				mii.BmpItem = h
			}
			if it.Sub != nil {
				sub, err := buildMenu(it.Sub, bm)
				if err != nil {
					winapi.DestroyMenu(hm)
					return 0, err
				}
				mii.Mask |= winapi.MIIM_SUBMENU
				mii.SubMenu = sub
			}
		}
		if err := winapi.InsertMenuItem(hm, uint32(i), &mii); err != nil {
			if mii.SubMenu != 0 {
				winapi.DestroyMenu(mii.SubMenu)
			}
			winapi.DestroyMenu(hm)
			return 0, err
		}
	}
	return hm, nil
}

// trackMenu shows items at the cursor and returns the chosen command.
func trackMenu(owner uintptr, items []trayui.Item, bm *bitmaps) int {
	hm, err := buildMenu(items, bm)
	if err != nil {
		return trayui.CmdNone
	}
	defer winapi.DestroyMenu(hm)
	pt := winapi.GetCursorPos()
	// the owner must be foreground, or the menu does not close when the
	// user clicks elsewhere (KB135788)
	winapi.SetForegroundWindow(owner)
	menuToggle.menu, menuToggle.items, menuToggle.cmd = hm, items, 0
	menuVolume.index, menuVolume.drag, menuVolume.dirty = volumeIndex(items), false, false
	hook, _ := winapi.SetMsgFilterHook(msgFilterCallback)
	cmd := int(winapi.TrackPopupMenuEx(hm, winapi.TPM_RETURNCMD|winapi.TPM_RIGHTBUTTON|winapi.TPM_NONOTIFY, pt.X, pt.Y, owner))
	if hook != 0 {
		winapi.UnhookWindowsHookEx(hook)
	}
	if cmd == 0 {
		cmd = menuToggle.cmd
	}
	if a := menuVolume.app; a != nil {
		a.storeMenuVolume() // closed during a drag: keep the volume
	}
	menuToggle.menu, menuToggle.items, menuToggle.cmd = 0, nil, 0
	menuVolume.index, menuVolume.drag, menuVolume.dirty = -1, false, false
	winapi.PostMessage(owner, winapi.WM_NULL, 0, 0)
	return cmd
}

// checkKey of a database check-mark bitmap.
type checkKey struct {
	c  trayui.Color
	on bool
}

// check returns the bold check mark bitmap (CHK-TRAY-91), cached like the
// squares.
func (b *bitmaps) check(c trayui.Color, on bool) uintptr {
	if b.checks == nil {
		b.checks = map[checkKey]uintptr{}
	}
	k := checkKey{c, on}
	if h, ok := b.checks[k]; ok {
		return h
	}
	h, err := winapi.CreateDIB32(squareSize, squareSize, trayui.CheckPixels(c, on, squareSize))
	if err != nil {
		return 0
	}
	b.checks[k] = h
	return h
}

// menuToggle is the state of the WH_MSGFILTER hook while trackMenu runs
// (UI thread only, one menu at a time): a left click on a top-level item
// with a toggle command (Item.Check with ID != 0, a database whose item
// also opens the points submenu) records the command and closes the menu.
var menuToggle struct {
	menu  uintptr
	items []trayui.Item
	cmd   int
}

var msgFilterCallback = syscall.NewCallback(msgFilterProc)

func msgFilterProc(code int32, wParam, lParam uintptr) uintptr {
	if code == winapi.MSGF_MENU && lParam != 0 && menuToggle.menu != 0 {
		m := winapi.HookMSG(lParam)
		if volumeHook(m) {
			return 1 // the slider drags, the menu stays open (CHK-TRAY-96)
		}
		if m.Message == winapi.WM_LBUTTONUP || m.Message == winapi.WM_LBUTTONDOWN {
			if cmd := toggleAtPoint(winapi.GetCursorPos()); cmd != 0 {
				if m.Message == winapi.WM_LBUTTONUP {
					menuToggle.cmd = cmd
					winapi.EndMenu()
				}
				return 1 // swallowed: the click toggles, it does not close the submenu
			}
		}
	}
	return winapi.CallNextHookEx(code, wParam, lParam)
}

// toggleAtPoint is the toggle command of the top-level item under screen
// point pt, only when the window there is the top-level menu itself: an
// open points submenu over the item (near a screen edge it opens over its
// parent) gets its own clicks (0 = none).
func toggleAtPoint(pt winapi.POINT) int {
	w := winapi.WindowFromPoint(pt)
	if w == 0 || winapi.GetClassName(w) != "#32768" || winapi.SendMessage(w, winapi.MN_GETHMENU, 0, 0) != menuToggle.menu {
		return 0
	}
	return toggleAt(winapi.MenuItemFromPoint(0, menuToggle.menu, pt))
}

// toggleAt is the toggle command of top-level item i (0 = none).
func toggleAt(i int) int {
	if i < 0 || i >= len(menuToggle.items) {
		return 0
	}
	it := menuToggle.items[i]
	if !it.Check || it.ID == 0 || it.Sub == nil {
		return 0
	}
	return it.ID
}
