//go:build windows

package sqlcheck

import (
	"errors"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Registry reads: REG_SZ through winapi.RegGetStringView (WOW64 view),
// REG_MULTI_SZ here with RegOpenKeyExW / RegQueryValueExW / RegCloseKey
// (advapi32, NT 3.1+, in the CHK-TRAY-10 allowlist, called through
// findCall = Find + Call). SCM: winapi.ServiceProcessID (read-only access).
var (
	modAdvapi32SQL       = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExWSQL = modAdvapi32SQL.NewProc("RegOpenKeyExW")
	procRegQueryValueSQL = modAdvapi32SQL.NewProc("RegQueryValueExW")
	procRegCloseKeySQL   = modAdvapi32SQL.NewProc("RegCloseKey")
)

const (
	regKeyQueryValue = 0x0001
	regMultiSZ       = 7
	errFileNotFound  = 2
	errMoreData      = 234
	errSvcNotExist   = 1060
	maxMultiSZBytes  = 64 << 10
)

type systemLocal struct{}

func (systemLocal) supported() bool { return true }

func viewFlag(view int) uint32 {
	if view == 32 {
		return winapi.KEY_WOW64_32KEY
	}
	return winapi.KEY_WOW64_64KEY
}

func (systemLocal) str(view int, path, name string) (string, bool, error) {
	return winapi.RegGetStringView(winapi.HKEY_LOCAL_MACHINE, path, name, viewFlag(view))
}

func (systemLocal) multiString(view int, path, name string) ([]string, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	if err := procRegQueryValueSQL.Find(); err != nil {
		return nil, err
	}
	if err := procRegOpenKeyExWSQL.Find(); err != nil {
		return nil, err
	}
	var k uintptr
	r := findCall(procRegOpenKeyExWSQL, winapi.HKEY_LOCAL_MACHINE, uintptr(unsafe.Pointer(p)), 0,
		uintptr(regKeyQueryValue|viewFlag(view)), uintptr(unsafe.Pointer(&k)))
	if r == errFileNotFound {
		return nil, nil
	}
	if r != 0 {
		return nil, syscall.Errno(r)
	}
	defer findCall(procRegCloseKeySQL, k)
	for attempt := 0; attempt < 3; attempt++ {
		var typ, size uint32
		r = findCall(procRegQueryValueSQL, k, uintptr(unsafe.Pointer(n)), 0, uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size)))
		if r == errFileNotFound {
			return nil, nil
		}
		if r != 0 {
			return nil, syscall.Errno(r)
		}
		if typ != regMultiSZ || size == 0 {
			return nil, nil
		}
		if size > maxMultiSZBytes {
			return nil, errors.New("InstalledInstances too large")
		}
		buf := make([]uint16, size/2+2)
		size = uint32(len(buf) * 2)
		r = findCall(procRegQueryValueSQL, k, uintptr(unsafe.Pointer(n)), 0, uintptr(unsafe.Pointer(&typ)),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
		if r == errMoreData {
			continue
		}
		if r != 0 {
			return nil, syscall.Errno(r)
		}
		return splitMultiSZ(buf[:size/2]), nil
	}
	return nil, syscall.Errno(errMoreData)
}

// splitMultiSZ splits a REG_MULTI_SZ buffer.
func splitMultiSZ(w []uint16) []string {
	var out []string
	start := 0
	for i, c := range w {
		if c == 0 {
			if i > start {
				out = append(out, string(utf16.Decode(w[start:i])))
			}
			start = i + 1
		}
	}
	if start < len(w) {
		out = append(out, string(utf16.Decode(w[start:])))
	}
	return out
}

func (systemLocal) browserState() string {
	state, _, err := winapi.ServiceProcessID("SQLBrowser")
	var en syscall.Errno
	if errors.As(err, &en) && en == errSvcNotExist {
		return model.SQLBrowserNotInstalled
	}
	if err != nil {
		return model.SQLBrowserUnknown
	}
	switch state {
	case 1:
		return model.SQLBrowserStopped
	case 2:
		return model.SQLBrowserStartPending
	case 3:
		return model.SQLBrowserStopPending
	case 4:
		return model.SQLBrowserRunning
	case 7:
		return model.SQLBrowserPaused
	}
	return model.SQLBrowserUnknown
}
