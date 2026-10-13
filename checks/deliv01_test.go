package checks_test

import (
	"archive/zip"
	"bytes"
	"debug/pe"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winres"
)

// TestCHK_DELIV_01_Zip — the delivery zip built in the CI job (tools/mkzip,
// never uploaded, CHK-CI-01): exactly one entry ExchangeCheckerSetup.exe,
// byte-identical to the built setup and to a second independent mkzip run
// (deterministic), PE amd64 GUI with VERSIONINFO = EC_BUILD_VERSION; the
// setup carries a gzip payload (smaller than the three exe together).
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
	if want := "ExchangeChecker-" + ver + "-windows-amd64.zip"; filepath.Base(zpath) != want {
		t.Errorf("zip name %s, want %s", filepath.Base(zpath), want)
	}
	zr, err := zip.NewReader(bytes.NewReader(z1), int64(len(z1)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != "ExchangeCheckerSetup.exe" {
		var names []string
		for _, f := range zr.File {
			names = append(names, f.Name)
		}
		t.Fatalf("zip entries %v, want exactly [ExchangeCheckerSetup.exe] (no sources, go.mod, tests, scripts)", names)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	built, err := os.ReadFile(filepath.Join(dir, "ExchangeCheckerSetup.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(exe, built) {
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
	for _, n := range []string{"ExchangeCheckerSvc.exe", "ExchangeCheckerTray.exe", "ExchangeCheckerUninstall.exe"} {
		st, err := os.Stat(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		raw += st.Size()
	}
	if int64(len(exe)) >= raw {
		t.Errorf("setup %d bytes >= uncompressed payload %d: payload not gzip-compressed", len(exe), raw)
	}
	t.Logf("zip %d bytes, setup %d bytes, uncompressed payload %d bytes", len(z1), len(exe), raw)
}
