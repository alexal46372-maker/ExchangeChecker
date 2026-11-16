//go:build windows

package wininstall

import "github.com/alexal46372-maker/ExchangeChecker/internal/winapi"

type winShellNotifier struct{}

func (winShellNotifier) UpdateItem(path string) { winapi.SHChangeNotifyUpdateItem(path) }
func (winShellNotifier) AssocChanged()          { winapi.SHChangeNotifyAssocChanged() }

func defaultShellNotifier() ShellNotifier { return winShellNotifier{} }
