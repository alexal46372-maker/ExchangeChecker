package trayui

import (
	"fmt"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

// Color of the square in front of a database item.
type Color int

const (
	ColorNone Color = iota
	ColorGreen
	ColorYellow
	ColorRed
	ColorGrey
)

// Menu command IDs (WM_COMMAND / TrackPopupMenu TPM_RETURNCMD).
const (
	CmdNone     = 0
	CmdSettings = 1001
	CmdAbout    = 1002
	CmdExit     = 1003
)

// Item is one menu item. Sub != nil makes it a submenu.
type Item struct {
	ID        int
	Text      string
	Color     Color
	Disabled  bool
	Separator bool
	Sub       []Item
}

// TimeLayout of menu times (UTC+3, PLAN §4.5).
const TimeLayout = "02.01 15:04"

// LevelColor maps a level to the square color.
func LevelColor(l model.AlertLevel) Color {
	switch l {
	case model.LevelError:
		return ColorRed
	case model.LevelWarning:
		return ColorYellow
	}
	return ColorGreen
}

// BuildLeftMenu is the left-click menu: the disabled service / config
// status line always on top (PLAN §12 p. 48), the config line when the
// configuration is stale or invalid, then ONLY the
// databases, each with its exchange points as a submenu (PLAN §7.1, §12
// p.7). The color square is duplicated by the status text.
func BuildLeftMenu(st trayipc.Status, lang string) []Item {
	ind := Evaluate(st, lang)
	var items []Item
	if ind.ServiceLine != "" {
		items = append(items, Item{Text: ind.ServiceLine, Disabled: true})
	}
	if ind.ConfigLine != "" {
		items = append(items, Item{Text: ind.ConfigLine, Disabled: true})
	}
	if len(items) > 0 {
		items = append(items, Item{Separator: true})
	}
	snap := st.Snapshot
	if snap == nil {
		return append(items, Item{Text: i18n.T(i18n.KeyDBNoData, lang), Disabled: true})
	}
	if len(snap.Profiles) == 0 {
		return append(items, Item{Text: i18n.T(i18n.KeyDBNone, lang), Disabled: true})
	}
	for _, p := range snap.Profiles {
		items = append(items, databaseItem(p, ind.Fresh, lang))
	}
	return items
}

func databaseItem(p model.ProfileSnapshot, fresh bool, lang string) Item {
	color := LevelColor(p.Level)
	status := levelText(p.Level, lang)
	switch {
	case !p.Enabled:
		color, status = ColorGrey, i18n.T(i18n.KeyDBDisabled, lang)
	case !p.Polled:
		color, status = ColorGrey, i18n.T(i18n.KeyDBPending, lang)
	}
	if !fresh {
		color = ColorGrey
		status += "; " + i18n.T(i18n.KeyDBStale, lang)
	}
	name := p.Name
	if name == "" {
		name = p.ProfileID
	}
	it := Item{Text: name + " — " + status, Color: color}
	var sub []Item
	if p.ErrorKey != "" || p.Error != "" {
		key := p.ErrorKey
		if key == "" {
			key = model.MsgMonitorPollError
		}
		sub = append(sub, Item{Text: levelText(model.LevelError, lang) + ": " + i18n.Message(key, lang), Disabled: true})
	}
	for _, w := range p.Warnings {
		sub = append(sub, Item{Text: i18n.Message(w, lang), Disabled: true})
	}
	if len(sub) > 0 && len(p.Points) > 0 {
		sub = append(sub, Item{Separator: true})
	}
	sub = append(sub, pointItems(p.Points, lang)...)
	if len(sub) == 0 {
		sub = []Item{{Text: i18n.T(i18n.KeyDBNoPoints, lang), Disabled: true}}
	}
	it.Sub = sub
	return it
}

func pointItems(pts []model.PointStatus, lang string) []Item {
	ps := append([]model.PointStatus(nil), pts...)
	model.SortPoints(ps)
	plans := map[string]bool{}
	for _, p := range ps {
		plans[p.PlanTable] = true
	}
	grouped := len(plans) > 1
	var out []Item
	cur := "\x00"
	for _, p := range ps {
		if grouped && p.PlanTable != cur {
			cur = p.PlanTable
			label := p.PlanLabel
			if label == "" {
				label = p.PlanTable
			}
			if label != "" {
				out = append(out, Item{Text: i18n.T(i18n.KeyPtPlan, lang) + ": " + label, Disabled: true})
			}
		}
		out = append(out, Item{Text: PointText(p, lang), Color: LevelColor(p.Level)})
	}
	return out
}

func fmtTime(t *time.Time, lang string) string {
	if t == nil {
		return i18n.T(i18n.KeyPtNever, lang)
	}
	return t.In(model.DBZone).Format(TimeLayout)
}

// PointText is the menu line of an exchange point: name, last send, last
// receive, lag (from FreshAt), level, baseline / acknowledged marks.
func PointText(p model.PointStatus, lang string) string {
	s := fmt.Sprintf("%s: %s %s, %s %s", p.Name,
		i18n.T(i18n.KeyPtSend, lang), fmtTime(p.LastSend, lang),
		i18n.T(i18n.KeyPtRecv, lang), fmtTime(p.LastRecv, lang))
	if p.LagMinutes >= 0 && p.FreshAt != nil {
		s += ", " + fmt.Sprintf(i18n.T(i18n.KeyPtLag, lang), p.LagMinutes)
	}
	s += " — " + levelText(p.Level, lang)
	if p.Baseline {
		s += " (" + i18n.T(i18n.KeyPtBaseline, lang) + ")"
	}
	if p.Acknowledged {
		s += " (" + i18n.T(i18n.KeyPtAcked, lang) + ")"
	}
	return s
}

// BuildRightMenu is the right-click menu. Settings is shown disabled with
// an explicit label until phase 6c implements it (no stub window).
func BuildRightMenu(lang string) []Item {
	return []Item{
		{ID: CmdSettings, Text: i18n.T(i18n.KeyMenuSettingsDisabled, lang), Disabled: true},
		{ID: CmdAbout, Text: i18n.T(i18n.KeyMenuAbout, lang)},
		{Separator: true},
		{ID: CmdExit, Text: i18n.T(i18n.KeyMenuExit, lang)},
	}
}
