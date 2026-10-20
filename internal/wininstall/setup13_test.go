package wininstall

import (
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
)

// TestCHK_SETUP_13_PlanFirewall — install adds the SQL Browser firewall
// rule for the service exe; upgrade does Remove then Add (idempotent);
// uninstall removes it; a COM/firewall error is a WARNING, not a failure.
func TestCHK_SETUP_13_PlanFirewall(t *testing.T) {
	svc := `C:\EC\` + config.ServiceExeName

	// fresh install: Add, and rollback removes
	f := newFake()
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatalf("fresh: code=%d err=%v", res.Code, res.Err)
	}
	if f.Firewall != svc {
		t.Fatalf("fresh firewall=%q want %q", f.Firewall, svc)
	}
	add := indexOf(f.Steps, "AddSQLBrowserFirewallRule|"+svc)
	if add < 0 {
		t.Fatalf("missing AddSQLBrowserFirewallRule in %v", f.Steps)
	}
	if indexOf(f.Steps, "WriteUninstallKey|") < add {
		t.Fatal("firewall rule must precede the Uninstall key")
	}

	Uninstall(f, UninstallConfig{DataDir: `C:\ProgramData\ExchangeChecker`}, &memLog{})
	if f.Firewall != "" {
		t.Fatal("uninstall must remove the firewall rule")
	}
	if indexOf(f.Steps, "RemoveSQLBrowserFirewallRule") < 0 {
		t.Fatal("uninstall missing RemoveSQLBrowserFirewallRule")
	}

	// upgrade: Remove-then-Add is inside AddSQLBrowserFirewallRule; plan
	// calls Add again (fake records one Add; winapi does Remove first).
	g := newFake()
	g.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	g.Service = ServiceInfo{Exists: true, ImagePath: `"` + svc + `"`, Running: false}
	g.Dirs[`C:\EC`] = true
	g.Firewall = `C:\old\` + config.ServiceExeName // stale path from a previous layout
	res = Install(g, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.1.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatalf("upgrade: code=%d err=%v", res.Code, res.Err)
	}
	if g.Firewall != svc {
		t.Fatalf("upgrade firewall=%q want %q", g.Firewall, svc)
	}
	if n := countPrefix(g.Steps, "AddSQLBrowserFirewallRule|"); n != 1 {
		t.Fatalf("upgrade Add count=%d steps=%v", n, g.Steps)
	}

	// firewall error on install: WARNING, install still succeeds
	h := newFake()
	h.FailAt = "AddSQLBrowserFirewallRule|"
	log := &memLog{}
	res = Install(h, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, log)
	if res.Code != ExitOK {
		t.Fatalf("firewall fail must not fail install: code=%d err=%v", res.Code, res.Err)
	}
	if len(res.Warnings) == 0 && !strings.Contains(strings.Join(log.lines, "\n"), "WARNING") {
		t.Fatalf("expected WARNING, log=%v warnings=%v", log.lines, res.Warnings)
	}
	if h.Firewall != "" {
		t.Fatal("failed Add must leave firewall unset")
	}

	// fresh rollback removes a rule that was just added
	r := newFake()
	r.FailAt = "WriteUninstallKey|"
	Install(r, InstallConfig{
		Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if r.Firewall != "" {
		t.Fatal("fresh rollback must remove the firewall rule")
	}
	if indexOf(r.Steps, "RemoveSQLBrowserFirewallRule") < 0 {
		t.Fatal("rollback missing RemoveSQLBrowserFirewallRule")
	}

	// uninstall: firewall error is a WARNING, not a failing step
	u := newFake()
	u.Installed = Installed{Present: true, Version: "1.0.0", Dir: `C:\EC`}
	u.Firewall = svc
	u.FailAt = "RemoveSQLBrowserFirewallRule"
	ulog := &memLog{}
	res = Uninstall(u, UninstallConfig{DataDir: `C:\ProgramData\ExchangeChecker`}, ulog)
	if res.Code != ExitOK && res.Code != ExitRebootRequired {
		t.Fatalf("firewall fail must not fail uninstall: code=%d err=%v", res.Code, res.Err)
	}
	if indexOf(u.Steps, "RemoveSQLBrowserFirewallRule") < 0 {
		t.Fatal("uninstall must attempt RemoveSQLBrowserFirewallRule")
	}
	if !strings.Contains(strings.Join(ulog.lines, "\n"), "WARNING") {
		t.Fatalf("expected WARNING on firewall remove failure, log=%v", ulog.lines)
	}
}


func countPrefix(steps []string, prefix string) int {
	n := 0
	for _, s := range steps {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}
