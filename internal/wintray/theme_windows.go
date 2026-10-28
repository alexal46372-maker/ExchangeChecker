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

// LoadTheme reads the preference and applies it to trayui.
// A nil store leaves the current theme alone (do not clobber SetTheme —
// screenshots and tests set dark before creating windows via NewApp).
// A store that is missing / unreadable → light.
func LoadTheme(s ThemeStore) trayui.Theme {
	if s == nil {
		return trayui.Current()
	}
	t := trayui.ThemeLight
	if v, err := s.Load(); err == nil {
		t = trayui.NormalizeTheme(string(v))
	}
	trayui.SetTheme(t)
	return t
}

var (
	themeBrushMu sync.Mutex
	panelBrush   uintptr
	pageBrush    uintptr
	lineBrush    uintptr
	brandBrush   uintptr
	brushTheme   trayui.Theme
)

func deleteBrush(h *uintptr) {
	if *h != 0 {
		winapi.DeleteObject(*h)
		*h = 0
	}
}

// invalidateThemeBrushes drops cached chrome brushes (call after SetTheme
// when a settings window keeps a required-field brush, etc.).
func invalidateThemeBrushes() {
	themeBrushMu.Lock()
	defer themeBrushMu.Unlock()
	deleteBrush(&panelBrush)
	deleteBrush(&pageBrush)
	deleteBrush(&lineBrush)
	deleteBrush(&brandBrush)
	brushTheme = ""
}

func ensureThemeBrushes() (panel, page uintptr) {
	panel, page, _, _ = ensureChromeBrushes()
	return panel, page
}

// ensureChromeBrushes returns persistent panel/page/line/brand brushes
// (installer pattern: create once, never Create+Delete per erase).
func ensureChromeBrushes() (panel, page, line, brand uintptr) {
	themeBrushMu.Lock()
	defer themeBrushMu.Unlock()
	cur := trayui.Current()
	if panelBrush != 0 && brushTheme == cur {
		return panelBrush, pageBrush, lineBrush, brandBrush
	}
	deleteBrush(&panelBrush)
	deleteBrush(&pageBrush)
	deleteBrush(&lineBrush)
	deleteBrush(&brandBrush)
	panelBrush = winapi.CreateSolidBrush(trayui.ChromePanel())
	pageBrush = winapi.CreateSolidBrush(trayui.ChromePage())
	lineBrush = winapi.CreateSolidBrush(trayui.ChromeCardFrame())
	brandBrush = winapi.CreateSolidBrush(trayui.ChromeBrandDark())
	brushTheme = cur
	return panelBrush, pageBrush, lineBrush, brandBrush
}

// ApplyWindowTheme sets immersive dark mode (Win10+, Find fallback) and
// invalidates the client so CTLCOLOR / paint pick up the active palette.
func ApplyWindowTheme(hwnd uintptr) {
	if hwnd == 0 || !winapi.IsWindow(hwnd) {
		return
	}
	invalidateThemeBrushes()
	winapi.SetImmersiveDarkMode(hwnd, trayui.IsDark())
	// class brush is COLOR_BTNFACE; force our erase + child CTLCOLOR
	winapi.RedrawWindow(hwnd, winapi.RDW_INVALIDATE|winapi.RDW_ERASE|winapi.RDW_ALLCHILDREN|winapi.RDW_UPDATENOW|winapi.RDW_FRAME)
}

// themeCtlColor answers WM_CTLCOLOR* with the active chrome colours
// for panel/footer frames (STATICs → panel brush). Content hosts use
// contentCtlColor instead (page brush; installer ctlColor pattern).
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

// contentCtlColor answers WM_CTLCOLOR* for scrolled content hosts /
// page panels: every content STATIC/EDIT matches the page brush
// (CHK-TRAY-68; installer setupChrome.ctlColor(content=true)).
func contentCtlColor(msg uint32, wp uintptr) (uintptr, bool) {
	switch msg {
	case winapi.WM_CTLCOLOREDIT, winapi.WM_CTLCOLORSTATIC, winapi.WM_CTLCOLORLISTBOX, winapi.WM_CTLCOLORBTN:
		dc := wp
		winapi.SetBkMode(dc, winapi.TRANSPARENT)
		winapi.SetTextColor(dc, trayui.ChromeText())
		winapi.SetBkColor(dc, trayui.ChromePage())
		_, page := ensureThemeBrushes()
		return page, true
	}
	return 0, false
}

// themeErase fills the client with the panel brush (WM_ERASEBKGND).
func themeErase(hdc, hwnd uintptr) uintptr {
	panel, _ := ensureThemeBrushes()
	winapi.FillRect(hdc, winapi.GetClientRect(hwnd), panel)
	return 1
}
