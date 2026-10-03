package trayipc

import "fmt"

// UntrustedError explains why the pipe server is not accepted (G7).
type UntrustedError struct {
	PipePID    uint32
	ServicePID uint32
	Reason     string
}

func (e *UntrustedError) Error() string {
	return fmt.Sprintf("trayipc: pipe server not trusted (pipe server PID %d, service PID %d): %s", e.PipePID, e.ServicePID, e.Reason)
}

// SERVICE_RUNNING (winsvc.h).
const serviceRunning = 4

// CheckServerPID is the G7 rule (PLAN §12): the process that owns the
// service pipe must be the running service known to the SCM. Any failure
// to establish that (pipe PID unknown, SCM query failed, service not
// running, PID mismatch) rejects the server.
func CheckServerPID(pipePID uint32, pipeErr error, svcState, svcPID uint32, svcErr error) error {
	switch {
	case pipeErr != nil:
		return &UntrustedError{Reason: "pipe server PID unknown: " + pipeErr.Error()}
	case svcErr != nil:
		return &UntrustedError{PipePID: pipePID, Reason: "service PID unknown: " + svcErr.Error()}
	case svcState != serviceRunning || svcPID == 0:
		return &UntrustedError{PipePID: pipePID, ServicePID: svcPID, Reason: "the service is not running"}
	case pipePID != svcPID:
		return &UntrustedError{PipePID: pipePID, ServicePID: svcPID, Reason: "the pipe is owned by another process"}
	}
	return nil
}
