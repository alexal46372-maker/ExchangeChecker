package trayui

import "github.com/alexal46372-maker/ExchangeChecker/internal/i18n"

// CmdSummary opens the summary dashboard (CHK-TRAY-53).
const CmdSummary = 1040

// Phase11Items: Summary… when the service has capability summary.
func Phase11Items(lang string, hasSummary bool) []Item {
	if !hasSummary {
		return nil
	}
	return []Item{{ID: CmdSummary, Text: i18n.T(i18n.KeyMenuSummary, lang)}}
}
