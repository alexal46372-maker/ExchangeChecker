// Package wininstall is the installer logic of Exchange Checker
// (ExchangeCheckerSetup.exe / ExchangeCheckerUninstall.exe, PLAN.md §9,
// docs/PHASE7.md, PLAN §12 п. 90).
//
// The portable part (this file set without a build tag) parses the command
// line, validates the install folder, applies the version policy and builds
// the step plans for install and uninstall; the steps call a System
// interface. Tests run the plans against a recording fake on every OS
// (CHK-SETUP-01 "dry-run"). The Windows System (system_windows.go) is
// WinAPI only through internal/winapi: SCM, registry, security API, files.
// No sc.exe / PowerShell / reg.exe and no helper processes (POLICY §4).
//
// Check IDs: CHK-SETUP-01 … CHK-SETUP-07, CHK-SETUP-12 (gated E2E).
package wininstall
