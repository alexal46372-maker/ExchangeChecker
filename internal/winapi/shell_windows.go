//go:build windows

package winapi

import (
	"syscall"
	"unsafe"
)

// Shell folders, browse-for-folder, change notification and common
// controls for the setup wizard (phase 7d, docs/PHASE7.md §1.3, §6.3).
// All exist since Windows 2000 (NT 5.0).

var (
	procSHGetFolderPathW     = modshell32.NewProc("SHGetFolderPathW")
	procSHChangeNotify       = modshell32.NewProc("SHChangeNotify")
	procSHBrowseForFolderW   = modshell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW = modshell32.NewProc("SHGetPathFromIDListW")

	modcomctl32              = newSystemDLL("comctl32.dll")
	procInitCommonControlsEx = modcomctl32.NewProc("InitCommonControlsEx")
)

const (
	CSIDL_PROGRAM_FILES   = 0x0026
	CSIDL_COMMON_PROGRAMS = 0x0017

	SHCNE_ASSOCCHANGED = 0x08000000
	SHCNE_UPDATEDIR    = 0x00001000
	SHCNF_IDLIST       = 0x0000
	SHCNF_PATHW        = 0x0005

	BIF_RETURNONLYFSDIRS = 0x0001
	BIF_EDITBOX          = 0x0010
	BIF_VALIDATE         = 0x0020
	BIF_NEWDIALOGSTYLE   = 0x0040

	ICC_PROGRESS_CLASS   = 0x00000020
	ICC_STANDARD_CLASSES = 0x00004000
)

// SHGetFolderPath returns a CSIDL folder (SHGFP_TYPE_CURRENT).
func SHGetFolderPath(csidl int32) (string, error) {
	buf := make([]uint16, 260)
	hr, _, _ := callProc(procSHGetFolderPathW, 0, uintptr(csidl), 0, 0, uintptr(unsafe.Pointer(&buf[0])))
	if err := hrErr("SHGetFolderPathW", hr); err != nil {
		return "", err
	}
	return syscall.UTF16ToString(buf), nil
}

// SHChangeNotifyDir tells the shell that dir changed (shortcuts).
func SHChangeNotifyDir(dir string) {
	callProc(procSHChangeNotify, SHCNE_UPDATEDIR, SHCNF_PATHW, uintptr(unsafe.Pointer(UTF16Ptr(dir))), 0)
}

type browseInfoW struct {
	Owner       uintptr
	Root        uintptr
	DisplayName *uint16
	Title       *uint16
	Flags       uint32
	Callback    uintptr
	LParam      uintptr
	Image       int32
}

// BrowseForFolder shows SHBrowseForFolderW (OLE must be initialised on the
// calling thread); ok = false when cancelled.
func BrowseForFolder(owner uintptr, title string) (string, bool) {
	name := make([]uint16, 260)
	bi := browseInfoW{
		Owner: owner, DisplayName: &name[0], Title: UTF16Ptr(title),
		Flags: BIF_RETURNONLYFSDIRS | BIF_NEWDIALOGSTYLE | BIF_EDITBOX | BIF_VALIDATE,
	}
	pidl, _, _ := callProc(procSHBrowseForFolderW, uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", false
	}
	defer CoTaskMemFree(pidl)
	buf := make([]uint16, 260)
	r, _, _ := callProc(procSHGetPathFromIDListW, pidl, uintptr(unsafe.Pointer(&buf[0])))
	if r == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

type initCommonControlsEx struct {
	Size uint32
	ICC  uint32
}

// InitCommonControls registers msctls_progress32 and standard classes
// (comctl32 v6 via the setup manifest).
func InitCommonControls() bool {
	icc := initCommonControlsEx{ICC: ICC_PROGRESS_CLASS | ICC_STANDARD_CLASSES}
	icc.Size = uint32(unsafe.Sizeof(icc))
	r, _, _ := callProc(procInitCommonControlsEx, uintptr(unsafe.Pointer(&icc)))
	return r != 0
}

var procMoveWindow = moduser32.NewProc("MoveWindow")

// MoveWindow moves / resizes a (child) window and repaints it.
func MoveWindow(hwnd uintptr, x, y, w, h int32) {
	callProc(procMoveWindow, hwnd, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 1)
}
