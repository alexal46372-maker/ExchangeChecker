//go:build windows

package wininstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_SETUP_08_ShellLinkRoundTrip — COM IShellLinkW/IPersistFile with
// the documented GUIDs and vtable indices: create a .lnk in a temp folder,
// load it back (GetPath 3, GetArguments 10, GetIconLocation 16), delete.
func TestCHK_SETUP_08_ShellLinkRoundTrip(t *testing.T) {
	dir := t.TempDir()
	win, err := winapi.WindowsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(win, "notepad.exe")
	lnk := filepath.Join(dir, "Exchange Checker.lnk")
	want := winapi.ShortcutSpec{Target: target, Args: "--settings", WorkDir: dir, Description: "EC test", Icon: target, IconIndex: 0}
	var got winapi.ShortcutSpec
	err = winapi.WithCOM(func() error {
		if err := winapi.CreateShortcut(lnk, want); err != nil {
			return err
		}
		var err error
		got, err = winapi.ReadShortcut(lnk)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(got.Target, want.Target) || got.Args != want.Args ||
		!strings.EqualFold(got.Icon, want.Icon) || got.IconIndex != 0 {
		t.Fatalf("read back %+v, want %+v", got, want)
	}
	if err := os.Remove(lnk); err != nil {
		t.Fatal(err)
	}
	if winapi.ShellLinkGetPath != 3 || winapi.ShellLinkSetDescription != 7 || winapi.ShellLinkSetWorkingDir != 9 ||
		winapi.ShellLinkGetArguments != 10 || winapi.ShellLinkSetArguments != 11 || winapi.ShellLinkGetIconLocation != 16 ||
		winapi.ShellLinkSetIconLocation != 17 || winapi.ShellLinkSetPath != 20 || winapi.PersistFileLoad != 5 || winapi.PersistFileSave != 6 {
		t.Fatal("vtable indices changed (docs/PHASE7.md §6.3)")
	}
	if winapi.CLSID_ShellLink.Data1 != 0x00021401 || winapi.IID_IShellLinkW.Data1 != 0x000214F9 || winapi.IID_IPersistFile.Data1 != 0x0000010B {
		t.Fatal("GUIDs changed")
	}
}
