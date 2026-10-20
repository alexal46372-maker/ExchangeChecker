//go:build windows

package wintray

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

// The test binary has no manifest, so its windows got Common Controls 5:
// no cue banners (EM_SETCUEBANNER needs v6) and classic buttons in the
// screenshots. useComCtl6 activates, on the calling (locked) thread, an
// activation context built from the tray manifest, like the real exe
// (phase 11, CHK-TRAY-47 fix 8: every cue is visible in ui-screens).
var (
	modKernel32Test    = syscall.NewLazyDLL("kernel32.dll")
	procCreateActCtxW  = modKernel32Test.NewProc("CreateActCtxW")
	procActivateActCtx = modKernel32Test.NewProc("ActivateActCtx")
)

type actCtxW struct {
	cbSize                 uint32
	dwFlags                uint32
	lpSource               *uint16
	wProcessorArchitecture uint16
	wLangID                uint16
	lpAssemblyDirectory    *uint16
	lpResourceName         *uint16
	lpApplicationName      *uint16
	hModule                uintptr
}

var actCtx uintptr

// useComCtl6 reports whether the v6 context is active on this thread.
func useComCtl6() bool {
	if procCreateActCtxW.Find() != nil || procActivateActCtx.Find() != nil {
		return false
	}
	if actCtx == 0 {
		path, err := filepath.Abs(filepath.Join("..", "..", "cmd", "exchtray", "exchtray.manifest"))
		if err != nil {
			return false
		}
		src, _ := syscall.UTF16PtrFromString(path)
		ac := actCtxW{lpSource: src}
		ac.cbSize = uint32(unsafe.Sizeof(ac))
		h, _, _ := procCreateActCtxW.Call(uintptr(unsafe.Pointer(&ac)))
		if h == 0 || h == ^uintptr(0) { // INVALID_HANDLE_VALUE
			return false
		}
		actCtx = h
	}
	var cookie uintptr
	r, _, _ := procActivateActCtx.Call(actCtx, uintptr(unsafe.Pointer(&cookie)))
	return r != 0
}
