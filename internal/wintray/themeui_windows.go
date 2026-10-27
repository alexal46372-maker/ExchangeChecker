//go:build windows

package wintray

import (
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// syncThemeCombo selects Light/Dark from the active trayui theme.
func (w *settingsWindow) syncThemeCombo() {
	h := w.ctl[idGenTheme]
	if h == 0 {
		return
	}
	idx := 0
	if trayui.IsDark() {
		idx = 1
	}
	winapi.SendMessage(h, winapi.CB_SETCURSEL, uintptr(idx), 0)
}

// applyThemeChoice stores the combo choice and repaints this window.
func (w *settingsWindow) applyThemeChoice() {
	h := w.ctl[idGenTheme]
	if h == 0 {
		return
	}
	i := int(winapi.SendMessage(h, winapi.CB_GETCURSEL, 0, 0))
	t := trayui.ThemeLight
	if i == 1 {
		t = trayui.ThemeDark
	}
	trayui.SetTheme(t)
	invalidateThemeBrushes()
	// required-edit brush was created with the previous theme's Required colour
	if w.reqBrush != 0 {
		winapi.DeleteObject(w.reqBrush)
		w.reqBrush = 0
	}
	if w.cfg.themeStore != nil {
		_ = w.cfg.themeStore.Save(t)
	}
	ApplyWindowTheme(w.hwnd)
	if w.host != 0 {
		ApplyWindowTheme(w.host)
	}
	w.syncThemeCombo()
}
