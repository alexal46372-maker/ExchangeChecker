//go:build !windows

package config

// MachineRunRegistry: there is no HKLM Run outside Windows (nil = the
// service does not manage the tray autostart).
func MachineRunRegistry() RunRegistry { return nil }
