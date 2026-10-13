//go:build windows

package winapi

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Tooltips (comctl32 v6 TOOLTIPS_CLASS) and edit cue banners for the tray
// windows (phase 8, CHK-TRAY-15 / CHK-TRAY-12). No new exports: the
// tooltip window is CreateWindowExW + SendMessageW, the class is
// registered by InitCommonControlsEx (already in the CHK-TRAY-10 list).

const (
	TOOLTIPS_CLASS = "tooltips_class32"
	WS_POPUP       = 0x80000000
	TTS_ALWAYSTIP  = 0x01
	TTS_NOPREFIX   = 0x02

	TTF_IDISHWND = 0x0001
	TTF_SUBCLASS = 0x0010

	TTM_SETMAXTIPWIDTH = WM_USER + 24
	TTM_ADDTOOLW       = WM_USER + 50
	TTM_GETTOOLCOUNT   = WM_USER + 13
	TTM_UPDATETIPTEXTW = WM_USER + 57
	TTM_SETDELAYTIME   = WM_USER + 3
	TTDT_AUTOPOP       = 2

	ECM_FIRST       = 0x1500
	EM_SETCUEBANNER = ECM_FIRST + 1
	EM_GETCUEBANNER = ECM_FIRST + 2

	ICC_WIN95_CLASSES = 0x000000FF

	// MessageBoxW of the tray service messages (CHK-TRAY-16)
	MB_OK              = 0x00000000
	MB_ICONINFORMATION = 0x00000040
)

// TOOLINFOW is TTTOOLINFOW (x64: 72 bytes with lpReserved; cbSize is the
// V2 size, 64, see SendToolInfo).
type TOOLINFOW struct {
	Size     uint32
	Flags    uint32
	Hwnd     uintptr
	ID       uintptr
	Rect     RECT
	Inst     uintptr
	Text     *uint16
	LParam   uintptr
	Reserved uintptr
}

// InitTooltips registers the comctl32 v6 tooltip class (and the other
// Win95 classes); true on success.
func InitTooltips() bool {
	icc := initCommonControlsEx{ICC: ICC_WIN95_CLASSES | ICC_STANDARD_CLASSES}
	icc.Size = uint32(unsafe.Sizeof(icc))
	r, _, _ := callProc(procInitCommonControlsEx, uintptr(unsafe.Pointer(&icc)))
	return r != 0
}

// SendToolInfo sends a TTM_* message with a TOOLINFOW for the tool ctrl
// of owner with text (the tooltip copies the text).
func SendToolInfo(tip uintptr, msg uint32, owner, ctrl uintptr, text string) uintptr {
	t := UTF16Ptr(text)
	ti := TOOLINFOW{Flags: TTF_IDISHWND | TTF_SUBCLASS, Hwnd: owner, ID: ctrl, Text: t}
	// TTTOOLINFOW_V2_SIZE (without lpReserved): accepted by comctl32 v6
	// (the tray manifest) and by v5 (test binaries without a manifest)
	ti.Size = uint32(unsafe.Offsetof(ti.Reserved))
	r, _, _ := callProc(procSendMessageW, tip, uintptr(msg), 0, uintptr(unsafe.Pointer(&ti)))
	runtime.KeepAlive(t)
	runtime.KeepAlive(&ti)
	return r
}

// SetCueBanner sets the grey hint of an edit box shown while it is empty
// (also while focused). Needs comctl32 v6 (the tray manifest); false
// without it (e.g. a test binary).
func SetCueBanner(edit uintptr, text string) bool {
	t := UTF16Ptr(text)
	r, _, _ := callProc(procSendMessageW, edit, EM_SETCUEBANNER, 1, uintptr(unsafe.Pointer(t)))
	runtime.KeepAlive(t)
	return r != 0
}

// CueBanner returns the cue banner of an edit box ("" if none).
func CueBanner(edit uintptr) string {
	buf := make([]uint16, 256)
	if r, _, _ := callProc(procSendMessageW, edit, EM_GETCUEBANNER, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
