package checks_test

import (
	"encoding/xml"
	"go/build"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// importGraph walks the windows/amd64 import graph of the module package
// rel and returns every imported path (module, std and external).
func importGraph(t *testing.T, root, rel string) map[string]bool {
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
	walk(rel)
	return all
}

// TestCHK_SETUP_11_SetupDeps — CHK-SETUP-11: the installer links no SQL
// driver, no internal/sqlcheck / internal/monitor / internal/ipc, no
// os/exec and no golang.org/x/sys (POLICY §4: WinAPI through syscall only,
// no helper processes); tools/ is never imported by the product.
func TestCHK_SETUP_11_SetupDeps(t *testing.T) {
	root := findModuleRoot(t)
	mod := modulePath(t, root)
	var bad []string
	for imp := range importGraph(t, root, "cmd/exchsetup") {
		switch {
		case imp == "os/exec",
			imp == "database/sql",
			strings.HasPrefix(imp, "github.com/microsoft/go-mssqldb"),
			strings.HasPrefix(imp, "github.com/denisenkom/go-mssqldb"),
			strings.HasPrefix(imp, "golang.org/x/sys"),
			imp == mod+"/internal/sqlcheck",
			imp == mod+"/internal/monitor",
			imp == mod+"/internal/ipc",
			strings.HasPrefix(imp, mod+"/tools/"):
			bad = append(bad, imp)
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("CHK-SETUP-11: forbidden setup dependencies: %v", bad)
	}
	for _, rel := range []string{"cmd/exchsvc", "cmd/exchtray", "cmd/exchsetup"} {
		for imp := range importGraph(t, root, rel) {
			if strings.HasPrefix(imp, mod+"/tools/") {
				t.Errorf("CHK-SETUP-11: %s imports %s (tools are not part of the product)", rel, imp)
			}
		}
	}
}

const (
	osWin7  = "{35138b9a-5d96-4fbd-8e2d-a2440225f93a}"
	osWin8  = "{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}"
	osWin81 = "{1f676c76-80e1-4239-95bb-83d0f6d0da78}"
	osWin10 = "{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"
)

func readManifest(t *testing.T, path string) (manifest, string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := xml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s: manifest XML: %v", path, err)
	}
	return m, string(raw)
}

func checkManifest(t *testing.T, path, level string, comctl bool) {
	t.Helper()
	m, raw := readManifest(t, path)
	if m.Level.Level != level || m.Level.UIAccess != "false" {
		t.Errorf("%s: execution level %+v, want %s", path, m.Level, level)
	}
	has := false
	for _, d := range m.Deps {
		if d.ID.Name == "Microsoft.Windows.Common-Controls" && d.ID.Version == "6.0.0.0" {
			has = true
		}
	}
	if has != comctl {
		t.Errorf("%s: Common Controls v6 = %v, want %v", path, has, comctl)
	}
	got := map[string]bool{}
	for _, o := range m.OS {
		got[strings.ToLower(o.ID)] = true
	}
	for _, id := range []string{osWin7, osWin8, osWin81, osWin10} {
		if !got[id] {
			t.Errorf("%s: supportedOS %s missing", path, id)
		}
	}
	// decision 21: no dpiAware (setup / service; the tray is DPI aware since phase 10 D3)
	if strings.Contains(strings.ToLower(raw), "dpiaware") {
		t.Errorf("%s: dpiAware must not be declared", path)
	}
}

// TestCHK_SETUP_11_Manifest — setup: requireAdministrator, Common Controls
// v6, supportedOS Windows 7 … 10/11, no dpiAware (phase 7a). The committed
// .syso freshness is TestCHK_RES_01_CommittedSysoFresh (tools/mkres).
func TestCHK_SETUP_11_Manifest(t *testing.T) {
	root := findModuleRoot(t)
	checkManifest(t, filepath.Join(root, "cmd", "exchsetup", "exchsetup.manifest"), "requireAdministrator", true)
}

// TestCHK_RES_01_ServiceManifest — service: asInvoker, no Common Controls
// (no UI), supportedOS Windows 7 … 10/11.
func TestCHK_RES_01_ServiceManifest(t *testing.T) {
	root := findModuleRoot(t)
	checkManifest(t, filepath.Join(root, "cmd", "exchsvc", "exchsvc.manifest"), "asInvoker", false)
}
