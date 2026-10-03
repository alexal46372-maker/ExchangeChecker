package trayui

import (
	"fmt"
	"unicode/utf16"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

// Service is the state of the service as the tray shows it, in priority
// order (a worse state hides the better ones in the icon).
type Service int

const (
	SvcOK Service = iota
	SvcConnecting
	SvcPollerHung    // the service answers, the poll cycle is hung
	SvcNotResponding // connected, no hello / pong in time
	SvcUnavailable   // dial failed / connection lost
	SvcNotRunning    // no pipe: the service is stopped / not installed
	SvcUntrusted     // the pipe is owned by another process (G7)
)

// Icon is the tray icon kind.
type Icon int

const (
	IconOK      Icon = iota // green
	IconWarning             // yellow
	IconError               // red
	IconOffline             // grey: no data from the service
	IconProblem             // grey with a red center: not responding / hung / untrusted
)

// Indicator is the evaluated tray state.
type Indicator struct {
	Service Service
	// Level is the worst level of the enabled databases (last snapshot).
	Level model.AlertLevel
	Icon  Icon
	// ServiceLine is the text of the disabled status line above the
	// databases ("" when the service is fine).
	ServiceLine string
	// ConfigLine is set for config_stale / config_invalid.
	ConfigLine string
	// Fresh: the databases come from a live connection (false = last
	// known data or none).
	Fresh   bool
	Tooltip string
}

// MaxTooltip is the NOTIFYICONDATAW.szTip capacity without the NUL.
const MaxTooltip = 127

// Evaluate derives the indicator from the session status.
func Evaluate(st trayipc.Status, lang string) Indicator {
	ind := Indicator{Service: serviceOf(st)}
	snap := st.Snapshot
	if snap != nil {
		ind.Level = model.WorstProfileLevel(snap.Profiles)
	}
	ind.Fresh = st.State == trayipc.StateConnected && snap != nil
	switch ind.Service {
	case SvcConnecting:
		ind.ServiceLine = i18n.T(i18n.KeySvcConnecting, lang)
	case SvcPollerHung:
		ind.ServiceLine = i18n.T(i18n.KeySvcPollerHung, lang)
	case SvcNotResponding:
		ind.ServiceLine = i18n.T(i18n.KeySvcNotResponding, lang)
	case SvcUnavailable:
		ind.ServiceLine = i18n.T(i18n.KeySvcUnavailable, lang)
	case SvcNotRunning:
		ind.ServiceLine = i18n.T(i18n.KeySvcNotRunning, lang)
	case SvcUntrusted:
		ind.ServiceLine = i18n.T(i18n.KeySvcUntrusted, lang)
	}
	if ind.Service == SvcOK && st.Version == 1 {
		ind.ServiceLine = fmt.Sprintf(i18n.T(i18n.KeySvcOldProtocol, lang), st.Version)
	}
	if ind.Fresh && snap.ConfigErrorKey != "" {
		ind.ConfigLine = i18n.Message(snap.ConfigErrorKey, lang)
	} else if ind.Fresh && snap.ConfigError != "" {
		ind.ConfigLine = i18n.Message(model.MsgMonitorConfigInvalid, lang)
	}
	switch ind.Service {
	case SvcOK:
		ind.Icon = levelIcon(ind.Level)
	case SvcPollerHung, SvcNotResponding, SvcUntrusted:
		ind.Icon = IconProblem
	default:
		ind.Icon = IconOffline
	}
	tip := i18n.T(i18n.KeyAppName, lang) + ": "
	switch {
	case ind.ServiceLine != "" && ind.Service != SvcOK:
		tip += ind.ServiceLine
	case ind.ConfigLine != "":
		tip += levelText(ind.Level, lang) + "; " + ind.ConfigLine
	default:
		tip += levelText(ind.Level, lang)
	}
	ind.Tooltip = TruncateUTF16(tip, MaxTooltip)
	return ind
}

func serviceOf(st trayipc.Status) Service {
	switch st.State {
	case trayipc.StateUntrusted:
		return SvcUntrusted
	case trayipc.StateNotRunning:
		return SvcNotRunning
	case trayipc.StateUnavailable:
		return SvcUnavailable
	case trayipc.StateNotResponding:
		return SvcNotResponding
	case trayipc.StateConnecting:
		return SvcConnecting
	}
	if st.Heartbeat != nil && st.Heartbeat.PollerHung {
		return SvcPollerHung
	}
	if st.Snapshot != nil && st.Snapshot.PollerHung {
		return SvcPollerHung
	}
	return SvcOK
}

func levelIcon(l model.AlertLevel) Icon {
	switch l {
	case model.LevelError:
		return IconError
	case model.LevelWarning:
		return IconWarning
	}
	return IconOK
}

func levelText(l model.AlertLevel, lang string) string {
	switch l {
	case model.LevelError:
		return i18n.T(i18n.KeyLevelError, lang)
	case model.LevelWarning:
		return i18n.T(i18n.KeyLevelWarning, lang)
	}
	return i18n.T(i18n.KeyLevelOK, lang)
}

// TruncateUTF16 cuts s to at most max UTF-16 code units (szTip is
// WCHAR[128]) without splitting a surrogate pair; "…" marks a cut.
func TruncateUTF16(s string, max int) string {
	if len(utf16.Encode([]rune(s))) <= max {
		return s
	}
	out := make([]rune, 0, max)
	n := 1 // room for the ellipsis
	for _, r := range s {
		w := 1
		if r >= 0x10000 {
			w = 2
		}
		if n+w > max {
			break
		}
		out = append(out, r)
		n += w
	}
	return string(out) + "…"
}
