package wininstall

import (
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
)

// TestCHK_SETUP_04_EventSourcePlan — install registers EventMessageFile; uninstall deletes.
func TestCHK_SETUP_04_EventSourcePlan(t *testing.T) {
	f := newFake()
	dir := `C:\EC`
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true, NoStart: true}, DefaultDir: dir, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	want := dir + `\` + config.ServiceExeName
	if f.EventSrc != want {
		t.Fatalf("EventSrc=%q", f.EventSrc)
	}
	f.Installed = Installed{Present: true, Version: "1.0.0", Dir: dir}
	_ = Uninstall(f, UninstallConfig{DataDir: `C:\ProgramData\ExchangeChecker`}, &memLog{})
	if f.EventSrc != "" {
		t.Fatal("event source not deleted")
	}
	if !strings.Contains(EventLogKeyPath, "EventLog") {
		t.Fatal(EventLogKeyPath)
	}
	if EventTypesSupported != 7 {
		t.Fatal(EventTypesSupported)
	}
}
