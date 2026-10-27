//go:build windows

package winapi

import (
	"runtime"
	"unsafe"
)

// SysTabControl32 helpers (comctl32 v6 via tray manifest). No new NewProc:
// CreateWindowExW + SendMessageW only (CHK-TRAY-10). ICC_WIN95_CLASSES
// from InitTooltips registers the class (same pattern as trackbar).

const (
	WC_TABCONTROL = "SysTabControl32"

	TCS_TABS     = 0x0000
	TCS_HOTTRACK = 0x0040

	TCM_FIRST          = 0x1300
	TCM_GETITEMCOUNT   = TCM_FIRST + 4
	TCM_DELETEITEM     = TCM_FIRST + 8
	TCM_DELETEALLITEMS = TCM_FIRST + 9 // not +7 (that is TCM_INSERTITEMA)
	TCM_GETCURSEL      = TCM_FIRST + 11
	TCM_SETCURSEL      = TCM_FIRST + 12
	TCM_INSERTITEMW    = TCM_FIRST + 62

	TCIF_TEXT = 0x0001

	// TCN_FIRST = (0-550); TCN_SELCHANGE = TCN_FIRST-1
	TCN_FIRST     = 0xFFFFFDDA
	TCN_SELCHANGE = TCN_FIRST - 1
)

// TCITEMW is the Unicode tab item (text only).
type TCITEMW struct {
	Mask      uint32
	State     uint32
	StateMask uint32
	Text      *uint16
	TextMax   int32
	Image     int32
	Param     uintptr
}

// TabInsertItem appends a tab with the given title; returns its index or -1.
func TabInsertItem(tab uintptr, title string) int {
	u := UTF16Ptr(title)
	it := TCITEMW{Mask: TCIF_TEXT, Text: u}
	n := int32(SendMessage(tab, TCM_GETITEMCOUNT, 0, 0))
	r := int32(SendMessage(tab, TCM_INSERTITEMW, uintptr(n), uintptr(unsafe.Pointer(&it))))
	runtime.KeepAlive(u)
	runtime.KeepAlive(&it)
	return int(r)
}

// TabDeleteAllItems removes every tab.
func TabDeleteAllItems(tab uintptr) {
	SendMessage(tab, TCM_DELETEALLITEMS, 0, 0)
}

// TabGetCurSel is the selected tab index, or -1.
func TabGetCurSel(tab uintptr) int {
	return int(int32(SendMessage(tab, TCM_GETCURSEL, 0, 0)))
}

// TabSetCurSel selects tab i (may notify TCN_SELCHANGE).
func TabSetCurSel(tab uintptr, i int) {
	SendMessage(tab, TCM_SETCURSEL, uintptr(i), 0)
}

// TabItemCount is the number of tabs.
func TabItemCount(tab uintptr) int {
	return int(int32(SendMessage(tab, TCM_GETITEMCOUNT, 0, 0)))
}
