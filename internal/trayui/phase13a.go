package trayui

import "github.com/alexal46372-maker/ExchangeChecker/internal/i18n"

// RightMenuOpts configures the Phase 13a right-click menu (CHK-TRAY-63).
type RightMenuOpts struct {
	Svc              SvcState
	Balloons         bool
	Connected        bool // «Проверить сейчас» enabled
	SummaryAvailable bool
}

// BuildTrayRightMenu is the Alex right-click order (CHK-TRAY-63 + CHK-TRAY-79;
// phase 13: no «Тема», CHK-TRAY-88): Settings → work (no Sound) → sep →
// About → sep → Exit.
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
	// CHK-TRAY-79: bottom→top Exit → sep → About → sep → rest.
	out = append(out,
		Item{Separator: true},
		Item{ID: CmdAbout, Text: i18n.T(i18n.KeyMenuAbout)},
		Item{Separator: true},
		Item{ID: CmdExit, Text: i18n.T(i18n.KeyMenuExit)},
	)
	return out
}
