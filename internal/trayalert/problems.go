package trayalert

import (
	"encoding/json"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

// Phase 10 C (CHK-TRAY-38/39): the problem list behind the blinking icon
// and the panel by the clock.

// Problem kinds (the phase10-svc status.problems[].kind values).
const (
	ProblemNoConnection = "no_connection"
	ProblemError        = "error"
	ProblemWarning      = "warning"
)

// Problem is one entry of the panel.
type Problem struct {
	Kind         string    `json:"kind"`
	ProfileID    string    `json:"profileId"`
	PointKey     string    `json:"pointKey,omitempty"`
	Since        time.Time `json:"since"`
	Acknowledged bool      `json:"acknowledged,omitempty"`
	Repeat       int       `json:"repeat,omitempty"`
	// Text is filled by the tray (localized line).
	Text string `json:"-"`
}

// StatusExt are the optional phase10-svc status fields. Missing (an
// older service) = HasProblems nil: the tray derives the list itself.
type StatusExt struct {
	HasProblems       *bool     `json:"hasProblems,omitempty"`
	Problems          []Problem `json:"problems,omitempty"`
	ProblemsTruncated bool      `json:"problemsTruncated,omitempty"`
}

// ParseStatusExt reads the optional fields from a raw status JSON; any
// error = none (old behaviour).
func ParseStatusExt(raw []byte) StatusExt {
	var x StatusExt
	if len(raw) == 0 || json.Unmarshal(raw, &x) != nil {
		return StatusExt{}
	}
	return x
}

// AlertRepeat is the optional alert_repeat event (capability alert_repeat).
type AlertRepeat struct {
	ProfileID    string           `json:"profileId"`
	Level        model.AlertLevel `json:"level"`
	CheckSeq     uint64           `json:"checkSeq"`
	At           time.Time        `json:"at"`
	Acknowledged bool             `json:"acknowledged"`
}

// CapAlertRepeat is the hello capability of the event.
const CapAlertRepeat = "alert_repeat"

// ParseAlertRepeat reads one event; ok = false when it is not usable.
func ParseAlertRepeat(raw []byte) (AlertRepeat, bool) {
	var r AlertRepeat
	if json.Unmarshal(raw, &r) != nil || r.ProfileID == "" {
		return AlertRepeat{}, false
	}
	return r, true
}

// MaxPanelLines limits the panel (the rest: «… и ещё N»).
const MaxPanelLines = 8

// Problems is the list of current problems: no connection to the
// service, profile errors (no SQL connection) and the active alerts.
// ext (optional) replaces the derived alert entries when the service sends
// them. Acknowledged entries stay listed (the icon keeps blinking until
// recovery; only the window and sound are acknowledged).
func Problems(st trayipc.Status, ext StatusExt, lang string) []Problem {
	switch {
	case st.State == trayipc.StateConnecting:
		return nil // still starting: not a problem yet
	case st.State != trayipc.StateConnected:
		return []Problem{{Kind: ProblemNoConnection, Text: i18n.T(i18n.KeyPanelNoService, lang)}}
	case st.Snapshot == nil:
		return nil
	}
	snap := st.Snapshot
	names := map[string]string{}
	for _, p := range snap.Profiles {
		names[p.ProfileID] = p.Name
	}
	var out []Problem
	if ext.HasProblems != nil {
		if !*ext.HasProblems {
			return nil
		}
		for _, p := range ext.Problems {
			p.Text = extLine(p, names, snap, lang)
			out = append(out, p)
		}
		return out
	}
	seen := map[string]bool{}
	for _, a := range snap.Alerts {
		if a.To <= model.LevelOK {
			continue
		}
		k := ProblemWarning
		if a.To >= model.LevelError {
			k = ProblemError
		}
		if a.ProfileLevel() {
			seen[a.ProfileID] = true
		}
		out = append(out, Problem{Kind: k, ProfileID: a.ProfileID, PointKey: a.Key, Since: a.At,
			Acknowledged: a.Acknowledged, Text: Line(a, lang)})
	}
	for _, p := range snap.Profiles {
		if p.Enabled && p.Error != "" && !seen[p.ProfileID] {
			// not yet debounced into an alert: still a problem for the icon
			out = append(out, Problem{Kind: ProblemError, ProfileID: p.ProfileID, Since: p.CheckedAt,
				Text: Line(model.AlertEvent{ProfileID: p.ProfileID, ProfileName: p.Name, To: model.LevelError,
					Error: p.Error, ErrorKey: p.ErrorKey}, lang)})
		}
	}
	return out
}

func extLine(p Problem, names map[string]string, snap *model.StatusSnapshot, lang string) string {
	for _, a := range snap.Alerts {
		if a.ProfileID == p.ProfileID && a.Key == p.PointKey {
			return Line(a, lang)
		}
	}
	name := names[p.ProfileID]
	if name == "" {
		name = p.ProfileID
	}
	switch p.Kind {
	case ProblemNoConnection:
		return sprintf(i18n.T(i18n.KeyPanelNoSQL, lang), name)
	case ProblemError:
		return name + ": " + levelText(model.LevelError, lang)
	}
	return name + ": " + levelText(model.LevelWarning, lang)
}

// Blink is the icon blink color.
type Blink int

const (
	BlinkNone Blink = iota
	BlinkYellow
	BlinkRed
)

// BlinkFor: red for an error or no connection (service / SQL), yellow for
// warnings only (CHK-TRAY-38).
func BlinkFor(ps []Problem) Blink {
	b := BlinkNone
	for _, p := range ps {
		switch p.Kind {
		case ProblemNoConnection, ProblemError:
			return BlinkRed
		case ProblemWarning:
			b = BlinkYellow
		}
	}
	return b
}

// PanelLines are the panel texts, worst first, limited to MaxPanelLines.
func PanelLines(ps []Problem, lang string) []string {
	var red, yellow []string
	for _, p := range ps {
		if p.Kind == ProblemWarning {
			yellow = append(yellow, p.Text)
		} else {
			red = append(red, p.Text)
		}
	}
	all := append(red, yellow...)
	if len(all) > MaxPanelLines {
		n := len(all) - MaxPanelLines + 1
		all = append(all[:MaxPanelLines-1], sprintf(i18n.T(i18n.KeyAlertMore, lang), n))
	}
	return all
}
