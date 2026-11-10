package winsvc

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrNotService is returned by Run when the process was not started by
// the service control manager (console / test start).
var ErrNotService = errors.New("winsvc: not started by the service control manager")

var (
	silenceMu sync.Mutex
	onSilence func()
)

// SetOnSilence registers a hook invoked from the control handler on
// STOP, SHUTDOWN, PRESHUTDOWN and POWEREVENT+PBT_APMSUSPEND (PC speaker
// silence, CHK-SVC-11). The hook must be short and must not block.
func SetOnSilence(fn func()) {
	silenceMu.Lock()
	onSilence = fn
	silenceMu.Unlock()
}

func currentOnSilence() func() {
	silenceMu.Lock()
	defer silenceMu.Unlock()
	return onSilence
}

// SCM constants (winsvc.h).
const (
	ServiceWin32OwnProcess = 0x00000010

	StateStopped      = 1
	StateStartPending = 2
	StateStopPending  = 3
	StateRunning      = 4

	AcceptStop        = 0x00000001
	AcceptShutdown    = 0x00000004
	AcceptPowerEvent  = 0x00000040 // SERVICE_ACCEPT_POWEREVENT
	AcceptPreshutdown = 0x00000100 // SERVICE_ACCEPT_PRESHUTDOWN
	AcceptRunning     = AcceptStop | AcceptShutdown | AcceptPreshutdown | AcceptPowerEvent

	ControlStop        = 0x00000001
	ControlInterrogate = 0x00000004
	ControlShutdown    = 0x00000005
	ControlPowerEvent  = 0x0000000D // SERVICE_CONTROL_POWEREVENT
	ControlPreshutdown = 0x0000000F // SERVICE_CONTROL_PRESHUTDOWN
	// PBTAPMSuspend is PBT_APMSUSPEND (winuser.h).
	PBTAPMSuspend = 0x00000004

	errorCallNotImplemented   = 120
	errorServiceSpecificError = 1066

	// StartWaitHint / StopWaitHint: SCM wait hints (ms).
	StartWaitHint = 10000
	StopWaitHint  = 30000
	// StopCheckpointEvery: while the service function is still stopping,
	// STOP_PENDING is re-reported with CheckPoint+1 this often, well
	// within StopWaitHint, so the SCM sees progress (CHK-MON-14).
	StopCheckpointEvery = 2 * time.Second
	// StopCheckpointMax caps the progress reports: after 60 s of
	// STOP_PENDING the CheckPoint is no longer incremented, so a stop that
	// hangs is detected by the SCM once the wait hint expires instead of
	// looking busy forever (CHK-MON-14).
	StopCheckpointMax = 60 * time.Second
)

// Status is SERVICE_STATUS.
type Status struct {
	ServiceType             uint32
	CurrentState            uint32
	ControlsAccepted        uint32
	Win32ExitCode           uint32
	ServiceSpecificExitCode uint32
	CheckPoint              uint32
	WaitHint                uint32
}

// controller is the platform-independent state machine behind the SCM
// callbacks (unit-tested on every GOOS).
type controller struct {
	mu     sync.Mutex
	st     Status
	cancel context.CancelFunc
	set    func(Status) error // SetServiceStatus
	// every is the STOP_PENDING progress period, maxStop the cap after
	// which the CheckPoint stops growing (tests shorten both).
	every   time.Duration
	maxStop time.Duration
	stopAt  time.Time
	// now / ticks: clock and progress ticker (tests inject both; nil =
	// time.Now / a time.Ticker with period every).
	now       func() time.Time
	ticks     <-chan time.Time
	onSilence func() // STOP/SHUTDOWN/PRESHUTDOWN/PBT_APMSUSPEND (CHK-SVC-11)
}

func newController(set func(Status) error) *controller {
	return &controller{st: Status{ServiceType: ServiceWin32OwnProcess, CurrentState: StateStopped}, set: set,
		every: StopCheckpointEvery, maxStop: StopCheckpointMax, now: time.Now}
}

func (c *controller) report(state, accepts, waitHint uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reportLocked(state, accepts, waitHint)
}

func (c *controller) reportLocked(state, accepts, waitHint uint32) {
	if state == StateStartPending || state == StateStopPending {
		c.st.CheckPoint++
	} else {
		c.st.CheckPoint = 0
	}
	c.st.CurrentState, c.st.ControlsAccepted, c.st.WaitHint = state, accepts, waitHint
	if c.set != nil {
		_ = c.set(c.st)
	}
}

// control handles one SCM control code and returns the handler result
// (NO_ERROR or ERROR_CALL_NOT_IMPLEMENTED).
func (c *controller) control(code uint32) uint32 { return c.controlEx(code, 0) }

// controlEx handles one SCM control with its eventType (POWEREVENT).
func (c *controller) controlEx(code, eventType uint32) uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ShouldSilence(code, eventType) {
		fn := c.onSilence
		if fn == nil {
			fn = currentOnSilence()
		}
		if fn != nil {
			fn()
		}
	}
	switch code {
	case ControlStop, ControlShutdown, ControlPreshutdown:
		if c.st.CurrentState == StateRunning || c.st.CurrentState == StateStartPending {
			c.stopAt = c.now()
			c.reportLocked(StateStopPending, 0, StopWaitHint)
		}
		if c.cancel != nil {
			c.cancel()
		}
		return 0
	case ControlPowerEvent:
		return 0
	case ControlInterrogate:
		if c.set != nil {
			_ = c.set(c.st)
		}
		return 0
	}
	return errorCallNotImplemented
}

// ShouldSilence: STOP / SHUTDOWN / PRESHUTDOWN, or POWEREVENT + PBT_APMSUSPEND.
func ShouldSilence(code, eventType uint32) bool {
	switch code {
	case ControlStop, ControlShutdown, ControlPreshutdown:
		return true
	case ControlPowerEvent:
		return eventType == PBTAPMSuspend
	}
	return false
}

// serve runs fn with the service lifecycle: START_PENDING -> RUNNING ->
// (Stop/Shutdown: STOP_PENDING, ctx canceled) -> STOPPED. A non-nil error
// from fn is reported as ERROR_SERVICE_SPECIFIC_ERROR with code 1.
func (c *controller) serve(fn func(ctx context.Context) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.cancel = cancel
	c.reportLocked(StateStartPending, 0, StartWaitHint)
	c.mu.Unlock()
	errc := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				errc <- errors.New("winsvc: service function panicked")
			}
		}()
		errc <- fn(ctx)
	}()
	c.report(StateRunning, AcceptRunning, 0)
	ticks := c.ticks
	if ticks == nil {
		tick := time.NewTicker(c.every)
		defer tick.Stop()
		ticks = tick.C
	}
	var err error
wait:
	for {
		select {
		case err = <-errc:
			break wait
		case <-ticks:
			c.stopProgress()
		}
	}
	cancel()
	c.mu.Lock()
	if err != nil {
		c.st.Win32ExitCode, c.st.ServiceSpecificExitCode = errorServiceSpecificError, 1
	}
	c.reportLocked(StateStopped, 0, 0)
	c.mu.Unlock()
	return err
}

// stopProgress re-reports STOP_PENDING with the next CheckPoint while the
// stop is in progress, but only during the first maxStop of it (no-op in
// any other state and after the cap).
func (c *controller) stopProgress() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.st.CurrentState == StateStopPending && c.now().Sub(c.stopAt) < c.maxStop {
		c.reportLocked(StateStopPending, 0, StopWaitHint)
	}
}

// state returns the last reported status (tests).
func (c *controller) state() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st
}
