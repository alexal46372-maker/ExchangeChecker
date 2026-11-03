package alert

import (
	"fmt"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

func levelName(l model.AlertLevel) string {
	ru := map[model.AlertLevel]string{model.LevelOK: "норма", model.LevelWarning: "предупреждение", model.LevelError: "ошибка"}
	if s, ok := ru[l]; ok {
		return s
	}
	return l.String()
}

// FormatAck renders the informational line of an acknowledged alert
// (ack_alert, CHK-IPC-02): "Тревога подтверждена. База «X», точка «Y»:
// уровень ошибка".
func FormatAck(e model.AlertEvent) string {
	var b strings.Builder
	b.WriteString(model.MonitorMessage(model.MsgMonitorAlertAcked))
	b.WriteString(" ")
	b.WriteString(subject(e))
	fmt.Fprintf(&b, ": уровень %s", levelName(e.To))
	return b.String()
}

// FormatConfigChange renders event 105 (phase 6): "Конфигурация
// изменена: действие save_profile, профиль «id», учётная запись
// DOMAIN\user". Only the account, the action and the profile id — never a
// password or a connection string.
func FormatConfigChange(c model.ConfigChange) string {
	var b strings.Builder
	b.WriteString(model.MonitorMessage(model.MsgMonitorConfigChanged))
	acc := c.Account
	if acc == "" {
		acc = "неизвестна"
	}
	fmt.Fprintf(&b, " Действие %s", c.Action)
	if c.ProfileID != "" {
		fmt.Fprintf(&b, ", профиль «%s»", c.ProfileID)
	}
	fmt.Fprintf(&b, ", учётная запись %s.", acc)
	return b.String()
}

// subject is "База «X»[, точка «Y»]".
func subject(e model.AlertEvent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "База «%s»", e.ProfileName)
	if !e.ProfileLevel() {
		name := e.PointName
		if e.PlanTable != "" {
			name = e.PlanTable + "/" + name
		}
		fmt.Fprintf(&b, ", точка «%s»", name)
	}
	return b.String()
}

// Format renders an event as one line of text:
// profile, point, transition, FreshAt (UTC+3), lag or error.
func Format(e model.AlertEvent) string {
	var b strings.Builder
	b.WriteString(subject(e))
	fmt.Fprintf(&b, ": %s -> %s", levelName(e.From), levelName(e.To))
	if e.FreshAt != nil {
		ts := e.FreshAt.In(model.DBZone).Format("2006-01-02 15:04:05") + " UTC+3"
		fmt.Fprintf(&b, "; последний обмен %s (%d мин назад)", ts, e.LagMinutes)
		if e.Baseline {
			b.WriteString(" — " + model.MonitorMessage(model.MsgMonitorBaseline))
		}
	}
	if e.Error != "" {
		fmt.Fprintf(&b, "; ошибка: %s", e.Error)
	}
	return b.String()
}
