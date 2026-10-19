package config

// Executable names of the installed product (PLAN §2, §12 phase 7,
// DELIVERY.md; phase 7a, decision 10/S5). All of them live in one install
// folder next to TrayExeName; the installer (cmd/exchsetup,
// internal/wininstall), the resource generator (tools/mkres, VERSIONINFO
// OriginalFilename) and the service use these names only.
const (
	// ServiceExeName is the Windows service executable (ImagePath of the
	// ExchangeChecker service, EventMessageFile of the Event Log source).
	ServiceExeName = "ExchangeCheckerSvc.exe"
	// SetupExeName is the installer's OriginalFilename / CI intermediate
	// build name (tools/mkres VERSIONINFO). The delivery zip entry is not
	// this name: it is ExchangeChecker-<V>.exe (CHK-DELIV-01,
	// phase 10). The running setup works under any file name.
	SetupExeName = "ExchangeCheckerSetup.exe"
	// UninstallExeName is the payload-free uninstaller copied into the
	// install folder (UninstallString; decision 11, variant C2). Fixed
	// canonical name of the installed copy (phase 10).
	UninstallExeName = "ExchangeCheckerUninstall.exe"
)
