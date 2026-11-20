// Package wininstall is the installer logic of Exchange Checker
// (delivery setup (any name) / ExchangeCheckerUninstall.exe, PLAN.md §9,
// docs/PHASE7.md, PLAN §12 п. 90).
//
// The portable part (this file set without a build tag) parses the command
// line, validates the install folder, applies the version policy and builds
// the step plans for install and uninstall; the steps call a System
// interface. Tests run the plans against a recording fake on every OS
// (CHK-SETUP-01 "dry-run"). The Windows System (system_windows.go) is
// WinAPI only through internal/winapi: SCM, registry, security API, files,
// shell links, the firewall COM API, shell change notifications. No sc.exe /
// PowerShell / reg.exe and no helper processes (POLICY §4).
//
// The wizard is Russian only (CHK-TRAY-86); the service checkbox is on the
// "Опции" page, the tray launch checkbox is on the "Готово" page.
//
// Check IDs: CHK-SETUP-01 … CHK-SETUP-21 (see checks/CHECKS.md): 01–07 plan,
// SCM, ACL, Run, Uninstall key; 08–09 shortcut, upgrade; 10, 14, 15 wizard UI;
// 11 build policy; 12 gated E2E (EC_SETUP_E2E=1); 13 firewall; 16 desktop
// shortcut; 17 migration to "Exchange Checker"; 18, 21 icon and icon cache;
// 19, 20 InpOutx64 driver.
package wininstall
