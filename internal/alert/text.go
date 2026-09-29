package alert

import (
	"fmt"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func levelName(l model.AlertLevel, lang string) string {
	ru := map[model.AlertLevel]string{model.LevelOK: "норма", model.LevelWarning: "предупреждение", model.LevelError: "ошибка"}
	if lang == model.LangEN {
		return strings.ToLower(l.String())
	}
	if s, ok := ru[l]; ok {
		return s
	}
	return l.String()
}

// FormatAck renders the informational line of an acknowledged alert
// (ack_alert, CHK-IPC-02): "Тревога подтверждена. База «X», точка «Y»:
// уровень ошибка".
func FormatAck(e model.AlertEvent, lang string) string {
	var b strings.Builder
	b.WriteString(model.MonitorMessage(model.MsgMonitorAlertAcked, lang))
	b.WriteString(" ")
	b.WriteString(subject(e, lang))
	if lang == model.LangEN {
		fmt.Fprintf(&b, ": level %s", levelName(e.To, lang))
	} else {
		fmt.Fprintf(&b, ": уровень %s", levelName(e.To, lang))
	}
	return b.String()
}

// subject is "База «X»[, точка «Y»]" in lang.
func subject(e model.AlertEvent, lang string) string {
	en := lang == model.LangEN
	var b strings.Builder
	if en {
		fmt.Fprintf(&b, "Database %q", e.ProfileName)
	} else {
		fmt.Fprintf(&b, "База «%s»", e.ProfileName)
	}
	if !e.ProfileLevel() {
		name := e.PointName
		if e.PlanTable != "" {
			name = e.PlanTable + "/" + name
		}
		if en {
			fmt.Fprintf(&b, ", point %q", name)
		} else {
			fmt.Fprintf(&b, ", точка «%s»", name)
		}
	}
	return b.String()
}

// Format renders an event as one line of text in lang ("en" or RU):
// profile, point, transition, FreshAt (UTC+3), lag or error.
func Format(e model.AlertEvent, lang string) string {
	en := lang == model.LangEN
	var b strings.Builder
	b.WriteString(subject(e, lang))
	fmt.Fprintf(&b, ": %s -> %s", levelName(e.From, lang), levelName(e.To, lang))
	if e.FreshAt != nil {
		ts := e.FreshAt.In(model.DBZone).Format("2006-01-02 15:04:05") + " UTC+3"
		if en {
			fmt.Fprintf(&b, "; last exchange %s (%d min ago)", ts, e.LagMinutes)
		} else {
			fmt.Fprintf(&b, "; последний обмен %s (%d мин назад)", ts, e.LagMinutes)
		}
		if e.Baseline {
			b.WriteString(" — " + model.MonitorMessage(model.MsgMonitorBaseline, lang))
		}
	}
	if e.Error != "" {
		if en {
			fmt.Fprintf(&b, "; error: %s", e.Error)
		} else {
			fmt.Fprintf(&b, "; ошибка: %s", e.Error)
		}
	}
	return b.String()
}
