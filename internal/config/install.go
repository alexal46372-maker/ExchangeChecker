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

// Install folder names under Program Files (CHK-SETUP-17). Visible product
// name is two words ("Exchange Checker"); ProgramData keeps AppDirName
// "ExchangeChecker" so existing configs/secrets paths stay valid.
const (
	InstallDirName       = "Exchange Checker"
	LegacyInstallDirName = "ExchangeChecker"
)

// DeliveryZipName is the technical delivery archive basename (download-stable;
// artifact name matches without .zip). CHK-DELIV-01 / phase 13.
func DeliveryZipName(ver string) string {
	return "ExchangeChecker-" + ver + ".zip"
}

// DeliveryInstallerName is the single zip entry: visible product name +
// version + .exe (spaces allowed), e.g. "Exchange Checker 0.13.0.exe".
func DeliveryInstallerName(ver string) string {
	return "Exchange Checker " + ver + ".exe"
}

// InpOutx64 kernel driver (phase 14, decision of 10.11.2026: the built-in
// «Тон» signal through the PC speaker on Win7 x64). Owner of these names:
// EC Service (feat/svc-speaker opens InpOutDevicePath and starts the
// service on demand); the installer copies the file and registers the
// service (CHK-SETUP-19/20). The file is third_party/inpout/inpoutx64.sys,
// unmodified, from the official highrez distribution (README there).
const (
	// InpOutServiceName is the SERVICE_KERNEL_DRIVER / SERVICE_DEMAND_START
	// service of the driver.
	InpOutServiceName = "ExchangeCheckerInpOut"
	// InpOutDevicePath is the Win32 name of the driver device
	// (\DosDevices\inpoutx64 in hwinterfacedrv.c of the distribution).
	InpOutDevicePath = `\\.\inpoutx64`
	// InpOutRelPath is the driver file relative to the install folder
	// (ImagePath = <install folder>\driver\inpoutx64.sys).
	InpOutRelPath = `driver\inpoutx64.sys`
	// InpOutSHA256 is the sha256 (lower-case hex) of inpoutx64.sys; the
	// service checks the file against it before loading the driver.
	InpOutSHA256 = "f8965fdce668692c3785afa3559159f9a18287bc0d53abb21902895a8ecf221b"
)
