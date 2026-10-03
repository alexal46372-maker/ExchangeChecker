//go:build !windows

package ipc

import "io"

// ServerProcessID is unavailable outside Windows.
func ServerProcessID(rw io.ReadWriteCloser) (uint32, error) { return 0, ErrNoPipes }
