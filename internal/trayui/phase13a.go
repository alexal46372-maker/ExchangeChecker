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
	Lang, Pref       string
	Svc              SvcState
	Theme            Theme
	Balloons         bool
	Connected        bool // «Проверить сейчас» enabled
	SummaryAvailable bool
}

// ThemeMenu is the Light/Dark submenu with the active theme checked.
func ThemeMenu(lang string, cur Theme) Item {
	cur = NormalizeTheme(string(cur))
	sub := []Item{
		{ID: CmdThemeLight, Text: i18n.T(i18n.KeyGenThemeLight, lang), Checked: cur == ThemeLight},
		{ID: CmdThemeDark, Text: i18n.T(i18n.KeyGenThemeDark, lang), Checked: cur == ThemeDark},
	}
	return Item{Text: i18n.T(i18n.KeyMenuTheme, lang), Sub: sub}
}

// BuildTrayRightMenu is the Alex right-click order (CHK-TRAY-63):
// Settings → work items (no «Звук тревог») → sep → Theme → sep → About → sep → Exit.
func BuildTrayRightMenu(o RightMenuOpts) []Item {
	lang := o.Lang
	langSub := []Item{
		{ID: CmdLangAuto, Text: i18n.T(i18n.KeyMenuLangAuto, lang)},
		{ID: CmdLangRU, Text: i18n.T(i18n.KeyMenuLangRU, lang)},
		{ID: CmdLangEN, Text: i18n.T(i18n.KeyMenuLangEN, lang)},
	}
	for i := range langSub {
		langSub[i].Checked = LangCommands[langSub[i].ID] == o.Pref
	}
	work := []Item{
		{ID: CmdStatusWin, Text: i18n.T(i18n.KeyMenuStatusWin, lang)},
		{ID: CmdCheckNow, Text: i18n.T(i18n.KeyMenuCheckNow, lang), Disabled: !o.Connected},
		{ID: CmdBalloons, Text: i18n.T(i18n.KeyMenuBalloons, lang), Checked: o.Balloons},
		{Text: i18n.T(i18n.KeyMenuLanguage, lang), Sub: langSub},
		{Text: i18n.T(i18n.KeyMenuService, lang), Sub: ServiceMenu(o.Svc, lang)},
	}
	work = append(work, Phase10Items(lang)...)
	if o.SummaryAvailable {
		work = append(work, Phase11Items(lang, true)...)
	}
	out := []Item{{ID: CmdSettings, Text: i18n.T(i18n.KeyMenuSettings, lang)}}
	out = append(out, work...)
	out = append(out,
		Item{Separator: true},
		ThemeMenu(lang, o.Theme),
		Item{Separator: true},
		Item{ID: CmdAbout, Text: i18n.T(i18n.KeyMenuAbout, lang)},
		Item{Separator: true},
		Item{ID: CmdExit, Text: i18n.T(i18n.KeyMenuExit, lang)},
	)
	return out
}
