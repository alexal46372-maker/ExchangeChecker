package config

import (
	"strings"
	"testing"
)

// TestCHK_SETUP_11_ExeNames — the product executable names (S5) are fixed,
// distinct, plain file names.
func TestCHK_SETUP_11_ExeNames(t *testing.T) {
	want := map[string]string{
		ServiceExeName:   "ExchangeCheckerSvc.exe",
		TrayExeName:      "ExchangeCheckerTray.exe",
		SetupExeName:     "ExchangeCheckerSetup.exe",
		UninstallExeName: "ExchangeCheckerUninstall.exe",
	}
	if len(want) != 4 {
		t.Fatal("executable names are not distinct")
	}
	for got, w := range want {
		if got != w || strings.ContainsAny(got, `\/:"`) || !strings.HasSuffix(got, ".exe") {
			t.Errorf("name %q, want %q", got, w)
		}
	}
}

func TestDeliveryNames(t *testing.T) {
	if got := DeliveryZipName("0.13.0"); got != "ExchangeChecker-0.13.0.zip" {
		t.Fatalf("DeliveryZipName=%q", got)
	}
	if got := DeliveryInstallerName("0.13.0"); got != "Exchange Checker 0.13.0.exe" {
		t.Fatalf("DeliveryInstallerName=%q", got)
	}
}
