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
func BuildLeftMenu(st trayipc.Status) []Item {
	ind := Evaluate(st)
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
		return append(items, Item{Text: i18n.T(i18n.KeyDBNoData), Disabled: true})
	}
	if len(snap.Profiles) == 0 {
		return append(items, Item{Text: i18n.T(i18n.KeyDBNone), Disabled: true})
	}
	canMute := st.Has(model.CapMuteProfile)
	for i, p := range snap.Profiles {
		items = append(items, databaseItem(p, ind.Fresh, canMute, i))
	}
	return items
}

func databaseItem(p model.ProfileSnapshot, fresh bool, canMute bool, idx int) Item {
	color := LevelColor(p.Level)
	status := levelText(p.Level)
	switch {
	case !p.Enabled:
		color, status = ColorGrey, i18n.T(i18n.KeyDBDisabled)
	case !p.Polled:
		color, status = ColorGrey, i18n.T(i18n.KeyDBPending)
	}
	if !fresh {
		color = ColorGrey
		status += "; " + i18n.T(i18n.KeyDBStale)
	}
	if p.Muted {
		status += "; " + i18n.T(i18n.KeyDBMuted)
	}
	name := p.Name
	if name == "" {
		name = p.ProfileID
	}
	it := Item{Text: name + " — " + status, Color: color}
	var sub []Item
	if canMute {
		if p.Muted {
			sub = append(sub, Item{ID: UnmuteCmd(idx), Text: i18n.T(i18n.KeyMenuUnmute)})
		} else {
			sub = append(sub, Item{ID: MuteCmd(idx), Text: i18n.T(i18n.KeyMenuMute)})
		}
		sub = append(sub, Item{Separator: true})
	}
	if p.ErrorKey != "" || p.Error != "" {
		key := p.ErrorKey
		if key == "" {
			key = model.MsgMonitorPollError
		}
		sub = append(sub, Item{Text: levelText(model.LevelError) + ": " + i18n.Message(key), Disabled: true})
	}
	for _, w := range p.Warnings {
		sub = append(sub, Item{Text: i18n.Message(w), Disabled: true})
	}
	if len(sub) > 0 && len(p.Points) > 0 {
		sub = append(sub, Item{Separator: true})
	}
	sub = append(sub, pointItems(p.Points)...)
	if len(sub) == 0 {
		sub = []Item{{Text: i18n.T(i18n.KeyDBNoPoints), Disabled: true}}
	}
	it.Sub = sub
	return it
}

func pointItems(pts []model.PointStatus) []Item {
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
				out = append(out, Item{Text: i18n.T(i18n.KeyPtPlan) + ": " + label, Disabled: true})
			}
		}
		out = append(out, Item{Text: PointText(p), Color: LevelColor(p.Level)})
	}
	return out
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return i18n.T(i18n.KeyPtNever)
	}
	return t.In(model.DBZone).Format(TimeLayout)
}

// PointText is the menu line of an exchange point: name, last send, last
// receive, lag (from FreshAt), level, baseline / acknowledged marks.
func PointText(p model.PointStatus) string {
	s := fmt.Sprintf("%s: %s %s, %s %s", p.Name,
		i18n.T(i18n.KeyPtSend), fmtTime(p.LastSend),
		i18n.T(i18n.KeyPtRecv), fmtTime(p.LastRecv))
	if p.LagMinutes >= 0 && p.FreshAt != nil {
		s += ", " + fmt.Sprintf(i18n.T(i18n.KeyPtLag), p.LagMinutes)
	}
	s += " — " + levelText(p.Level)
	if p.Baseline {
		s += " (" + i18n.T(i18n.KeyPtBaseline) + ")"
	}
	if p.Acknowledged {
		s += " (" + i18n.T(i18n.KeyPtAcked) + ")"
	}
	return s
}

// BuildRightMenu is the right-click menu: Settings (phase 6c: read-only
// window, «Изменить (администратор)» elevates), About, the «Служба» submenu with the service state svc
// (phase 8, CHK-TRAY-16), Exit.
func BuildRightMenu(svc SvcState) []Item {
	return []Item{
		{ID: CmdSettings, Text: i18n.T(i18n.KeyMenuSettings)},
		{ID: CmdAbout, Text: i18n.T(i18n.KeyMenuAbout)},
		{Text: i18n.T(i18n.KeyMenuService), Sub: ServiceMenu(svc)},
		{Separator: true},
		{ID: CmdExit, Text: i18n.T(i18n.KeyMenuExit)},
	}
}
