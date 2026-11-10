package trayalert

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_98_ListenView — an alarm view with only test alarms
// (alarm_test push) is listen mode of the service (no warning/error alarm
// of its own, CHK-IPC-20); a real alarm (also next to a test one) or no
// alarm at all is not.
func TestCHK_TRAY_98_ListenView(t *testing.T) {
	real := model.AlertEvent{ProfileID: "a", Key: "k", To: model.LevelError}
	test := model.AlertEvent{ProfileID: model.AlarmTestProfileID, Key: "t", To: model.LevelWarning, Test: true}
	for _, c := range []struct {
		alerts []model.AlertEvent
		want   bool
	}{
		{nil, false},
		{[]model.AlertEvent{real}, false},
		{[]model.AlertEvent{test}, true},
		{[]model.AlertEvent{test, real}, false},
	} {
		if got := ListenView(View{Alerts: c.alerts}); got != c.want {
			t.Errorf("ListenView(%+v) = %v, want %v", c.alerts, got, c.want)
		}
	}
}
