//go:build windows

package winapi

import (
	"syscall"
	"unsafe"
)

// Win32 GUI wrappers for the tray (phase 6a, CHK-TRAY-10): user32, gdi32,
// shell32. Every function is available on Windows 7 / Server 2008 R2
// (NT 6.1); each call goes through callProc, which resolves the export
// with LazyProc.Find first, so a missing export is an error, not a panic.

var (
	moduser32  = newSystemDLL("user32.dll")
	modgdi32   = newSystemDLL("gdi32.dll")
	modshell32 = newSystemDLL("shell32.dll")

	procRegisterClassExW       = moduser32.NewProc("RegisterClassExW")
	procUnregisterClassW       = moduser32.NewProc("UnregisterClassW")
	procCreateWindowExW        = moduser32.NewProc("CreateWindowExW")
	procDefWindowProcW         = moduser32.NewProc("DefWindowProcW")
	procDestroyWindow          = moduser32.NewProc("DestroyWindow")
	procIsWindow               = moduser32.NewProc("IsWindow")
	procShowWindow             = moduser32.NewProc("ShowWindow")
	procGetMessageW            = moduser32.NewProc("GetMessageW")
	procPeekMessageW           = moduser32.NewProc("PeekMessageW")
	procTranslateMessage       = moduser32.NewProc("TranslateMessage")
	procDispatchMessageW       = moduser32.NewProc("DispatchMessageW")
	procPostMessageW           = moduser32.NewProc("PostMessageW")
	procPostQuitMessage        = moduser32.NewProc("PostQuitMessage")
	procSendMessageW           = moduser32.NewProc("SendMessageW")
	procCreatePopupMenu        = moduser32.NewProc("CreatePopupMenu")
	procInsertMenuItemW        = moduser32.NewProc("InsertMenuItemW")
	procDestroyMenu            = moduser32.NewProc("DestroyMenu")
	procTrackPopupMenuEx       = moduser32.NewProc("TrackPopupMenuEx")
	procGetMenuItemCount       = moduser32.NewProc("GetMenuItemCount")
	procGetMenuItemInfoW       = moduser32.NewProc("GetMenuItemInfoW")
	procGetSubMenu             = moduser32.NewProc("GetSubMenu")
	procSetForegroundWindow    = moduser32.NewProc("SetForegroundWindow")
	procGetCursorPos           = moduser32.NewProc("GetCursorPos")
	procRegisterWindowMessageW = moduser32.NewProc("RegisterWindowMessageW")
	procGetDlgItem             = moduser32.NewProc("GetDlgItem")
	procGetWindowTextW         = moduser32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW   = moduser32.NewProc("GetWindowTextLengthW")
	procFindWindowW            = moduser32.NewProc("FindWindowW")
	procGetClassNameW          = moduser32.NewProc("GetClassNameW")
	procLoadCursorW            = moduser32.NewProc("LoadCursorW")
	procCreateIconIndirect     = moduser32.NewProc("CreateIconIndirect")
	procDestroyIcon            = moduser32.NewProc("DestroyIcon")
	procGetSystemMetrics       = moduser32.NewProc("GetSystemMetrics")

	procCreateDIBSection = modgdi32.NewProc("CreateDIBSection")
	procCreateBitmap     = modgdi32.NewProc("CreateBitmap")
	procDeleteObject     = modgdi32.NewProc("DeleteObject")
	procGetStockObject   = modgdi32.NewProc("GetStockObject")

	procShellNotifyIconW = modshell32.NewProc("Shell_NotifyIconW")

	procGetModuleHandleW = modkernel32.NewProc("GetModuleHandleW")
	procCreateMutexW     = modkernel32.NewProc("CreateMutexW")
)

// lastErr returns e unless it is the "success" errno.
func lastErr(e error) error {
	if en, ok := e.(syscall.Errno); ok && en == 0 {
		return syscall.EINVAL
	}
	return e
}

// Window messages and constants used by the tray.
const (
	WM_NULL        = 0x0000
	WM_CREATE      = 0x0001
	WM_DESTROY     = 0x0002
	WM_CLOSE       = 0x0010
	WM_QUIT        = 0x0012
	WM_SETFONT     = 0x0030
	WM_CONTEXTMENU = 0x007B
	WM_COMMAND     = 0x0111
	WM_LBUTTONUP   = 0x0202
	WM_RBUTTONUP   = 0x0205
	WM_USER        = 0x0400
	WM_APP         = 0x8000

	WS_OVERLAPPED       = 0x00000000
	WS_CAPTION          = 0x00C00000
	WS_SYSMENU          = 0x00080000
	WS_CHILD            = 0x40000000
	WS_VISIBLE          = 0x10000000
	WS_TABSTOP          = 0x00010000
	WS_EX_TOPMOST       = 0x00000008
	WS_EX_DLGMODALFRAME = 0x00000001

	BS_DEFPUSHBUTTON = 0x00000001
	SS_LEFT          = 0x00000000
	BN_CLICKED       = 0
	IDOK             = 1

	SW_HIDE = 0
	SW_SHOW = 5

	PM_REMOVE = 0x0001

	MIIM_STATE    = 0x00000001
	MIIM_ID       = 0x00000002
	MIIM_SUBMENU  = 0x00000004
	MIIM_STRING   = 0x00000040
	MIIM_BITMAP   = 0x00000080
	MIIM_FTYPE    = 0x00000100
	MFT_STRING    = 0x00000000
	MFT_SEPARATOR = 0x00000800
	MFS_ENABLED   = 0x00000000
	MFS_DISABLED  = 0x00000003

	TPM_RIGHTBUTTON = 0x0002
	TPM_RETURNCMD   = 0x0100
	TPM_NONOTIFY    = 0x0080

	IDC_ARROW = 32512

	DEFAULT_GUI_FONT = 17

	SM_CXSCREEN = 0
	SM_CYSCREEN = 1

	ERROR_ALREADY_EXISTS = 183
)

// POINT is the Win32 POINT.
type POINT struct{ X, Y int32 }

// MSG is the Win32 MSG.
type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
	Private uint32
}

// WNDCLASSEXW is the Win32 WNDCLASSEXW.
type WNDCLASSEXW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

// MENUITEMINFOW is the Win32 MENUITEMINFOW (with hbmpItem, Win2000+).
type MENUITEMINFOW struct {
	Size         uint32
	Mask         uint32
	Type         uint32
	State        uint32
	ID           uint32
	SubMenu      uintptr
	BmpChecked   uintptr
	BmpUnchecked uintptr
	ItemData     uintptr
	TypeData     *uint16
	Cch          uint32
	BmpItem      uintptr
}

// NOTIFYICONDATAW is the full (Vista+) NOTIFYICONDATAW.
type NOTIFYICONDATAW struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32 // union with uTimeout
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        [16]byte
	BalloonIcon     uintptr
}

// Shell_NotifyIcon messages / flags.
const (
	NIM_ADD        = 0x0
	NIM_MODIFY     = 0x1
	NIM_DELETE     = 0x2
	NIM_SETVERSION = 0x4

	NIF_MESSAGE = 0x01
	NIF_ICON    = 0x02
	NIF_TIP     = 0x04
	NIF_SHOWTIP = 0x80

	NOTIFYICON_VERSION_4 = 4

	NIN_SELECT    = WM_USER + 0
	NIN_KEYSELECT = WM_USER + 1
)

// BITMAPINFOHEADER is the Win32 BITMAPINFOHEADER.
type BITMAPINFOHEADER struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// ICONINFO is the Win32 ICONINFO.
type ICONINFO struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	Mask     uintptr
	Color    uintptr
}

// UTF16Ptr converts s (no NUL inside expected) to a NUL-terminated UTF-16
// pointer; an embedded NUL truncates.
func UTF16Ptr(s string) *uint16 {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			s = s[:i]
			break
		}
	}
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

// GetModuleHandle(NULL) — the instance of the exe.
func GetModuleHandle() uintptr {
	r, _, _ := callProc(procGetModuleHandleW, 0)
	return r
}

// RegisterClassEx registers a window class; returns the atom.
func RegisterClassEx(wc *WNDCLASSEXW) (uint16, error) {
	wc.Size = uint32(unsafe.Sizeof(*wc))
	r, _, e := callProc(procRegisterClassExW, uintptr(unsafe.Pointer(wc)))
	if r == 0 {
		return 0, lastErr(e)
	}
	return uint16(r), nil
}

// UnregisterClass unregisters a window class.
func UnregisterClass(name string, inst uintptr) {
	callProc(procUnregisterClassW, uintptr(unsafe.Pointer(UTF16Ptr(name))), inst)
}

// CreateWindowEx creates a window.
func CreateWindowEx(exStyle uint32, class, title string, style uint32, x, y, w, h int32, parent, menu, inst uintptr) (uintptr, error) {
	r, _, e := callProc(procCreateWindowExW, uintptr(exStyle),
		uintptr(unsafe.Pointer(UTF16Ptr(class))), uintptr(unsafe.Pointer(UTF16Ptr(title))),
		uintptr(style), uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, menu, inst, 0)
	if r == 0 {
		return 0, lastErr(e)
	}
	return r, nil
}

// DefWindowProc calls DefWindowProcW.
func DefWindowProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := callProc(procDefWindowProcW, hwnd, uintptr(msg), wp, lp)
	return r
}

// DestroyWindow destroys hwnd.
func DestroyWindow(hwnd uintptr) bool {
	r, _, _ := callProc(procDestroyWindow, hwnd)
	return r != 0
}

// IsWindow reports whether hwnd is an existing window.
func IsWindow(hwnd uintptr) bool {
	r, _, _ := callProc(procIsWindow, hwnd)
	return r != 0
}

// ShowWindow calls ShowWindow.
func ShowWindow(hwnd uintptr, cmd int32) {
	callProc(procShowWindow, hwnd, uintptr(cmd))
}

// GetMessage calls GetMessageW; returns >0 message, 0 WM_QUIT, <0 error.
func GetMessage(m *MSG) int32 {
	r, _, _ := callProc(procGetMessageW, uintptr(unsafe.Pointer(m)), 0, 0, 0)
	return int32(r)
}

// PeekMessage calls PeekMessageW with PM_REMOVE.
func PeekMessage(m *MSG) bool {
	r, _, _ := callProc(procPeekMessageW, uintptr(unsafe.Pointer(m)), 0, 0, 0, PM_REMOVE)
	return r != 0
}

// TranslateMessage / DispatchMessage.
func TranslateMessage(m *MSG) { callProc(procTranslateMessage, uintptr(unsafe.Pointer(m))) }

// DispatchMessage dispatches m to its window procedure.
func DispatchMessage(m *MSG) { callProc(procDispatchMessageW, uintptr(unsafe.Pointer(m))) }

// PostMessage posts a message (any thread).
func PostMessage(hwnd uintptr, msg uint32, wp, lp uintptr) bool {
	r, _, _ := callProc(procPostMessageW, hwnd, uintptr(msg), wp, lp)
	return r != 0
}

// PostQuitMessage posts WM_QUIT to the calling thread.
func PostQuitMessage(code int32) { callProc(procPostQuitMessage, uintptr(code)) }

// SendMessage sends a message and waits for the result.
func SendMessage(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := callProc(procSendMessageW, hwnd, uintptr(msg), wp, lp)
	return r
}

// CreatePopupMenu creates an empty popup menu.
func CreatePopupMenu() (uintptr, error) {
	r, _, e := callProc(procCreatePopupMenu)
	if r == 0 {
		return 0, lastErr(e)
	}
	return r, nil
}

// InsertMenuItem appends mii at position pos (by position).
func InsertMenuItem(menu uintptr, pos uint32, mii *MENUITEMINFOW) error {
	mii.Size = uint32(unsafe.Sizeof(*mii))
	r, _, e := callProc(procInsertMenuItemW, menu, uintptr(pos), 1, uintptr(unsafe.Pointer(mii)))
	if r == 0 {
		return lastErr(e)
	}
	return nil
}

// DestroyMenu destroys a menu and its submenus.
func DestroyMenu(menu uintptr) { callProc(procDestroyMenu, menu) }

// TrackPopupMenuEx shows a popup menu; with TPM_RETURNCMD it returns the
// chosen command ID (0 = none).
func TrackPopupMenuEx(menu uintptr, flags uint32, x, y int32, hwnd uintptr) uint32 {
	r, _, _ := callProc(procTrackPopupMenuEx, menu, uintptr(flags), uintptr(x), uintptr(y), hwnd, 0)
	return uint32(r)
}

// GetMenuItemCount returns the number of items (-1 on error).
func GetMenuItemCount(menu uintptr) int {
	r, _, _ := callProc(procGetMenuItemCount, menu)
	return int(int32(r))
}

// GetMenuItemInfo reads item pos (by position) into mii (Mask set by the
// caller; for MIIM_STRING TypeData/Cch must describe a buffer).
func GetMenuItemInfo(menu uintptr, pos uint32, mii *MENUITEMINFOW) error {
	mii.Size = uint32(unsafe.Sizeof(*mii))
	r, _, e := callProc(procGetMenuItemInfoW, menu, uintptr(pos), 1, uintptr(unsafe.Pointer(mii)))
	if r == 0 {
		return lastErr(e)
	}
	return nil
}

// GetSubMenu returns the submenu at pos (0 = none).
func GetSubMenu(menu uintptr, pos int32) uintptr {
	r, _, _ := callProc(procGetSubMenu, menu, uintptr(pos))
	return r
}

// SetForegroundWindow calls SetForegroundWindow.
func SetForegroundWindow(hwnd uintptr) { callProc(procSetForegroundWindow, hwnd) }

// GetCursorPos returns the cursor position.
func GetCursorPos() POINT {
	var p POINT
	callProc(procGetCursorPos, uintptr(unsafe.Pointer(&p)))
	return p
}

// RegisterWindowMessage returns the message ID for name (0 on error).
func RegisterWindowMessage(name string) uint32 {
	r, _, _ := callProc(procRegisterWindowMessageW, uintptr(unsafe.Pointer(UTF16Ptr(name))))
	return uint32(r)
}

// GetDlgItem returns the child window with control ID id.
func GetDlgItem(hwnd uintptr, id int32) uintptr {
	r, _, _ := callProc(procGetDlgItem, hwnd, uintptr(id))
	return r
}

// GetWindowText returns the text of hwnd.
func GetWindowText(hwnd uintptr) string {
	n, _, _ := callProc(procGetWindowTextLengthW, hwnd)
	buf := make([]uint16, n+1)
	callProc(procGetWindowTextW, hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

// GetWindowTextLength returns the length of the text of hwnd in UTF-16
// units, without copying the text.
func GetWindowTextLength(hwnd uintptr) int {
	n, _, _ := callProc(procGetWindowTextLengthW, hwnd)
	return int(n)
}

// GetWindowTextUTF16 copies the text of hwnd into a new []uint16 (without
// the terminating zero) so a secret can be wiped by the caller with
// ZeroUTF16 after use.
func GetWindowTextUTF16(hwnd uintptr) []uint16 {
	n, _, _ := callProc(procGetWindowTextLengthW, hwnd)
	buf := make([]uint16, n+1)
	r, _, _ := callProc(procGetWindowTextW, hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if int(r) < len(buf) {
		return buf[:r]
	}
	return buf[:n]
}

// ZeroUTF16 overwrites b with zeros.
func ZeroUTF16(b []uint16) {
	for i := range b {
		b[i] = 0
	}
}

// GetClassName returns the window class name of hwnd ("" on failure).
func GetClassName(hwnd uintptr) string {
	buf := make([]uint16, 257) // class names are at most 256 characters
	n, _, _ := callProc(procGetClassNameW, hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

// FindWindow finds a top-level window by class name (0 = not found).
func FindWindow(class string) uintptr {
	r, _, _ := callProc(procFindWindowW, uintptr(unsafe.Pointer(UTF16Ptr(class))), 0)
	return r
}

// LoadCursor loads a system cursor (IDC_*).
func LoadCursor(id uintptr) uintptr {
	r, _, _ := callProc(procLoadCursorW, 0, id)
	return r
}

// GetSystemMetrics calls GetSystemMetrics.
func GetSystemMetrics(i int32) int32 {
	r, _, _ := callProc(procGetSystemMetrics, uintptr(i))
	return int32(r)
}

// GetStockObject returns a stock GDI object.
func GetStockObject(i int32) uintptr {
	r, _, _ := callProc(procGetStockObject, uintptr(i))
	return r
}

// DeleteObject deletes a GDI object.
func DeleteObject(h uintptr) { callProc(procDeleteObject, h) }

// CreateDIB32 creates a top-down 32-bit DIB section of w×h with the
// given premultiplied BGRA pixels (len w*h).
func CreateDIB32(w, h int, pixels []uint32) (uintptr, error) {
	bi := BITMAPINFOHEADER{Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	bi.Size = uint32(unsafe.Sizeof(bi))
	var bits unsafe.Pointer
	r, _, e := callProc(procCreateDIBSection, 0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if r == 0 || bits == nil {
		return 0, lastErr(e)
	}
	dst := unsafe.Slice((*uint32)(bits), w*h)
	copy(dst, pixels)
	return r, nil
}

// CreateIconFromPixels creates an HICON (w×h, 32-bit with alpha).
func CreateIconFromPixels(w, h int, pixels []uint32) (uintptr, error) {
	color, err := CreateDIB32(w, h, pixels)
	if err != nil {
		return 0, err
	}
	defer DeleteObject(color)
	maskBits := make([]byte, ((w+15)/16*2)*h) // all zero: alpha decides
	mask, _, e := callProc(procCreateBitmap, uintptr(w), uintptr(h), 1, 1, uintptr(unsafe.Pointer(&maskBits[0])))
	if mask == 0 {
		return 0, lastErr(e)
	}
	defer DeleteObject(mask)
	ii := ICONINFO{FIcon: 1, Mask: mask, Color: color}
	r, _, e := callProc(procCreateIconIndirect, uintptr(unsafe.Pointer(&ii)))
	if r == 0 {
		return 0, lastErr(e)
	}
	return r, nil
}

// DestroyIcon destroys an HICON.
func DestroyIcon(h uintptr) { callProc(procDestroyIcon, h) }

// ShellNotifyIcon calls Shell_NotifyIconW.
func ShellNotifyIcon(msg uint32, nid *NOTIFYICONDATAW) error {
	nid.Size = uint32(unsafe.Sizeof(*nid))
	r, _, e := callProc(procShellNotifyIconW, uintptr(msg), uintptr(unsafe.Pointer(nid)))
	if r == 0 {
		return lastErr(e)
	}
	return nil
}

// CreateMutex creates (or opens) a named mutex; existed reports whether
// it already existed (ERROR_ALREADY_EXISTS).
func CreateMutex(name string) (h syscall.Handle, existed bool, err error) {
	r, _, e := callProc(procCreateMutexW, 0, 0, uintptr(unsafe.Pointer(UTF16Ptr(name))))
	if r == 0 {
		return 0, false, lastErr(e)
	}
	if en, ok := e.(syscall.Errno); ok && en == ERROR_ALREADY_EXISTS {
		existed = true
	}
	return syscall.Handle(r), existed, nil
}
