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
	start, cerr := serviceStartType("SQLBrowser")
	return mapBrowserState(state, start, cerr == nil)
}

// SCM, read-only (SC_MANAGER_CONNECT; SERVICE_QUERY_CONFIG): the start
// type through QueryServiceConfigW (advapi32, NT 3.1+, CHK-TRAY-10 list),
// all calls Find + Call.
var (
	procOpenSCManagerWSQL      = modAdvapi32SQL.NewProc("OpenSCManagerW")
	procOpenServiceWSQL        = modAdvapi32SQL.NewProc("OpenServiceW")
	procQueryServiceConfigWSQL = modAdvapi32SQL.NewProc("QueryServiceConfigW")
	procCloseServiceHandleSQL  = modAdvapi32SQL.NewProc("CloseServiceHandle")
)

const (
	scManagerConnectSQL   = 0x0001
	serviceQueryConfigSQL = 0x0001
	errInsufficientBuffer = 122
	maxServiceConfigBytes = 8 << 10 // QueryServiceConfig documents 8 KiB as the maximum
)

// serviceStartType returns dwStartType of a service (two-call pattern:
// size query, then the buffer; ERROR_INSUFFICIENT_BUFFER is retried).
func serviceStartType(name string) (uint32, error) {
	for _, p := range []*syscall.LazyProc{procOpenSCManagerWSQL, procOpenServiceWSQL, procQueryServiceConfigWSQL, procCloseServiceHandleSQL} {
		if err := p.Find(); err != nil {
			return 0, err
		}
	}
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	scm, _ := findCallErr(procOpenSCManagerWSQL, 0, 0, scManagerConnectSQL)
	if scm == 0 {
		return 0, errors.New("OpenSCManager failed")
	}
	defer findCall(procCloseServiceHandleSQL, scm)
	svc, e := findCallErr(procOpenServiceWSQL, scm, uintptr(unsafe.Pointer(n)), serviceQueryConfigSQL)
	if svc == 0 {
		return 0, e
	}
	defer findCall(procCloseServiceHandleSQL, svc)
	var need uint32
	r, e := findCallErr(procQueryServiceConfigWSQL, svc, 0, 0, uintptr(unsafe.Pointer(&need)))
	for attempt := 0; r == 0 && attempt < 3; attempt++ {
		if e != errInsufficientBuffer || need == 0 || need > maxServiceConfigBytes {
			return 0, e
		}
		buf := make([]uint64, (need+7)/8) // QUERY_SERVICE_CONFIGW is pointer-aligned
		r, e = findCallErr(procQueryServiceConfigWSQL, svc, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*8), uintptr(unsafe.Pointer(&need)))
		if r != 0 {
			// dwServiceType, dwStartType: the first two DWORDs
			return (*[2]uint32)(unsafe.Pointer(&buf[0]))[1], nil
		}
	}
	return 0, errors.New("QueryServiceConfig: unexpected success with an empty buffer")
}
