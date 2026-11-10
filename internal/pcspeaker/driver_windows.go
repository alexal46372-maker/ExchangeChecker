//go:build windows

package pcspeaker

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

const (
	errnoServiceDoesNotExist   syscall.Errno = 1060
	errnoServiceAlreadyRunning syscall.Errno = 1056
	errnoServiceNotActive      syscall.Errno = 1062
)

func isErrno(err error, no syscall.Errno) bool {
	return errors.Is(err, no)
}

func defaultKernel() Kernel {
	return &winKernel{}
}

// winKernel talks to SCM and \\.\inpoutx64 through internal/winapi
// (StartServiceW, ControlService, QueryServiceConfigW, CreateFile,
// DeviceIoControl). No DLL, no os/exec.
type winKernel struct {
	scm, svc winapi.ServiceHandle
}

func (k *winKernel) Open(name string) (missing bool, err error) {
	k.Close()
	scm, err := winapi.OpenSCManager(winapi.SCManagerConnect)
	if err != nil {
		return false, err
	}
	access := uint32(winapi.ServiceQueryConfig | winapi.ServiceQueryStatus | winapi.ServiceStart | winapi.ServiceStop)
	svc, err := winapi.OpenService(scm, name, access)
	if err != nil {
		scm.Close()
		if isErrno(err, errnoServiceDoesNotExist) {
			return true, nil
		}
		return false, err
	}
	k.scm, k.svc = scm, svc
	return false, nil
}

func (k *winKernel) ImagePath() (string, error) {
	cfg, err := winapi.QueryServiceConfig(k.svc)
	if err != nil {
		return "", err
	}
	return cfg.BinaryPath, nil
}

func (k *winKernel) Start() error {
	err := winapi.StartService(k.svc)
	if err != nil && isErrno(err, errnoServiceAlreadyRunning) {
		return nil
	}
	return err
}

func (k *winKernel) Stop() error {
	if k.svc == 0 {
		return nil
	}
	_, err := winapi.ControlService(k.svc, winapi.ServiceControlStop)
	if err != nil && isErrno(err, errnoServiceNotActive) {
		return nil
	}
	return err
}

func (k *winKernel) Close() {
	if k.svc != 0 {
		k.svc.Close()
		k.svc = 0
	}
	if k.scm != 0 {
		k.scm.Close()
		k.scm = 0
	}
}

func (k *winKernel) OpenDevice(name string) (DeviceHandle, error) {
	h, err := winapi.OpenDeviceFile(name)
	if err != nil {
		return nil, err
	}
	return &winDev{h: h}, nil
}

type winDev struct {
	h syscall.Handle
}

func (d *winDev) DeviceIoControl(code uint32, in, out []byte) error {
	if d == nil || d.h == 0 || d.h == syscall.InvalidHandle {
		return fmt.Errorf("DeviceIoControl: handle closed")
	}
	_, err := winapi.DeviceIoControl(d.h, code, in, out)
	return err
}

func (d *winDev) Close() error {
	if d == nil || d.h == 0 || d.h == syscall.InvalidHandle {
		return nil
	}
	err := syscall.CloseHandle(d.h)
	d.h = 0
	return err
}
