//go:build windows

package winapi

import (
	"fmt"
	"syscall"
	"unsafe"
)

// FormatMessageW: message tables in the exe's resources (CHK-SVC-02 test).
var procFormatMessageW = modkernel32.NewProc("FormatMessageW")

const (
	formatMessageFromHModule   = 0x00000800
	formatMessageArgumentArray = 0x00002000
)

// FormatModuleMessage formats message id (language 0) from the message
// table of module mod (FORMAT_MESSAGE_FROM_HMODULE |
// FORMAT_MESSAGE_ARGUMENT_ARRAY, inserts expanded from args).
func FormatModuleMessage(mod uintptr, id uint32, args []string) (string, error) {
	ptrs := make([]*uint16, len(args)+1)
	for i, a := range args {
		p, err := syscall.UTF16PtrFromString(a)
		if err != nil {
			return "", err
		}
		ptrs[i] = p
	}
	buf := make([]uint16, 1024)
	n, _, e := callProc(procFormatMessageW, formatMessageFromHModule|formatMessageArgumentArray,
		mod, uintptr(id), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		uintptr(unsafe.Pointer(&ptrs[0])))
	if n == 0 {
		return "", fmt.Errorf("winapi: FormatMessageW(%d): %w", id, e)
	}
	return syscall.UTF16ToString(buf[:n]), nil
}
