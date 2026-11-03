package trayui

import "github.com/alexal46372-maker/ExchangeChecker/internal/i18n"

// Phase 13a tray-menu theme commands (CHK-TRAY-63); persist via ThemeStore/HKCU
// as CHK-TRAY-55.
const (
	CmdThemeLight = 1051
	CmdThemeDark  = 1052
)

// ThemeCommands maps menu IDs to Theme values.
var ThemeCommands = map[int]Theme{
	CmdThemeLight: ThemeLight,
	CmdThemeDark:  ThemeDark,
}

// RightMenuOpts configures the Phase 13a right-click menu (CHK-TRAY-63).
type RightMenuOpts struct {
	Svc              SvcState
	Theme            Theme
	Balloons         bool
	Connected        bool // «Проверить сейчас» enabled
	SummaryAvailable bool
}

// ThemeMenu is the Light/Dark submenu with the active theme checked.
func ThemeMenu(cur Theme) Item {
	cur = NormalizeTheme(string(cur))
	sub := []Item{
		{ID: CmdThemeLight, Text: i18n.T(i18n.KeyGenThemeLight), Checked: cur == ThemeLight},
		{ID: CmdThemeDark, Text: i18n.T(i18n.KeyGenThemeDark), Checked: cur == ThemeDark},
	}
	return Item{Text: i18n.T(i18n.KeyMenuTheme), Sub: sub}
}

// BuildTrayRightMenu is the Alex right-click order (CHK-TRAY-63 + CHK-TRAY-79):
// Settings → work (no Sound) → sep → Theme → sep → About → sep → Exit.
func BuildTrayRightMenu(o RightMenuOpts) []Item {
	work := []Item{
		{ID: CmdStatusWin, Text: i18n.T(i18n.KeyMenuStatusWin)},
		{ID: CmdCheckNow, Text: i18n.T(i18n.KeyMenuCheckNow), Disabled: !o.Connected},
		{ID: CmdBalloons, Text: i18n.T(i18n.KeyMenuBalloons), Checked: o.Balloons},
		{Text: i18n.T(i18n.KeyMenuService), Sub: ServiceMenu(o.Svc)},
	}
	work = append(work, Phase10Items()...)
	if o.SummaryAvailable {
		work = append(work, Phase11Items(true)...)
	}
	out := []Item{{ID: CmdSettings, Text: i18n.T(i18n.KeyMenuSettings)}}
	out = append(out, work...)
	// CHK-TRAY-79: bottom→top Exit → sep → About → sep → Theme → sep → rest.
	out = append(out,
		Item{Separator: true},
		ThemeMenu(o.Theme),
		Item{Separator: true},
		Item{ID: CmdAbout, Text: i18n.T(i18n.KeyMenuAbout)},
		Item{Separator: true},
		Item{ID: CmdExit, Text: i18n.T(i18n.KeyMenuExit)},
	)
	return out
}
