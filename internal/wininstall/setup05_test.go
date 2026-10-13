package wininstall

import (
	"strings"
	"testing"
	"time"
)

// TestCHK_SETUP_05_ACLConstants — ProgramData without BU; install dir with BU FRFX.
func TestCHK_SETUP_05_ACLConstants(t *testing.T) {
	if strings.Contains(DataDirSDDL, "BU") {
		t.Fatalf("ProgramData must not grant Users: %s", DataDirSDDL)
	}
	if !strings.Contains(InstallDirSDDL, "FRFX;;;BU") {
		t.Fatalf("install dir must grant Users FRFX: %s", InstallDirSDDL)
	}
	f := newFake()
	dir := `D:\Apps\ExchangeChecker`
	data := `C:\ProgramData\ExchangeChecker`
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true, NoStart: true, Dir: dir}, DefaultDir: `C:\PF\EC`, DataDir: data,
		Files: sampleFiles(), Version: "1.0.0", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	if f.DACLs[data] != DataDirSDDL {
		t.Fatalf("data DACL=%q", f.DACLs[data])
	}
	if f.DACLs[dir] != InstallDirSDDL {
		t.Fatalf("install DACL=%q", f.DACLs[dir])
	}
	if !f.Owners[data] {
		t.Fatal("TakeOwnership not called on data dir")
	}
}
