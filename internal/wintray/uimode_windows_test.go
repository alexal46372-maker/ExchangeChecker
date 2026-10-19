//go:build windows

package wintray

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_40_UIModeStore — HKCU UIMode, «Простой» by default.
func TestCHK_TRAY_40_UIModeStore(t *testing.T) {
	key := `Software\ExchangeChecker\TrayTest10`
	t.Cleanup(func() { winapi.RegDeleteKey(winapi.HKEY_CURRENT_USER, key) })
	s := RegistryUIModeStore{Key: key}
	if m, err := s.Load(); m != traysettings.UISimple || err != nil {
		t.Fatalf("default %v %v", m, err)
	}
	if err := s.Save(traysettings.UIExpert); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.Load(); m != traysettings.UIExpert {
		t.Error("expert")
	}
}
