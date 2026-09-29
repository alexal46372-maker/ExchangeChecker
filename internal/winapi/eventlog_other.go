//go:build !windows

package winapi

// EventLog is unavailable outside Windows (unit tests of pure packages).
type EventLog struct{}

// OpenEventLog returns ErrNotSupported outside Windows.
func OpenEventLog(string) (*EventLog, error) { return nil, ErrNotSupported }

// Report returns ErrNotSupported.
func (*EventLog) Report(uint16, uint32, string) error { return ErrNotSupported }

// Close is a no-op.
func (*EventLog) Close() error { return nil }
