package trayui

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
)

func tp(s string) *time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, model.DBZone)
	if err != nil {
		panic(err)
	}
	return &t
}

func sampleSnapshot() *model.StatusSnapshot {
	return &model.StatusSnapshot{Seq: 5, Level: model.LevelError, Profiles: []model.ProfileSnapshot{
		{ProfileID: "a", Name: "Торговля", Enabled: true, Polled: true, Level: model.LevelWarning, Points: []model.PointStatus{
			{ExchangePoint: model.ExchangePoint{Name: "Склад", Level: model.LevelWarning, LastSend: tp("2026-10-03 12:00"), LastRecv: tp("2026-10-03 12:10"), PlanTable: "_Node124"}, FreshAt: tp("2026-10-03 12:10"), LagMinutes: 40, Key: "_Node124/Склад", Acknowledged: true},
			{ExchangePoint: model.ExchangePoint{Name: "Магазин", Level: model.LevelOK, LastSend: tp("2026-10-03 12:45"), PlanTable: "_Node123", PlanLabel: "Розница"}, FreshAt: tp("2026-10-03 12:45"), LagMinutes: 5},
		}},
		{ProfileID: "b", Name: "Бухгалтерия", Enabled: true, Polled: true, Level: model.LevelError, ErrorKey: model.MsgMonitorConnError, Error: "dial tcp: refused",
			Warnings: []string{"sql.warn_plaintext_login"}, Points: []model.PointStatus{}},
		{ProfileID: "c", Name: "Архив", Enabled: false, Level: model.LevelOK, Points: []model.PointStatus{}},
		{ProfileID: "d", Name: "Новая", Enabled: true, Polled: false, Points: []model.PointStatus{}},
	}}
}

func connected(snap *model.StatusSnapshot) trayipc.Status {
	return trayipc.Status{State: trayipc.StateConnected, Version: 2, Capabilities: []string{model.CapHeartbeat},
		Heartbeat: &model.Heartbeat{Protocol: 2}, Snapshot: snap, EverConnected: true}
}

// TestCHK_TRAY_01_MenuOnlyDatabases — the left menu lists only databases
// (no points at the root), colored squares + status text.
func TestCHK_TRAY_01_MenuOnlyDatabases(t *testing.T) {
	items := BuildLeftMenu(connected(sampleSnapshot()), i18n.RU)
	if len(items) != 4 {
		t.Fatalf("want 4 database items, got %d: %+v", len(items), items)
	}
	want := []struct {
		prefix string
		color  Color
		status string
	}{
		{"Торговля", ColorYellow, "предупреждение"},
		{"Бухгалтерия", ColorRed, "ошибка"},
		{"Архив", ColorGrey, "выключена"},
		{"Новая", ColorGrey, "ожидание опроса"},
	}
	for i, w := range want {
		it := items[i]
		if !strings.HasPrefix(it.Text, w.prefix+" — ") || !strings.Contains(it.Text, w.status) {
			t.Errorf("item %d text %q", i, it.Text)
		}
		if it.Color != w.color {
			t.Errorf("item %d color %v want %v", i, it.Color, w.color)
		}
		if it.Sub == nil || it.Disabled {
			t.Errorf("item %d must be an enabled submenu", i)
		}
		if strings.Contains(it.Text, "Склад") || strings.Contains(it.Text, "Магазин") {
			t.Errorf("points must not be at the root: %q", it.Text)
		}
	}
	en := BuildLeftMenu(connected(sampleSnapshot()), i18n.EN)
	if !strings.Contains(en[0].Text, "warning") || !strings.Contains(en[1].Text, "error") {
		t.Errorf("EN statuses: %q %q", en[0].Text, en[1].Text)
	}
}

// TestCHK_TRAY_01_PointsSubmenu — points of a database: sorted, grouped by
// exchange plan, times in UTC+3, lag, level, acknowledged mark.
func TestCHK_TRAY_01_PointsSubmenu(t *testing.T) {
	sub := BuildLeftMenu(connected(sampleSnapshot()), i18n.RU)[0].Sub
	if len(sub) != 4 {
		t.Fatalf("want 2 plan headers + 2 points, got %+v", sub)
	}
	if !sub[0].Disabled || sub[0].Text != "План обмена: Розница" {
		t.Errorf("plan header %q", sub[0].Text)
	}
	if !strings.HasPrefix(sub[1].Text, "Магазин: отпр. 03.10 12:45, приём —") || sub[1].Color != ColorGreen {
		t.Errorf("point %q", sub[1].Text)
	}
	if sub[2].Text != "План обмена: _Node124" {
		t.Errorf("plan header without label %q", sub[2].Text)
	}
	p := sub[3].Text
	for _, part := range []string{"Склад: отпр. 03.10 12:00", "приём 03.10 12:10", "40 мин назад", "предупреждение", "(подтверждено)"} {
		if !strings.Contains(p, part) {
			t.Errorf("point text %q lacks %q", p, part)
		}
	}
	// a UTC time is shown in UTC+3
	utc := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	got := PointText(model.PointStatus{ExchangePoint: model.ExchangePoint{Name: "X", LastSend: &utc}, LagMinutes: -1}, i18n.EN)
	if !strings.Contains(got, "sent 03.10 12:00") || strings.Contains(got, "ago") {
		t.Errorf("UTC+3 conversion / unknown lag: %q", got)
	}
	bl := PointText(model.PointStatus{ExchangePoint: model.ExchangePoint{Name: "Y"}, Baseline: true, LagMinutes: -1}, i18n.RU)
	if !strings.Contains(bl, "не раньше запуска службы") {
		t.Errorf("baseline: %q", bl)
	}
}

// TestCHK_TRAY_01_ErrorAndWarnings — a database in error shows the
// localized error first; warnings follow; empty database -> a disabled
// "no points" line.
func TestCHK_TRAY_01_ErrorAndWarnings(t *testing.T) {
	items := BuildLeftMenu(connected(sampleSnapshot()), i18n.RU)
	sub := items[1].Sub
	if len(sub) != 2 || !sub[0].Disabled || !strings.Contains(sub[0].Text, model.MonitorMessage(model.MsgMonitorConnError, i18n.RU)) {
		t.Fatalf("error line: %+v", sub)
	}
	if !strings.Contains(sub[1].Text, "sql.warn_plaintext_login") {
		t.Errorf("warning line %q", sub[1].Text)
	}
	if s := items[2].Sub; len(s) != 1 || s[0].Text != "Нет точек обмена" || !s[0].Disabled {
		t.Errorf("empty database: %+v", s)
	}
	none := BuildLeftMenu(connected(&model.StatusSnapshot{}), i18n.RU)
	if len(none) != 1 || none[0].Text != "Нет настроенных баз" {
		t.Errorf("no databases: %+v", none)
	}
}

// TestCHK_TRAY_01_ServiceLineOnTop — a disabled service status line and a
// separator above the databases; distinct texts for not responding,
// poller hung, not running; config stale / invalid shown.
func TestCHK_TRAY_01_ServiceLineOnTop(t *testing.T) {
	st := connected(sampleSnapshot())
	if it := BuildLeftMenu(st, i18n.RU); it[0].Disabled {
		t.Fatal("healthy service: no status line")
	}
	texts := map[string]bool{}
	for _, mod := range []func(*trayipc.Status){
		func(s *trayipc.Status) { s.State = trayipc.StateNotResponding },
		func(s *trayipc.Status) { s.Heartbeat = &model.Heartbeat{PollerHung: true} },
		func(s *trayipc.Status) { s.State = trayipc.StateNotRunning },
		func(s *trayipc.Status) { s.State = trayipc.StateUntrusted },
		func(s *trayipc.Status) { s.State = trayipc.StateUnavailable },
	} {
		s := connected(sampleSnapshot())
		mod(&s)
		items := BuildLeftMenu(s, i18n.RU)
		if !items[0].Disabled || !items[1].Separator || items[0].Sub != nil {
			t.Fatalf("status line on top expected: %+v", items[:2])
		}
		texts[items[0].Text] = true
		if s.State != trayipc.StateConnected && items[2].Color != ColorGrey {
			t.Errorf("stale data must be grey: %+v", items[2])
		}
	}
	if len(texts) != 5 {
		t.Errorf("service states must have distinct texts: %v", texts)
	}
	for _, key := range []string{model.MsgMonitorConfigStale, model.MsgMonitorConfigInvalid} {
		snap := sampleSnapshot()
		snap.ConfigErrorKey, snap.ConfigError, snap.ConfigStale = key, "boom", key == model.MsgMonitorConfigStale
		items := BuildLeftMenu(connected(snap), i18n.RU)
		if items[0].Text != model.MonitorMessage(key, i18n.RU) || !items[0].Disabled || !items[1].Separator {
			t.Errorf("%s line: %+v", key, items[:2])
		}
	}
}

// TestCHK_TRAY_01_RightClickItems — Settings (disabled until 6c, explicit
// label), About, Exit.
func TestCHK_TRAY_01_RightClickItems(t *testing.T) {
	for _, lang := range []string{i18n.RU, i18n.EN} {
		it := BuildRightMenu(lang)
		if len(it) != 4 || it[0].ID != CmdSettings || !it[0].Disabled || it[1].ID != CmdAbout || it[1].Disabled ||
			!it[2].Separator || it[3].ID != CmdExit {
			t.Fatalf("%s: %+v", lang, it)
		}
		if it[0].Text != i18n.T(i18n.KeyMenuSettingsDisabled, lang) {
			t.Error("settings label")
		}
	}
}

// TestCHK_TRAY_02_IndicatorPriority — icon / service state priority.
func TestCHK_TRAY_02_IndicatorPriority(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*trayipc.Status)
		svc  Service
		icon Icon
	}{
		{"ok error level", func(*trayipc.Status) {}, SvcOK, IconError},
		{"warning only", func(s *trayipc.Status) { s.Snapshot.Profiles = s.Snapshot.Profiles[:1] }, SvcOK, IconWarning},
		{"all ok", func(s *trayipc.Status) { s.Snapshot = &model.StatusSnapshot{} }, SvcOK, IconOK},
		{"disabled error ignored", func(s *trayipc.Status) {
			s.Snapshot.Profiles = []model.ProfileSnapshot{{Enabled: false, Level: model.LevelError}}
		}, SvcOK, IconOK},
		{"hung (pong)", func(s *trayipc.Status) { s.Heartbeat.PollerHung = true }, SvcPollerHung, IconProblem},
		{"hung (snapshot)", func(s *trayipc.Status) { s.Snapshot.PollerHung = true }, SvcPollerHung, IconProblem},
		{"not responding", func(s *trayipc.Status) { s.State = trayipc.StateNotResponding }, SvcNotResponding, IconProblem},
		{"untrusted", func(s *trayipc.Status) { s.State = trayipc.StateUntrusted }, SvcUntrusted, IconProblem},
		{"not running", func(s *trayipc.Status) { s.State = trayipc.StateNotRunning; s.Err = errors.New("x") }, SvcNotRunning, IconOffline},
		{"unavailable", func(s *trayipc.Status) { s.State = trayipc.StateUnavailable }, SvcUnavailable, IconOffline},
		{"connecting", func(s *trayipc.Status) { s.State = trayipc.StateConnecting; s.Snapshot = nil }, SvcConnecting, IconOffline},
	}
	for _, c := range cases {
		st := connected(sampleSnapshot())
		c.mod(&st)
		ind := Evaluate(st, i18n.RU)
		if ind.Service != c.svc || ind.Icon != c.icon {
			t.Errorf("%s: service %v icon %v, want %v %v", c.name, ind.Service, ind.Icon, c.svc, c.icon)
		}
		if ind.Tooltip == "" || !strings.HasPrefix(ind.Tooltip, "Exchange Checker: ") {
			t.Errorf("%s: tooltip %q", c.name, ind.Tooltip)
		}
		if c.svc != SvcOK && !strings.Contains(ind.Tooltip, ind.ServiceLine) {
			t.Errorf("%s: tooltip must name the service problem: %q", c.name, ind.Tooltip)
		}
	}
	v1 := connected(sampleSnapshot())
	v1.Version, v1.Heartbeat = 1, nil
	if ind := Evaluate(v1, i18n.RU); ind.Service != SvcOK || !strings.Contains(ind.ServiceLine, "протокол 1") {
		t.Errorf("v1 service line %q", ind.ServiceLine)
	}
}

// TestCHK_TRAY_02_ConfigStaleInvalid — config errors are shown with the
// service texts and do not change the icon level.
func TestCHK_TRAY_02_ConfigStaleInvalid(t *testing.T) {
	snap := sampleSnapshot()
	snap.ConfigErrorKey = model.MsgMonitorConfigStale
	ind := Evaluate(connected(snap), i18n.EN)
	if ind.ConfigLine != model.MonitorMessage(model.MsgMonitorConfigStale, i18n.EN) || ind.Icon != IconError {
		t.Errorf("stale: %+v", ind)
	}
	snap.ConfigErrorKey, snap.ConfigError = "", "settings.json: bad"
	if ind := Evaluate(connected(snap), i18n.RU); ind.ConfigLine != model.MonitorMessage(model.MsgMonitorConfigInvalid, i18n.RU) {
		t.Errorf("error without key: %q", ind.ConfigLine)
	}
}

// TestCHK_TRAY_02_TooltipTruncate — szTip holds 127 UTF-16 units; no
// surrogate pair is split.
func TestCHK_TRAY_02_TooltipTruncate(t *testing.T) {
	long := strings.Repeat("Ж", 200)
	got := TruncateUTF16(long, MaxTooltip)
	if n := len(utf16.Encode([]rune(got))); n > MaxTooltip || !strings.HasSuffix(got, "…") {
		t.Errorf("len %d", n)
	}
	emoji := strings.Repeat("a", 125) + "😀😀"
	got = TruncateUTF16(emoji, MaxTooltip)
	if n := len(utf16.Encode([]rune(got))); n > MaxTooltip || strings.ContainsRune(got, '\uFFFD') {
		t.Errorf("emoji cut: %d %q", n, got)
	}
	if TruncateUTF16("short", MaxTooltip) != "short" {
		t.Error("short text unchanged")
	}
	snap := sampleSnapshot()
	snap.ConfigErrorKey = model.MsgMonitorConfigInvalid
	if ind := Evaluate(connected(snap), i18n.RU); len(utf16.Encode([]rune(ind.Tooltip))) > MaxTooltip {
		t.Errorf("tooltip too long: %q", ind.Tooltip)
	}
}

// TestCHK_TRAY_02_Pixels — square and icon images: right size, colors,
// transparent margin.
func TestCHK_TRAY_02_Pixels(t *testing.T) {
	sq := SquarePixels(ColorRed, 16)
	if len(sq) != 256 || sq[0] != 0 {
		t.Fatal("margin must be transparent")
	}
	r, g, b := RGB(ColorRed)
	if want := uint32(0xFF)<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b); sq[8*16+8] != want {
		t.Errorf("center %08x want %08x", sq[8*16+8], want)
	}
	if SquarePixels(ColorYellow, 16)[8*16+8] == sq[8*16+8] {
		t.Error("colors differ")
	}
	for _, k := range []Icon{IconOK, IconWarning, IconError, IconOffline, IconProblem} {
		px := IconPixels(k, 16)
		if len(px) != 256 || px[0] != 0 || px[8*16+8]>>24 != 0xFF {
			t.Errorf("icon %v: corner %08x center %08x", k, px[0], px[8*16+8])
		}
	}
	if IconPixels(IconProblem, 16)[8*16+8] == IconPixels(IconOffline, 16)[8*16+8] {
		t.Error("problem icon has a red center")
	}
}

// TestCHK_TRAY_01_AboutText — product, version, service protocol.
func TestCHK_TRAY_01_AboutText(t *testing.T) {
	txt := AboutText(connected(nil), i18n.RU)
	for _, part := range []string{version.ProductName, "Версия: " + version.Version, "Служба: протокол 2"} {
		if !strings.Contains(txt, part) {
			t.Errorf("about %q lacks %q", txt, part)
		}
	}
	if !strings.Contains(AboutText(trayipc.Status{State: trayipc.StateNotRunning}, i18n.EN), "Service: not connected") {
		t.Error("about without service")
	}
	if AboutTitle(i18n.EN) != "About Exchange Checker" {
		t.Error("title")
	}
}
