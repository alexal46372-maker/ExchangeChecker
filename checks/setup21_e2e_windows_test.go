//go:build windows

package checks_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wininstall"
)

// trayIconFiles lists exchtray-*.ico in dir (missing dir = none).
func trayIconFiles(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if wininstall.IsTrayIconFile(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

// checkInstalledIcon — CHK-SETUP-18 / CHK-SETUP-21 on a real install done by
// the setup exe (TestCHK_SETUP_12_E2E): exactly one exchtray-*.ico in dir,
// named exchtray-<ver>.ico, bytes = cmd/exchtray/exchtray.ico; the real
// Start-menu and Public Desktop .lnk carry IconLocation = that file,0 and
// target = the tray exe; HKLM Uninstall DisplayIcon = "<file>,0".
func checkInstalledIcon(t *testing.T, dir, ver string) {
	t.Helper()
	name := "exchtray-" + ver + ".ico"
	if got := trayIconFiles(t, dir); len(got) != 1 || !strings.EqualFold(got[0], name) {
		t.Fatalf("CHK-SETUP-21: icon files in %s = %v, want only %s", dir, got, name)
	}
	ico := filepath.Join(dir, name)
	b, err := os.ReadFile(ico)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, trayui.ExeICO()) {
		t.Fatalf("CHK-SETUP-18: %s (%d bytes) is not cmd/exchtray/exchtray.ico", ico, len(b))
	}
	tray := filepath.Join(dir, config.TrayExeName)
	startLnk, deskLnk := wininstall.NewSystem().ShortcutPaths()
	for _, lnk := range []string{startLnk, deskLnk} {
		var sc winapi.ShortcutSpec
		if err := winapi.WithCOM(func() error { var e error; sc, e = winapi.ReadShortcut(lnk); return e }); err != nil {
			t.Fatalf("CHK-SETUP-18: read %s: %v", lnk, err)
		}
		if !winapi.SameImage(sc.Icon, ico) || sc.IconIndex != 0 {
			t.Fatalf("CHK-SETUP-18: %s IconLocation=%q,%d want %q,0", lnk, sc.Icon, sc.IconIndex, ico)
		}
		if !winapi.SameImage(sc.Target, tray) {
			t.Fatalf("CHK-SETUP-18: %s target=%q want %q", lnk, sc.Target, tray)
		}
	}
	di, ok, err := winapi.RegGetStringView(winapi.HKEY_LOCAL_MACHINE, wininstall.UninstallKeyPath, "DisplayIcon", uint32(winapi.KEY_WOW64_64KEY))
	if err != nil || !ok || !strings.HasSuffix(di, ",0") || !winapi.SameImage(strings.TrimSuffix(di, ",0"), ico) {
		t.Fatalf("CHK-SETUP-18: DisplayIcon=%q ok=%v err=%v want %q,0", di, ok, err, ico)
	}
	t.Logf("CHK-SETUP-18/21 e2e: %s; Start/Desktop .lnk and DisplayIcon -> %s,0", name, ico)
}
