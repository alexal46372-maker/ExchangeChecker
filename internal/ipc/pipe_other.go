//go:build !windows

package ipc

import (
	"errors"
	"io"
	"time"
)

// ErrNoPipes: named pipes exist only on Windows (non-Windows builds are
// for unit tests; the console service uses MemListener there).
var ErrNoPipes = errors.New("ipc: named pipes are only available on Windows")

// ListenPipe is unavailable outside Windows.
func ListenPipe(name, sddl string) (Listener, error) { return nil, ErrNoPipes }

// DialPipe is unavailable outside Windows.
func DialPipe(name string, timeout time.Duration) (io.ReadWriteCloser, error) { return nil, ErrNoPipes }
