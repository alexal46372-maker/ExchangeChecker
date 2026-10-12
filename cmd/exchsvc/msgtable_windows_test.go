//go:build windows

package main

import (
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/alert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_SVC_02_MessageTable — the package .syso is linked into the test
// binary too: FormatMessageW(FROM_HMODULE|ARGUMENT_ARRAY, GetModuleHandleW
// (NULL), id, 0, …, ["x"]) yields "x" for every alert.EventIDs() entry.
func TestCHK_SVC_02_MessageTable(t *testing.T) {
	mod := winapi.GetModuleHandle()
	if mod == 0 {
		t.Fatal("GetModuleHandleW(NULL) = 0")
	}
	for _, id := range alert.EventIDs() {
		s, err := winapi.FormatModuleMessage(mod, id, []string{"x"})
		if err != nil {
			t.Errorf("CHK-SVC-02: id %d: %v", id, err)
			continue
		}
		if got := strings.TrimRight(s, "\r\n"); got != "x" {
			t.Errorf("CHK-SVC-02: id %d: FormatMessageW = %q, want %q", id, s, "x")
		}
	}
}
