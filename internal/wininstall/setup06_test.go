package wininstall

import (
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
)

// TestCHK_SETUP_06_TrayRunPlan — install applies tray run; uninstall deletes after stop.
func TestCHK_SETUP_06_TrayRunPlan(t *testing.T) {
	f := newFake()
	dir := `C:\EC`
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true, NoStart: true}, DefaultDir: dir, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	if f.TrayRun != dir+`\`+config.ServiceExeName {
		t.Fatalf("TrayRun=%q", f.TrayRun)
	}
	cmd, err := config.TrayRunCommand(f.TrayRun)
	if err != nil || !strings.Contains(cmd, config.TrayExeName) {
		t.Fatalf("TrayRunCommand=%q %v", cmd, err)
	}
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: dir}
	f.Service = ServiceInfo{Exists: true, Running: true, ImagePath: ServiceImagePath(dir + `\` + config.ServiceExeName)}
	_ = Uninstall(f, UninstallConfig{DataDir: `C:\ProgramData\ExchangeChecker`}, &memLog{})
	if f.TrayRun != "" {
		t.Fatal("tray run not deleted")
	}
	stop := indexOf(f.Steps, "StopService|")
	del := indexOf(f.Steps, "DeleteTrayRun")
	if stop > del {
		t.Fatalf("stop after DeleteTrayRun: %v", f.Steps)
	}
}
