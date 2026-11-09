//go:build windows

package wininstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_SETUP_18_ShortcutIconLocation — Windows: Start-menu / desktop
// shortcuts created by the installer set IconLocation to the tray exe
// (index 0), which embeds cmd/exchtray/exchtray.ico (CHK-TRAY-89).
func TestCHK_SETUP_18_ShortcutIconLocation(t *testing.T) {
	dir := t.TempDir()
	tray := filepath.Join(dir, config.TrayExeName)
	if err := os.WriteFile(tray, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	sys := NewSystem()
	lnk, err := sys.CreateStartShortcut(tray, dir, "EC icon test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sys.DeleteStartShortcut() })
	got, err := winapi.ReadShortcut(lnk)
	if err != nil {
		// ReadShortcut may need COM init
		err = winapi.WithCOM(func() error {
			var e error
			got, e = winapi.ReadShortcut(lnk)
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !strings.EqualFold(got.Icon, tray) || got.IconIndex != 0 {
		t.Fatalf("IconLocation=%q,%d want %q,0", got.Icon, got.IconIndex, tray)
	}
	desk, err := sys.CreateDesktopShortcut(tray, dir, "EC icon test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sys.DeleteDesktopShortcut() })
	err = winapi.WithCOM(func() error {
		var e error
		got, e = winapi.ReadShortcut(desk)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(got.Icon, tray) || got.IconIndex != 0 {
		t.Fatalf("desktop IconLocation=%q,%d want %q,0", got.Icon, got.IconIndex, tray)
	}
}
