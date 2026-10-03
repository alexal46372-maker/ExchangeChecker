package trayipc

import (
	"errors"
	"testing"
)

// TestCHK_IPC_01_CheckServerPID — G7 rule: only the running service's own
// PID is trusted.
func TestCHK_IPC_01_CheckServerPID(t *testing.T) {
	if err := CheckServerPID(42, nil, serviceRunning, 42, nil); err != nil {
		t.Fatalf("same PID: %v", err)
	}
	bad := []struct {
		name          string
		pipePID       uint32
		pipeErr       error
		state, svcPID uint32
		svcErr        error
	}{
		{"mismatch", 42, nil, serviceRunning, 7, nil},
		{"pipe pid unknown", 0, errors.New("x"), serviceRunning, 42, nil},
		{"service missing", 42, nil, 0, 0, errors.New("OpenService: 1060")},
		{"service stopped", 42, nil, 1, 0, nil},
		{"pid zero", 0, nil, serviceRunning, 0, nil},
	}
	for _, c := range bad {
		err := CheckServerPID(c.pipePID, c.pipeErr, c.state, c.svcPID, c.svcErr)
		var ue *UntrustedError
		if !errors.As(err, &ue) {
			t.Errorf("%s: want UntrustedError, got %v", c.name, err)
		}
	}
}
