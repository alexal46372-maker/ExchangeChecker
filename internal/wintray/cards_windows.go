//go:build windows

package wintray

import (
	"fmt"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Database cards of the «Состояние» window (phase 11, PLAN §12 p.143,
// CHK-TRAY-48): one card per database with a coloured stripe and pale
// background of its state, the state, the freshness and the key
// numbers; the tooltip of a card lists its points. The cards wrap in
// rows of the panel width; when there are more databases than places
// the last place says how many more there are (the table has them all).
const (
	cardsClass = "ExchangeCheckerCards"
	cardClass  = "ExchangeCheckerCard"
	cardW      = 236 // units: the narrowest card
	cardH      = 76
	cardGap    = 8
	cardSlots  = 12
)

// cardStripe: the strong colour of the stripe (COLORREF 0x00BBGGRR).
var cardStripe = map[trayui.Color]uint32{
	trayui.ColorGreen:  0x3CA03C,
	trayui.ColorYellow: 0x00B8F0,
	trayui.ColorRed:    0x3030D0,
	trayui.ColorGrey:   0x909090,
}

type cardPanel struct {
	hwnd  uintptr
	slots []uintptr
	cards []trayui.Card // all databases
	shown []trayui.Card // the cards in the places
	tips  *tooltips
	lang  string
	rows  int32
}

// cardPanelHeight in units for rows rows of cards.
func cardPanelHeight(rows int32) int32 { return rows*cardH + (rows-1)*cardGap }

func newCardPanel(parent uintptr, tips *tooltips, lang string, rows int32) (*cardPanel, error) {
	p := &cardPanel{tips: tips, lang: lang, rows: rows}
	h, err := createWindow(cardsClass, "", 0, winapi.WS_CHILD|winapi.WS_VISIBLE|winapi.WS_CLIPCHILDREN, 0, 0, 0, 0, parent,
		func(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
			switch msg {
			case winapi.WM_SIZE:
				p.place()
				return 0, true
			case winapi.WM_PAINT:
				var ps winapi.PAINTSTRUCT
				dc := winapi.BeginPaint(hw, &ps)
				bg := winapi.CreateSolidBrush(0xF0F0F0)
				winapi.FillRect(dc, winapi.GetClientRect(hw), bg)
				winapi.DeleteObject(bg)
				winapi.EndPaint(hw, &ps)
				return 0, true
			case winapi.WM_ERASEBKGND:
				return 1, true
			}
			return 0, false
		})
	if err != nil {
		return nil, err
	}
	p.hwnd = h
	for i := 0; i < cardSlots; i++ {
		i := i
		s, err := createWindow(cardClass, "", 0, winapi.WS_CHILD, 0, 0, 0, 0, h,
			func(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
				switch msg {
				case winapi.WM_PAINT:
					p.paint(hw, i)
					return 0, true
				case winapi.WM_ERASEBKGND:
					return 1, true
				}
				return 0, false
			})
		if err != nil {
			return nil, err
		}
		p.slots = append(p.slots, s)
	}
	return p, nil
}

// set shows the cards of the latest status.
func (p *cardPanel) set(cards []trayui.Card) {
	p.cards = cards
	p.place()
}

// visible is the number of places in use (tests).
func (p *cardPanel) visible() int { return len(p.shown) }

// place lays the cards out in the panel width.
func (p *cardPanel) place() {
	if p == nil || p.hwnd == 0 {
		return
	}
	rc := winapi.GetClientRect(p.hwnd)
	w := rc.Right - rc.Left
	gap, cw, ch := px(cardGap), px(cardW), px(cardH)
	cols := (w + gap) / (cw + gap)
	if cols < 1 {
		cols = 1
	}
	if cols > 1 {
		cw = (w - (cols-1)*gap) / cols
	} else if w > 0 {
		cw = w
	}
	places := int(cols * p.rows)
	if places > len(p.slots) {
		places = len(p.slots)
	}
	p.shown = p.cards
	if len(p.cards) > places {
		more := len(p.cards) - places + 1
		p.shown = append(append([]trayui.Card(nil), p.cards[:places-1]...), trayui.Card{
			Name:  fmt.Sprintf(i18n.T(i18n.KeyCardMore, p.lang), more),
			State: i18n.T(i18n.KeyCardMoreHint, p.lang),
			Tip:   i18n.T(i18n.KeyCardMoreHint, p.lang)})
	}
	for i, s := range p.slots {
		if i >= len(p.shown) {
			winapi.ShowWindow(s, winapi.SW_HIDE)
			continue
		}
		col, row := int32(i)%cols, int32(i)/cols
		winapi.SetWindowPos(s, 0, col*(cw+gap), row*(ch+gap), cw, ch, winapi.SWP_NOZORDER|winapi.SWP_NOACTIVATE|winapi.SWP_SHOWWINDOW)
		p.tips.set(s, p.shown[i].Tip)
		winapi.InvalidateRect(s, true)
	}
}

// paint draws card i: pale background, stripe, frame, four lines.
func (p *cardPanel) paint(hw uintptr, i int) {
	var ps winapi.PAINTSTRUCT
	dc := winapi.BeginPaint(hw, &ps)
	defer winapi.EndPaint(hw, &ps)
	rc := winapi.GetClientRect(hw)
	fill := func(r winapi.RECT, clr uint32) {
		b := winapi.CreateSolidBrush(clr)
		winapi.FillRect(dc, r, b)
		winapi.DeleteObject(b)
	}
	var c trayui.Card
	if i < len(p.shown) {
		c = p.shown[i]
	}
	bk, ok := rowBk[c.Color]
	if !ok {
		bk = 0xF8F8F8
	}
	fill(rc, 0xA0A0A0) // frame
	fill(winapi.RECT{Left: rc.Left + 1, Top: rc.Top + 1, Right: rc.Right - 1, Bottom: rc.Bottom - 1}, bk)
	stripe, ok := cardStripe[c.Color]
	if !ok {
		stripe = 0xC0C0C0
	}
	sw := px(6)
	fill(winapi.RECT{Left: rc.Left + 1, Top: rc.Top + 1, Right: rc.Left + 1 + sw, Bottom: rc.Bottom - 1}, stripe)
	winapi.SetBkMode(dc, winapi.TRANSPARENT)
	winapi.SetTextColor(dc, clrText)
	old := winapi.SelectObject(dc, uiFont())
	defer winapi.SelectObject(dc, old)
	_, lh := winapi.TextSize(uiFont(), "Ag", 0)
	x, y, right := rc.Left+1+sw+px(6), rc.Top+px(4), rc.Right-px(6)
	for _, s := range []string{c.Name, c.State, c.Fresh, c.Info} {
		if s != "" {
			winapi.DrawText(dc, s, winapi.RECT{Left: x, Top: y, Right: right, Bottom: y + lh}, winapi.DT_SINGLELINE|winapi.DT_END_ELLIPSIS)
		}
		y += lh + px(1)
	}
}
