//go:build windows

package winapi

import (
	"syscall"
	"unsafe"
)

// kernel32 is a KnownDLL and is always mapped, so loading it by name is safe.
var (
	modkernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemDirectoryW      = modkernel32.NewProc("GetSystemDirectoryW")
	procLocalFree                = modkernel32.NewProc("LocalFree")
	procMoveFileExW              = modkernel32.NewProc("MoveFileExW")
	procGetUserDefaultUILanguage = modkernel32.NewProc("GetUserDefaultUILanguage")
)

// systemDirectory returns the System32 directory via GetSystemDirectoryW.
func systemDirectory() (string, error) {
	buf := make([]uint16, 260)
	for {
		n, _, e := callProc(procGetSystemDirectoryW, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 {
			return "", e
		}
		if int(n) < len(buf) {
			return syscall.UTF16ToString(buf[:n]), nil
		}
		buf = make([]uint16, n)
	}
}

// newSystemDLL references a system DLL by absolute System32 path to avoid
// DLL search-order preloading.
func newSystemDLL(name string) *syscall.LazyDLL {
	dir, err := systemDirectory()
	if err != nil || dir == "" {
		return syscall.NewLazyDLL(name)
	}
	return syscall.NewLazyDLL(dir + `\` + name)
}

func localFree(p unsafe.Pointer) {
	if p != nil {
		callProc(procLocalFree, uintptr(p))
	}
}

// callProc resolves p (LazyProc.Find) and calls it. A missing DLL or
// export returns its error instead of panicking in Call (CHK-TRAY-10).
//
// uintptrescapes (like syscall.LazyProc.Call, also variadic): pointers
// converted to uintptr in the argument list of a callProc call are moved
// to the heap and kept alive until callProc returns, so neither a stack
// move (growth inside p.Find) nor the GC can invalidate them.
//
//go:uintptrescapes
func callProc(p *syscall.LazyProc, args ...uintptr) (uintptr, uintptr, error) {
	if err := p.Find(); err != nil {
		return 0, 0, err
	}
	return p.Call(args...)
}
