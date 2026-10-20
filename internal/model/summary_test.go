package model

import (
	"errors"
	"testing"
	"time"
)

// TestCHK_SVC_07_SummaryParams — defaults 24/48 (30 min), ranges and
// hours*60 % buckets == 0, else invalid_request + monitor.bad_request.
func TestCHK_SVC_07_SummaryParams(t *testing.T) {
	h, b, d, err := SummaryParams(0, 0)
	if err != nil || h != 24 || b != 48 || d != 30*time.Minute {
		t.Fatal(h, b, d, err)
	}
	if _, _, d, err := SummaryParams(1, 288); err == nil || d != 0 {
		t.Error("1h/288 accepted") // 60 % 288 != 0
	}
	if _, _, d, err := SummaryParams(24, 288); err != nil || d != 5*time.Minute {
		t.Error(d, err)
	}
	for _, c := range [][2]int{{25, 48}, {-1, 48}, {24, 289}, {24, -2}, {24, 7}} {
		_, _, _, err := SummaryParams(c[0], c[1])
		var ie *IPCError
		if !errors.As(err, &ie) || ie.Code != ErrCodeInvalidRequest || ie.Key != MsgMonitorBadRequest {
			t.Errorf("%v: %v", c, err)
		}
	}
}
