//go:build windows

package config

import "github.com/alexal46372-maker/ExchangeChecker/internal/winapi"

// RegistryRun is a RunRegistry over one registry key through advapi32
// (internal/winapi, callProc). View is the registry view flag
// (winapi.KEY_WOW64_64KEY for the service; 0 = the process default).
type RegistryRun struct {
	Root uintptr
	Path string
	View uint32
}

// MachineRunRegistry is HKLM\Software\Microsoft\Windows\CurrentVersion\Run
// in the 64-bit registry view (KEY_WOW64_64KEY): the value is the same for
// every user and is not redirected to Wow6432Node.
func MachineRunRegistry() RunRegistry {
	return RegistryRun{Root: winapi.HKEY_LOCAL_MACHINE, Path: RunKeyPath, View: winapi.KEY_WOW64_64KEY}
}

// Get implements RunRegistry.
func (r RegistryRun) Get(name string) (string, bool, error) {
	return winapi.RegGetStringView(r.Root, r.Path, name, r.View)
}

// Set implements RunRegistry.
func (r RegistryRun) Set(name, value string) error {
	return winapi.RegSetStringView(r.Root, r.Path, name, value, r.View)
}

// Delete implements RunRegistry.
func (r RegistryRun) Delete(name string) error {
	return winapi.RegDeleteValueView(r.Root, r.Path, name, r.View)
}
