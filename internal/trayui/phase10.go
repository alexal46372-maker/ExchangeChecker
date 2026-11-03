package trayui

import "github.com/alexal46372-maker/ExchangeChecker/internal/i18n"

// Phase 10 menu commands (CHK-TRAY-33/35/37).
const (
	CmdSound   = 1041 // kept for tests / direct OpenSound; not in tray menu (CHK-TRAY-62)
	CmdVolume  = 1042 // the master volume slider in the menu (CHK-TRAY-96)
	CmdPinHelp = 1043 // «Как закрепить значок…»
)

// Phase10Items are the phase 10 items of the right-click menu (CHK-TRAY-62:
// alarm sound profiles live in Settings → General; CmdSound is omitted).
func Phase10Items() []Item {
	return []Item{
		{ID: CmdVolume, Text: i18n.T(i18n.KeyMenuVolume), Volume: true},
		{ID: CmdPinHelp, Text: i18n.T(i18n.KeyMenuPinHelp)},
	}
}
