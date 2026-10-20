//go:build windows

package winapi

import (
	"syscall"
	"unsafe"
)

// Activation contexts (phase 11, CHK-TRAY-47/48): the GUI tests run their
// windows under the tray manifest (Common Controls 6, cue banners). The
// tray exe itself gets the manifest from its resources and needs none.
var (
	modkernel32Sys       = newSystemDLL("kernel32.dll")
	procCreateActCtxW    = modkernel32Sys.NewProc("CreateActCtxW")
	procActivateActCtx   = modkernel32Sys.NewProc("ActivateActCtx")
	procDeactivateActCtx = modkernel32Sys.NewProc("DeactivateActCtx")
	procReleaseActCtx    = modkernel32Sys.NewProc("ReleaseActCtx")
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

// ActivateManifest creates an activation context from the manifest file
// and activates it on the calling (locked) thread. done deactivates and
// releases it; ok is false when the context cannot be made.
func ActivateManifest(path string) (done func(), ok bool) {
	src, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return func() {}, false
	}
	ac := actCtxW{lpSource: src}
	ac.cbSize = uint32(unsafe.Sizeof(ac))
	h, _, _ := callProc(procCreateActCtxW, uintptr(unsafe.Pointer(&ac)))
	if h == 0 || h == ^uintptr(0) { // INVALID_HANDLE_VALUE
		return func() {}, false
	}
	var cookie uintptr
	if r, _, _ := callProc(procActivateActCtx, h, uintptr(unsafe.Pointer(&cookie))); r == 0 {
		callProc(procReleaseActCtx, h)
		return func() {}, false
	}
	return func() {
		callProc(procDeactivateActCtx, 0, cookie)
		callProc(procReleaseActCtx, h)
	}, true
}
