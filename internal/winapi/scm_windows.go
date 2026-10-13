//go:build windows

package winapi

import (
	"fmt"
	"syscall"
	"unsafe"
)

// SCM wrappers for the installer (phase 7b, CHK-SETUP-03). Every NewProc is
// in the CHK-TRAY-10 allowlist (NT ≤ 6.1). LocalSystem = nil account.

var (
	procCreateServiceW             = modadvapi32.NewProc("CreateServiceW")
	procChangeServiceConfigW       = modadvapi32.NewProc("ChangeServiceConfigW")
	procChangeServiceConfig2W      = modadvapi32.NewProc("ChangeServiceConfig2W")
	procQueryServiceConfigW        = modadvapi32.NewProc("QueryServiceConfigW")
	procQueryServiceConfig2W       = modadvapi32.NewProc("QueryServiceConfig2W")
	procQueryServiceObjectSecurity = modadvapi32.NewProc("QueryServiceObjectSecurity")
	procSetServiceObjectSecurity   = modadvapi32.NewProc("SetServiceObjectSecurity")
	procStartServiceW              = modadvapi32.NewProc("StartServiceW")
	procControlService             = modadvapi32.NewProc("ControlService")
	procDeleteService              = modadvapi32.NewProc("DeleteService")
)

// Service type / start / error / access / config levels (winsvc.h).
const (
	ServiceWin32OwnProcess = 0x00000010
	ServiceAutoStart       = 0x00000002
	ServiceDemandStart     = 0x00000003
	ServiceErrorNormal     = 0x00000001
	ServiceNoChange        = 0xFFFFFFFF

	SCManagerConnect       = 0x0001
	SCManagerCreateService = 0x0002
	SCManagerAllAccess     = 0xF003F

	ServiceQueryConfig   = 0x0001
	ServiceChangeConfig  = 0x0002
	ServiceQueryStatus   = 0x0004
	ServiceEnumerateDeps = 0x0008
	ServiceStart         = 0x0010
	ServiceStop          = 0x0020
	ServiceInterrogate   = 0x0080
	ServiceUserControl   = 0x0100
	ServiceDelete        = 0x00010000
	ServiceReadControl   = 0x00020000
	ServiceWriteDAC      = 0x00040000

	ServiceConfigDescription        = 1
	ServiceConfigFailureActions     = 2
	ServiceConfigDelayedAutoStart   = 3
	ServiceConfigFailureActionsFlag = 4

	SCActionNone    = 0
	SCActionRestart = 1
	SCActionReboot  = 2
	SCActionRun     = 3

	ServiceControlStop = 0x00000001
	ServiceStopped     = 0x00000001
	ServiceStartPend   = 0x00000002
	ServiceStopPend    = 0x00000003
	// ServiceRunning already defined in svcpid_windows.go (= 4)

	DACLSecurityInformation = 0x4
)

// Desired access when creating / updating a service (includes WRITE_DAC for
// SetServiceObjectSecurity and SERVICE_START for SC_ACTION_RESTART).
const ServiceAccessForSetup = ServiceChangeConfig | ServiceStart | ServiceStop |
	ServiceQueryStatus | ServiceQueryConfig | ServiceReadControl | ServiceWriteDAC | ServiceDelete

// QueryServiceConfigW layout (variable; we only read fixed prefix + pointers).
type queryServiceConfigW struct {
	ServiceType      uint32
	StartType        uint32
	ErrorControl     uint32
	_                uint32
	BinaryPathName   *uint16
	LoadOrderGroup   *uint16
	TagId            uint32
	_                uint32
	Dependencies     *uint16
	ServiceStartName *uint16
	DisplayName      *uint16
}

// ServiceHandle is an open SC_HANDLE (SCM or service).
type ServiceHandle uintptr

// Close closes an SC_HANDLE.
func (h ServiceHandle) Close() {
	if h != 0 {
		callProc(procCloseServiceHandle, uintptr(h))
	}
}

// OpenSCManager opens the local SCM with access.
func OpenSCManager(access uint32) (ServiceHandle, error) {
	h, _, e := callProc(procOpenSCManagerW, 0, 0, uintptr(access))
	if h == 0 {
		return 0, fmt.Errorf("OpenSCManager: %w", lastErr(e))
	}
	return ServiceHandle(h), nil
}

// OpenService opens name under scm with access.
func OpenService(scm ServiceHandle, name string, access uint32) (ServiceHandle, error) {
	h, _, e := callProc(procOpenServiceW, uintptr(scm), uintptr(unsafe.Pointer(UTF16Ptr(name))), uintptr(access))
	if h == 0 {
		return 0, fmt.Errorf("OpenService %s: %w", name, lastErr(e))
	}
	return ServiceHandle(h), nil
}

// CreateService wraps CreateServiceW (LocalSystem when account is "").
func CreateService(scm ServiceHandle, name, displayName string, access, serviceType, startType, errorControl uint32, binaryPath string) (ServiceHandle, error) {
	var acct uintptr // nil = LocalSystem
	h, _, e := callProc(procCreateServiceW,
		uintptr(scm),
		uintptr(unsafe.Pointer(UTF16Ptr(name))),
		uintptr(unsafe.Pointer(UTF16Ptr(displayName))),
		uintptr(access),
		uintptr(serviceType),
		uintptr(startType),
		uintptr(errorControl),
		uintptr(unsafe.Pointer(UTF16Ptr(binaryPath))),
		0, 0, 0, acct, 0,
	)
	if h == 0 {
		return 0, fmt.Errorf("CreateService %s: %w", name, lastErr(e))
	}
	return ServiceHandle(h), nil
}

// ChangeServiceConfig wraps ChangeServiceConfigW; pass ServiceNoChange to
// keep a field. binaryPath / displayName empty = unchanged (nil pointer).
func ChangeServiceConfig(svc ServiceHandle, serviceType, startType, errorControl uint32, binaryPath, displayName string) error {
	var bin, disp uintptr
	if binaryPath != "" {
		bin = uintptr(unsafe.Pointer(UTF16Ptr(binaryPath)))
	}
	if displayName != "" {
		disp = uintptr(unsafe.Pointer(UTF16Ptr(displayName)))
	}
	r, _, e := callProc(procChangeServiceConfigW, uintptr(svc),
		uintptr(serviceType), uintptr(startType), uintptr(errorControl),
		bin, 0, 0, 0, 0, 0, disp)
	if r == 0 {
		return fmt.Errorf("ChangeServiceConfig: %w", lastErr(e))
	}
	return nil
}

// ChangeServiceConfig2 wraps ChangeServiceConfig2W.
func ChangeServiceConfig2(svc ServiceHandle, level uint32, info unsafe.Pointer) error {
	r, _, e := callProc(procChangeServiceConfig2W, uintptr(svc), uintptr(level), uintptr(info))
	if r == 0 {
		return fmt.Errorf("ChangeServiceConfig2(%d): %w", level, lastErr(e))
	}
	return nil
}

// SetServiceDescription sets SERVICE_CONFIG_DESCRIPTION.
func SetServiceDescription(svc ServiceHandle, text string) error {
	d := ServiceDescriptionW{Description: UTF16Ptr(text)}
	return ChangeServiceConfig2(svc, ServiceConfigDescription, unsafe.Pointer(&d))
}

// SetServiceFailureActions sets restart actions + FAILURE_ACTIONS_FLAG.
func SetServiceFailureActions(svc ServiceHandle, resetSec uint32, delaysMs []uint32) error {
	actions := make([]SCAction, len(delaysMs))
	for i, d := range delaysMs {
		actions[i] = SCAction{Type: SCActionRestart, Delay: d}
	}
	var fa ServiceFailureActionsW
	fa.ResetPeriod = resetSec
	fa.ActionsLen = uint32(len(actions))
	if len(actions) > 0 {
		fa.Actions = &actions[0]
	}
	if err := ChangeServiceConfig2(svc, ServiceConfigFailureActions, unsafe.Pointer(&fa)); err != nil {
		return err
	}
	flag := ServiceFailureActionsFlag{FailureActionsOnNonCrashFailures: 1}
	return ChangeServiceConfig2(svc, ServiceConfigFailureActionsFlag, unsafe.Pointer(&flag))
}

// SetServiceDelayedAutoStart sets SERVICE_CONFIG_DELAYED_AUTO_START_INFO.
func SetServiceDelayedAutoStart(svc ServiceHandle, delayed bool) error {
	var v uint32
	if delayed {
		v = 1
	}
	info := ServiceDelayedAutoStartInfo{DelayedAutoStart: v}
	return ChangeServiceConfig2(svc, ServiceConfigDelayedAutoStart, unsafe.Pointer(&info))
}

// SetServiceDACL applies an SDDL DACL via SetServiceObjectSecurity.
func SetServiceDACL(svc ServiceHandle, sddl string) error {
	sa, free, err := SecurityAttributesFromSDDL(sddl)
	if err != nil {
		return err
	}
	defer free()
	r, _, e := callProc(procSetServiceObjectSecurity, uintptr(svc), DACLSecurityInformation, sa.SecurityDescriptor)
	if r == 0 {
		return fmt.Errorf("SetServiceObjectSecurity: %w", lastErr(e))
	}
	return nil
}

// ServiceDACL returns the service DACL as SDDL.
func ServiceDACL(svc ServiceHandle) (string, error) {
	var need uint32
	callProc(procQueryServiceObjectSecurity, uintptr(svc), DACLSecurityInformation, 0, 0, uintptr(unsafe.Pointer(&need)))
	if need == 0 {
		need = 256
	}
	buf := make([]byte, need)
	r, _, e := callProc(procQueryServiceObjectSecurity, uintptr(svc), DACLSecurityInformation,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return "", fmt.Errorf("QueryServiceObjectSecurity: %w", lastErr(e))
	}
	var str *uint16
	r, _, e = callProc(procConvertSDToStringSDW, uintptr(unsafe.Pointer(&buf[0])), sddlRevision1, DACLSecurityInformation, uintptr(unsafe.Pointer(&str)), 0)
	if r == 0 {
		return "", fmt.Errorf("ConvertSDToStringSD: %w", lastErr(e))
	}
	defer callProc(procLocalFree, uintptr(unsafe.Pointer(str)))
	return utf16PtrToString(str), nil
}

// StartService wraps StartServiceW (no args).
func StartService(svc ServiceHandle) error {
	r, _, e := callProc(procStartServiceW, uintptr(svc), 0, 0)
	if r == 0 {
		return fmt.Errorf("StartService: %w", lastErr(e))
	}
	return nil
}

// ControlService wraps ControlService; returns the status after the call.
func ControlService(svc ServiceHandle, control uint32) (state uint32, err error) {
	var st serviceStatusProcess
	r, _, e := callProc(procControlService, uintptr(svc), uintptr(control), uintptr(unsafe.Pointer(&st)))
	if r == 0 {
		return 0, fmt.Errorf("ControlService: %w", lastErr(e))
	}
	return st.CurrentState, nil
}

// DeleteService wraps DeleteService (service must be stopped).
func DeleteService(svc ServiceHandle) error {
	r, _, e := callProc(procDeleteService, uintptr(svc))
	if r == 0 {
		return fmt.Errorf("DeleteService: %w", lastErr(e))
	}
	return nil
}

// QueryServiceStatus returns CurrentState and ProcessID.
func QueryServiceStatus(svc ServiceHandle) (state, pid uint32, err error) {
	var st serviceStatusProcess
	var need uint32
	r, _, e := callProc(procQueryServiceStatusEx, uintptr(svc), scStatusProcessInfo,
		uintptr(unsafe.Pointer(&st)), unsafe.Sizeof(st), uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return 0, 0, fmt.Errorf("QueryServiceStatusEx: %w", lastErr(e))
	}
	return st.CurrentState, st.ProcessID, nil
}

// ServiceConfig is a parsed QUERY_SERVICE_CONFIGW.
type ServiceConfig struct {
	ServiceType  uint32
	StartType    uint32
	ErrorControl uint32
	BinaryPath   string
	DisplayName  string
	StartName    string
}

// QueryServiceConfig wraps QueryServiceConfigW.
func QueryServiceConfig(svc ServiceHandle) (ServiceConfig, error) {
	var need uint32
	callProc(procQueryServiceConfigW, uintptr(svc), 0, 0, uintptr(unsafe.Pointer(&need)))
	if need == 0 {
		need = 1024
	}
	buf := make([]byte, need)
	r, _, e := callProc(procQueryServiceConfigW, uintptr(svc), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return ServiceConfig{}, fmt.Errorf("QueryServiceConfig: %w", lastErr(e))
	}
	cfg := (*queryServiceConfigW)(unsafe.Pointer(&buf[0]))
	return ServiceConfig{
		ServiceType:  cfg.ServiceType,
		StartType:    cfg.StartType,
		ErrorControl: cfg.ErrorControl,
		BinaryPath:   utf16PtrToString(cfg.BinaryPathName),
		DisplayName:  utf16PtrToString(cfg.DisplayName),
		StartName:    utf16PtrToString(cfg.ServiceStartName),
	}, nil
}

// QueryServiceConfig2Bytes returns the raw buffer of ChangeServiceConfig2 level.
func QueryServiceConfig2Bytes(svc ServiceHandle, level uint32) ([]byte, error) {
	var need uint32
	callProc(procQueryServiceConfig2W, uintptr(svc), uintptr(level), 0, 0, uintptr(unsafe.Pointer(&need)))
	if need == 0 {
		need = 256
	}
	buf := make([]byte, need)
	r, _, e := callProc(procQueryServiceConfig2W, uintptr(svc), uintptr(level),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return nil, fmt.Errorf("QueryServiceConfig2(%d): %w", level, lastErr(e))
	}
	return buf[:need], nil
}

// ErrServiceDoesNotExist is ERROR_SERVICE_DOES_NOT_EXIST (1060).
const ErrServiceDoesNotExist syscall.Errno = 1060

// ErrServiceAlreadyRunning is ERROR_SERVICE_ALREADY_RUNNING (1056).
const ErrServiceAlreadyRunning syscall.Errno = 1056

// ErrServiceNotActive is ERROR_SERVICE_NOT_ACTIVE (1062).
const ErrServiceNotActive syscall.Errno = 1062

// ErrServiceMarkedForDelete is ERROR_SERVICE_MARKED_FOR_DELETE (1072).
const ErrServiceMarkedForDelete syscall.Errno = 1072

// ErrServiceExists is ERROR_SERVICE_EXISTS (1073).
const ErrServiceExists syscall.Errno = 1073

// ErrServiceCannotAcceptCtrl is ERROR_SERVICE_CANNOT_ACCEPT_CTRL (1061).
const ErrServiceCannotAcceptCtrl syscall.Errno = 1061

// ServiceMarkedForDelete reports whether svc is marked for deletion
// (ChangeServiceConfigW with SERVICE_NO_CHANGE → ERROR_SERVICE_MARKED_FOR_DELETE).
func ServiceMarkedForDelete(svc ServiceHandle) bool {
	r, _, e := callProc(procChangeServiceConfigW, uintptr(svc),
		uintptr(ServiceNoChange), uintptr(ServiceNoChange), uintptr(ServiceNoChange),
		0, 0, 0, 0, 0, 0, 0)
	if r != 0 {
		return false
	}
	en, ok := e.(syscall.Errno)
	return ok && en == ErrServiceMarkedForDelete
}
