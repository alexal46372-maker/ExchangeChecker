package trayui

import (
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
)

// TestCHK_TRAY_48_Cards — the database cards (PLAN §12 p.143): one card
// per database in order, the worst colour, the stalest freshness, the
// point / problem counts, the error of a failed database, RU+EN, a
// tooltip listing the points.
func TestCHK_TRAY_48_Cards(t *testing.T) {
	rows := []StatusRow{
		{Database: "Склад", ProfileID: "p1", Point: "ЦБ", PointKey: "a", State: "OK", Color: ColorGreen, Ago: "5 мин назад", AgoMinutes: 5},
		{Database: "Склад", ProfileID: "p1", Point: "Филиал", PointKey: "b", State: "Предупреждение", Color: ColorYellow, Ago: "45 мин назад", AgoMinutes: 45},
		{Database: "Склад", ProfileID: "p1", Point: "Магазин", PointKey: "c", State: "OK", Color: ColorGreen, Ago: "1 мин назад", AgoMinutes: 1},
		{Database: "HR", ProfileID: "p2", State: "Ошибка", Color: ColorRed, Note: "login failed", AgoMinutes: -1},
		{Database: "Касса", ProfileID: "p3", Point: "ЦБ", PointKey: "a", State: "OK", Color: ColorGreen, AgoMinutes: -1},
	}
	cs := Cards(rows, i18n.RU)
	if len(cs) != 3 || cs[0].Name != "Склад" || cs[1].Name != "HR" || cs[2].Name != "Касса" {
		t.Fatalf("cards %+v", cs)
	}
	c := cs[0]
	if c.Color != ColorYellow || c.State != "Предупреждение" || c.Points != 3 || c.Bad != 1 ||
		c.Fresh != "Свежесть: 45 мин назад" || c.Info != "Точек: 3, с проблемами: 1" {
		t.Errorf("Склад %+v", c)
	}
	if !strings.Contains(c.Tip, "Филиал: Предупреждение, 45 мин назад") || !strings.Contains(c.Tip, "Подробности") {
		t.Errorf("tip %q", c.Tip)
	}
	if c := cs[1]; c.Color != ColorRed || c.Info != "login failed" || c.Fresh != "Свежесть: нет данных" {
		t.Errorf("HR %+v", c)
	}
	if c := cs[2]; c.Fresh != "Свежесть: нет данных" || c.Points != 1 || c.Bad != 0 {
		t.Errorf("Касса %+v", c)
	}
	en := Cards(rows, i18n.EN)
	if en[0].Fresh != "Freshness: 45 мин назад" || en[0].Info != "Points: 3, with problems: 1" || !strings.Contains(en[0].Tip, "Details") {
		t.Errorf("EN %+v", en[0])
	}
	off := Cards([]StatusRow{{Database: "Архив", ProfileID: "p9", State: i18n.T(i18n.KeyStDisabled, i18n.RU), Color: ColorGrey, AgoMinutes: -1}}, i18n.RU)
	if c := off[0]; !c.Disabled || c.State != "Опрос выключен" || c.Fresh != "" || c.Info != "" {
		t.Errorf("disabled %+v", c)
	}
	if c := Cards([]StatusRow{{Database: "A", State: i18n.T(i18n.KeyStDisabled, i18n.EN), Color: ColorGrey}}, i18n.EN)[0]; c.State != "Polling disabled" {
		t.Errorf("EN disabled %+v", c)
	}
	if Cards(nil, i18n.RU) != nil {
		t.Error("no rows, no cards")
	}
}
