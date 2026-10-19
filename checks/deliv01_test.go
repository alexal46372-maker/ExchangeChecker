package checks_test

import (
	"archive/zip"
	"bytes"
	"debug/pe"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wininstall"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winres"
)

// TestCHK_DELIV_01_Zip — the delivery zip built in the CI job (tools/mkzip,
// never uploaded, CHK-CI-01): exactly one entry named after the zip
// (ExchangeChecker-<V>.exe), byte-identical to the built setup
// and to a second independent mkzip run (deterministic), PE amd64 GUI with
// VERSIONINFO = EC_BUILD_VERSION; the setup carries a gzip payload (smaller
// than the three exe together).
func TestCHK_DELIV_01_Zip(t *testing.T) {
	dir, ver := built(t)
	zpath, zpath2 := os.Getenv("EC_BUILD_ZIP"), os.Getenv("EC_BUILD_ZIP2")
	if zpath == "" || zpath2 == "" {
		t.Skip("EC_BUILD_ZIP / EC_BUILD_ZIP2 not set (CI release-like build step)")
	}
	z1, err := os.ReadFile(zpath)
	if err != nil {
		t.Fatal(err)
	}
	z2, err := os.ReadFile(zpath2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(z1, z2) {
		t.Fatal("zip is not deterministic (two mkzip runs differ)")
	}
	if want := "ExchangeChecker-" + ver + ".zip"; filepath.Base(zpath) != want {
		t.Errorf("zip name %s, want %s", filepath.Base(zpath), want)
	}
	wantEntry := "ExchangeChecker-" + ver + ".exe"
	zr, err := zip.NewReader(bytes.NewReader(z1), int64(len(z1)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != wantEntry {
		var names []string
		for _, f := range zr.File {
			names = append(names, f.Name)
		}
		t.Fatalf("zip entries %v, want exactly [%s] (no sources, go.mod, tests, scripts)", names, wantEntry)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	// Build dir still uses the canonical intermediate name (OriginalFilename).
	builtExe, err := os.ReadFile(filepath.Join(dir, config.SetupExeName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(exe, builtExe) {
		t.Fatal("zip entry differs from the built setup")
	}
	f, err := pe.NewFile(bytes.NewReader(exe))
	if err != nil {
		t.Fatal(err)
	}
	oh, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || !ok || oh.Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_GUI {
		t.Fatalf("setup is not a PE amd64 GUI image (machine %#x)", f.Machine)
	}
	res, err := winres.ParsePE(exe)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := winres.Find(res, winres.TypeVersion)
	if !ok {
		t.Fatal("setup has no VERSIONINFO")
	}
	pv, err := winres.ParseVersionInfo(v.Data)
	if err != nil || pv.Strings["ProductVersion"] != ver {
		t.Fatalf("setup VERSIONINFO %v %v, want %s", pv.Strings, err, ver)
	}
	var raw int64
	for _, n := range []string{config.ServiceExeName, config.TrayExeName, config.UninstallExeName} {
		st, err := os.Stat(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		raw += st.Size()
	}
	if int64(len(exe)) >= raw {
		t.Errorf("setup %d bytes >= uncompressed payload %d: payload not gzip-compressed", len(exe), raw)
	}
	t.Logf("zip %d bytes, setup entry %s (%d bytes), uncompressed payload %d bytes", len(z1), wantEntry, len(exe), raw)
}

// TestCHK_DELIV_01_InstallerAnyName — phase 10 (a)(b): the installer does not
// require its process image to be named ExchangeCheckerSetup.exe; uninstall
// uses the fixed installed name ExchangeCheckerUninstall.exe and SelfExe from
// os.Executable (no basename check against SetupExeName).
func TestCHK_DELIV_01_InstallerAnyName(t *testing.T) {
	root := findModuleRoot(t)
	for _, rel := range []string{"cmd/exchsetup", "internal/wininstall"} {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		for _, e := range ents {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			src, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			// Literal required image name would break a renamed zip entry.
			if bytes.Contains(src, []byte(`"ExchangeCheckerSetup.exe"`)) {
				t.Errorf("%s/%s: hardcodes ExchangeCheckerSetup.exe (installer must work under any file name)", rel, name)
			}
			f, err := parser.ParseFile(fset, name, src, 0)
			if err != nil {
				t.Fatalf("parse %s/%s: %v", rel, name, err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if ok && id.Name == "SetupExeName" {
					t.Errorf("%s/%s: references SetupExeName (process image must not depend on it)", rel, name)
				}
				return true
			})
		}
	}
	// (b) installed copy + UninstallString use the fixed canonical uninstaller name.
	if config.UninstallExeName != "ExchangeCheckerUninstall.exe" {
		t.Fatalf("installed uninstaller name %q", config.UninstallExeName)
	}
	vals := wininstall.UninstallValues(
		`C:\EC`,
		`C:\EC\`+config.UninstallExeName,
		`C:\EC\`+config.TrayExeName,
		"1.0.0",
		1,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	)
	var uninstallStr string
	for _, v := range vals {
		if v.Name == "UninstallString" {
			uninstallStr = v.Str
		}
	}
	if !strings.Contains(uninstallStr, config.UninstallExeName) || strings.Contains(uninstallStr, config.SetupExeName) {
		t.Fatalf("UninstallString %q must point at %s, not Setup", uninstallStr, config.UninstallExeName)
	}
}
