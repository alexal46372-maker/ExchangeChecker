package wininstall

import (
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
)

// TestCHK_SETUP_16_DesktopShortcutPlan — Public Desktop .lnk on install,
// removed on uninstall / fresh rollback; preference stored in Uninstall
// DesktopShortcut DWORD; upgrade honors saved /NODESKTOPSHORTCUT.
func TestCHK_SETUP_16_DesktopShortcutPlan(t *testing.T) {
	svc := `C:\EC\` + config.ServiceExeName
	tray := `C:\EC\` + config.TrayExeName

	f := newFake()
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatalf("fresh: code=%d err=%v", res.Code, res.Err)
	}
	if f.DesktopLnk != tray {
		t.Fatalf("fresh desktop=%q want %q", f.DesktopLnk, tray)
	}
	if indexOf(f.Steps, "CreateDesktopShortcut|"+tray) < 0 {
		t.Fatalf("missing CreateDesktopShortcut in %v", f.Steps)
	}
	var deskDWORD *uint32
	for _, v := range f.Uninst {
		if v.Name == "DesktopShortcut" && v.DWord {
			n := v.Num
			deskDWORD = &n
		}
	}
	if deskDWORD == nil || *deskDWORD != 1 {
		t.Fatalf("Uninstall DesktopShortcut DWORD want 1, got %v", deskDWORD)
	}

	// uninstall removes desktop lnk
	Uninstall(f, UninstallConfig{DataDir: `C:\ProgramData\ExchangeChecker`}, &memLog{})
	if f.DesktopLnk != "" {
		t.Fatal("uninstall must delete desktop shortcut")
	}
	if indexOf(f.Steps, "DeleteDesktopShortcut") < 0 {
		t.Fatal("uninstall missing DeleteDesktopShortcut")
	}

	// fresh rollback removes desktop lnk
	r := newFake()
	r.FailAt = "WriteUninstallKey|"
	Install(r, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if r.DesktopLnk != "" {
		t.Fatal("fresh rollback must remove desktop shortcut")
	}

	// /NODESKTOPSHORTCUT: no create; preference 0
	h := newFake()
	res = Install(h, InstallConfig{
		Opts: Options{Silent: true, NoDesktopShortcut: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatalf("nodesktop: code=%d", res.Code)
	}
	if h.DesktopLnk != "" || indexOf(h.Steps, "CreateDesktopShortcut|") >= 0 {
		t.Fatalf("NODESKTOPSHORTCUT must not create: steps=%v", h.Steps)
	}
	for _, v := range h.Uninst {
		if v.Name == "DesktopShortcut" && v.DWord && v.Num != 0 {
			t.Fatalf("DesktopShortcut DWORD want 0, got %d", v.Num)
		}
	}

	// upgrade with saved pref 0 → no create, delete existing
	g := newFake()
	g.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	g.Service = ServiceInfo{Exists: true, ImagePath: `"` + svc + `"`, Running: false}
	g.Dirs[`C:\EC`] = true
	g.DesktopLnk = `C:\old\` + config.TrayExeName
	g.Uninst = UninstallValues(`C:\EC`, `C:\EC\`+config.UninstallExeName, tray, "1.0.0", 1, time.Now(), false)
	opts := ApplyDesktopShortcutPref(Options{Silent: true}, g.Uninst)
	if !opts.NoDesktopShortcut {
		t.Fatal("ApplyDesktopShortcutPref must honor saved 0")
	}
	res = Install(g, InstallConfig{
		Opts: opts, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatalf("upgrade: code=%d err=%v", res.Code, res.Err)
	}
	if g.DesktopLnk != "" {
		t.Fatalf("upgrade with pref 0 must clear desktop lnk, got %q", g.DesktopLnk)
	}
	if indexOf(g.Steps, "DeleteDesktopShortcut") < 0 {
		t.Fatal("upgrade pref 0 must DeleteDesktopShortcut")
	}

	// catalog
	ru, en := CatalogKeys("ru"), CatalogKeys("en")
	if ru["setup.options.desktop"] == "" || en["setup.options.desktop"] == "" {
		t.Fatal("setup.options.desktop missing")
	}
	if !strings.Contains(ru["setup.options.desktop"], "рабочем столе") {
		t.Fatalf("RU desktop label %q", ru["setup.options.desktop"])
	}
	if IDDesktopShortcut != 116 {
		t.Fatalf("IDDesktopShortcut=%d", IDDesktopShortcut)
	}
}

func TestCHK_SETUP_16_ParseNODESKTOPSHORTCUT(t *testing.T) {
	o, err := ParseArgs([]string{"/S", "/NODESKTOPSHORTCUT"})
	if err != nil || !o.NoDesktopShortcut || !o.Silent {
		t.Fatalf("parse: %+v err=%v", o, err)
	}
}
