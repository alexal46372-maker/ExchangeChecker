package trayui

import (
	"fmt"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
)

// Card is one database of the «Состояние» window shown as a card
// (phase 11, PLAN §12 p.143, CHK-TRAY-48): the colour and state of its
// worst row, the freshness of its stalest point and the key numbers.
type Card struct {
	ProfileID, Name string
	Color           Color
	State           string
	Fresh           string // «Свежесть: 37 мин назад» / «нет данных»
	Info            string // «Точек: 3, с проблемами: 1» or the error
	Tip             string // the tooltip: every point with its state
	Points, Bad     int
	// Disabled: polling is off; the card shows «Опрос выключен» only
	// (phase 11, CHK-TRAY-49)
	Disabled bool
	// Muted: mute_until_recovered (CHK-TRAY-59)
	Muted bool
}

// cardRank orders the colours: the card takes its worst one.
func cardRank(c Color) int {
	switch c {
	case ColorRed:
		return 4
	case ColorYellow:
		return 3
	case ColorGreen:
		return 2
	case ColorGrey:
		return 1
	}
	return 0
}

// Cards groups the status rows by database, in their order.
func Cards(rows []StatusRow, lang string) []Card {
	var out []Card
	idx := map[string]int{}
	stale := map[string]int{}
	tips := map[string][]string{}
	for _, r := range rows {
		key := r.ProfileID + "\x00" + r.Database
		i, ok := idx[key]
		if !ok {
			i = len(out)
			idx[key] = i
			stale[key] = -1
			out = append(out, Card{ProfileID: r.ProfileID, Name: r.Database})
		}
		c := &out[i]
		if cardRank(r.Color) > cardRank(c.Color) {
			c.Color, c.State = r.Color, r.State
		}
		if r.PointKey == "" && r.Point == "" {
			if r.State == i18n.T(i18n.KeyStDisabled, lang) {
				c.Disabled = true
			}
			if r.Note != "" {
				c.Info = r.Note
				if strings.Contains(r.Note, i18n.T(i18n.KeyCardMuted, lang)) {
					c.Muted = true
				}
			}
			if c.State == "" {
				c.State = r.State
			}
			continue
		}
		c.Points++
		if r.Color == ColorRed || r.Color == ColorYellow {
			c.Bad++
		}
		// the stalest point; one never exchanged is the stalest of all
		if age := r.AgoMinutes; age < 0 {
			if stale[key] < 1<<30 {
				stale[key], c.Fresh = 1<<30, ""
			}
		} else if age > stale[key] {
			stale[key], c.Fresh = age, r.Ago
		}
		line := r.Point + ": " + r.State
		if r.Ago != "" {
			line += ", " + r.Ago
		}
		tips[key] = append(tips[key], line)
	}
	for key, i := range idx {
		c := &out[i]
		switch {
		case c.Fresh != "":
			c.Fresh = fmt.Sprintf(i18n.T(i18n.KeyCardFresh, lang), c.Fresh)
		default:
			c.Fresh = fmt.Sprintf(i18n.T(i18n.KeyCardFresh, lang), i18n.T(i18n.KeyCardNoData, lang))
		}
		if c.Disabled {
			c.State, c.Fresh, c.Info = i18n.T(i18n.KeyCardPollingOff, lang), "", ""
			c.Tip = c.Name + " — " + c.State
			continue
		}
		if c.Info == "" {
			c.Info = fmt.Sprintf(i18n.T(i18n.KeyCardPoints, lang), c.Points, c.Bad)
		}
		tip := c.Name + " — " + c.State
		if len(tips[key]) > 0 {
			tip += "\r\n" + strings.Join(tips[key], "\r\n")
		}
		c.Tip = tip + "\r\n" + i18n.T(i18n.KeyCardTip, lang)
	}
	for i := range out {
		if out[i].Muted && out[i].State != "" {
			out[i].State = out[i].State + " · " + i18n.T(i18n.KeyCardMuted, lang)
		} else if out[i].Muted {
			out[i].State = i18n.T(i18n.KeyCardMuted, lang)
		}
	}
	return out
}
