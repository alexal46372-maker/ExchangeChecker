package checks_test

import (
	"bytes"
	"encoding/xml"
	"go/build"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func modulePath(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^module\s+(\S+)`).FindSubmatch(b)
	if m == nil {
		t.Fatal("module line not found")
	}
	return string(m[1])
}

// trayImports walks the windows/amd64 import graph of cmd/exchtray inside
// the module and returns every imported path (module, std and external).
func trayImports(t *testing.T, root string) map[string]bool {
	t.Helper()
	mod := modulePath(t, root)
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = "windows", "amd64", false
	all := map[string]bool{}
	seen := map[string]bool{}
	var walk func(rel string)
	walk = func(rel string) {
		if seen[rel] {
			return
		}
		seen[rel] = true
		p, err := ctx.ImportDir(filepath.Join(root, filepath.FromSlash(rel)), 0)
		if err != nil {
			t.Fatalf("import %s: %v", rel, err)
		}
		for _, imp := range p.Imports {
			all[imp] = true
			if strings.HasPrefix(imp, mod+"/") {
				walk(strings.TrimPrefix(imp, mod+"/"))
			}
		}
	}
	walk("cmd/exchtray")
	return all
}

// TestCHK_TRAY_10_TrayDeps — CHK-TRAY-10: the tray never links the SQL
// driver, the SQL checker, os/exec or golang.org/x/sys (PLAN §12: syscall
// only; the tray talks to the service over the pipe only).
func TestCHK_TRAY_10_TrayDeps(t *testing.T) {
	root := findModuleRoot(t)
	mod := modulePath(t, root)
	imps := trayImports(t, root)
	for _, need := range []string{mod + "/internal/trayipc", mod + "/internal/wintray", mod + "/internal/winapi"} {
		if !imps[need] {
			t.Errorf("tray must import %s", need)
		}
	}
	var bad []string
	for imp := range imps {
		switch {
		case imp == "os/exec",
			strings.HasPrefix(imp, "github.com/microsoft/go-mssqldb"),
			strings.HasPrefix(imp, "github.com/denisenkom/go-mssqldb"),
			imp == "database/sql",
			imp == mod+"/internal/sqlcheck",
			strings.HasPrefix(imp, "golang.org/x/sys"):
			bad = append(bad, imp)
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("CHK-TRAY-10: forbidden tray dependencies: %v", bad)
	}
	gomod, _ := os.ReadFile(filepath.Join(root, "go.mod"))
	if bytes.Contains(gomod, []byte("golang.org/x/sys")) {
		t.Fatal("CHK-TRAY-10: golang.org/x/sys must not be a dependency (syscall only)")
	}
}

// winAPIMinVersion: every WinAPI export the project loads, with its
// documented minimum client version (MSDN "Minimum supported client").
// A new NewProc must be added here; anything newer than NT 6.1
// (Windows 7) fails (PLAN §12).
var winAPIMinVersion = map[string]float64{
	// kernel32
	"ConnectNamedPipe": 5.0, "CreateEventW": 5.0, "CreateMutexW": 5.0, "CreateNamedPipeW": 5.0,
	"DisconnectNamedPipe": 5.0, "GetModuleHandleW": 5.0, "GetNamedPipeServerProcessId": 6.0,
	"GetOverlappedResult": 5.0, "GetSystemDirectoryW": 5.0, "GetUserDefaultUILanguage": 5.0,
	"LocalFree": 5.0, "MoveFileExW": 5.0, "ResetEvent": 5.0, "SetEvent": 5.0,
	"WaitForMultipleObjects": 5.0, "WaitNamedPipeW": 5.0,
	"MultiByteToWideChar": 5.0, // internal/sqlcheck: SSRP ANSI names (CP_ACP), phase 9
	// advapi32
	"CloseServiceHandle": 5.0, "ConvertStringSecurityDescriptorToSecurityDescriptorW": 5.0,
	"DeregisterEventSource": 5.0, "OpenSCManagerW": 5.0, "OpenServiceW": 5.0,
	"QueryServiceStatusEx": 5.0, "RegisterEventSourceW": 5.0, "RegisterServiceCtrlHandlerExW": 5.0,
	"ReportEventW": 5.0, "SetServiceStatus": 5.0, "StartServiceCtrlDispatcherW": 5.0,
	// advapi32 / kernel32 — phase 6 service (G5 admin check, secrets.dpapi DACL, CHK-IPC-06, CHK-CFG-03)
	"CheckTokenMembership": 5.0, "ConvertSecurityDescriptorToStringSecurityDescriptorW": 5.0,
	"CreateRestrictedToken": 5.0, "GetCurrentThread": 5.0, "GetNamedSecurityInfoW": 5.0,
	"ImpersonateLoggedOnUser": 5.0, "ImpersonateNamedPipeClient": 5.0, "OpenThreadToken": 5.0,
	"RevertToSelf": 5.0,
	// advapi32 — phase 6b: secrets.dpapi DACL at service start (CHK-CFG-03)
	"GetSecurityDescriptorDacl": 5.0, "SetNamedSecurityInfoW": 5.0,
	// crypt32
	"CryptProtectData": 5.0, "CryptUnprotectData": 5.0,
	// user32
	"CreateIconIndirect": 5.0, "CreatePopupMenu": 5.0, "CreateWindowExW": 5.0, "DefWindowProcW": 5.0,
	"DestroyIcon": 5.0, "DestroyMenu": 5.0, "DestroyWindow": 5.0, "DispatchMessageW": 5.0,
	"FindWindowW": 5.0, "FindWindowExW": 5.0, "GetWindowThreadProcessId": 5.0, "GetCursorPos": 5.0, "GetDlgItem": 5.0, "GetMenuItemCount": 5.0,
	"GetMenuItemInfoW": 5.0, "GetMessageW": 5.0, "GetSubMenu": 5.0, "GetSystemMetrics": 5.0,
	"GetWindowTextLengthW": 5.0, "GetWindowTextW": 5.0, "InsertMenuItemW": 5.0, "IsWindow": 5.0,
	"LoadCursorW": 5.0, "PeekMessageW": 5.0, "PostMessageW": 5.0, "PostQuitMessage": 5.0,
	"RegisterClassExW": 5.0, "RegisterWindowMessageW": 5.0, "SendMessageW": 5.0,
	"SetForegroundWindow": 5.0, "ShowWindow": 5.0, "TrackPopupMenuEx": 5.0, "TranslateMessage": 5.0,
	"UnregisterClassW": 5.0,
	// phase 6b alert window and sound (CHK-TRAY-03)
	"Beep": 5.0, "SetWindowTextW": 5.0, "EnableWindow": 5.0, "IsWindowEnabled": 5.0, "IsWindowVisible": 5.0,
	"LoadIconW": 5.0, "SetWindowPos": 5.0, "FlashWindowEx": 5.0, "GetForegroundWindow": 5.0,
	// phase 6c settings window (CHK-TRAY-04/05/06)
	"SetFocus": 5.0, "GetFocus": 5.0, "MessageBoxW": 5.0, "GetModuleFileNameW": 5.0,
	"IsDialogMessageW": 5.0, "AllowSetForegroundWindow": 5.0, "GetProcessId": 5.1,
	"GetClassNameW": 5.0,
	// phase 6d fixes: dialog layout within the work area (CHK-TRAY-07)
	"SystemParametersInfoW": 5.0, "AdjustWindowRectEx": 5.0,
	// advapi32 — phase 6e per-user tray language in HKCU (CHK-TRAY-08); the same
	// exports serve the service's HKLM Run tray autostart (CHK-CFG-04, regview_windows.go)
	"RegCreateKeyExW": 5.0, "RegOpenKeyExW": 5.0, "RegQueryValueExW": 5.0, "RegSetValueExW": 5.0,
	"RegDeleteValueW": 5.0, "RegDeleteKeyW": 5.0, "RegCloseKey": 5.0,
	// advapi32 — service: delete a key in an explicit registry view (CHK-CFG-04 test
	// cleanup); Vista / XP x64 (present on Windows 7 x64)
	"RegDeleteKeyExW": 6.0,
	// kernel32 — phase 7a: service message table test (CHK-SVC-02)
	"FormatMessageW": 5.0,
	// gdi32
	"CreateBitmap": 5.0, "CreateDIBSection": 5.0, "DeleteObject": 5.0, "GetStockObject": 5.0,
	// shell32
	"Shell_NotifyIconW": 5.0, "ShellExecuteExW": 5.0,
	// advapi32 / kernel32 — phase 7b installer (CHK-SETUP-03..07)
	"CreateServiceW": 5.0, "ChangeServiceConfigW": 5.0, "ChangeServiceConfig2W": 6.0,
	"QueryServiceConfigW": 5.0, "QueryServiceConfig2W": 5.0,
	"QueryServiceObjectSecurity": 5.0, "SetServiceObjectSecurity": 5.0,
	"StartServiceW": 5.0, "ControlService": 5.0, "DeleteService": 5.0,
	"GetDriveTypeW": 5.0, "GetDiskFreeSpaceExW": 5.0, "GetWindowsDirectoryW": 5.0,
	"OpenProcess": 5.0, "GetLongPathNameW": 5.0, "WaitForSingleObject": 5.0,
	"ProcessIdToSessionId": 5.0, "GetCurrentProcessId": 5.0,
	"CreateToolhelp32Snapshot": 5.0, "Process32FirstW": 5.0, "Process32NextW": 5.0,
	"TerminateProcess": 5.0, "QueryFullProcessImageNameW": 6.0, // + PROCESS_QUERY_LIMITED_INFORMATION (Vista+, NT 6.0; target 6.1 OK)

	"SendMessageTimeoutW": 5.0,
	"OpenProcessToken":    5.0, "LookupPrivilegeValueW": 5.0, "AdjustTokenPrivileges": 5.0,
	// phase 7d wizard / shortcut / tray launch (docs/PHASE7.md App. A)
	"CoInitializeEx": 5.0, "CoUninitialize": 5.0, "CoCreateInstance": 5.0, "CoTaskMemFree": 5.0,
	"OleInitialize": 5.0, "OleUninitialize": 5.0,
	"SHGetFolderPathW": 5.0, "SHChangeNotify": 5.0, "SHBrowseForFolderW": 5.0, "SHGetPathFromIDListW": 5.0,
	"InitCommonControlsEx": 5.0, "MoveWindow": 5.0, "DuplicateTokenEx": 5.0, "CreateProcessWithTokenW": 6.0,
	"SetLastError": 5.0, "GetLastError": 5.0,
	// phase 9a «Состояние» window: NM_CUSTOMDRAW copy (kernel32) and the
	// export save dialog (comdlg32) (CHK-TRAY-19 / CHK-TRAY-21)
	"RtlMoveMemory": 5.0, "GetSaveFileNameW": 5.0,
	// phase 9b history chart, plain GDI (CHK-TRAY-26)
	"BeginPaint": 5.0, "EndPaint": 5.0, "GetClientRect": 5.0, "InvalidateRect": 5.0, "FillRect": 5.0,
	"CreatePen": 5.0, "CreateSolidBrush": 5.0, "SelectObject": 5.0, "MoveToEx": 5.0, "LineTo": 5.0,
	// phase 10 A–C: alarm sounds (winmm), blinking icon timer, sound file
	// dialog, minimized alert window, tray icon rectangle for the panel
	// by the clock (CHK-TRAY-33..39)
	"PlaySoundW": 5.0, "mciSendStringW": 5.0, "mciGetErrorStringW": 5.0,
	"SetTimer": 5.0, "KillTimer": 5.0, "GetOpenFileNameW": 5.0, "IsIconic": 5.0, "SetBkColor": 5.0,
	"Shell_NotifyIconGetRect": 6.1,
	// phase 10 D3: system DPI, Segoe UI 9, the layout overlap test (CHK-TRAY-42)
	"GetDC": 5.0, "ReleaseDC": 5.0, "GetDeviceCaps": 5.0, "CreateFontW": 5.0, "DrawTextW": 5.0,
	"GetWindowRect": 5.0, "GetWindow": 5.0, "GetWindowLongW": 5.0,
	// phase 10 E: scrolled settings pages, controls inside the window (CHK-TRAY-42)
	"SetScrollInfo": 5.0, "ClientToScreen": 5.0,
	"Polyline": 5.0, "Polygon": 5.0, "Ellipse": 5.0, "TextOutW": 5.0, "SetBkMode": 5.0, "SetTextColor": 5.0,
	// phase 9b part 2 «Скопировать SQL-скрипт прав» (CHK-TRAY-32)
	"OpenClipboard": 5.0, "EmptyClipboard": 5.0, "SetClipboardData": 5.0, "CloseClipboard": 5.0,
	"GlobalAlloc": 5.0, "GlobalLock": 5.0, "GlobalUnlock": 5.0, "GlobalFree": 5.0,
	// phase 11: font metrics of the layout framework (CHK-TRAY-45), window
	// capture of the CI screenshots (CHK-TRAY-46)
	"GetTextMetricsW": 5.0, "CreateCompatibleDC": 5.0, "CreateCompatibleBitmap": 5.0, "GetDIBits": 5.0,
	"DeleteDC": 5.0, "BitBlt": 5.0, "GetWindowDC": 5.0, "PrintWindow": 5.1, "RedrawWindow": 5.0, "SetParent": 5.0,
	// phase 11: visible frame for screenshots, falls back to GetWindowRect
	"DwmGetWindowAttribute": 6.0, "DwmFlush": 6.0, "SetWindowLongW": 5.0,
	// phase 11: Common Controls 6 for the GUI tests (cues in the screenshots, CHK-TRAY-47)
	"CreateActCtxW": 5.1, "ActivateActCtx": 5.1,
}

func goSources(t *testing.T, root string, dirs ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, d := range dirs {
		filepath.Walk(filepath.Join(root, d), func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			out[filepath.ToSlash(rel)] = string(b)
			return nil
		})
	}
	return out
}

// TestCHK_TRAY_10_WinAPIMinVersion — every loaded export is known and
// available on NT 6.1; DLLs are loaded lazily (no import table entries
// beyond what the Go runtime itself needs).
func TestCHK_TRAY_10_WinAPIMinVersion(t *testing.T) {
	root := findModuleRoot(t)
	reProc := regexp.MustCompile(`NewProc\("(\w+)"\)`)
	found := 0
	for file, src := range goSources(t, root, "cmd", "internal") {
		for _, m := range reProc.FindAllStringSubmatch(src, -1) {
			found++
			v, ok := winAPIMinVersion[m[1]]
			switch {
			case !ok:
				t.Errorf("CHK-TRAY-10: %s loads %s: not in the allowlist (add it with its documented minimum Windows version)", file, m[1])
			case v > 6.1:
				t.Errorf("CHK-TRAY-10: %s loads %s: needs NT %.1f, newer than Windows 7 (6.1)", file, m[1], v)
			}
		}
		if strings.Contains(src, "MustLoadDLL") || strings.Contains(src, "MustFindProc") {
			t.Errorf("CHK-TRAY-10: %s: use NewLazyDLL/LazyProc with Find(), not MustLoadDLL/MustFindProc", file)
		}
	}
	if found < 40 {
		t.Fatalf("scan found only %d NewProc calls", found)
	}
}

// TestCHK_TRAY_10_CallsThroughFind — WinAPI calls in internal/winapi go
// through callProc, which resolves with LazyProc.Find() first: a missing
// export is an error, never a panic.
func TestCHK_TRAY_10_CallsThroughFind(t *testing.T) {
	root := findModuleRoot(t)
	srcs := goSources(t, root, "cmd", "internal")
	reDirect := regexp.MustCompile(`\bproc\w+\.Call\(`)
	for file, src := range srcs {
		if loc := reDirect.FindStringIndex(src); loc != nil {
			line := strings.Count(src[:loc[0]], "\n") + 1
			t.Errorf("CHK-TRAY-10: %s:%d calls a LazyProc directly; use callProc (Find first)", file, line)
		}
	}
	dll := srcs["internal/winapi/dll_windows.go"]
	i := strings.Index(dll, "func callProc(")
	if i < 0 || !strings.Contains(dll[i:], "p.Find()") {
		t.Fatal("CHK-TRAY-10: callProc must call LazyProc.Find before Call")
	}
	// pointers converted to uintptr in callProc arguments must stay valid
	// (stack growth in Find, GC), like syscall.LazyProc.Call
	// (CRLF on a Windows checkout; the compiler accepts the directive either way)
	if !strings.Contains(strings.ReplaceAll(dll, "\r\n", "\n"), "//go:uintptrescapes\nfunc callProc(") {
		t.Error("CHK-TRAY-10: callProc must be marked //go:uintptrescapes")
	}
}

type manifest struct {
	Deps []struct {
		ID struct {
			Name    string `xml:"name,attr"`
			Version string `xml:"version,attr"`
		} `xml:"dependentAssembly>assemblyIdentity"`
	} `xml:"dependency"`
	Level struct {
		Level    string `xml:"level,attr"`
		UIAccess string `xml:"uiAccess,attr"`
	} `xml:"trustInfo>security>requestedPrivileges>requestedExecutionLevel"`
	OS []struct {
		ID string `xml:"Id,attr"`
	} `xml:"compatibility>application>supportedOS"`
}

// TestCHK_TRAY_10_Manifest — the tray manifest is asInvoker, uses Common
// Controls v6, declares Windows 7+, and the committed .syso (tools/mkres,
// phase 7e T3) was built from it; byte freshness and VERSIONINFO are
// TestCHK_RES_01_CommittedSysoFresh / TestCHK_VER_01_Built.
func TestCHK_TRAY_10_Manifest(t *testing.T) {
	root := findModuleRoot(t)
	dir := filepath.Join(root, "cmd", "exchtray")
	raw, err := os.ReadFile(filepath.Join(dir, "exchtray.manifest"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := xml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest XML: %v", err)
	}
	if m.Level.Level != "asInvoker" || m.Level.UIAccess != "false" {
		t.Errorf("execution level %+v, want asInvoker", m.Level)
	}
	comctl := false
	for _, d := range m.Deps {
		if d.ID.Name == "Microsoft.Windows.Common-Controls" && d.ID.Version == "6.0.0.0" {
			comctl = true
		}
	}
	if !comctl {
		t.Error("Common Controls v6 dependency missing")
	}
	const win7 = "{35138b9a-5d96-4fbd-8e2d-a2440225f93a}"
	hasWin7 := false
	for _, o := range m.OS {
		if strings.EqualFold(o.ID, win7) {
			hasWin7 = true
		}
	}
	if !hasWin7 {
		t.Error("supportedOS Windows 7 missing")
	}
	// phase 10 D3 (CHK-TRAY-42): system DPI aware, no per-monitor mode
	// (Windows 7 has none; the windows scale to the system DPI)
	if !bytes.Contains(raw, []byte(`<dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware>`)) ||
		bytes.Contains(raw, []byte("dpiAwareness")) {
		t.Error("tray manifest: dpiAware true (system), no dpiAwareness")
	}
	syso, err := os.ReadFile(filepath.Join(dir, "rsrc_windows_amd64.syso"))
	if err != nil {
		t.Fatal(err)
	}
	// a Windows checkout may turn LF into CRLF; the .syso holds the LF file
	lf := bytes.ReplaceAll(bytes.TrimSpace(raw), []byte("\r\n"), []byte("\n"))
	if !bytes.Contains(syso, lf) {
		t.Error("rsrc_windows_amd64.syso is stale: run go run ./tools/mkres (cmd/exchtray/RESOURCES.md)")
	}
	ico, err := os.ReadFile(filepath.Join(dir, "exchtray.ico"))
	if err != nil || len(ico) < 6 || ico[2] != 1 || ico[4] < 3 {
		t.Fatalf("exchtray.ico must be an icon with 16/32/48 images (%v)", err)
	}
	// the icon images are embedded too (the 48x48 image bytes)
	if !bytes.Contains(syso, ico[len(ico)-512:]) {
		t.Error("rsrc_windows_amd64.syso does not contain exchtray.ico")
	}
}
