//go:build windows

package sqlcheck

import (
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// SSRP strings are in the server's ANSI code page ([MC-SQLR] 2.2.5:
// "multibyte character set"); on Windows they are decoded with the
// system ANSI code page (CP_ACP) through MultiByteToWideChar, the only
// WinAPI function of this package (allowlist: sqlcheckWinAPI,
// TestCHK_SQL_06_WinAPIAllowlist). kernel32 is a KnownDLL.
var (
	modKernel32SSRP         = syscall.NewLazyDLL("kernel32.dll")
	procMultiByteToWideChar = modKernel32SSRP.NewProc("MultiByteToWideChar")
)

const cpACP = 0

// ssrpDecode converts SSRP bytes to UTF-8 (ASCII unchanged; invalid
// input is dropped later by ssrpValue).
func ssrpDecode(b []byte) string {
	ascii := true
	for _, c := range b {
		if c >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii || len(b) == 0 {
		return string(b)
	}
	n, _, _ := procMultiByteToWideChar.Call(cpACP, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, 0)
	if n == 0 || n > uintptr(len(b)) {
		return string(b)
	}
	w := make([]uint16, n)
	n, _, _ = procMultiByteToWideChar.Call(cpACP, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&w[0])), n)
	if n == 0 {
		return string(b)
	}
	return string(utf16.Decode(w[:n]))
}
