//go:build windows

package config

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// testRunKey creates a unique test key standing in for HKLM Run (the real
// Run key is never touched on the runner): HKLM\Software\<unique> when the
// test can write HKLM (GitHub runner: administrator), else
// HKCU\Software\<unique>. t.Cleanup deletes the key in the 64-bit view
// (RegDeleteKeyExW + KEY_WOW64_64KEY) and checks with RegOpenKeyExW that
// the key itself is gone (Вика: checking a value proved nothing).
func testRunKey(t *testing.T) (root uintptr, path string, hklm bool) {
	t.Helper()
	const view = winapi.KEY_WOW64_64KEY
	path = fmt.Sprintf(`Software\ExchangeCheckerTest-CFG04-%d-%d`, os.Getpid(), time.Now().UnixNano())
	root, hklm = winapi.HKEY_LOCAL_MACHINE, true
	if err := winapi.RegSetStringView(root, path, "probe", "1", view); err != nil {
		t.Logf("HKLM not writable (%v): using HKCU", err)
		root, hklm = winapi.HKEY_CURRENT_USER, false
		if err := winapi.RegSetStringView(root, path, "probe", "1", view); err != nil {
			t.Fatalf("create test key: %v", err)
		}
	}
	t.Cleanup(func() {
		if err := winapi.RegDeleteKeyView(root, path, view); err != nil {
			t.Errorf("cleanup %s: %v", path, err)
		}
		if ok, err := winapi.RegKeyExistsView(root, path, view); err != nil || ok {
			t.Errorf("cleanup: key %s still exists (%v, %v)", path, ok, err)
		}
	})
	// the existence check sees the key (so the cleanup check can fail)
	if ok, err := winapi.RegKeyExistsView(root, path, view); err != nil || !ok {
		t.Fatalf("test key not found right after creation: %v %v", ok, err)
	}
	if err := winapi.RegDeleteValueView(root, path, "probe", view); err != nil {
		t.Fatal(err)
	}
	return root, path, hklm
}

// TestCHK_CFG_04_RegDeleteKeyView — RegDeleteKeyExW with KEY_WOW64_64KEY
// removes the key (RegOpenKeyExW no longer finds it); a missing key is
// not an error.
func TestCHK_CFG_04_RegDeleteKeyView(t *testing.T) {
	const view = winapi.KEY_WOW64_64KEY
	root := uintptr(winapi.HKEY_CURRENT_USER)
	path := fmt.Sprintf(`Software\ExchangeCheckerTest-CFG04-del-%d-%d`, os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { winapi.RegDeleteKeyView(root, path, view) })
	if err := winapi.RegSetStringView(root, path, "v", "1", view); err != nil {
		t.Fatal(err)
	}
	if ok, err := winapi.RegKeyExistsView(root, path, view); err != nil || !ok {
		t.Fatalf("created key not found: %v %v", ok, err)
	}
	if err := winapi.RegDeleteKeyView(root, path, view); err != nil {
		t.Fatal(err)
	}
	if ok, err := winapi.RegKeyExistsView(root, path, view); err != nil || ok {
		t.Fatalf("key still exists after RegDeleteKeyExW: %v %v", ok, err)
	}
	if err := winapi.RegDeleteKeyView(root, path, view); err != nil {
		t.Fatalf("delete a missing key: %v", err)
	}
}

// TestCHK_CFG_04_RegistryRunWindows — RegistryRun (advapi32 through
// callProc) on a unique test key: REG_SZ written with the quotes, read
// back, rewritten when different, deleted (missing = nil, no key = nil);
// in HKLM the value lands in the 64-bit view only (not in Wow6432Node).
func TestCHK_CFG_04_RegistryRunWindows(t *testing.T) {
	root, path, hklm := testRunKey(t)
	reg := RegistryRun{Root: root, Path: path, View: winapi.KEY_WOW64_64KEY}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := TrayRunCommand(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cmd, `"`) || !strings.HasSuffix(cmd, `\`+TrayExeName+`"`) {
		t.Fatalf("command %s", cmd)
	}
	if v, ok, err := reg.Get(TrayRunValue); err != nil || ok || v != "" {
		t.Fatalf("empty key: %q %v %v", v, ok, err)
	}
	if act, err := ApplyTrayAutostart(reg, true, cmd); err != nil || act != AutostartWritten {
		t.Fatalf("on: %v %v", act, err)
	}
	if v, ok, err := reg.Get(TrayRunValue); err != nil || !ok || v != cmd {
		t.Fatalf("read back: %q %v %v, want %q", v, ok, err, cmd)
	}
	// the same through the tray's existing reader (REG_SZ, quotes kept)
	if v, err := winapi.RegGetString(root, path, TrayRunValue); err != nil || v != cmd {
		t.Fatalf("RegGetString: %q %v", v, err)
	}
	if act, err := ApplyTrayAutostart(reg, true, cmd); err != nil || act != AutostartUnchanged {
		t.Fatalf("on again: %v %v", act, err)
	}
	if err := reg.Set(TrayRunValue, `C:\old\exchtray.exe`); err != nil {
		t.Fatal(err)
	}
	if act, err := ApplyTrayAutostart(reg, true, cmd); err != nil || act != AutostartWritten {
		t.Fatalf("different: %v %v", act, err)
	}
	if hklm {
		// KEY_WOW64_64KEY: the 32-bit view (Wow6432Node) has neither the
		// key nor the value
		if v, ok, err := winapi.RegGetStringView(root, path, TrayRunValue, winapi.KEY_WOW64_32KEY); err != nil || ok {
			t.Errorf("32-bit view sees the value: %q %v %v", v, ok, err)
		}
	}
	if act, err := ApplyTrayAutostart(reg, false, cmd); err != nil || act != AutostartDeleted {
		t.Fatalf("off: %v %v", act, err)
	}
	if _, ok, err := reg.Get(TrayRunValue); err != nil || ok {
		t.Fatalf("after off: %v %v", ok, err)
	}
	if err := reg.Delete(TrayRunValue); err != nil {
		t.Fatalf("delete missing value: %v", err)
	}
	missing := RegistryRun{Root: root, Path: path + `\NoSuchKey`, View: winapi.KEY_WOW64_64KEY}
	if err := missing.Delete(TrayRunValue); err != nil {
		t.Fatalf("delete in a missing key: %v", err)
	}
	if _, ok, err := missing.Get(TrayRunValue); err != nil || ok {
		t.Fatalf("get in a missing key: %v %v", ok, err)
	}
	if m, ok := MachineRunRegistry().(RegistryRun); !ok || m.Root != winapi.HKEY_LOCAL_MACHINE || m.Path != RunKeyPath || m.View != winapi.KEY_WOW64_64KEY {
		t.Errorf("MachineRunRegistry = %+v", MachineRunRegistry())
	}
}

// TestCHK_CFG_04_AccessDeniedCode — ERROR_ACCESS_DENIED (Errno 5) from
// advapi32 is the access_denied code of get_status.trayAutostartError.
func TestCHK_CFG_04_AccessDeniedCode(t *testing.T) {
	if got := AutostartErrorCode(fmt.Errorf("autostart: write: %w", syscall.Errno(5))); got != model.AutostartErrAccessDenied {
		t.Fatalf("Errno 5 -> %q", got)
	}
	if got := AutostartErrorCode(syscall.Errno(2)); got != model.AutostartErrRegistry {
		t.Fatalf("Errno 2 -> %q", got)
	}
}
