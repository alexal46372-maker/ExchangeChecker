//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// SetChromeTheme stores Light/Dark (HKCU ThemeStore) and repaints open
// tray windows (CHK-TRAY-63 menu; was the General combo in CHK-TRAY-55).
func (a *App) SetChromeTheme(t trayui.Theme) {
	t = trayui.NormalizeTheme(string(t))
	trayui.SetTheme(t)
	invalidateThemeBrushes()
	if a.opt.ThemeStore != nil {
		_ = a.opt.ThemeStore.Save(t)
	}
	ApplyWindowTheme(a.hwnd)
	if a.settings != nil && winapi.IsWindow(a.settings.hwnd) {
		if a.settings.reqBrush != 0 {
			winapi.DeleteObject(a.settings.reqBrush)
			a.settings.reqBrush = 0
		}
		ApplyWindowTheme(a.settings.hwnd)
		if a.settings.host != 0 {
			ApplyWindowTheme(a.settings.host)
		}
	}
	if a.awin != nil && winapi.IsWindow(a.awin.hwnd) {
		ApplyWindowTheme(a.awin.hwnd)
	}
}

// applyThemeToSettingsWindow repaints a settings window after ThemeStore
// reload (CHK-TRAY-57); theme combo removed in CHK-TRAY-61/63.
func (w *settingsWindow) applyThemeToSettingsWindow() {
	if w == nil || !winapi.IsWindow(w.hwnd) {
		return
	}
	invalidateThemeBrushes()
	if w.reqBrush != 0 {
		winapi.DeleteObject(w.reqBrush)
		w.reqBrush = 0
	}
	ApplyWindowTheme(w.hwnd)
	if w.host != 0 {
		ApplyWindowTheme(w.host)
	}
}
