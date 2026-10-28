//go:build windows

package winapi

import (
	"syscall"
	"unsafe"
)

// Phase 9a «Состояние» window (CHK-TRAY-19): report-view ListView with
// per-row colours (NM_CUSTOMDRAW), balloons (CHK-TRAY-18) and the save
// dialog of the export (CHK-TRAY-21).

var (
	// RtlMoveMemory copies the NM_CUSTOMDRAW structure in and out of the
	// lParam address (an integer from the window procedure) without an
	// uintptr -> unsafe.Pointer conversion.
	procRtlMoveMemory = modkernel32.NewProc("RtlMoveMemory")

	modcomdlg32          = newSystemDLL("comdlg32.dll")
	procGetSaveFileNameW = modcomdlg32.NewProc("GetSaveFileNameW")
)

const (
	WC_LISTVIEW = "SysListView32"

	LVS_REPORT        = 0x0001
	LVS_SINGLESEL     = 0x0004
	LVS_SHOWSELALWAYS = 0x0008

	LVM_GETITEMCOUNT             = 0x1004
	LVM_DELETEALLITEMS           = 0x1009
	LVM_SETEXTENDEDLISTVIEWSTYLE = 0x1036
	LVM_INSERTITEMW              = 0x104D
	LVM_INSERTCOLUMNW            = 0x1061
	LVM_GETITEMTEXTW             = 0x1073
	LVM_SETITEMTEXTW             = 0x1074

	LVS_EX_GRIDLINES     = 0x00000001
	LVS_EX_CHECKBOXES    = 0x00000004
	LVS_EX_FULLROWSELECT = 0x00000020

	LVIS_STATEIMAGEMASK = 0xF000
	LVIF_STATE          = 0x0008

	LVM_GETITEMSTATE = 0x102C // LVM_FIRST+44

	LVN_ITEMCHANGED = 0xFFFFFF9B // (uint32)(-101)

	LVCF_FMT   = 0x1
	LVCF_WIDTH = 0x2
	LVCF_TEXT  = 0x4
	LVIF_TEXT  = 0x1

	WM_NOTIFY = 0x004E
	WM_SIZE   = 0x0005

	NM_CUSTOMDRAW       = 0xFFFFFFF4 // (UINT)-12
	CDDS_PREPAINT       = 0x00000001
	CDDS_ITEMPREPAINT   = 0x00010001
	CDRF_DODEFAULT      = 0x00
	CDRF_NEWFONT        = 0x02
	CDRF_NOTIFYITEMDRAW = 0x20

	NIF_INFO     = 0x10
	NIIF_INFO    = 0x1
	NIIF_WARNING = 0x2
	NIIF_ERROR   = 0x3
	NIIF_NOSOUND = 0x10 // the alert sound is played by trayalert

	OFN_OVERWRITEPROMPT = 0x00000002
	OFN_HIDEREADONLY    = 0x00000004
	OFN_NOCHANGEDIR     = 0x00000008
	OFN_PATHMUSTEXIST   = 0x00000800
)

// LVCOLUMNW (Windows 7 layout without cxMin / cxDefault / cxIdeal).
type LVCOLUMNW struct {
	Mask    uint32
	Fmt     int32
	Cx      int32
	Text    *uint16
	TextMax int32
	SubItem int32
	Image   int32
	Order   int32
}

// LVITEMW up to iIndent (enough for LVIF_TEXT).
type LVITEMW struct {
	Mask      uint32
	Item      int32
	SubItem   int32
	State     uint32
	StateMask uint32
	Text      *uint16
	TextMax   int32
	Image     int32
	Param     uintptr
	Indent    int32
}

// NMHDR is the WM_NOTIFY header.
type NMHDR struct {
	From uintptr
	ID   uintptr
	Code uint32
}

// NMLVCUSTOMDRAW up to iSubItem (the part every comctl32 version has).
type NMLVCUSTOMDRAW struct {
	Hdr       NMHDR
	DrawStage uint32
	DC        uintptr
	Rc        RECT
	ItemSpec  uintptr
	ItemState uint32
	ItemParam uintptr
	ClrText   uint32
	ClrTextBk uint32
	SubItem   int32
}

// OPENFILENAMEW (full size, Windows 2000+).
type OPENFILENAMEW struct {
	StructSize    uint32
	Owner         uintptr
	Instance      uintptr
	Filter        *uint16
	CustomFilter  *uint16
	MaxCustFilter uint32
	FilterIndex   uint32
	File          *uint16
	MaxFile       uint32
	FileTitle     *uint16
	MaxFileTitle  uint32
	InitialDir    *uint16
	Title         *uint16
	Flags         uint32
	FileOffset    uint16
	FileExtension uint16
	DefExt        *uint16
	CustData      uintptr
	Hook          uintptr
	TemplateName  *uint16
	Reserved      uintptr
	Reserved2     uint32
	FlagsEx       uint32
}

// ReadNMHDR copies the WM_NOTIFY header at lp.
func ReadNMHDR(lp uintptr) NMHDR {
	var h NMHDR
	if lp != 0 {
		callProc(procRtlMoveMemory, uintptr(unsafe.Pointer(&h)), lp, unsafe.Sizeof(h))
	}
	return h
}

// ReadCustomDraw / WriteCustomDraw copy NMLVCUSTOMDRAW at lp.
func ReadCustomDraw(lp uintptr) NMLVCUSTOMDRAW {
	var cd NMLVCUSTOMDRAW
	if lp != 0 {
		callProc(procRtlMoveMemory, uintptr(unsafe.Pointer(&cd)), lp, unsafe.Sizeof(cd))
	}
	return cd
}

// WriteCustomDrawColors stores clrText / clrTextBk back at lp.
func WriteCustomDrawColors(lp uintptr, text, bk uint32) {
	if lp == 0 {
		return
	}
	c := [2]uint32{text, bk}
	callProc(procRtlMoveMemory, lp+unsafe.Offsetof(NMLVCUSTOMDRAW{}.ClrText), uintptr(unsafe.Pointer(&c)), 8)
}

// ListViewInsertColumn adds a column.
func ListViewInsertColumn(lv uintptr, i int, text string, width int32) bool {
	c := LVCOLUMNW{Mask: LVCF_FMT | LVCF_WIDTH | LVCF_TEXT, Cx: width, Text: UTF16Ptr(text)}
	return int32(callSend(lv, LVM_INSERTCOLUMNW, uintptr(i), uintptr(unsafe.Pointer(&c)))) >= 0
}

// ListViewAddRow appends a row with its cells; returns the index or -1.
func ListViewAddRow(lv uintptr, cells []string) int {
	if len(cells) == 0 {
		return -1
	}
	n := int32(SendMessage(lv, LVM_GETITEMCOUNT, 0, 0))
	it := LVITEMW{Mask: LVIF_TEXT, Item: n, Text: UTF16Ptr(cells[0])}
	idx := int32(callSend(lv, LVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&it))))
	if idx < 0 {
		return -1
	}
	for c := 1; c < len(cells); c++ {
		s := LVITEMW{SubItem: int32(c), Text: UTF16Ptr(cells[c])}
		callSend(lv, LVM_SETITEMTEXTW, uintptr(idx), uintptr(unsafe.Pointer(&s)))
	}
	return int(idx)
}

// ListViewText reads a cell.
func ListViewText(lv uintptr, row, col int) string {
	buf := make([]uint16, 512)
	it := LVITEMW{SubItem: int32(col), Text: &buf[0], TextMax: int32(len(buf))}
	n := callSend(lv, LVM_GETITEMTEXTW, uintptr(row), uintptr(unsafe.Pointer(&it)))
	return syscall.UTF16ToString(buf[:n])
}

func callSend(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := callProc(procSendMessageW, hwnd, uintptr(msg), wp, lp)
	return r
}

// GetSaveFileName shows the modal save dialog; filter is "name\x00*.ext"
// pairs; ok = false on cancel or error.
func GetSaveFileName(owner uintptr, title, filter, defExt, initial string) (string, bool) {
	buf := make([]uint16, 1024)
	if u, err := syscall.UTF16FromString(initial); err == nil && len(u) < len(buf) {
		copy(buf, u)
	}
	f := utf16Multi(filter) // "a\0b\0...\0\0"
	o := OPENFILENAMEW{Owner: owner, Filter: &f[0], FilterIndex: 1, File: &buf[0], MaxFile: uint32(len(buf)),
		Title: UTF16Ptr(title), DefExt: UTF16Ptr(defExt),
		Flags: OFN_OVERWRITEPROMPT | OFN_HIDEREADONLY | OFN_NOCHANGEDIR | OFN_PATHMUSTEXIST}
	o.StructSize = uint32(unsafe.Sizeof(o))
	r, _, _ := callProc(procGetSaveFileNameW, uintptr(unsafe.Pointer(&o)))
	if r == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

func utf16Multi(s string) []uint16 {
	var out []uint16
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == 0 {
			u, _ := syscall.UTF16FromString(s[start:i])
			out = append(out, u...) // includes the terminating NUL
			start = i + 1
		}
	}
	return append(out, 0)
}

// LVM_SETCOLUMNWIDTH sets the width of a report column (phase 11).
const LVM_SETCOLUMNWIDTH = 0x101E

// ListViewSetColumnWidth sets the width (pixels) of column i.
func ListViewSetColumnWidth(lv uintptr, i int, width int32) {
	SendMessage(lv, LVM_SETCOLUMNWIDTH, uintptr(i), uintptr(width))
}

// NMLISTVIEW is the LVN_ITEMCHANGED payload (subset used by settings).
type NMLISTVIEW struct {
	Hdr       NMHDR
	Item      int32
	SubItem   int32
	NewState  uint32
	OldState  uint32
	Changed   uint32
	PtAction  POINT
	Param     uintptr
}

// POINT is a Win32 POINT.
type POINT struct{ X, Y int32 }

// ReadNMLISTVIEW copies NMLISTVIEW at lp.
func ReadNMLISTVIEW(lp uintptr) NMLISTVIEW {
	var n NMLISTVIEW
	if lp != 0 {
		callProc(procRtlMoveMemory, uintptr(unsafe.Pointer(&n)), lp, unsafe.Sizeof(n))
	}
	return n
}

// indexToStateImage packs a state-image index into LVIS_STATEIMAGEMASK.
func indexToStateImage(i int) uint32 { return uint32(i) << 12 }

// ListViewSetChecked sets the LVS_EX_CHECKBOXES mark on row i.
func ListViewSetChecked(lv uintptr, i int, on bool) {
	img := 1 // unchecked
	if on {
		img = 2
	}
	it := LVITEMW{State: indexToStateImage(img), StateMask: LVIS_STATEIMAGEMASK}
	callSend(lv, LVM_SETITEMSTATE, uintptr(i), uintptr(unsafe.Pointer(&it)))
}

// ListViewChecked reports the LVS_EX_CHECKBOXES mark on row i.
func ListViewChecked(lv uintptr, i int) bool {
	st := uint32(SendMessage(lv, LVM_GETITEMSTATE, uintptr(i), LVIS_STATEIMAGEMASK))
	return (st >> 12) == 2
}
