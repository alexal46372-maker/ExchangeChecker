//go:build windows

package wininstall

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

func readLnk(t *testing.T, lnk string) winapi.ShortcutSpec {
	t.Helper()
	var got winapi.ShortcutSpec
	err := winapi.WithCOM(func() error {
		var e error
		got, e = winapi.ReadShortcut(lnk)
		return e
	})
	if err != nil {
		t.Fatalf("read %s: %v", lnk, err)
	}
	return got
}

// TestCHK_SETUP_18_ShortcutIconLocation — Windows runner: the real
// Start-menu and Public Desktop .lnk written by winSystem carry
// IconLocation = <dir>\exchtray-<version>.ico,0 (file exists, bytes =
// cmd/exchtray/exchtray.ico) and target = the tray exe. Negative: without an
// icon file the shortcuts fall back to the tray exe, not to an empty path.
func TestCHK_SETUP_18_ShortcutIconLocation(t *testing.T) {
	_, want := committedTrayICO(t)
	dir := t.TempDir()
	tray := filepath.Join(dir, config.TrayExeName)
	if err := os.WriteFile(tray, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	sys := NewSystem()
	ico := filepath.Join(dir, IconFileName("0.16.0", AppIconBytes()))
	if filepath.Base(ico) != "exchtray-0.16.0.ico" {
		t.Fatalf("icon name %q", filepath.Base(ico))
	}
	if _, err := sys.WriteFile(ico, AppIconBytes()); err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(ico)
	if err != nil || !bytes.Equal(onDisk, want) {
		t.Fatalf("%s on disk: %d bytes err=%v, want exchtray.ico (%d bytes)", ico, len(onDisk), err, len(want))
	}
	lnk, err := sys.CreateStartShortcut(tray, dir, "EC icon test", ico)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sys.DeleteStartShortcut() })
	desk, err := sys.CreateDesktopShortcut(tray, dir, "EC icon test", ico)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sys.DeleteDesktopShortcut() })
	startP, deskP := sys.ShortcutPaths()
	if !strings.EqualFold(startP, lnk) || !strings.EqualFold(deskP, desk) {
		t.Fatalf("ShortcutPaths=%q,%q want %q,%q", startP, deskP, lnk, desk)
	}
	for _, p := range []string{lnk, desk} {
		got := readLnk(t, p)
		if !strings.EqualFold(got.Icon, ico) || got.IconIndex != 0 {
			t.Fatalf("%s: IconLocation=%q,%d want %q,0", p, got.Icon, got.IconIndex, ico)
		}
		if !winapi.SameImage(got.Target, tray) {
			t.Fatalf("%s: target %q want %q", p, got.Target, tray)
		}
		t.Logf("%s -> %s, icon %s,%d", p, got.Target, got.Icon, got.IconIndex)
	}
	// negative: no icon file → tray exe
	if _, err := sys.CreateDesktopShortcut(tray, dir, "EC icon test", ""); err != nil {
		t.Fatal(err)
	}
	if got := readLnk(t, desk); !strings.EqualFold(got.Icon, tray) || got.IconIndex != 0 {
		t.Fatalf("fallback IconLocation=%q,%d want %q,0", got.Icon, got.IconIndex, tray)
	}
}
