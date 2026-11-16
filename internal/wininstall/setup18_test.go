package wininstall

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winres"
)

// committedTrayICO reads cmd/exchtray/exchtray.ico from the repository.
func committedTrayICO(t *testing.T) (string, []byte) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	p := filepath.Join(root, "cmd", "exchtray", "exchtray.ico")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return p, b
}

// TestCHK_SETUP_18_SetupIconSameAsTray — linux/CI: setup .syso embeds the
// same icon bytes as cmd/exchtray/exchtray.ico (single source, CHK-TRAY-89);
// that file = trayui.ExeICO() = AppIconBytes() (the bytes the installer
// writes as exchtray-<version>.ico). Wizard uses LoadAppIcon(1).
func TestCHK_SETUP_18_SetupIconSameAsTray(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	icoPath, ico := committedTrayICO(t)
	gen := trayui.ExeICO()
	if !bytes.Equal(ico, gen) {
		hs := sha256.Sum256(ico)
		hg := sha256.Sum256(gen)
		t.Fatalf("exchtray.ico diverged from trayui.ExeICO: file=%s gen=%s",
			hex.EncodeToString(hs[:8]), hex.EncodeToString(hg[:8]))
	}
	if !bytes.Equal(AppIconBytes(), ico) {
		t.Fatal("AppIconBytes (written as exchtray-<version>.ico) differs from cmd/exchtray/exchtray.ico")
	}
	syso, err := os.ReadFile(filepath.Join(root, "cmd", "exchsetup", "rsrc_windows_amd64.syso"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := winres.ParseSyso(syso)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := winres.Find(res, winres.TypeGroupIcon); !ok {
		t.Fatal("setup .syso has no RT_GROUP_ICON")
	}
	if !bytes.Contains(syso, ico[len(ico)-512:]) {
		t.Fatal("setup .syso does not embed exchtray.ico image bytes")
	}
	chrome, err := os.ReadFile(filepath.Join(root, "internal", "wininstall", "wizard_chrome_windows.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(chrome, []byte("LoadAppIcon(1)")) {
		t.Fatal("wizard must LoadAppIcon(1) from setup exe resource")
	}
	sum := sha256.Sum256(ico)
	t.Logf("shared icon %s sha256=%s (%d bytes)", icoPath, hex.EncodeToString(sum[:]), len(ico))
}

func displayIcon(vals []RegValue) string {
	for _, v := range vals {
		if v.Name == "DisplayIcon" {
			return v.Str
		}
	}
	return ""
}

// TestCHK_SETUP_18_VersionedIconFile — the plan writes
// <dir>\exchtray-<version>.ico with the bytes of cmd/exchtray/exchtray.ico;
// both shortcuts get IconLocation = that file (index 0) while their target
// stays the tray exe; Uninstall DisplayIcon = "<file>,0". Negative: if the
// .ico cannot be written the install still succeeds (WARNING) and every
// icon reference falls back to the tray exe — never to a missing file.
func TestCHK_SETUP_18_VersionedIconFile(t *testing.T) {
	_, want := committedTrayICO(t)
	tray := `C:\EC\` + config.TrayExeName
	ico := `C:\EC\exchtray-0.16.0.ico`

	f := newFake()
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "0.16.0", Now: time.Now(), Notifier: &recNotifier{},
	}, &memLog{})
	if res.Code != ExitOK || len(iconWarnings(res.Warnings)) != 0 {
		t.Fatalf("code=%d err=%v warnings=%v", res.Code, res.Err, res.Warnings)
	}
	got, ok := f.Files[ico]
	if !ok {
		t.Fatalf("%s not written; files=%v", ico, keysOf(f.Files))
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: %d bytes, not cmd/exchtray/exchtray.ico (%d bytes)", ico, len(got), len(want))
	}
	if f.Shortcut != tray || f.DesktopLnk != tray {
		t.Fatalf("shortcut targets changed: start=%q desktop=%q want %q", f.Shortcut, f.DesktopLnk, tray)
	}
	if f.ShortcutIcon != ico || f.DesktopIcon != ico {
		t.Fatalf("IconLocation start=%q desktop=%q want %q", f.ShortcutIcon, f.DesktopIcon, ico)
	}
	if d := displayIcon(f.Uninst); d != ico+",0" {
		t.Fatalf("DisplayIcon=%q want %q", d, ico+",0")
	}
	// the icon is written after the tray exe and before the shortcuts
	iw, tw := indexOf(f.Steps, "WriteFile|"+ico), indexOf(f.Steps, "WriteFile|"+tray)
	if iw < 0 || tw < 0 || iw < tw || iw > indexOf(f.Steps, "CreateStartShortcut|") {
		t.Fatalf("order: tray=%d ico=%d shortcut=%d", tw, iw, indexOf(f.Steps, "CreateStartShortcut|"))
	}

	// negative: the .ico write fails → WARNING, fallback to tray exe, exit 0
	g := newFake()
	g.FailAt = "WriteFile|" + ico
	res = Install(g, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "0.16.0", Now: time.Now(), Notifier: &recNotifier{},
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatalf("icon failure must not fail the install: code=%d err=%v", res.Code, res.Err)
	}
	if len(iconWarnings(res.Warnings)) != 1 || !strings.Contains(iconWarnings(res.Warnings)[0], "файл значка") {
		t.Fatalf("want one WARNING about the icon file, got %v", res.Warnings)
	}
	if _, ok := g.Files[ico]; ok {
		t.Fatal("failed .ico must not be recorded as written")
	}
	if g.ShortcutIcon != tray || g.DesktopIcon != tray || displayIcon(g.Uninst) != tray+",0" {
		t.Fatalf("fallback: start=%q desktop=%q DisplayIcon=%q want %q", g.ShortcutIcon, g.DesktopIcon, displayIcon(g.Uninst), tray)
	}
}

// TestCHK_SETUP_18_UninstallValuesDisplayIcon — DisplayIcon is the given
// icon file at index 0 (not hard-wired to the tray exe).
func TestCHK_SETUP_18_UninstallValuesDisplayIcon(t *testing.T) {
	icon := `C:\Program Files\Exchange Checker\exchtray-1.2.3.ico`
	vals := UninstallValues(`C:\Program Files\Exchange Checker`,
		`C:\Program Files\Exchange Checker\`+config.UninstallExeName, icon,
		"1.2.3", 1, time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), true)
	if d := displayIcon(vals); d != icon+",0" {
		t.Fatalf("DisplayIcon=%q want %q", d, icon+",0")
	}
	if p := iconLocationPath(displayIcon(vals)); p != icon {
		t.Fatalf("iconLocationPath=%q want %q", p, icon)
	}
}

func keysOf(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// iconWarnings: the WARNINGs about the icon file (sampleFiles carries no
// InpOutx64 driver, whose WARNING is CHK-SETUP-20's business).
func iconWarnings(ws []string) []string {
	var out []string
	for _, w := range ws {
		if strings.Contains(w, "значк") {
			out = append(out, w)
		}
	}
	return out
}
