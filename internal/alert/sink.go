package alert

import (
	"sync"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Logger is the minimal line logger (RotatingFile, test doubles).
type Logger interface {
	Printf(format string, args ...interface{})
}

// Sink receives confirmed alert events.
type Sink interface {
	Alert(e model.AlertEvent)
}

// SinkFunc adapts a function to Sink.
type SinkFunc func(model.AlertEvent)

// Alert implements Sink.
func (f SinkFunc) Alert(e model.AlertEvent) { f(e) }

// Multi fans an event out to several sinks (nil sinks are skipped).
type Multi []Sink

// Alert implements Sink.
func (m Multi) Alert(e model.AlertEvent) {
	for _, s := range m {
		if s != nil {
			s.Alert(e)
		}
	}
}

// LogSink writes events to a Logger (the service log file).
type LogSink struct {
	Log  Logger
	Lang string
}

// Alert implements Sink.
func (s LogSink) Alert(e model.AlertEvent) {
	if s.Log != nil {
		s.Log.Printf("ALERT #%d %s", e.ID, Format(e, s.Lang))
	}
}

// Event Log event IDs of the service (all <= 1000).
const (
	EventIDService  uint32 = 100 // service started / stopped (information)
	EventIDRecover  uint32 = 101 // level decreased (information)
	EventIDWarning  uint32 = 102 // level raised to warning
	EventIDError    uint32 = 103 // level raised to error
	EventIDInternal uint32 = 110 // configuration / internal error
)

// Event Log entry types (winnt.h EVENTLOG_*).
const (
	EventTypeError       uint16 = 0x0001
	EventTypeWarning     uint16 = 0x0002
	EventTypeInformation uint16 = 0x0004
)

// Reporter writes one entry to the Windows Event Log (winapi.EventLog).
type Reporter interface {
	Report(eventType uint16, eventID uint32, msg string) error
}

// EventLogSink writes events to the Windows Event Log: raised to error ->
// EVENTLOG_ERROR_TYPE, raised to warning -> WARNING, recoveries -> INFORMATION.
// Report failures go to Fallback (the log file), once per failure.
type EventLogSink struct {
	R        Reporter
	Lang     string
	Fallback Logger

	mu sync.Mutex
}

// EventTypeOf maps an event to (entry type, event ID).
func EventTypeOf(e model.AlertEvent) (uint16, uint32) {
	switch {
	case !e.Raised():
		return EventTypeInformation, EventIDRecover
	case e.To == model.LevelError:
		return EventTypeError, EventIDError
	default:
		return EventTypeWarning, EventIDWarning
	}
}

// Alert implements Sink.
func (s *EventLogSink) Alert(e model.AlertEvent) {
	if s == nil || s.R == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	typ, id := EventTypeOf(e)
	if err := s.R.Report(typ, id, Format(e, s.Lang)); err != nil && s.Fallback != nil {
		s.Fallback.Printf("event log: %v", err)
	}
}
