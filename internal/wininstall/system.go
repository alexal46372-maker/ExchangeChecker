package wininstall

import "time"

// ServiceSpec is the SCM registration of the service.
type ServiceSpec struct {
	Name, DisplayName, ImagePath, Description string
	// AutoStart: SERVICE_AUTO_START + delayed (checkbox on); false =
	// SERVICE_DEMAND_START.
	AutoStart     bool
	ResetPeriod   time.Duration
	FailureDelays []time.Duration
	SDDL          string
}

// ServiceInfo is the current registration of an existing service.
type ServiceInfo struct {
	Exists      bool
	ImagePath   string
	Description string
	// DescriptionOK: the description was read (false: query failed, do not
	// overwrite it on rollback).
	DescriptionOK bool
	DisplayName   string
	AutoStart     bool
	Running       bool
}

// Installed describes an existing installation (Uninstall key).
type Installed struct {
	Present bool
	Version string
	Dir     string
}

// System is everything the plans touch. The Windows implementation is
// WinAPI only (system_windows.go); tests use a recording fake. Every
// remove / delete is idempotent: a missing object is success.
type System interface {
	// files and folders
	MkdirAll(dir string) (created bool, err error)
	SetDACL(path, sddl string) error
	// TakeOwnership makes BUILTIN\Administrators the owner of path and
	// everything below when the current owner is neither SYSTEM nor
	// Administrators (decision 16); changed reports a foreign owner.
	TakeOwnership(path string) (changed bool, err error)
	CheckFixedDrive(dir string) error
	// WriteFile writes data via a temporary file + MoveFileEx and reads it
	// back (sha256). reboot is true when a locked target was renamed to
	// *.old and scheduled for deletion at reboot (§7.4).
	WriteFile(path string, data []byte) (reboot bool, err error)
	// BackupFile copies path to path+".bak" when it exists (upgrade).
	// existed is false when path was missing (rollback should RemoveFile).
	BackupFile(path string) (existed bool, err error)
	// RestoreBackup renames path+".bak" over path (upgrade rollback).
	RestoreBackup(path string) error
	// RemoveBackup deletes path+".bak" if present.
	RemoveBackup(path string) error
	// RemoveFile deletes path; a file in use is scheduled for deletion at
	// reboot (reboot = true).
	RemoveFile(path string) (reboot bool, err error)
	RemoveDirIfEmpty(dir string) (reboot bool, err error)
	RemoveAll(dir string) error
	// SelfDelete removes the running uninstaller exe (MoveFileEx out of the
	// folder + delete at reboot).
	SelfDelete(exe string) (reboot bool, err error)

	// service
	QueryService(name string) (ServiceInfo, error)
	CreateService(s ServiceSpec) error
	// UpdateService sets image path, display name, description, failure
	// actions and DACL; the start type is changed only when setStart.
	UpdateService(s ServiceSpec, setStart bool) error
	StartService(name string, timeout time.Duration) error
	StopService(name string, timeout time.Duration) error
	// ForceStopService stops the service; if it stays START_PENDING / will
	// not stop, it terminates only ExchangeCheckerSvc.exe processes whose
	// image lives under installDir (rollback must not hit a locked file).
	ForceStopService(name, installDir string) error
	DeleteService(name string) error

	// registry (HKLM, 64-bit view)
	SetEventSource(msgFile string) error
	DeleteEventSource() error
	ApplyTrayRun(serviceExe, dataDir string) (string, error)
	DeleteTrayRun() error
	// StopTray asks tray processes under installDir to exit: same session
	// gets TrayExitMessage (window classes + broadcast), other sessions get
	// TerminateProcess; leftovers after ~5s are terminated.
	StopTray(installDir string) error
	WriteUninstallKey(vals []RegValue) error
	// ReadUninstallKey returns current Uninstall values (for upgrade rollback).
	ReadUninstallKey() ([]RegValue, error)
	DeleteUninstallKey() error
	ReadInstalled() (Installed, error)
	DeleteUserKey() error
}

// Logger receives the setup log lines.
type Logger interface {
	Printf(format string, args ...interface{})
}
