// Package config reads and writes Exchange Checker configuration files
// (PLAN.md §4.1) under %ProgramData%\ExchangeChecker\:
//
//	settings.json  global monitoring settings (model.Settings)
//	profiles.json  SQL profiles without passwords (model.Profile)
//	secrets.dpapi  profile passwords, DPAPI machine-key protected
//
// Saves are atomic (temp file in the same directory + MoveFileEx replace).
// Check IDs: CHK-CFG-01, CHK-CFG-02.
package config
