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
// Check IDs: CHK-SETUP-01 … CHK-SETUP-21 (see checks/CHECKS.md):
//   - 01 install / uninstall / purge plans, CLI, exit codes, rollback (fake System);
//   - 02 payload: -tags payload, gzip, sha256 after write, atomic replace;
//   - 03 service in the SCM (own process, LocalSystem, failure actions, delayed start);
//   - 04 Event Log source; 05 ACL of ProgramData and the install folder;
//   - 06 HKLM Run of the tray; 07 Uninstall key, uninstall.exe, purge / keep data;
//   - 08 Start-menu shortcut (IShellLinkW); 09 upgrade, downgrade, rollback of V2;
//   - 10 wizard pages and options; 14 modern wizard chrome; 15 Folder page hint;
//   - 11 setup build policy (dependencies, manifest, exe names);
//   - 12 gated E2E of the real setup (EC_SETUP_E2E=1; 17 and 20 have gated E2E too);
//   - 13 Windows Firewall rule for SQL Browser replies (COM INetFwPolicy2);
//   - 16 Public Desktop shortcut; 17 migration to "Exchange Checker" folder;
//   - 18 setup / shortcut icon (exchtray-<version>.ico); 21 shell icon cache update;
//   - 19 InpOutx64 driver file and signature; 20 driver service step of the plan.
package wininstall
