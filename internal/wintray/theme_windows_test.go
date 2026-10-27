//go:build windows

package wintray

import (
	"fmt"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_55_ThemeRegistry — HKCU Theme round-trip; empty = light
// (PLAN §12 p.194).
func TestCHK_TRAY_55_ThemeRegistry(t *testing.T) {
	key := fmt.Sprintf(`Software\ExchangeChecker\TrayTest-55-%d`, time.Now().UnixNano())
	t.Cleanup(func() {
		winapi.RegDeleteKey(winapi.HKEY_CURRENT_USER, key)
		trayui.SetTheme(trayui.ThemeLight)
	})
	s := RegistryThemeStore{Key: key}
	if got, err := s.Load(); err != nil || got != trayui.ThemeLight {
		t.Fatalf("missing Load: %q %v", got, err)
	}
	if err := s.Save(trayui.ThemeDark); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Load(); err != nil || got != trayui.ThemeDark {
		t.Fatalf("dark Load: %q %v", got, err)
	}
	if err := s.Save(trayui.Theme("")); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Load(); err != nil || got != trayui.ThemeLight {
		t.Fatalf("empty Save→light: %q %v", got, err)
	}
	if LoadTheme(s) != trayui.ThemeLight || trayui.Current() != trayui.ThemeLight {
		t.Fatal("LoadTheme")
	}
	_ = s.Save(trayui.ThemeDark)
	if LoadTheme(s) != trayui.ThemeDark || !trayui.IsDark() {
		t.Fatal("LoadTheme dark")
	}
}

// TestCHK_TRAY_55_AllowlistMentionsDwmSet — DwmSetWindowAttribute is
// wired for immersive dark (Find fallback); the CHK-TRAY-10 allowlist
// covers the NewProc (checked in checks/).
func TestCHK_TRAY_55_AllowlistMentionsDwmSet(t *testing.T) {
	// compile-time presence: the winapi helper must exist
	winapi.SetImmersiveDarkMode(0, false)
}
