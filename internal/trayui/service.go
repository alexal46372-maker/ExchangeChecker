package trayui

import (
	"fmt"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
)

// Service menu commands (phase 8, CHK-TRAY-16).
const (
	CmdSvcStart = 1021
	CmdSvcStop  = 1022
)

// SvcState is the SCM state of the Exchange Checker service as the tray
// sees it (QueryServiceStatus with SERVICE_QUERY_STATUS only).
type SvcState int

// Service states.
const (
	SvcStateUnknown SvcState = iota
	SvcStateNotInstalled
	SvcStateNoAccess
	SvcStateStopped
	SvcStateStartPending
	SvcStateRunning
	SvcStateStopPending
)

var svcStateKeys = map[SvcState]string{
	SvcStateUnknown: i18n.KeySvcUnknown, SvcStateNotInstalled: i18n.KeySvcNotInstalled, SvcStateNoAccess: i18n.KeySvcNoAccess,
	SvcStateStopped: i18n.KeySvcStopped, SvcStateStartPending: i18n.KeySvcStartPending, SvcStateRunning: i18n.KeySvcRunning,
	SvcStateStopPending: i18n.KeySvcStopPending,
}

// SvcStateText is the state line of the service submenu.
func SvcStateText(s SvcState, lang string) string {
	key, ok := svcStateKeys[s]
	if !ok {
		key = i18n.KeySvcUnknown
	}
	return fmt.Sprintf(i18n.T(i18n.KeySvcState, lang), i18n.T(key, lang))
}

// ServiceMenu is the «Служба» submenu: the disabled state line, then
// «Запустить» (only when stopped) and «Остановить» (only when running).
// Unknown / no access states keep both enabled: the SCM decides and an
// access error is explained after the attempt.
func ServiceMenu(s SvcState, lang string) []Item {
	open := s == SvcStateUnknown || s == SvcStateNoAccess
	return []Item{
		{Text: SvcStateText(s, lang), Disabled: true},
		{Separator: true},
		{ID: CmdSvcStart, Text: i18n.T(i18n.KeyMenuSvcStart, lang), Disabled: !(open || s == SvcStateStopped)},
		{ID: CmdSvcStop, Text: i18n.T(i18n.KeyMenuSvcStop, lang), Disabled: !(open || s == SvcStateRunning)},
	}
}
