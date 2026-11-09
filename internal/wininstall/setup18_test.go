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

// TestCHK_SETUP_18_SetupIconSameAsTray — linux/CI: setup .syso embeds the
// same icon bytes as cmd/exchtray/exchtray.ico (single source, CHK-TRAY-89),
// and that file matches trayui.ExeICO(). Wizard uses LoadAppIcon(1); shortcuts
// / DisplayIcon point at the tray exe (IconIndex 0).
func TestCHK_SETUP_18_SetupIconSameAsTray(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	icoPath := filepath.Join(root, "cmd", "exchtray", "exchtray.ico")
	ico, err := os.ReadFile(icoPath)
	if err != nil {
		t.Fatal(err)
	}
	gen := trayui.ExeICO()
	if !bytes.Equal(ico, gen) {
		hs := sha256.Sum256(ico)
		hg := sha256.Sum256(gen)
		t.Fatalf("exchtray.ico diverged from trayui.ExeICO: file=%s gen=%s",
			hex.EncodeToString(hs[:8]), hex.EncodeToString(hg[:8]))
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
	// Embedded RT_ICON image bytes come from the .ico (same check as tray).
	if !bytes.Contains(syso, ico[len(ico)-512:]) {
		t.Fatal("setup .syso does not embed exchtray.ico image bytes")
	}
	// Source wiring: wizard brand/window icon = LoadAppIcon(1); shortcuts and
	// Uninstall DisplayIcon use the tray exe at index 0.
	chrome, err := os.ReadFile(filepath.Join(root, "internal", "wininstall", "wizard_chrome_windows.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(chrome, []byte("LoadAppIcon(1)")) {
		t.Fatal("wizard must LoadAppIcon(1) from setup exe resource")
	}
	sysWin, err := os.ReadFile(filepath.Join(root, "internal", "wininstall", "system_windows.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(sysWin, []byte("Icon: trayExe, IconIndex: 0")) {
		t.Fatal("shortcuts must set IconLocation to tray exe, index 0")
	}
	sum := sha256.Sum256(ico)
	t.Logf("shared icon %s sha256=%s (%d bytes)", icoPath, hex.EncodeToString(sum[:]), len(ico))
	_ = config.TrayExeName
}


// TestCHK_SETUP_18_DisplayIconTray — Uninstall DisplayIcon is tray.exe,0.
func TestCHK_SETUP_18_DisplayIconTray(t *testing.T) {
	vals := UninstallValues(`C:\Program Files\Exchange Checker`,
		`C:\Program Files\Exchange Checker\`+config.UninstallExeName,
		`C:\Program Files\Exchange Checker\`+config.TrayExeName,
		"1.0.0", 1, time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), true)
	var icon string
	for _, v := range vals {
		if v.Name == "DisplayIcon" {
			icon = v.Str
		}
	}
	want := `C:\Program Files\Exchange Checker\` + config.TrayExeName + ",0"
	if !strings.EqualFold(icon, want) {
		t.Fatalf("DisplayIcon=%q want %q", icon, want)
	}
}
