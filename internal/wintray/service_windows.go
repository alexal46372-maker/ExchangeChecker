//go:build windows

package wintray

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Service control from the tray menu «Служба» (phase 8, CHK-TRAY-16):
// state, start and stop through the SCM with the least access each step
// needs (SC_MANAGER_CONNECT; SERVICE_QUERY_STATUS / SERVICE_START /
// SERVICE_STOP). The service configuration is never changed and the tray
// never elevates itself for it: ERROR_ACCESS_DENIED is explained in a
// message.

// ServiceControl controls the Exchange Checker service; the default is
// the SCM (scmService), tests use a fake.
type ServiceControl interface {
	Name() string
	State() (trayui.SvcState, error)
	Start() error
	Stop() error
}

// Win32 errors of the SCM calls (winerror.h).
const (
	errServiceAlreadyRunning = syscall.Errno(1056)
	errServiceDoesNotExist   = syscall.Errno(1060)
	errServiceNotActive      = syscall.Errno(1062)
)

// scmService is the SCM ServiceControl of the service name.
type scmService struct{ name string }

// SCMService returns the SCM control of service name (model.ServiceName
// in the tray).
func SCMService(name string) ServiceControl { return scmService{name: name} }

func (s scmService) Name() string { return s.name }

func (s scmService) open(access uint32) (winapi.ServiceHandle, func(), error) {
	scm, err := winapi.OpenSCManager(winapi.SCManagerConnect)
	if err != nil {
		return 0, nil, err
	}
	svc, err := winapi.OpenService(scm, s.name, access)
	if err != nil {
		scm.Close()
		return 0, nil, err
	}
	return svc, func() { svc.Close(); scm.Close() }, nil
}

// State implements ServiceControl (QueryServiceStatus).
func (s scmService) State() (trayui.SvcState, error) {
	svc, done, err := s.open(winapi.ServiceQueryStatus)
	if err != nil {
		return stateOfError(err), err
	}
	defer done()
	st, _, err := winapi.QueryServiceStatus(svc)
	if err != nil {
		return stateOfError(err), err
	}
	return SvcStateOf(st), nil
}

// Start implements ServiceControl (StartService).
func (s scmService) Start() error {
	svc, done, err := s.open(winapi.ServiceStart)
	if err != nil {
		return err
	}
	defer done()
	return winapi.StartService(svc)
}

// Stop implements ServiceControl (ControlService SERVICE_CONTROL_STOP).
func (s scmService) Stop() error {
	svc, done, err := s.open(winapi.ServiceStop)
	if err != nil {
		return err
	}
	defer done()
	_, err = winapi.ControlService(svc, winapi.ServiceControlStop)
	return err
}

// SvcStateOf maps SERVICE_STATUS.dwCurrentState.
func SvcStateOf(state uint32) trayui.SvcState {
	switch state {
	case winapi.ServiceStopped:
		return trayui.SvcStateStopped
	case winapi.ServiceStartPend:
		return trayui.SvcStateStartPending
	case winapi.ServiceStopPend:
		return trayui.SvcStateStopPending
	case winapi.ServiceRunning:
		return trayui.SvcStateRunning
	}
	return trayui.SvcStateUnknown
}

func stateOfError(err error) trayui.SvcState {
	switch {
	case errors.Is(err, syscall.ERROR_ACCESS_DENIED):
		return trayui.SvcStateNoAccess
	case errors.Is(err, errServiceDoesNotExist):
		return trayui.SvcStateNotInstalled
	}
	return trayui.SvcStateUnknown
}

// ServiceResultText is the message after «Запустить» / «Остановить»
// (err nil = the request was accepted) and whether it is a warning.
func ServiceResultText(name string, start bool, err error, lang string) (string, bool) {
	t := func(key string) string { return fmt.Sprintf(i18n.T(key, lang), name) }
	switch {
	case err == nil && start:
		return t(i18n.KeySvcStartSent), false
	case err == nil:
		return t(i18n.KeySvcStopSent), false
	case errors.Is(err, syscall.ERROR_ACCESS_DENIED):
		return t(i18n.KeySvcErrAccess), true
	case errors.Is(err, errServiceDoesNotExist):
		return t(i18n.KeySvcErrNotInstalled), true
	case errors.Is(err, errServiceAlreadyRunning):
		return t(i18n.KeySvcErrRunning), false
	case errors.Is(err, errServiceNotActive):
		return t(i18n.KeySvcErrNotActive), false
	}
	return fmt.Sprintf(i18n.T(i18n.KeySvcErrOther, lang), name, err.Error()), true
}

// service is the control of the App (Options.Service or the SCM).
func (a *App) service() ServiceControl {
	if a.opt.Service != nil {
		return a.opt.Service
	}
	return SCMService(model.ServiceName)
}

// serviceState is the state shown in the «Служба» submenu.
func (a *App) serviceState() trayui.SvcState {
	st, _ := a.service().State()
	return st
}

// serviceCommand runs «Запустить» / «Остановить» off the UI thread (the
// SCM may take a while) and shows the result.
func (a *App) serviceCommand(start bool) {
	ctl, lang := a.service(), a.opt.Lang
	go func() {
		var err error
		if start {
			err = ctl.Start()
		} else {
			err = ctl.Stop()
		}
		text, warn := ServiceResultText(ctl.Name(), start, err, lang)
		a.runOnUI(func() { a.notify(text, warn) })
	}()
}

// notify shows a message of the tray (Options.Notify or MessageBoxW).
func (a *App) notify(text string, warn bool) {
	if a.opt.Notify != nil {
		a.opt.Notify(text, warn)
		return
	}
	flags := uint32(winapi.MB_OK | winapi.MB_ICONINFORMATION)
	if warn {
		flags = winapi.MB_OK | winapi.MB_ICONWARNING
	}
	winapi.MessageBox(a.hwnd, text, i18n.T(i18n.KeyAppName, a.opt.Lang), flags)
}
