//go:build !windows

package winapi

import (
	"errors"
	"testing"
)

func TestCHK_MON_05_EventLogStub(t *testing.T) {
	if _, err := OpenEventLog("x"); !errors.Is(err, ErrNotSupported) {
		t.Errorf("OpenEventLog: %v", err)
	}
}
