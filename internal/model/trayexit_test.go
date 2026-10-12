package model

import "testing"

// TestCHK_TRAY_11_ExitMessageName — CHK-TRAY-11: the registered window
// message name shared by the tray and the installer is fixed.
func TestCHK_TRAY_11_ExitMessageName(t *testing.T) {
	if TrayExitMessage != "ExchangeChecker.TrayExit" {
		t.Fatalf("TrayExitMessage = %q", TrayExitMessage)
	}
}
