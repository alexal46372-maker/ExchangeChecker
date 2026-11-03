//go:build windows

package wininstall

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_SETUP_16_DesktopShortcutRoundTrip — real Public Desktop .lnk via
// IShellLinkW (same COM path as start-menu), then delete.
func TestCHK_SETUP_16_DesktopShortcutRoundTrip(t *testing.T) {
	win, err := winapi.WindowsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(win, "notepad.exe")
	sys := winSystem{}
	lnk, err := sys.CreateDesktopShortcut(target, win, "CHK-SETUP-16 test")
	if err != nil {
		t.Fatalf("CreateDesktopShortcut: %v", err)
	}
	defer sys.DeleteDesktopShortcut()
	if _, err := os.Stat(lnk); err != nil {
		t.Fatalf("desktop lnk missing: %s: %v", lnk, err)
	}
	desk, err := winapi.SHGetFolderPath(winapi.CSIDL_COMMON_DESKTOPDIRECTORY)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(lnk) != desk {
		t.Fatalf("lnk dir %q want Public Desktop %q", filepath.Dir(lnk), desk)
	}
	if err := sys.DeleteDesktopShortcut(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lnk); !os.IsNotExist(err) {
		t.Fatalf("after delete: %v", err)
	}
	t.Logf("desktop shortcut ok: %s", lnk)
}
