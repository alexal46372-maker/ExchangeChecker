package checks_test

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"go/build"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winres"
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
	"GetOverlappedResult": 5.0, "GetSystemDirectoryW": 5.0, "DeviceIoControl": 5.0, // InpOutx64 port IOCTL (CHK-SVC-12, Windows 2000+)
	"GetUserDefaultUILanguage": 5.0,
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
	"FindWindowW": 5.0, "FindWindowExW": 5.0, "GetWindowThreadProcessId": 5.0, "GetCursorPos": 5.0, "WindowFromPoint": 5.0, "GetDlgItem": 5.0, "GetMenuItemCount": 5.0,
	"GetMenuItemInfoW": 5.0, "GetMessageW": 5.0, "GetSubMenu": 5.0, "GetSystemMetrics": 5.0,
	"GetWindowTextLengthW": 5.0, "GetWindowTextW": 5.0, "InsertMenuItemW": 5.0, "IsWindow": 5.0,
	"LoadCursorW": 5.0, "PeekMessageW": 5.0, "PostMessageW": 5.0, "PostQuitMessage": 5.0,
	"RegisterClassExW": 5.0, "RegisterWindowMessageW": 5.0, "SendMessageW": 5.0,
	"SetForegroundWindow": 5.0, "ShowWindow": 5.0, "TrackPopupMenuEx": 5.0,
	// CHK-TRAY-91: menu click hook (Windows 2000+ / XP+)
	"SetWindowsHookExW": 5.0, "UnhookWindowsHookEx": 5.0, "CallNextHookEx": 5.0, "MenuItemFromPoint": 5.0, "EndMenu": 5.0, "GetCurrentThreadId": 5.0,
	// CHK-TRAY-96: volume slider in the menu (Windows 2000+)
	"GetMenuItemRect": 5.0, "GetSysColor": 5.0, "SetCursorPos": 5.0, "WindowFromDC": 5.0,
	// phase 13 #21 review: the menu font of the slider text (CHK-TRAY-96)
	"CreateFontIndirectW": 5.0,
	// CHK-TRAY-95: full-screen flash overlay (Windows 2000+)
	"SetLayeredWindowAttributes": 5.0, "TranslateMessage": 5.0,
	"UnregisterClassW": 5.0,
	// phase 6b alert window and sound (CHK-TRAY-03)
	// CHK-TRAY-97: last fallback of «Тон» (Windows 2000+)
	"MessageBeep": 5.0,
	"Beep":        5.0, "SetWindowTextW": 5.0, "EnableWindow": 5.0, "IsWindowEnabled": 5.0, "IsWindowVisible": 5.0,
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
	// phase 11: Windows Firewall COM for SQL Browser SSRP replies (CHK-SETUP-13)
	"SysAllocString": 5.0, "SysFreeString": 5.0, "SysStringLen": 5.0, "VariantClear": 5.0,
	"CLSIDFromProgID": 5.0,
	// phase 11: font metrics of the layout framework (CHK-TRAY-45), window
	// capture of the CI screenshots (CHK-TRAY-46)
	"GetTextMetricsW": 5.0, "CreateCompatibleDC": 5.0, "CreateCompatibleBitmap": 5.0, "GetDIBits": 5.0,
	"DeleteDC": 5.0, "BitBlt": 5.0, "GetWindowDC": 5.0, "PrintWindow": 5.1, "RedrawWindow": 5.0, "SetParent": 5.0,
	// phase 11: visible frame for screenshots, falls back to GetWindowRect
	"DwmGetWindowAttribute": 6.0, "DwmFlush": 6.0,
	"SetWindowLongW": 5.0,
	// phase 11: Common Controls 6 for the GUI tests (cues in the screenshots, CHK-TRAY-47)
	"CreateActCtxW": 5.1, "ActivateActCtx": 5.1, "DeactivateActCtx": 5.1, "ReleaseActCtx": 5.1,
	// phase 11: the generated icons on the database cards (CHK-TRAY-49)
	"DrawIconEx": 5.0,
	// phase 14 (CHK-SETUP-19): wintrust — Authenticode check of the
	// InpOutx64 driver in checks/setup19_windows_test.go (test only; the
	// driver service itself uses the SCM exports of phase 7b above:
	// CreateServiceW, ChangeServiceConfigW, ControlService, DeleteService,
	// QueryServiceConfigW, SetServiceObjectSecurity)
	"WinVerifyTrust": 5.0,
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
	if err != nil || len(ico) < 6 || ico[2] != 1 || ico[4] != 4 {
		t.Fatalf("exchtray.ico must be an icon with 16/32/48/256 images (%v)", err)
	}
	// the icon images are embedded too (the 256 PNG is the last image)
	if !bytes.Contains(syso, ico[len(ico)-512:]) {
		t.Error("rsrc_windows_amd64.syso does not contain exchtray.ico")
	}
}

// Phase 15 («metal and chrome», CHK-TRAY-89/90): the icons as Windows
// gets them — RT_GROUP_ICON / RT_ICON of the committed .syso of the tray
// and the installer, and the .ico files the tray embeds — decoded here
// independently of internal/trayui (BMP DIB and image/png).

// iconImg: straight-alpha RGBA, row-major top-down.
type iconImg struct {
	size int
	pix  []color.NRGBA
}

func decodeIconImage(t *testing.T, data []byte, size int) iconImg {
	t.Helper()
	if bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		m, err := png.Decode(bytes.NewReader(data))
		if err != nil || m.Bounds() != image.Rect(0, 0, size, size) {
			t.Fatalf("PNG %d: %v", size, err)
		}
		im := iconImg{size: size}
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				im.pix = append(im.pix, color.NRGBAModel.Convert(m.At(x, y)).(color.NRGBA))
			}
		}
		return im
	}
	le := binary.LittleEndian
	if len(data) < 40+size*size*4 || le.Uint32(data) != 40 || int(le.Uint32(data[4:])) != size ||
		int(le.Uint32(data[8:])) != 2*size || le.Uint16(data[14:]) != 32 {
		t.Fatalf("BMP %d: bad header", size)
	}
	im := iconImg{size: size, pix: make([]color.NRGBA, size*size)}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			o := 40 + ((size-1-y)*size+x)*4
			im.pix[y*size+x] = color.NRGBA{R: data[o+2], G: data[o+1], B: data[o], A: data[o+3]}
		}
	}
	return im
}

func lin8(c uint8) float64 {
	v := float64(c) / 255
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// probe: mean WCAG luminance and mean sRGB over design rects (48×48 grid,
// mapped as u·size/48); every pixel must be opaque.
func (im iconImg) probe(t *testing.T, rs [][4]float64) (float64, [3]float64) {
	t.Helper()
	var l float64
	var c [3]float64
	n := 0
	for _, r := range rs {
		for y := 0; y < im.size; y++ {
			for x := 0; x < im.size; x++ {
				u, v := (float64(x)+0.5)*48/float64(im.size), (float64(y)+0.5)*48/float64(im.size)
				if u < r[0] || u > r[2] || v < r[1] || v > r[3] {
					continue
				}
				p := im.pix[y*im.size+x]
				if p.A != 255 {
					t.Fatalf("%d: probe pixel (%d,%d) not opaque", im.size, x, y)
				}
				l += 0.2126*lin8(p.R) + 0.7152*lin8(p.G) + 0.0722*lin8(p.B)
				c[0], c[1], c[2] = c[0]+float64(p.R), c[1]+float64(p.G), c[2]+float64(p.B)
				n++
			}
		}
	}
	if n == 0 {
		t.Fatalf("%d: empty probe", im.size)
	}
	f := 1 / float64(n)
	return l * f, [3]float64{c[0] * f, c[1] * f, c[2] * f}
}

func hue(c [3]float64) (h, s float64) {
	mx := math.Max(c[0], math.Max(c[1], c[2]))
	mn := math.Min(c[0], math.Min(c[1], c[2]))
	if mx == 0 || mx == mn {
		return 0, 0
	}
	d := mx - mn
	switch mx {
	case c[0]:
		h = math.Mod((c[1]-c[2])/d, 6)
	case c[1]:
		h = (c[2]-c[0])/d + 2
	default:
		h = (c[0]-c[1])/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, d / mx
}

type arrowWant struct {
	hue      float64 // HSV hue of the palette colour; < 0: grey
	contrast float64
}

// palette of phase 15: #3DFF7A, #FFC61A, #FF4F4F, #D2D8E2 on #071A55 → #030B2C
var (
	wantGreen  = arrowWant{138.9, 10}
	wantYellow = arrowWant{45.1, 9}
	wantRed    = arrowWant{0, 4.5}
	wantGrey   = arrowWant{-1, 10}
)

func checkIconArrows(t *testing.T, what string, im iconImg, w arrowWant, problem bool) {
	t.Helper()
	arrow := [][4]float64{{12, 16, 24, 18}, {24, 30, 36, 32}}
	bg := [][4]float64{{8, 7, 22, 11}, {26, 37, 40, 41}}
	if problem { // the red dot covers the bottom right
		arrow, bg = arrow[:1], bg[:1]
	}
	la, ca := im.probe(t, arrow)
	lb, cb := im.probe(t, bg)
	h, s := hue(ca)
	if w.hue < 0 {
		if s > 0.12 {
			t.Errorf("%s %d: grey arrows saturation %.2f", what, im.size, s)
		}
	} else {
		d := math.Abs(h - w.hue)
		if d > 180 {
			d = 360 - d
		}
		if d > 10 || s < 0.5 {
			t.Errorf("%s %d: arrows hue %.1f° sat %.2f, want %.1f° ±10", what, im.size, h, s, w.hue)
		}
	}
	if c := (la + 0.05) / (lb + 0.05); c < w.contrast {
		t.Errorf("%s %d: WCAG arrows / background %.2f < %.1f", what, im.size, c, w.contrast)
	}
	if bh, bs := hue(cb); lb > 0.03 || bh < 200 || bh > 255 || bs < 0.5 {
		t.Errorf("%s %d: background L %.4f hue %.0f is not the dark-blue tile", what, im.size, lb, bh)
	}
}

// icoImages: the images of an .ico file by size.
func icoImages(t *testing.T, ico []byte) map[int][]byte {
	t.Helper()
	le := binary.LittleEndian
	out := map[int][]byte{}
	n := int(le.Uint16(ico[4:]))
	for i := 0; i < n; i++ {
		e := ico[6+16*i:]
		s := int(e[0])
		if s == 0 {
			s = 256
		}
		off, sz := le.Uint32(e[12:]), le.Uint32(e[8:])
		out[s] = ico[off : off+sz]
	}
	return out
}

// TestCHK_TRAY_89_LinkedIconMetal — the exe icon linked into the tray and
// the installer (RT_GROUP_ICON 1 of the committed .syso): images
// 16/32/48/256 (256 as PNG), green arrows of the palette with WCAG ≥ 10 to
// the dark-blue tile at 32 and 256 px.
func TestCHK_TRAY_89_LinkedIconMetal(t *testing.T) {
	root := findModuleRoot(t)
	for _, dir := range []string{"cmd/exchtray", "cmd/exchsetup"} {
		syso, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dir), "rsrc_windows_amd64.syso"))
		if err != nil {
			t.Fatal(err)
		}
		res, err := winres.ParseSyso(syso)
		if err != nil {
			t.Fatal(err)
		}
		group, ok := winres.Find(res, winres.TypeGroupIcon)
		if !ok || group.ID != 1 {
			t.Fatalf("%s: no RT_GROUP_ICON 1", dir)
		}
		le := binary.LittleEndian
		n := int(le.Uint16(group.Data[4:]))
		var sizes []int
		for i := 0; i < n; i++ {
			g := group.Data[6+14*i:]
			s := int(g[0])
			if s == 0 {
				s = 256
			}
			sizes = append(sizes, s)
			id := le.Uint16(g[12:])
			var data []byte
			for _, r := range res {
				if r.Type == winres.TypeIcon && r.ID == id {
					data = r.Data
				}
			}
			if data == nil || int(le.Uint32(g[8:])) != len(data) {
				t.Fatalf("%s: RT_ICON %d missing", dir, id)
			}
			if (s == 256) != bytes.HasPrefix(data, []byte("\x89PNG")) {
				t.Errorf("%s: %d px image PNG mismatch", dir, s)
			}
			im := decodeIconImage(t, data, s)
			if s == 32 || s == 256 {
				checkIconArrows(t, dir, im, wantGreen, false)
			}
		}
		if len(sizes) != 4 || sizes[0] != 16 || sizes[1] != 32 || sizes[2] != 48 || sizes[3] != 256 {
			t.Errorf("%s: exe icon sizes %v, want 16/32/48/256", dir, sizes)
		}
	}
}

// TestCHK_TRAY_90_EmbeddedTrayIcons — the notification icons embedded in
// the tray (internal/wintray/icons/tray_*.ico): all sizes
// 16/20/24/32/40/48/256, state arrows (hue, WCAG at 32 and 256 px: green
// ≥ 10, yellow ≥ 9, red ≥ 4.5, grey ≥ 10); tray_blank (blink «off» phase)
// is byte-identical to tray_ok; state_blank is empty.
func TestCHK_TRAY_90_EmbeddedTrayIcons(t *testing.T) {
	root := findModuleRoot(t)
	dir := filepath.Join(root, "internal", "wintray", "icons")
	read := func(n string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, n+".ico"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for name, w := range map[string]arrowWant{"tray_ok": wantGreen, "tray_warning": wantYellow,
		"tray_error": wantRed, "tray_offline": wantGrey, "tray_problem": wantGrey} {
		imgs := icoImages(t, read(name))
		for _, s := range []int{16, 20, 24, 32, 40, 48, 256} {
			data, ok := imgs[s]
			if !ok {
				t.Fatalf("%s: no %d px image", name, s)
			}
			im := decodeIconImage(t, data, s)
			if s == 32 || s == 256 {
				checkIconArrows(t, name, im, w, name == "tray_problem")
			}
		}
		if len(imgs) != 7 {
			t.Errorf("%s: %d images", name, len(imgs))
		}
	}
	if !bytes.Equal(read("tray_blank"), read("tray_ok")) {
		t.Error("tray_blank.ico must be the normal icon (tray_ok.ico)")
	}
	for s, data := range icoImages(t, read("state_blank")) {
		for _, p := range decodeIconImage(t, data, s).pix {
			if p.A == 255 {
				t.Fatalf("state_blank %d: opaque pixel", s)
			}
		}
	}
}
