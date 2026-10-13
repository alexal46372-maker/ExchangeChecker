//go:build windows

package winapi

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// COM for the Start-menu shortcut (phase 7d, CHK-SETUP-08). The exports of
// ole32 go through NewProc + callProc (CHK-TRAY-10). Interface methods are
// called through the object's vtable with syscall.SyscallN — the
// documented exception to the callProc rule (CHECKS.md, CHK-SETUP-08 /
// CHK-SETUP-11): a vtable slot is not a DLL export and cannot be a LazyProc.

var (
	modole32             = newSystemDLL("ole32.dll")
	procCoInitializeEx   = modole32.NewProc("CoInitializeEx")
	procCoUninitialize   = modole32.NewProc("CoUninitialize")
	procCoCreateInstance = modole32.NewProc("CoCreateInstance")
	procCoTaskMemFree    = modole32.NewProc("CoTaskMemFree")
	procOleInitialize    = modole32.NewProc("OleInitialize")
	procOleUninitialize  = modole32.NewProc("OleUninitialize")
)

// GUID is the Win32 GUID.
type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	// CLSID_ShellLink {00021401-0000-0000-C000-000000000046}
	CLSID_ShellLink = GUID{0x00021401, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	// IID_IShellLinkW {000214F9-0000-0000-C000-000000000046}
	IID_IShellLinkW = GUID{0x000214F9, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	// IID_IPersistFile {0000010B-0000-0000-C000-000000000046}
	IID_IPersistFile = GUID{0x0000010B, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	rpcEChangedMode         = 0x80010106
	sFalse                  = 1
)

// vtable slots (docs/PHASE7.md §6.3; verified by TestCHK_SETUP_08_*).
const (
	vtQueryInterface = 0
	vtRelease        = 2

	ShellLinkGetPath         = 3
	ShellLinkSetDescription  = 7
	ShellLinkSetWorkingDir   = 9
	ShellLinkGetArguments    = 10
	ShellLinkSetArguments    = 11
	ShellLinkGetIconLocation = 16
	ShellLinkSetIconLocation = 17
	ShellLinkSetPath         = 20

	PersistFileLoad = 5
	PersistFileSave = 6
)

// comCall invokes vtable slot idx of the COM object obj (exception to
// callProc, see the package comment above).
type comObject struct{ vtbl *[32]uintptr }

// uintptrescapes: callers convert pointers (UTF16Ptr, &GUID, &pf, buffers)
// to uintptr in comCall's own argument list; the directive keeps those
// objects alive and unmoved until comCall returns (as for callProc).
//
//go:uintptrescapes
func comCall(obj *comObject, idx int, args ...uintptr) uintptr {
	fn := obj.vtbl[idx]
	all := append([]uintptr{uintptr(unsafe.Pointer(obj))}, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return r
}

func hrErr(what string, hr uintptr) error {
	if int32(hr) < 0 {
		return fmt.Errorf("%s: HRESULT 0x%08X", what, uint32(hr))
	}
	return nil
}

// WithCOM locks the goroutine to its OS thread, initialises an STA and
// runs fn (CoUninitialize afterwards when this call initialised COM).
func WithCOM(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := callProc(procCoInitializeEx, 0, coinitApartmentThreaded)
	switch {
	case hr == 0 || hr == sFalse:
		defer callProc(procCoUninitialize)
	case uint32(hr) == rpcEChangedMode:
		// already an MTA on this thread: usable for in-proc ShellLink
	default:
		return hrErr("CoInitializeEx", hr)
	}
	return fn()
}

// OleInitialize / OleUninitialize (SHBrowseForFolderW BIF_NEWDIALOGSTYLE).
func OleInitialize() error {
	hr, _, _ := callProc(procOleInitialize, 0)
	return hrErr("OleInitialize", hr)
}

func OleUninitialize() { callProc(procOleUninitialize) }

// CoTaskMemFree frees memory from the shell (PIDLs).
func CoTaskMemFree(p uintptr) { callProc(procCoTaskMemFree, p) }

// ShortcutSpec is the content of a .lnk file.
type ShortcutSpec struct {
	Target, Args, WorkDir, Description, Icon string
	IconIndex                                int32
}

func newShellLink() (sl, pf *comObject, err error) {
	hr, _, _ := callProc(procCoCreateInstance, uintptr(unsafe.Pointer(&CLSID_ShellLink)), 0,
		clsctxInprocServer, uintptr(unsafe.Pointer(&IID_IShellLinkW)), uintptr(unsafe.Pointer(&sl)))
	if err := hrErr("CoCreateInstance(ShellLink)", hr); err != nil {
		return nil, nil, err
	}
	hr = comCall(sl, vtQueryInterface, uintptr(unsafe.Pointer(&IID_IPersistFile)), uintptr(unsafe.Pointer(&pf)))
	if err := hrErr("QueryInterface(IPersistFile)", hr); err != nil {
		comCall(sl, vtRelease)
		return nil, nil, err
	}
	return sl, pf, nil
}

// CreateShortcut writes lnk (IShellLinkW + IPersistFile::Save). The caller
// must run inside WithCOM.
func CreateShortcut(lnk string, s ShortcutSpec) error {
	sl, pf, err := newShellLink()
	if err != nil {
		return err
	}
	defer comCall(sl, vtRelease)
	defer comCall(pf, vtRelease)
	set := func(what string, idx int, v string) error {
		return hrErr(what, comCall(sl, idx, uintptr(unsafe.Pointer(UTF16Ptr(v)))))
	}
	if err := set("SetPath", ShellLinkSetPath, s.Target); err != nil {
		return err
	}
	if err := set("SetArguments", ShellLinkSetArguments, s.Args); err != nil {
		return err
	}
	if err := set("SetWorkingDirectory", ShellLinkSetWorkingDir, s.WorkDir); err != nil {
		return err
	}
	if err := set("SetDescription", ShellLinkSetDescription, s.Description); err != nil {
		return err
	}
	if s.Icon != "" {
		hr := comCall(sl, ShellLinkSetIconLocation, uintptr(unsafe.Pointer(UTF16Ptr(s.Icon))), uintptr(s.IconIndex))
		if err := hrErr("SetIconLocation", hr); err != nil {
			return err
		}
	}
	return hrErr("IPersistFile::Save", comCall(pf, PersistFileSave, uintptr(unsafe.Pointer(UTF16Ptr(lnk))), 1))
}

// ReadShortcut loads lnk and returns path, arguments and icon location
// (CHK-SETUP-08 read-back). The caller must run inside WithCOM.
func ReadShortcut(lnk string) (ShortcutSpec, error) {
	var out ShortcutSpec
	sl, pf, err := newShellLink()
	if err != nil {
		return out, err
	}
	defer comCall(sl, vtRelease)
	defer comCall(pf, vtRelease)
	if err := hrErr("IPersistFile::Load", comCall(pf, PersistFileLoad, uintptr(unsafe.Pointer(UTF16Ptr(lnk))), 0)); err != nil {
		return out, err
	}
	buf := make([]uint16, 1024)
	// GetPath(pszFile, cch, pfd = NULL, SLGP_RAWPATH = 4)
	if err := hrErr("GetPath", comCall(sl, ShellLinkGetPath, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 4)); err != nil {
		return out, err
	}
	out.Target = syscall.UTF16ToString(buf)
	buf = make([]uint16, 1024)
	if err := hrErr("GetArguments", comCall(sl, ShellLinkGetArguments, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))); err != nil {
		return out, err
	}
	out.Args = syscall.UTF16ToString(buf)
	buf = make([]uint16, 1024)
	var idx int32
	if err := hrErr("GetIconLocation", comCall(sl, ShellLinkGetIconLocation, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&idx)))); err != nil {
		return out, err
	}
	out.Icon = syscall.UTF16ToString(buf)
	out.IconIndex = idx
	return out, nil
}
