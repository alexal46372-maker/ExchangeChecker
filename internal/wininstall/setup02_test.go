package wininstall

import (
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
)

// TestCHK_SETUP_02_PayloadRequired — without payload bytes install refuses.
func TestCHK_SETUP_02_PayloadRequired(t *testing.T) {
	f := newFake()
	for _, files := range []map[string][]byte{
		nil,
		{},
		{config.ServiceExeName: []byte("x")}, // incomplete
	} {
		res := Install(f, InstallConfig{
			Opts: Options{Silent: true}, DefaultDir: `C:\EC`, DataDir: `C:\PD\EC`,
			Files: files, Version: "0.0.0-dev", Now: time.Now(),
		}, &memLog{})
		if res.Code != ExitError {
			t.Fatalf("files=%v code=%d", files, res.Code)
		}
	}
}

// TestCHK_SETUP_02_WriteRecordsHash — fake WriteFile records sha256 prefix.
func TestCHK_SETUP_02_WriteRecordsHash(t *testing.T) {
	f := newFake()
	res := Install(f, InstallConfig{
		Opts: Options{Silent: true, NoStart: true}, DefaultDir: `C:\EC`, DataDir: `C:\ProgramData\ExchangeChecker`,
		Files: sampleFiles(), Version: "0.0.0-dev", Now: time.Now(),
	}, &memLog{})
	if res.Code != ExitOK {
		t.Fatal(res.Err)
	}
	if len(f.Files[joinDir(`C:\EC`, config.ServiceExeName)]) == 0 {
		t.Fatal("svc not written")
	}
}
