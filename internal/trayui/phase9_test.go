package trayui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
)

// CHK-TRAY-18: balloons on overdue / recovery.
func TestBalloonFor(t *testing.T) {
	ev := model.AlertEvent{ProfileName: "ERP", PointName: "Склад", From: model.LevelOK, To: model.LevelWarning, LagMinutes: 75}
	b, ok := BalloonFor(ev, "ru")
	if !ok || b.Recovered || b.Level != model.LevelWarning || !strings.Contains(b.Text, "75 мин") || !strings.Contains(b.Text, "ERP — Склад") {
		t.Fatalf("overdue: %+v %v", b, ok)
	}
	ev.From, ev.To = model.LevelWarning, model.LevelError
	if b, ok = BalloonFor(ev, "en"); !ok || b.Level != model.LevelError || b.Title != "Exchange overdue" {
		t.Fatalf("escalation: %+v", b)
	}
	ev.From, ev.To = model.LevelError, model.LevelOK
	if b, ok = BalloonFor(ev, "en"); !ok || !b.Recovered || b.Title != "Exchange recovered" {
		t.Fatalf("recovery: %+v", b)
	}
	ev.From, ev.To = model.LevelError, model.LevelWarning
	if _, ok = BalloonFor(ev, "en"); ok {
		t.Fatal("error->warning must not balloon")
	}
	if _, ok = BalloonFor(model.AlertEvent{ProfileName: "ERP", From: 0, To: model.LevelError}, "en"); ok {
		t.Fatal("profile-level event must not balloon")
	}
	c, ok := CombineBalloons([]Balloon{{Level: model.LevelWarning}, {Level: model.LevelError}, {Recovered: true}}, "en")
	if !ok || c.Level != model.LevelError || c.Text != "Points overdue: 2, recovered: 1" {
		t.Fatalf("combine: %+v", c)
	}
	if _, ok = CombineBalloons(nil, "en"); ok {
		t.Fatal("empty combine")
	}
}

func sampleStatus(now time.Time) trayipc.Status {
	f := now.Add(-37 * time.Minute)
	old := now.Add(-150 * time.Minute)
	return trayipc.Status{Snapshot: &model.StatusSnapshot{Profiles: []model.ProfileSnapshot{
		{ProfileID: "a", Name: "ERP", Enabled: true, Polled: true, Level: model.LevelWarning, Points: []model.PointStatus{
			{ExchangePoint: model.ExchangePoint{Name: "Склад; \"1\"", Level: model.LevelOK, LastSend: &f}, FreshAt: &f},
			{ExchangePoint: model.ExchangePoint{Name: "=Касса", Level: model.LevelWarning, LastRecv: &old}, FreshAt: &old},
		}},
		{ProfileID: "b", Name: "<HR>", Enabled: true, Polled: true, Level: model.LevelError, Error: "login failed"},
	}}}
}

// CHK-TRAY-19: «Состояние» rows with colour and «N мин назад».
func TestStatusRows(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, model.DBZone)
	rows := StatusRows(sampleStatus(now), "ru", now)
	if len(rows) != 3 {
		t.Fatalf("rows: %+v", rows)
	}
	byPoint := map[string]StatusRow{}
	for _, r := range rows {
		byPoint[r.Point] = r
	}
	if r := byPoint["Склад; \"1\""]; r.Ago != "37 мин назад" || r.Color != ColorGreen || r.AgoMinutes != 37 {
		t.Fatalf("ok row: %+v", r)
	}
	if r := byPoint["=Касса"]; r.Ago != "2 ч 30 мин назад" || r.Color != ColorYellow {
		t.Fatalf("warn row: %+v", r)
	}
	if r := byPoint[""]; r.Database != "<HR>" || r.Color != ColorRed || r.Note != "login failed" {
		t.Fatalf("db row: %+v", r)
	}
	if s, _ := AgoText(nil, now, "en"); s != "" {
		t.Fatal(s)
	}
	n := now.Add(-20 * time.Second)
	if s, _ := AgoText(&n, now, "en"); s != "just now" {
		t.Fatal(s)
	}
	d := now.Add(-72 * time.Hour)
	if s, _ := AgoText(&d, now, "en"); s != "3 days ago" {
		t.Fatal(s)
	}
	if StatusRows(trayipc.Status{}, "ru", now) != nil {
		t.Fatal("no snapshot")
	}
}

// CHK-TRAY-21: CSV (BOM, ";", quoting, formula guard) and HTML (escaped).
func TestExport(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, model.DBZone)
	rows := StatusRows(sampleStatus(now), "ru", now)
	c := ExportCSV(rows, "ru")
	if !bytes.HasPrefix(c, []byte("\xEF\xBB\xBFБаза;Точка;")) {
		t.Fatalf("csv head: %q", c[:40])
	}
	s := string(c)
	if !strings.Contains(s, `;"Склад; ""1""";`) || !strings.Contains(s, ";'=Касса;") || !strings.HasSuffix(s, "\r\n") {
		t.Fatalf("csv: %s", s)
	}
	if strings.Count(s, "\r\n") != 4 {
		t.Fatalf("csv lines: %q", s)
	}
	h := string(ExportHTML(rows, "en", now))
	if !strings.Contains(h, "&lt;HR&gt;") || strings.Contains(h, "<HR>") || !strings.Contains(h, `<meta charset="utf-8">`) ||
		!strings.Contains(h, "#f8d0d0") || !strings.Contains(h, "13.10.2026 12:00") || strings.Contains(h, "<script") {
		t.Fatalf("html: %s", h)
	}
}
