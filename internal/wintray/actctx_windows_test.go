//go:build windows

package wintray

import (
	"path/filepath"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// The test binary has no manifest, so its windows got Common Controls 5:
// no cue banners (EM_SETCUEBANNER needs v6) and classic buttons in the
// screenshots. useComCtl6 activates, on the calling (locked) thread, an
// activation context built from the tray manifest, like the real exe
// (phase 11, CHK-TRAY-47 fix 8); call done at the end (deactivate and
// release). kernel32 is loaded from the system directory (winapi).
func useComCtl6() (done func(), ok bool) {
	path, err := filepath.Abs(filepath.Join("..", "..", "cmd", "exchtray", "exchtray.manifest"))
	if err != nil {
		return func() {}, false
	}
	return winapi.ActivateManifest(path)
}
