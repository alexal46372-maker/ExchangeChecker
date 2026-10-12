//go:build windows

package winapi

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// Windows Event Log through advapi32 (RegisterEventSourceW / ReportEventW /
// DeregisterEventSource), no helper processes (CHK-MON-05). Without a
// registered EventMessageFile for the source the viewer shows the string
// after a "description not found" preamble; the text is still recorded.
var (
	modadvapi32                 = newSystemDLL("advapi32.dll")
	procRegisterEventSourceW    = modadvapi32.NewProc("RegisterEventSourceW")
	procReportEventW            = modadvapi32.NewProc("ReportEventW")
	procDeregisterEventSource   = modadvapi32.NewProc("DeregisterEventSource")
	procConvertStringSDToSDW    = modadvapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	procRegisterServiceCtrlHExW = modadvapi32.NewProc("RegisterServiceCtrlHandlerExW")
	procSetServiceStatus        = modadvapi32.NewProc("SetServiceStatus")
	procStartServiceCtrlDispW   = modadvapi32.NewProc("StartServiceCtrlDispatcherW")
)

// EventLog is a registered event source handle.
type EventLog struct {
	mu sync.Mutex
	h  uintptr
}

// OpenEventLog registers source on the local machine (RegisterEventSourceW).
func OpenEventLog(source string) (*EventLog, error) {
	if err := procRegisterEventSourceW.Find(); err != nil {
		return nil, err
	}
	s, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return nil, err
	}
	h, _, e := callProc(procRegisterEventSourceW, 0, uintptr(unsafe.Pointer(s)))
	if h == 0 {
		return nil, fmt.Errorf("winapi: RegisterEventSourceW: %w", e)
	}
	return &EventLog{h: h}, nil
}

// Report writes one entry with a single insertion string (ReportEventW).
func (l *EventLog) Report(eventType uint16, eventID uint32, msg string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.h == 0 {
		return errors.New("winapi: event log closed")
	}
	msg = TruncateUTF8(msg, EventLogMaxBytes) // rune boundary (CHK-MON-15)
	s, err := syscall.UTF16PtrFromString(sanitizeNUL(msg))
	if err != nil {
		return err
	}
	strs := [1]*uint16{s}
	// ReportEventW contract (CHK-SVC-02, PLAN-phase7 §3.3 S4): wCategory 0,
	// dwEventID is the bare ID from alert.EventIDs() (no severity /
	// customer / facility bits; the message table is built with the same
	// bare IDs), lpUserSid NULL, wNumStrings 1 (exactly one insertion
	// string, %1 in the message table), dwDataSize 0, lpRawData NULL.
	// Locked by TestCHK_SVC_02_ReportEventContract.
	r, _, e := reportEventCall(l.h, uintptr(eventType), 0, uintptr(eventID), 0,
		1, 0, uintptr(unsafe.Pointer(&strs[0])), 0)
	if r == 0 {
		return fmt.Errorf("winapi: ReportEventW: %w", e)
	}
	return nil
}

// reportEventHook performs the ReportEventW call; tests replace it to
// inspect the arguments (CHK-SVC-02). Production: callProc as before.
// Override only in non-parallel tests (no t.Parallel): it is an
// unsynchronized package global and every EventLog.Report goes through it.
var reportEventHook = func(args ...uintptr) (uintptr, uintptr, error) {
	return callProc(procReportEventW, args...)
}

// reportEventCall keeps pointers converted to uintptr in its argument
// list alive until it returns (same guarantee as callProc).
//
//go:uintptrescapes
func reportEventCall(args ...uintptr) (uintptr, uintptr, error) {
	return reportEventHook(args...)
}

// Close deregisters the source (DeregisterEventSource).
func (l *EventLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.h == 0 {
		return nil
	}
	r, _, e := callProc(procDeregisterEventSource, l.h)
	l.h = 0
	if r == 0 {
		return fmt.Errorf("winapi: DeregisterEventSource: %w", e)
	}
	return nil
}

func sanitizeNUL(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c == 0 {
			b[i] = ' '
		}
	}
	return string(b)
}
