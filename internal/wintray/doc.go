// Package wintray is the Win32 tray UI (PLAN.md §7, phase 6): hidden
// owner window, NotifyIcon (Shell_NotifyIconW), left/right click popup
// menus built from internal/trayui models (colored squares via
// MENUITEMINFO.hbmpItem, 32-bit DIB), runtime-generated tray icons and the
// About window. WinAPI only through internal/winapi (syscall LazyProc with
// Find, NT 6.1+); no helper processes (POLICY).
package wintray
