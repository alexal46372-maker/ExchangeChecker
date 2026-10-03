//go:build windows

package wintray

import (
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// squareSize of the color square in menus (pixels).
const squareSize = 16

// bitmaps caches the colored squares (MENUITEMINFO.hbmpItem, 32-bit
// premultiplied DIB — PLAN §12: no owner-draw).
type bitmaps struct{ m map[trayui.Color]uintptr }

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
	b.m = nil
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
			if h := bm.get(it.Color); h != 0 {
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
	cmd := winapi.TrackPopupMenuEx(hm, winapi.TPM_RETURNCMD|winapi.TPM_RIGHTBUTTON|winapi.TPM_NONOTIFY, pt.X, pt.Y, owner)
	winapi.PostMessage(owner, winapi.WM_NULL, 0, 0)
	return int(cmd)
}
