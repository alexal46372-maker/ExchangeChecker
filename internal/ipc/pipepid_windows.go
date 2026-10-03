//go:build windows

package ipc

import (
	"errors"
	"io"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// ErrNotPipe: the connection is not a named pipe opened by DialPipe.
var ErrNotPipe = errors.New("ipc: not a named pipe connection")

// ServerProcessID returns the process ID of the server end of a pipe
// opened with DialPipe (GetNamedPipeServerProcessId, NT 6.0+). The tray
// compares it with the service PID before sending anything (PLAN §12, G7).
func ServerProcessID(rw io.ReadWriteCloser) (uint32, error) {
	c, ok := rw.(*pipeConn)
	if !ok {
		return 0, ErrNotPipe
	}
	return winapi.NamedPipeServerProcessID(c.h)
}
