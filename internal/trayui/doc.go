// Package trayui holds the tray's view models without WinAPI (phase 6a):
// the service indicator (icon, tooltip, status lines, CHK-TRAY-02), the
// left / right click menus (CHK-TRAY-01), the colored squares and icon
// pixels, and the About text. internal/wintray turns them into HMENU /
// HICON / windows. Everything here is pure and tested on every OS.
package trayui
