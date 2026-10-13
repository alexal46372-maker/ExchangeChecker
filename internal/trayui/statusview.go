package trayui

import (
	"fmt"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

// StatusRow is one line of the «Состояние» window and of the export
// (phase 9a, CHK-TRAY-19 / CHK-TRAY-21): a point, or the database itself
// when it has an error or no points.
type StatusRow struct {
	Database, Point, Plan string
	LastSend, LastRecv    string // TimeLayout, UTC+3; "" = never
	Fresh                 string // the later of both
	Ago                   string // «N мин назад»
	AgoMinutes            int    // -1 = unknown
	State                 string
	Level                 model.AlertLevel
	Color                 Color
	Note                  string // error text / baseline / acknowledged
}

// StatusColumns are the column header keys, in StatusRow order of the
// window and the export.
var StatusColumns = []string{i18n.KeyStDatabase, i18n.KeyStPoint, i18n.KeyStPlan, i18n.KeyStFresh, i18n.KeyStAgo,
	i18n.KeyStState, i18n.KeyStSend, i18n.KeyStRecv, i18n.KeyStNote}

// Cells returns the row in StatusColumns order.
func (r StatusRow) Cells() []string {
	return []string{r.Database, r.Point, r.Plan, r.Fresh, r.Ago, r.State, r.LastSend, r.LastRecv, r.Note}
}

// AgoText is «N мин назад» («только что» under a minute, hours and days
// for long gaps); "" for an unknown time.
func AgoText(t *time.Time, now time.Time, lang string) (string, int) {
	if t == nil || t.IsZero() {
		return "", -1
	}
	m := int(now.Sub(*t) / time.Minute)
	switch {
	case m < 1:
		return i18n.T(i18n.KeyAgoNow, lang), 0
	case m < 120:
		return fmt.Sprintf(i18n.T(i18n.KeyAgoMin, lang), m), m
	case m < 48*60:
		return fmt.Sprintf(i18n.T(i18n.KeyAgoHours, lang), m/60, m%60), m
	}
	return fmt.Sprintf(i18n.T(i18n.KeyAgoDays, lang), m/(24*60)), m
}

func timeText(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(model.DBZone).Format(TimeLayout)
}

// StatusRows builds the rows of the snapshot; now is the clock for «N мин
// назад» (the tray and the service share the machine clock).
func StatusRows(st trayipc.Status, lang string, now time.Time) []StatusRow {
	if st.Snapshot == nil {
		return nil
	}
	var rows []StatusRow
	for _, p := range st.Snapshot.Profiles {
		db := p.Name
		if db == "" {
			db = p.ProfileID
		}
		if !p.Enabled || p.Error != "" || p.ErrorKey != "" || !p.Polled || len(p.Points) == 0 {
			r := StatusRow{Database: db, AgoMinutes: -1, Level: p.Level, Color: LevelColor(p.Level)}
			switch {
			case !p.Enabled:
				r.State, r.Color = i18n.T(i18n.KeyStDisabled, lang), ColorGrey
			case p.ErrorKey != "":
				r.State, r.Note = levelText(model.LevelError, lang), i18n.Message(p.ErrorKey, lang)
			case p.Error != "":
				r.State, r.Note = levelText(model.LevelError, lang), p.Error
			case !p.Polled:
				r.State, r.Color = i18n.T(i18n.KeyStNotPolled, lang), ColorGrey
			default:
				r.State, r.Note = levelText(p.Level, lang), i18n.T(i18n.KeyDBNoPoints, lang)
			}
			rows = append(rows, r)
			if len(p.Points) == 0 {
				continue
			}
		}
		ps := append([]model.PointStatus(nil), p.Points...)
		model.SortPoints(ps)
		for _, pt := range ps {
			r := StatusRow{Database: db, Point: pt.Name, Plan: pt.PlanLabel, Level: pt.Level, Color: LevelColor(pt.Level),
				LastSend: timeText(pt.LastSend), LastRecv: timeText(pt.LastRecv), Fresh: timeText(pt.FreshAt),
				State: levelText(pt.Level, lang)}
			if r.Plan == "" {
				r.Plan = pt.PlanTable
			}
			r.Ago, r.AgoMinutes = AgoText(pt.FreshAt, now, lang)
			if pt.FreshAt == nil {
				r.Fresh = i18n.T(i18n.KeyPtNever, lang)
			}
			if pt.Baseline {
				r.Note = i18n.T(i18n.KeyPtBaseline, lang)
			}
			if pt.Acknowledged {
				if r.Note != "" {
					r.Note += "; "
				}
				r.Note += i18n.T(i18n.KeyPtAcked, lang)
			}
			rows = append(rows, r)
		}
	}
	return rows
}
