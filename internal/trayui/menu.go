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
	// phase 13 CHK-TRAY-91: the check mark on a database item toggles
	// polling (set_enabled). CmdToggleBase+i addresses snap.Profiles[i]
	// (i < 1000).
	CmdToggleBase  = 14000
	cmdProfileSlot = 1000
)

// ToggleCmd is the left-menu command of the check mark of snap.Profiles[i].
func ToggleCmd(i int) int { return CmdToggleBase + i }

// ToggleProfileIndex reports whether cmd is a database check mark and
// which profile index it addresses.
func ToggleProfileIndex(cmd int) (i int, ok bool) {
	if cmd >= CmdToggleBase && cmd < CmdToggleBase+cmdProfileSlot {
		return cmd - CmdToggleBase, true
	}
	return 0, false
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
	// Check (CHK-TRAY-91): a database item. Instead of the color square it
	// shows a bold check mark of Color (red error, yellow warning, green
	// OK) when Checked (polling on), an empty box otherwise; a click on
	// the item (it also opens the points submenu) sends ID = ToggleCmd(i)
	// unless ID is 0 (no profile_enabled / not elevated).
	Check bool
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
	canToggle := CanToggle(st)
	for i, p := range snap.Profiles {
		items = append(items, databaseItem(p, ind.Fresh, canToggle, i))
	}
	return items
}

// CanToggle reports whether the database check marks send set_enabled:
// the service offers profile_enabled and this (admin-only) command is
// allowed for the connection (clientElevated).
func CanToggle(st trayipc.Status) bool {
	return st.Has(model.CapProfileEnabled) && st.Elevated()
}

// CheckColor is the check mark color of a database item: red error,
// yellow warning, green OK; grey while the data is stale, polling is off
// or the first poll is pending.
func CheckColor(p model.ProfileSnapshot, fresh bool) Color {
	if !p.Enabled || !fresh || !p.Polled {
		return ColorGrey
	}
	if p.ErrorKey != "" || p.Error != "" {
		return ColorRed
	}
	return LevelColor(p.Level)
}

// ToggleFailedBalloon is the balloon after a failed set_enabled of the
// database name (err: transport error; m: the MsgError reply).
func ToggleFailedBalloon(name string, m model.IPCMessage, err error) Balloon {
	text := i18n.T(i18n.KeyDBToggleFailed)
	switch {
	case err != nil:
		text += ": " + i18n.T(i18n.KeyDBNoData)
	case m.ErrorKey != "":
		text += ": " + i18n.Message(m.ErrorKey)
	case m.Error != "":
		text += ": " + m.Error
	}
	return Balloon{Title: name, Text: text, Level: model.LevelWarning}
}

func databaseItem(p model.ProfileSnapshot, fresh bool, canToggle bool, idx int) Item {
	status := levelText(p.Level)
	switch {
	case !p.Enabled:
		status = i18n.T(i18n.KeyDBDisabled)
	case !p.Polled:
		status = i18n.T(i18n.KeyDBPending)
	}
	if !fresh {
		status += "; " + i18n.T(i18n.KeyDBStale)
	}
	name := p.Name
	if name == "" {
		name = p.ProfileID
	}
	it := Item{Text: name + " — " + status, Color: CheckColor(p, fresh), Check: true, Checked: p.Enabled}
	if canToggle {
		it.ID = ToggleCmd(idx)
	}
	var sub []Item
	if p.ErrorKey != "" || p.Error != "" {
		key := p.ErrorKey
		if key == "" {
			key = model.MsgMonitorPollError
		}
		sub = append(sub, Item{Text: levelText(model.LevelError) + ": " + i18n.Message(key), Disabled: true})
	}
	// p.Warnings (e.g. «Шифрование отключено…») are not shown in the
	// menu (CHK-TRAY-91).
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
