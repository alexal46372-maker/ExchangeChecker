//go:build !windows

package wininstall

type nopShellNotifier struct{}

func (nopShellNotifier) UpdateItem(string) {}
func (nopShellNotifier) AssocChanged()     {}

func defaultShellNotifier() ShellNotifier { return nopShellNotifier{} }
