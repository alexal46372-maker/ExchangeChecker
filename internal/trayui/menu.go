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
	// language submenu (per-user tray language, phase 6e)
	CmdLangAuto = 1011
	CmdLangRU   = 1012
	CmdLangEN   = 1013
	// phase 12 CHK-TRAY-59: mute/unmute per profile in the left (databases) menu.
	// CmdMuteBase+i / CmdUnmuteBase+i address snap.Profiles[i] (i < 1000).
	CmdMuteBase   = 12000
	CmdUnmuteBase = 13000
	cmdProfileSlot = 1000
)

// MuteCmd / UnmuteCmd are the left-menu commands for snap.Profiles[i].
func MuteCmd(i int) int   { return CmdMuteBase + i }
func UnmuteCmd(i int) int { return CmdUnmuteBase + i }

// MuteProfileIndex reports whether cmd is mute/unmute and which profile index.
func MuteProfileIndex(cmd int) (i int, mute bool, ok bool) {
	if cmd >= CmdMuteBase && cmd < CmdMuteBase+cmdProfileSlot {
		return cmd - CmdMuteBase, true, true
	}
	if cmd >= CmdUnmuteBase && cmd < CmdUnmuteBase+cmdProfileSlot {
		return cmd - CmdUnmuteBase, false, true
	}
	return 0, false, false
}

// LangCommands maps the language commands to the stored preference
// ("" = as in Windows).
var LangCommands = map[int]string{CmdLangAuto: "", CmdLangRU: model.LangRU, CmdLangEN: model.LangEN}

// Item is one menu item. Sub != nil makes it a submenu.
type Item struct {
	ID        int
	Text      string
	Color     Color
	Disabled  bool
	Separator bool
	Checked   bool
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
	canMute := st.Has(model.CapMuteProfile)
	for i, p := range snap.Profiles {
		items = append(items, databaseItem(p, ind.Fresh, lang, canMute, i))
	}
	return items
}

func databaseItem(p model.ProfileSnapshot, fresh bool, lang string, canMute bool, idx int) Item {
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
	if p.Muted {
		status += "; " + i18n.T(i18n.KeyDBMuted, lang)
	}
	name := p.Name
	if name == "" {
		name = p.ProfileID
	}
	it := Item{Text: name + " — " + status, Color: color}
	var sub []Item
	if canMute {
		if p.Muted {
			sub = append(sub, Item{ID: UnmuteCmd(idx), Text: i18n.T(i18n.KeyMenuUnmute, lang)})
		} else {
			sub = append(sub, Item{ID: MuteCmd(idx), Text: i18n.T(i18n.KeyMenuMute, lang)})
		}
		sub = append(sub, Item{Separator: true})
	}
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

// BuildRightMenu is the right-click menu: Settings (phase 6c: read-only
// window, «Изменить (администратор)» elevates), About, the language
// submenu with the current per-user preference pref checked ("" = as in
// Windows; phase 6e), the «Служба» submenu with the service state svc
// (phase 8, CHK-TRAY-16), Exit.
func BuildRightMenu(lang, pref string, svc SvcState) []Item {
	sub := []Item{
		{ID: CmdLangAuto, Text: i18n.T(i18n.KeyMenuLangAuto, lang)},
		{ID: CmdLangRU, Text: i18n.T(i18n.KeyMenuLangRU, lang)},
		{ID: CmdLangEN, Text: i18n.T(i18n.KeyMenuLangEN, lang)},
	}
	for i := range sub {
		sub[i].Checked = LangCommands[sub[i].ID] == pref
	}
	return []Item{
		{ID: CmdSettings, Text: i18n.T(i18n.KeyMenuSettings, lang)},
		{ID: CmdAbout, Text: i18n.T(i18n.KeyMenuAbout, lang)},
		{Text: i18n.T(i18n.KeyMenuLanguage, lang), Sub: sub},
		{Text: i18n.T(i18n.KeyMenuService, lang), Sub: ServiceMenu(svc, lang)},
		{Separator: true},
		{ID: CmdExit, Text: i18n.T(i18n.KeyMenuExit, lang)},
	}
}
