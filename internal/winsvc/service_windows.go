//go:build windows

package winsvc

import (
	"context"
	"sync"
	"syscall"
	"unsafe"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

const errorFailedServiceControllerConnect = syscall.Errno(1063)

// The SCM calls ServiceMain and the control handler on its own threads;
// syscall.NewCallback supports that. One service per process.
var (
	svcMu     sync.Mutex
	svcName   *uint16
	svcFn     func(ctx context.Context) error
	svcCtl    *controller
	svcHandle uintptr
	svcErr    error

	serviceMainCB = syscall.NewCallback(serviceMain)
	ctlHandlerCB  = syscall.NewCallback(ctlHandler)
)

type serviceTableEntry struct {
	name *uint16
	proc uintptr
}

func setStatus(st Status) error {
	svcMu.Lock()
	h := svcHandle
	svcMu.Unlock()
	if h == 0 {
		return nil
	}
	return winapi.SetServiceStatus(h, unsafe.Pointer(&st))
}

func ctlHandler(code, eventType, eventData, ctx uintptr) uintptr {
	svcMu.Lock()
	c := svcCtl
	svcMu.Unlock()
	if c == nil {
		return errorCallNotImplemented
	}
	return uintptr(c.control(uint32(code)))
}

func serviceMain(argc uint32, argv uintptr) uintptr {
	h, err := winapi.RegisterServiceCtrlHandlerEx(svcName, ctlHandlerCB)
	if err != nil {
		svcMu.Lock()
		svcErr = err
		svcMu.Unlock()
		return 0
	}
	svcMu.Lock()
	svcHandle = h
	c := svcCtl
	fn := svcFn
	svcMu.Unlock()
	err = c.serve(fn)
	svcMu.Lock()
	svcErr = err
	svcMu.Unlock()
	return 0
}

// Run connects to the SCM and runs fn as the service name (own process).
// It returns when the service stopped, or ErrNotService immediately when
// the process was not started by the SCM.
func Run(name string, fn func(ctx context.Context) error) error {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	svcMu.Lock()
	svcName, svcFn, svcCtl = n, fn, newController(setStatus)
	svcMu.Unlock()
	table := []serviceTableEntry{{name: n, proc: serviceMainCB}, {}}
	if err := winapi.StartServiceCtrlDispatcher(unsafe.Pointer(&table[0])); err != nil {
		if err == errorFailedServiceControllerConnect {
			return ErrNotService
		}
		return err
	}
	svcMu.Lock()
	defer svcMu.Unlock()
	return svcErr
}
