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
	// SetupExeName is the installer delivered in the zip (CHK-DELIV-01).
	SetupExeName = "ExchangeCheckerSetup.exe"
	// UninstallExeName is the payload-free uninstaller copied into the
	// install folder (UninstallString; decision 11, variant C2).
	UninstallExeName = "ExchangeCheckerUninstall.exe"
)
