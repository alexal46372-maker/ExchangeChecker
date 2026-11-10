package inpout

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
)

// TestCHK_SETUP_19_ConstMatchesFile — config.InpOutSHA256 is the sha256 of
// the committed third_party file and of the embedded bytes (lower-case
// hex); a single flipped byte gives a different hash.
func TestCHK_SETUP_19_ConstMatchesFile(t *testing.T) {
	disk, err := os.ReadFile(SysName)
	if err != nil {
		t.Fatal(err)
	}
	sum := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	if got := sum(disk); got != config.InpOutSHA256 {
		t.Fatalf("CHK-SETUP-19: sha256(%s)=%s, config.InpOutSHA256=%s", SysName, got, config.InpOutSHA256)
	}
	if got := sum(SysFile); got != config.InpOutSHA256 {
		t.Fatalf("CHK-SETUP-19: embedded SysFile sha256=%s, want %s", got, config.InpOutSHA256)
	}
	if config.InpOutSHA256 != strings.ToLower(config.InpOutSHA256) || len(config.InpOutSHA256) != 64 {
		t.Fatalf("CHK-SETUP-19: InpOutSHA256 must be 64 lower-case hex chars")
	}
	bad := append([]byte(nil), disk...)
	bad[len(bad)/2] ^= 0x01
	if sum(bad) == config.InpOutSHA256 {
		t.Fatal("CHK-SETUP-19: a flipped byte must change the hash")
	}
	if config.InpOutServiceName != "ExchangeCheckerInpOut" || config.InpOutDevicePath != `\\.\inpoutx64` ||
		config.InpOutRelPath != `driver\`+SysName {
		t.Fatalf("CHK-SETUP-19: names %q %q %q", config.InpOutServiceName, config.InpOutDevicePath, config.InpOutRelPath)
	}
}
