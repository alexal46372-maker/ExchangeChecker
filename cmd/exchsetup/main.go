// Command exchsetup is the installer of Exchange Checker
// (ExchangeCheckerSetup.exe, PLAN.md §9, docs/PHASE7.md).
//
// Phase 7a: still a stub; the exe already carries its resources
// (rsrc_windows_amd64.syso from tools/mkres: requireAdministrator manifest,
// icon, VERSIONINFO — CHK-RES-01, CHK-VER-01, CHK-SETUP-11). The wizard,
// silent mode and the payload come in 7b–7d.
package main

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
)

func main() {
	_ = version.Version
}
