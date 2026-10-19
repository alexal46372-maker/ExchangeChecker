package trayui

import "github.com/alexal46372-maker/ExchangeChecker/internal/i18n"

// Phase 10 menu commands (CHK-TRAY-33/35/37).
const (
	CmdSound   = 1041 // «Звук тревог…»: the sound profiles window
	CmdVolume  = 1042 // «Громкость…»: master volume by the clock
	CmdPinHelp = 1043 // «Как закрепить значок…»
)

// Phase10Items are the phase 10 items of the right-click menu.
func Phase10Items(lang string) []Item {
	return []Item{
		{ID: CmdSound, Text: i18n.T(i18n.KeyMenuSound, lang)},
		{ID: CmdVolume, Text: i18n.T(i18n.KeyMenuVolume, lang)},
		{ID: CmdPinHelp, Text: i18n.T(i18n.KeyMenuPinHelp, lang)},
	}
}
