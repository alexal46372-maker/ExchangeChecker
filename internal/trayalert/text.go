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

func levelText(l model.AlertLevel, lang string) string {
	if l >= model.LevelError {
		return i18n.T(i18n.KeyLevelError, lang)
	}
	return i18n.T(i18n.KeyLevelWarning, lang)
}

// Title of the alert window (style by the worst level).
func Title(v View, lang string) string {
	base := i18n.T(i18n.KeyAlertTitleWarning, lang)
	if v.Level >= model.LevelError {
		base = i18n.T(i18n.KeyAlertTitleError, lang)
	}
	for _, a := range v.Alerts {
		if a.Test {
			return "[" + i18n.T(i18n.KeyAlertTestBadge, lang) + "] " + base
		}
	}
	return base
}

// Line describes one alert.
func Line(a model.AlertEvent, lang string) string {
	db := a.ProfileName
	if db == "" {
		db = a.ProfileID
	}
	lvl := levelText(a.To, lang)
	if a.ProfileLevel() {
		why := ""
		if a.ErrorKey != "" {
			why = i18n.Message(a.ErrorKey, lang)
		} else if a.Error != "" {
			why = a.Error
		}
		if why == "" {
			return db + ": " + lvl
		}
		return sprintf(i18n.T(i18n.KeyAlertProfile, lang), db, lvl, why)
	}
	pt := a.PointName
	if pt == "" {
		pt = a.Key
	}
	if a.FreshAt == nil {
		return sprintf(i18n.T(i18n.KeyAlertPointNever, lang), db, pt, lvl)
	}
	return sprintf(i18n.T(i18n.KeyAlertPoint, lang), db, pt, lvl, a.LagMinutes)
}

// Text is the window body: heading and the alerts, worst first, lines
// separated by CRLF (STATIC control).
func Text(v View, lang string) string {
	head := i18n.T(i18n.KeyAlertHeadWarning, lang)
	if v.Level >= model.LevelError {
		head = i18n.T(i18n.KeyAlertHeadError, lang)
	}
	lines := []string{head}
	for i, a := range v.Alerts {
		if i == MaxLines {
			lines = append(lines, sprintf(i18n.T(i18n.KeyAlertMore, lang), len(v.Alerts)-MaxLines))
			break
		}
		line := Line(a, lang)
		if a.Test {
			line = "[" + i18n.T(i18n.KeyAlertTestBadge, lang) + "] " + line
		}
		lines = append(lines, "• "+line)
	}
	return strings.Join(lines, "\r\n")
}
