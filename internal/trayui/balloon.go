package trayui

import (
	"fmt"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Balloon is a Shell_NotifyIcon NIF_INFO notification (phase 9a,
// CHK-TRAY-18): a point became overdue (warning / error) or recovered.
type Balloon struct {
	Title, Text string
	Level       model.AlertLevel // icon: warning / error; OK = info
	Recovered   bool
}

// BalloonFor maps an alert push to a balloon: only point events, raise to
// warning / error ("просрочка") or back to OK ("восстановление");
// warning -> error escalations count as overdue too.
func BalloonFor(ev model.AlertEvent, lang string) (Balloon, bool) {
	if ev.PointName == "" || ev.From == ev.To {
		return Balloon{}, false
	}
	where := ev.ProfileName + " — " + ev.PointName
	if ev.To == model.LevelOK {
		return Balloon{Title: i18n.T(i18n.KeyBalloonRecoveredTitle, lang),
			Text: fmt.Sprintf(i18n.T(i18n.KeyBalloonRecovered, lang), where), Level: model.LevelOK, Recovered: true}, true
	}
	if ev.To < ev.From {
		return Balloon{}, false // error -> warning: still overdue, no balloon
	}
	text := fmt.Sprintf(i18n.T(i18n.KeyBalloonOverdue, lang), where, ev.LagMinutes, levelText(ev.To, lang))
	if ev.LagMinutes <= 0 {
		text = fmt.Sprintf(i18n.T(i18n.KeyBalloonOverdueNoLag, lang), where, levelText(ev.To, lang))
	}
	return Balloon{Title: i18n.T(i18n.KeyBalloonOverdueTitle, lang), Text: text, Level: ev.To}, true
}

// CombineBalloons merges the balloons of one batch of pushes (one poll):
// a single one is shown as is, several as one summary with the worst icon.
func CombineBalloons(bs []Balloon, lang string) (Balloon, bool) {
	switch len(bs) {
	case 0:
		return Balloon{}, false
	case 1:
		return bs[0], true
	}
	over, rec := 0, 0
	worst := model.LevelOK
	for _, b := range bs {
		if b.Recovered {
			rec++
			continue
		}
		over++
		if b.Level > worst {
			worst = b.Level
		}
	}
	title := i18n.T(i18n.KeyBalloonOverdueTitle, lang)
	if over == 0 {
		title = i18n.T(i18n.KeyBalloonRecoveredTitle, lang)
	}
	return Balloon{Title: title, Text: fmt.Sprintf(i18n.T(i18n.KeyBalloonSummary, lang), over, rec), Level: worst, Recovered: over == 0}, true
}

// Phase 9a right-menu commands.
const (
	CmdStatusWin = 1031 // «Состояние…» (CHK-TRAY-19)
	CmdCheckNow  = 1032 // «Проверить сейчас», poll_now (CHK-TRAY-20)
	CmdBalloons  = 1033 // «Уведомления о просрочке» on / off (CHK-TRAY-18)
)

// Phase9Items are inserted into the right menu before «Выход»:
// «Состояние…», «Проверить сейчас» (disabled when not connected) and the
// checkable balloon switch.
func Phase9Items(lang string, balloons, connected bool) []Item {
	return []Item{
		{ID: CmdStatusWin, Text: i18n.T(i18n.KeyMenuStatusWin, lang)},
		{ID: CmdCheckNow, Text: i18n.T(i18n.KeyMenuCheckNow, lang), Disabled: !connected},
		{ID: CmdBalloons, Text: i18n.T(i18n.KeyMenuBalloons, lang), Checked: balloons},
	}
}
