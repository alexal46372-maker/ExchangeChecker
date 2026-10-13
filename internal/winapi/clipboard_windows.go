package winapi

import (
	"unicode/utf16"
	"unsafe"
)

// Clipboard (phase 9b part 2, CHK-TRAY-32: «Скопировать SQL-скрипт
// прав»). All of it is NT 5.0 API.
var (
	procOpenClipboard    = moduser32.NewProc("OpenClipboard")
	procEmptyClipboard   = moduser32.NewProc("EmptyClipboard")
	procSetClipboardData = moduser32.NewProc("SetClipboardData")
	procCloseClipboard   = moduser32.NewProc("CloseClipboard")
	procGlobalAlloc      = modkernel32.NewProc("GlobalAlloc")
	procGlobalLock       = modkernel32.NewProc("GlobalLock")
	procGlobalUnlock     = modkernel32.NewProc("GlobalUnlock")
	procGlobalFree       = modkernel32.NewProc("GlobalFree")
)

// Clipboard constants.
const (
	CF_UNICODETEXT = 13
	GMEM_MOVEABLE  = 0x0002
)

// SetClipboardText puts s on the clipboard as CF_UNICODETEXT (owner: the
// window that opens it). The memory goes to the clipboard on success and
// is freed otherwise; the UTF-16 copy of s is wiped.
func SetClipboardText(owner uintptr, s string) error {
	u := append(utf16.Encode([]rune(s)), 0)
	defer ZeroUTF16(u)
	if r, _, e := callProc(procOpenClipboard, owner); r == 0 {
		return lastErr(e)
	}
	defer callProc(procCloseClipboard)
	if r, _, e := callProc(procEmptyClipboard); r == 0 {
		return lastErr(e)
	}
	n := uintptr(len(u)) * 2
	h, _, e := callProc(procGlobalAlloc, GMEM_MOVEABLE, n)
	if h == 0 {
		return lastErr(e)
	}
	p, _, e := callProc(procGlobalLock, h)
	if p == 0 {
		callProc(procGlobalFree, h)
		return lastErr(e)
	}
	callProc(procRtlMoveMemory, p, uintptr(unsafe.Pointer(&u[0])), n)
	callProc(procGlobalUnlock, h)
	if r, _, e := callProc(procSetClipboardData, CF_UNICODETEXT, h); r == 0 {
		callProc(procGlobalFree, h)
		return lastErr(e)
	}
	return nil
}
