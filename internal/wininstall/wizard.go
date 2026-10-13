package wininstall

import "fmt"

// Wizard model (docs/PHASE7.md §1.3, CHK-SETUP-10): page order, defaults
// and folder validation, free of WinAPI so it is tested on linux; the
// Windows window (wizard_windows.go) only renders it.

// Page is a wizard page.
type Page int

const (
	PageWelcome Page = iota
	PageFolder
	PageOptions
	PageProgress
	PageFinish
)

// WizardState is the wizard's data.
type WizardState struct {
	Page       Page
	Lang       string
	Version    string
	Mode       Mode
	Installed  string // installed version (upgrade/reinstall)
	Dir        string
	DirLocked  bool // upgrade/reinstall: folder is fixed
	DataDir    string
	Shortcut   bool // default on
	Service    bool // "start the service and start it automatically", default on (§D.1)
	LaunchTray bool // Finish page, default on (§D.8)
	Result     *Result
	// CheckDrive validates the drive (fixed disk) — winSystem.CheckFixedDrive.
	CheckDrive func(dir string) error
}

// NewWizardState: defaults per §D.1 / §7.5 (upgrade: service checkbox =
// "was running").
func NewWizardState(lang, ver string, mode Mode, installed, dir string, locked bool, dataDir string, svcWasRunning bool) *WizardState {
	s := &WizardState{
		Lang: NormLang(lang), Version: ver, Mode: mode, Installed: installed,
		Dir: dir, DirLocked: locked, DataDir: dataDir,
		Shortcut: true, Service: true, LaunchTray: true,
	}
	if mode == ModeUpgrade || mode == ModeReinstall || mode == ModeDowngrade {
		s.Service = svcWasRunning
	}
	return s
}

// WelcomeText is the text of page 1.
func (s *WizardState) WelcomeText() string {
	switch s.Mode {
	case ModeUpgrade, ModeDowngrade:
		return T(s.Lang, "setup.welcome.upgrade", s.Installed, s.Version)
	case ModeReinstall:
		return T(s.Lang, "setup.welcome.reinstall", s.Installed)
	}
	return T(s.Lang, "setup.welcome.fresh", s.Version)
}

// Next advances; leaving the folder page validates the folder. From
// Options it moves to Progress (the caller then runs the install).
func (s *WizardState) Next() error {
	switch s.Page {
	case PageWelcome:
		s.Page = PageFolder
	case PageFolder:
		if !s.DirLocked {
			if err := ValidateDir(s.Dir, s.DataDir); err != nil {
				return err
			}
			if s.CheckDrive != nil {
				if err := s.CheckDrive(CleanDir(s.Dir)); err != nil {
					return err
				}
			}
		}
		s.Page = PageOptions
	case PageOptions:
		s.Page = PageProgress
	case PageProgress:
		if s.Result == nil {
			return fmt.Errorf("setup still running")
		}
		s.Page = PageFinish
	}
	return nil
}

// Back goes one page back (not from Progress/Finish).
func (s *WizardState) Back() {
	switch s.Page {
	case PageFolder:
		s.Page = PageWelcome
	case PageOptions:
		s.Page = PageFolder
	}
}

// CanBack / CanCancel drive the button states.
func (s *WizardState) CanBack() bool   { return s.Page == PageFolder || s.Page == PageOptions }
func (s *WizardState) CanCancel() bool { return s.Page < PageProgress }

// Options maps the wizard choices onto the install options.
func (s *WizardState) Options(base Options) Options {
	o := base
	o.Silent = false
	if !s.DirLocked {
		o.Dir = CleanDir(s.Dir)
	}
	o.NoShortcut = !s.Shortcut
	o.NoStart = !s.Service
	if s.Lang != "" {
		o.Lang = s.Lang
	}
	return o
}

// FinishText is the text of the Finish page.
func (s *WizardState) FinishText(logPath string) string {
	r := s.Result
	switch {
	case r == nil:
		return ""
	case r.Code == ExitOK:
		return T(s.Lang, "setup.finish.ok", s.Version)
	case r.Code == ExitRebootRequired:
		return T(s.Lang, "setup.finish.reboot", s.Version)
	}
	msg := "?"
	if r.Err != nil {
		msg = r.Err.Error()
	}
	return T(s.Lang, "setup.finish.error", msg, logPath)
}

// ShowTrayCheckbox: launching the tray is offered only after success.
func (s *WizardState) ShowTrayCheckbox() bool {
	return s.Result != nil && (s.Result.Code == ExitOK || s.Result.Code == ExitRebootRequired)
}
