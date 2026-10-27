//go:build windows

package wintray

import (
	"sync"

	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// ThemeValue is the HKCU REG_SZ of the tray chrome theme under LangKey
// (phase 12, CHK-TRAY-55): "light" / "dark"; empty / missing = light.
const ThemeValue = "Theme"

// ThemeStore keeps the per-user light/dark preference.
type ThemeStore interface {
	Load() (trayui.Theme, error)
	Save(trayui.Theme) error
}

// RegistryThemeStore stores Theme in HKCU\Software\ExchangeChecker\Tray.
type RegistryThemeStore struct {
	Key string // "" = LangKey
}

func (s RegistryThemeStore) key() string {
	if s.Key == "" {
		return LangKey
	}
	return s.Key
}

// Load implements ThemeStore; missing = light.
func (s RegistryThemeStore) Load() (trayui.Theme, error) {
	v, ok, err := winapi.RegGetStringView(winapi.HKEY_CURRENT_USER, s.key(), ThemeValue, 0)
	if err != nil || !ok {
		return trayui.ThemeLight, err
	}
	return trayui.NormalizeTheme(v), nil
}

// Save implements ThemeStore.
func (s RegistryThemeStore) Save(t trayui.Theme) error {
	t = trayui.NormalizeTheme(string(t))
	return winapi.RegSetStringView(winapi.HKEY_CURRENT_USER, s.key(), ThemeValue, string(t), 0)
}

// LoadTheme reads the preference (errors / nil store = light) and applies
// it to trayui.
func LoadTheme(s ThemeStore) trayui.Theme {
	t := trayui.ThemeLight
	if s != nil {
		if v, err := s.Load(); err == nil {
			t = trayui.NormalizeTheme(string(v))
		}
	}
	trayui.SetTheme(t)
	return t
}

var (
	themeBrushMu sync.Mutex
	panelBrush   uintptr
	pageBrush    uintptr
	brushTheme   trayui.Theme
)

func ensureThemeBrushes() (panel, page uintptr) {
	themeBrushMu.Lock()
	defer themeBrushMu.Unlock()
	cur := trayui.Current()
	if panelBrush != 0 && brushTheme == cur {
		return panelBrush, pageBrush
	}
	if panelBrush != 0 {
		winapi.DeleteObject(panelBrush)
		panelBrush = 0
	}
	if pageBrush != 0 {
		winapi.DeleteObject(pageBrush)
		pageBrush = 0
	}
	panelBrush = winapi.CreateSolidBrush(trayui.ChromePanel())
	pageBrush = winapi.CreateSolidBrush(trayui.ChromePage())
	brushTheme = cur
	return panelBrush, pageBrush
}

// ApplyWindowTheme sets immersive dark mode (Win10+, Find fallback) and
// invalidates the client so CTLCOLOR / paint pick up the active palette.
func ApplyWindowTheme(hwnd uintptr) {
	if hwnd == 0 || !winapi.IsWindow(hwnd) {
		return
	}
	winapi.SetImmersiveDarkMode(hwnd, trayui.IsDark())
	winapi.RedrawWindow(hwnd, winapi.RDW_INVALIDATE|winapi.RDW_ERASE|winapi.RDW_ALLCHILDREN|winapi.RDW_FRAME)
}

// themeCtlColor answers WM_CTLCOLOR* with the active chrome colours.
func themeCtlColor(msg uint32, wp, _ uintptr) (uintptr, bool) {
	switch msg {
	case winapi.WM_CTLCOLOREDIT, winapi.WM_CTLCOLORSTATIC, winapi.WM_CTLCOLORLISTBOX, winapi.WM_CTLCOLORBTN:
		dc := wp
		winapi.SetTextColor(dc, trayui.ChromeText())
		panel, page := ensureThemeBrushes()
		if msg == winapi.WM_CTLCOLOREDIT {
			winapi.SetBkColor(dc, trayui.ChromePage())
			return page, true
		}
		winapi.SetBkColor(dc, trayui.ChromePanel())
		return panel, true
	}
	return 0, false
}

// themeErase fills the client with the panel brush (WM_ERASEBKGND).
func themeErase(hdc, hwnd uintptr) uintptr {
	panel, _ := ensureThemeBrushes()
	winapi.FillRect(hdc, winapi.GetClientRect(hwnd), panel)
	return 1
}
