//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// syncThemeComboHwnd selects Light/Dark on a settings window's theme
// combo (idGenTheme) from the active trayui theme. No-op when the
// control is absent (other windows). Used by ui-screens (CHK-TRAY-55).
func syncThemeComboHwnd(hwnd uintptr) {
	h := winapi.GetDlgItem(hwnd, idGenTheme)
	if h == 0 {
		return
	}
	idx := 0
	if trayui.IsDark() {
		idx = 1
	}
	winapi.SendMessage(h, winapi.CB_SETCURSEL, uintptr(idx), 0)
}
