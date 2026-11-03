package trayui

import (
	"fmt"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
)

// AboutTitle is the About window caption.
func AboutTitle() string { return i18n.T(i18n.KeyAboutTitle) }

// AboutText is the About window body: product, version, the protocol
// version of the connected service, a short description (PLAN §7.3).
func AboutText(st trayipc.Status) string {
	lines := []string{
		version.ProductName,
		fmt.Sprintf(i18n.T(i18n.KeyAboutVersion), version.Version),
	}
	if st.State == trayipc.StateConnected && st.Version > 0 {
		lines = append(lines, fmt.Sprintf(i18n.T(i18n.KeyAboutProtocol), st.Version))
	} else {
		lines = append(lines, i18n.T(i18n.KeyAboutNoService))
	}
	lines = append(lines, "", i18n.T(i18n.KeyAboutText))
	return strings.Join(lines, "\r\n")
}
