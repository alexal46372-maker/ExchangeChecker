//go:build windows

package winapi

import "testing"

// TestCHK_MON_05_EventLogReport — RegisterEventSourceW / ReportEventW /
// DeregisterEventSource on the real Application log (CI windows job).
func TestCHK_MON_05_EventLogReport(t *testing.T) {
	l, err := OpenEventLog("ExchangeChecker-CI-Test")
	if err != nil {
		t.Fatalf("OpenEventLog: %v", err)
	}
	for _, typ := range []uint16{0x0004, 0x0002, 0x0001} {
		if err := l.Report(typ, 100, "ExchangeChecker CHK-MON-05 test entry: проверка\x00nul"); err != nil {
			t.Errorf("Report(%d): %v", typ, err)
		}
	}
	if err := l.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := l.Report(4, 100, "closed"); err == nil {
		t.Error("Report after Close must fail")
	}
	if err := l.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}
