package checks_test

import (
	"bytes"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winres"
)

const versionVar = "github.com/alexal46372-maker/ExchangeChecker/internal/version.Version"

func sysoResources(t *testing.T, root, dir string) []winres.Resource {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, dir, "rsrc_windows_amd64.syso"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := winres.ParseSyso(b)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	return res
}

func peResources(t *testing.T, path string) ([]winres.Resource, *pe.File, []byte) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := pe.NewFile(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	res, err := winres.ParsePE(b)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return res, f, b
}

func goBuild(t *testing.T, root, out, pkg string, ldflags string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-ldflags="+ldflags, "-o", out, pkg)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, b)
	}
}

// TestCHK_RES_01_LinkedExe — the Go linker (go1.20.14, windows/amd64)
// accepts the committed .syso of setup and service: the linked image's
// resources equal the object's, byte for byte; and -X sets
// version.Version (CHK-VER-01, the service prints it at start).
func TestCHK_RES_01_LinkedExe(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not in PATH")
	}
	root := findModuleRoot(t)
	tmp := t.TempDir()
	const v = "9.8.7-chkver.42"
	for _, c := range []struct{ dir, ld string }{
		{"cmd/exchsetup", "-H windowsgui -X " + versionVar + "=" + v},
		{"cmd/exchsvc", "-X " + versionVar + "=" + v},
	} {
		exe := filepath.Join(tmp, filepath.Base(c.dir)+".exe")
		goBuild(t, root, exe, "./"+c.dir, c.ld)
		got, _, img := peResources(t, exe)
		want := sysoResources(t, root, c.dir)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: linked resources differ from the .syso (%d vs %d)", c.dir, len(got), len(want))
		}
		if c.dir == "cmd/exchsvc" {
			if !bytes.Contains(img, []byte(v)) {
				t.Errorf("CHK-VER-01: -X %s=%s not applied to %s", versionVar, v, c.dir)
			}
		}
	}
}

// built returns EC_BUILD_DIR / EC_BUILD_VERSION of the CI release-like
// build step, or skips.
func built(t *testing.T) (dir, ver string) {
	dir, ver = os.Getenv("EC_BUILD_DIR"), os.Getenv("EC_BUILD_VERSION")
	if dir == "" || ver == "" {
		t.Skip("EC_BUILD_DIR / EC_BUILD_VERSION not set (CI release-like build step)")
	}
	return dir, ver
}

func level(t *testing.T, res []winres.Resource) string {
	t.Helper()
	m, ok := winres.Find(res, winres.TypeManifest)
	if !ok {
		t.Fatal("no manifest resource")
	}
	for _, l := range []string{"requireAdministrator", "highestAvailable", "asInvoker"} {
		if bytes.Contains(m.Data, []byte(`level="`+l+`"`)) {
			return l
		}
	}
	return ""
}

// TestCHK_RES_01_Built — release-like exe files of the CI step: PE amd64,
// subsystem (setup/tray GUI, service console), manifests, icon, the
// service's message table with every Event Log ID (CHK-SVC-02 checks it
// against alert.EventIDs() on the EC Service side).
func TestCHK_RES_01_Built(t *testing.T) {
	dir, _ := built(t)
	cases := []struct {
		name, level string
		gui, icon   bool
	}{
		{"ExchangeCheckerSetup.exe", "requireAdministrator", true, true},
		{"ExchangeCheckerSvc.exe", "asInvoker", false, false},
		{"ExchangeCheckerTray.exe", "asInvoker", true, true},
	}
	for _, c := range cases {
		res, f, _ := peResources(t, filepath.Join(dir, c.name))
		oh, ok := f.OptionalHeader.(*pe.OptionalHeader64)
		if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || !ok {
			t.Errorf("%s: not PE32+ amd64", c.name)
			continue
		}
		wantSub := uint16(pe.IMAGE_SUBSYSTEM_WINDOWS_CUI)
		if c.gui {
			wantSub = pe.IMAGE_SUBSYSTEM_WINDOWS_GUI
		}
		if oh.Subsystem != wantSub {
			t.Errorf("%s: subsystem %d, want %d", c.name, oh.Subsystem, wantSub)
		}
		if l := level(t, res); l != c.level {
			t.Errorf("%s: execution level %q, want %q", c.name, l, c.level)
		}
		if _, ok := winres.Find(res, winres.TypeGroupIcon); ok != c.icon {
			t.Errorf("%s: icon %v, want %v", c.name, ok, c.icon)
		}
	}
	res, _, _ := peResources(t, filepath.Join(dir, "ExchangeCheckerSvc.exe"))
	m, ok := winres.Find(res, winres.TypeMessageTable)
	if !ok {
		t.Fatal("ExchangeCheckerSvc.exe: no message table")
	}
	msgs, err := winres.ParseMessageTable(m.Data)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint32{100, 101, 102, 103, 104, 105, 110} {
		if !strings.Contains(msgs[id], "%1") {
			t.Errorf("message %d = %q, want %%1", id, msgs[id])
		}
	}
}

// TestCHK_VER_01_Built — VERSIONINFO of setup and service = EC_BUILD_VERSION
// (FILEVERSION per winres.ParseVersion), and -X put the version string
// into service and tray (the tray gets VERSIONINFO in 7e, T3).
func TestCHK_VER_01_Built(t *testing.T) {
	dir, ver := built(t)
	fv, pre, err := winres.ParseVersion(ver)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, orig string }{
		{"ExchangeCheckerSetup.exe", "ExchangeCheckerSetup.exe"},
		{"ExchangeCheckerSvc.exe", "ExchangeCheckerSvc.exe"},
	} {
		res, _, _ := peResources(t, filepath.Join(dir, c.name))
		v, ok := winres.Find(res, winres.TypeVersion)
		if !ok {
			t.Errorf("%s: no VERSIONINFO", c.name)
			continue
		}
		pv, err := winres.ParseVersionInfo(v.Data)
		if err != nil {
			t.Fatal(err)
		}
		if pv.Strings["ProductVersion"] != ver || pv.Strings["FileVersion"] != ver || pv.FileVersion != fv ||
			pv.ProductVersion != fv || (pv.FileFlags&2 != 0) != pre || pv.Strings["OriginalFilename"] != c.orig {
			t.Errorf("%s: VERSIONINFO %+v, want %s (%v)", c.name, pv, ver, fv)
		}
	}
	for _, name := range []string{"ExchangeCheckerSvc.exe", "ExchangeCheckerTray.exe"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(b, []byte(ver)) {
			t.Errorf("%s: version %s not linked in (-X)", name, ver)
		}
	}
}
