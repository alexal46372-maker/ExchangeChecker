package config

import (
	"fmt"
	"strings"
)

// Tray autostart in HKLM Run (phase 6, CHK-CFG-04, решение Вики 03.10, PLAN
// §6.2, §12 п. 72): the service (LocalSystem) applies settings.trayAutostart
// to the value TrayRunValue of HKLM\RunKeyPath (64-bit registry view) at
// start and after every save_settings; the installer (phase 7) writes the
// initial value and removes it on uninstall.
const (
	// RunKeyPath is the machine Run key (under HKEY_LOCAL_MACHINE).
	RunKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	// TrayRunValue is the name of the tray's value in the Run key.
	TrayRunValue = "ExchangeCheckerTray"
	// TrayExeName is the tray executable, installed in the folder of the
	// service executable (PLAN §2, DELIVERY.md).
	TrayExeName = "ExchangeCheckerTray.exe"
)

// RunRegistry is the Run key as the service needs it: one REG_SZ value
// per name. The Windows implementation is RegistryRun (advapi32 through
// internal/winapi); tests use a fake.
type RunRegistry interface {
	// Get returns the REG_SZ value name; ok = false when it is missing.
	Get(name string) (value string, ok bool, err error)
	// Set writes the REG_SZ value name.
	Set(name, value string) error
	// Delete removes the value name; a missing value is not an error.
	Delete(name string) error
}

// TrayRunCommand is the Run value for a tray installed next to the service
// executable serviceExe: the full path of TrayExeName in double quotes
// ("C:\Program Files\ExchangeChecker\ExchangeCheckerTray.exe"), so a path
// with spaces is never split by the shell.
func TrayRunCommand(serviceExe string) (string, error) {
	i := strings.LastIndexAny(serviceExe, `\/`)
	if i <= 0 {
		return "", fmt.Errorf("autostart: no folder in the service path %q", serviceExe)
	}
	return `"` + serviceExe[:i+1] + TrayExeName + `"`, nil
}

// AutostartAction is what ApplyTrayAutostart did.
type AutostartAction string

// Actions of ApplyTrayAutostart.
const (
	AutostartUnchanged AutostartAction = "unchanged" // already as wanted
	AutostartWritten   AutostartAction = "written"   // value created or replaced
	AutostartDeleted   AutostartAction = "deleted"   // value removed (or absent)
)

// ApplyTrayAutostart brings the Run value TrayRunValue in line with
// enabled: enabled -> the REG_SZ command (written only when missing or
// different); disabled -> the value is removed (missing = nothing to do).
// A registry error is returned; the caller logs it and keeps going (the
// service never stops because of the Run key).
func ApplyTrayAutostart(reg RunRegistry, enabled bool, command string) (AutostartAction, error) {
	if reg == nil {
		return AutostartUnchanged, fmt.Errorf("autostart: no registry")
	}
	cur, ok, err := reg.Get(TrayRunValue)
	if !enabled {
		if err == nil && !ok {
			return AutostartUnchanged, nil
		}
		// a read error does not block the removal: Delete reports its own
		if err := reg.Delete(TrayRunValue); err != nil {
			return AutostartUnchanged, fmt.Errorf("autostart: delete %s: %w", TrayRunValue, err)
		}
		return AutostartDeleted, nil
	}
	if command == "" {
		return AutostartUnchanged, fmt.Errorf("autostart: empty tray command")
	}
	if err == nil && ok && cur == command {
		return AutostartUnchanged, nil
	}
	if err := reg.Set(TrayRunValue, command); err != nil {
		return AutostartUnchanged, fmt.Errorf("autostart: write %s: %w", TrayRunValue, err)
	}
	return AutostartWritten, nil
}
