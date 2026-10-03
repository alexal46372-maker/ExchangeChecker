//go:build windows

package trayipc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"syscall"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// PipeDialer dials the service pipe; a missing pipe is reported as
// ErrServiceNotRunning.
func PipeDialer(name string, timeout time.Duration) Dialer {
	return func(context.Context) (io.ReadWriteCloser, error) {
		rw, err := ipc.DialPipe(name, timeout)
		if err != nil {
			if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
				return nil, fmt.Errorf("%w: %v", ErrServiceNotRunning, err)
			}
			return nil, err
		}
		return rw, nil
	}
}

// ServiceVerifier returns the G7 check: the pipe server PID
// (GetNamedPipeServerProcessId) must equal the PID of the running service
// from the SCM (QueryServiceStatusEx).
func ServiceVerifier(service string) func(io.ReadWriteCloser) error {
	return func(rw io.ReadWriteCloser) error {
		pipePID, perr := ipc.ServerProcessID(rw)
		state, svcPID, serr := winapi.ServiceProcessID(service)
		return CheckServerPID(pipePID, perr, state, svcPID, serr)
	}
}
