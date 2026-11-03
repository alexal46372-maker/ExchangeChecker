package trayalert

import (
	"fmt"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// MaxLines of alerts in the window; the rest is «…и ещё N».
const MaxLines = 12

func sprintf(f string, a ...interface{}) string { return fmt.Sprintf(f, a...) }

func levelText(l model.AlertLevel) string {
	if l >= model.LevelError {
		return i18n.T(i18n.KeyLevelError)
	}
	return i18n.T(i18n.KeyLevelWarning)
}

// Title of the alert window (style by the worst level).
func Title(v View) string {
	base := i18n.T(i18n.KeyAlertTitleWarning)
	if v.Level >= model.LevelError {
		base = i18n.T(i18n.KeyAlertTitleError)
	}
	for _, a := range v.Alerts {
		if a.Test {
			return "[" + i18n.T(i18n.KeyAlertTestBadge) + "] " + base
		}
	}
	return base
}

// Line describes one alert.
func Line(a model.AlertEvent) string {
	db := a.ProfileName
	if db == "" {
		db = a.ProfileID
	}
	lvl := levelText(a.To)
	if a.ProfileLevel() {
		why := ""
		if a.ErrorKey != "" {
			why = i18n.Message(a.ErrorKey)
		} else if a.Error != "" {
			why = a.Error
		}
		if why == "" {
			return db + ": " + lvl
		}
		return sprintf(i18n.T(i18n.KeyAlertProfile), db, lvl, why)
	}
	pt := a.PointName
	if pt == "" {
		pt = a.Key
	}
	if a.FreshAt == nil {
		return sprintf(i18n.T(i18n.KeyAlertPointNever), db, pt, lvl)
	}
	return sprintf(i18n.T(i18n.KeyAlertPoint), db, pt, lvl, a.LagMinutes)
}

// Text is the window body: heading and the alerts, worst first, lines
// separated by CRLF (STATIC control).
func Text(v View) string {
	head := i18n.T(i18n.KeyAlertHeadWarning)
	if v.Level >= model.LevelError {
		head = i18n.T(i18n.KeyAlertHeadError)
	}
	lines := []string{head}
	for i, a := range v.Alerts {
		if i == MaxLines {
			lines = append(lines, sprintf(i18n.T(i18n.KeyAlertMore), len(v.Alerts)-MaxLines))
			break
		}
		line := Line(a)
		if a.Test {
			line = "[" + i18n.T(i18n.KeyAlertTestBadge) + "] " + line
		}
		lines = append(lines, "• "+line)
	}
	return strings.Join(lines, "\r\n")
}
