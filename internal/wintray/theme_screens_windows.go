//go:build windows

package wintray

// syncThemeComboHwnd is a no-op: the General theme combo was removed
// (CHK-TRAY-61); chrome theme is the tray menu (CHK-TRAY-63). Kept so
// ui-screens still compile after SetTheme.
func syncThemeComboHwnd(hwnd uintptr) {}
