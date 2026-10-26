//go:build windows

package wintray

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

var sampleNow = time.Date(2026, 10, 20, 12, 0, 0, 0, model.DBZone)

// sampleCardsStatus: three databases (warning, error, disabled) for the
// cards test, the layout test and the screenshots.
func sampleCardsStatus() trayipc.Status {
	f := sampleNow.Add(-7 * time.Minute)
	old := sampleNow.Add(-95 * time.Minute)
	return trayipc.Status{Snapshot: &model.StatusSnapshot{Profiles: []model.ProfileSnapshot{
		{ProfileID: "p1", Name: "Склад", Enabled: true, Polled: true, Level: model.LevelWarning, WarnMinutes: 30, ErrorMinutes: 60, Points: []model.PointStatus{
			{ExchangePoint: model.ExchangePoint{Name: "ЦБ", Level: model.LevelOK, LastSend: &f}, FreshAt: &f},
			{ExchangePoint: model.ExchangePoint{Name: "Магазин 12", Level: model.LevelWarning, LastRecv: &old}, FreshAt: &old},
		}},
		{ProfileID: "p2", Name: "Бухгалтерия", Enabled: true, Polled: true, Level: model.LevelError, Error: "login failed"},
		{ProfileID: "p3", Name: "Архив", Enabled: false},
	}}}
}

// sampleDemoStatus: the customer demo of the screenshots, 14 databases
// (more than the places of one card row: «Ещё баз: N»).
func sampleDemoStatus() trayipc.Status {
	st := sampleCardsStatus()
	names := []string{"Магазин 1", "Магазин 2", "Магазин 3", "Магазин 4", "Магазин 5", "Филиал Север", "Филиал Юг",
		"Розница", "Опт", "Логистика", "Производство"}
	for i, n := range names {
		f := sampleNow.Add(-time.Duration(3+i*4) * time.Minute)
		lvl := model.LevelOK
		if i%5 == 3 {
			lvl = model.LevelWarning
		}
		st.Snapshot.Profiles = append(st.Snapshot.Profiles, model.ProfileSnapshot{ProfileID: fmt.Sprintf("d%d", i), Name: n,
			Enabled: true, Polled: true, Level: lvl, WarnMinutes: 30, ErrorMinutes: 60, Points: []model.PointStatus{
				{ExchangePoint: model.ExchangePoint{Name: "ЦБ", Level: lvl, LastSend: &f, LastRecv: &f}, FreshAt: &f}}})
	}
	return st
}

// TestCHK_TRAY_49_IconsInUse — the generated icons are used at 96 and
// 144 dpi (PLAN §12 p.144): the window icons (small / big), the settings
// navigation buttons, the tray icon of every state; sizes follow the DPI.
func TestCHK_TRAY_49_IconsInUse(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	done, _ := useComCtl6()
	defer done()
	defer func() { testDPI = 0 }()
	for _, dpi := range []int32{96, 144} {
		testDPI = dpi
		var ic icons
		for _, k := range []trayui.Icon{trayui.IconOK, trayui.IconWarning, trayui.IconError, trayui.IconOffline, trayui.IconProblem, trayui.IconBlank} {
			if ic.get(k) == 0 {
				t.Errorf("%d dpi: tray icon %d", dpi, k)
			}
		}
		for _, n := range trayui.IconNames() {
			if uiIcons.ico(n, px(icoSmall)) == 0 || uiIcons.ico(n, px(icoLarge)) == 0 {
				t.Errorf("%d dpi: icon %s", dpi, n)
			}
		}
		ic.free()
		if want := map[int32]int32{96: 16, 144: 24}[dpi]; px(icoSmall) != want {
			t.Errorf("%d dpi: small icon %d px, want %d", dpi, px(icoSmall), want)
		}
		sw, err := newStatusWindow(i18n.RU, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		if winapi.SendMessage(sw.hwnd, winapi.WM_GETICON, winapi.ICON_SMALL, 0) == 0 || winapi.SendMessage(sw.hwnd, winapi.WM_GETICON, winapi.ICON_BIG, 0) == 0 {
			t.Errorf("%d dpi: status window without icons", dpi)
		}
		winapi.DestroyWindow(sw.hwnd)
	}
	testDPI = 0
	app := startCfgApp(t, newCfgService(), true, nil)
	defer app.Exit()
	w := openAdmin(t, app)
	if len(w.navIco) != 4 {
		t.Fatalf("%d navigation icons", len(w.navIco))
	}
	for _, p := range w.navIco {
		b, ic := p[0].H, p[1].H
		if winapi.SendMessage(ic, winapi.STM_GETICON, 0, 0) == 0 {
			t.Errorf("nav %q without icon", winapi.GetWindowText(b))
		}
		if visible(b) != visible(ic) {
			t.Errorf("nav %q: icon visible %v, button %v", winapi.GetWindowText(b), visible(ic), visible(b))
		}
		if visible(b) && winapi.GetWindowRect(ic).Right > winapi.GetWindowRect(b).Left {
			t.Errorf("nav %q: icon not left of the button", winapi.GetWindowText(b))
		}
	}
	if winapi.SendMessage(w.hwnd, winapi.WM_GETICON, winapi.ICON_SMALL, 0) == 0 {
		t.Error("settings window without icon")
	}
}

// TestCHK_TRAY_48_CardPanel — the «Состояние» window shows one card per
// database above the table (PLAN §12 p.143): visible, inside the panel,
// not overlapping, with tooltips listing the points, RU+EN; with more
// databases than places the last place counts the rest.
func TestCHK_TRAY_48_CardPanel(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	done, _ := useComCtl6()
	defer done()
	for _, lang := range []string{i18n.RU, i18n.EN} {
		sw, err := newStatusWindow(lang, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		sw.now = func() time.Time { return sampleNow }
		sw.update(sampleCardsStatus(), true)
		p := sw.cards
		if p.visible() != 3 {
			t.Fatalf("%s: %d cards, want 3", lang, p.visible())
		}
		pr := winapi.GetWindowRect(p.hwnd)
		var prev []winapi.RECT
		for i := 0; i < p.visible(); i++ {
			s := p.slots[i]
			if winapi.WindowStyle(s)&winapi.WS_VISIBLE == 0 {
				t.Errorf("%s: card %d hidden", lang, i)
			}
			r := winapi.GetWindowRect(s)
			if r.Left < pr.Left || r.Top < pr.Top || r.Right > pr.Right || r.Bottom > pr.Bottom || r.Right-r.Left < px(cardW)-1 {
				t.Errorf("%s: card %d %+v outside the panel %+v or too narrow", lang, i, r, pr)
			}
			for j, q := range prev {
				if r.Left < q.Right && q.Left < r.Right && r.Top < q.Bottom && q.Top < r.Bottom {
					t.Errorf("%s: cards %d and %d overlap", lang, i, j)
				}
			}
			prev = append(prev, r)
		}
		if tip := sw.tips.text(p.slots[0]); !strings.Contains(tip, "Магазин 12") {
			t.Errorf("%s: tooltip of the first card %q", lang, tip)
		}
		if sw.tips.text(p.hwnd) != i18n.T(i18n.KeyTipCards, lang) {
			t.Errorf("%s: panel tooltip", lang)
		}
		if c := p.shown[1]; c.Info != "login failed" {
			t.Errorf("%s: error card %+v", lang, c)
		}
		// more databases than places
		st := sampleCardsStatus()
		for i := 0; i < 30; i++ {
			st.Snapshot.Profiles = append(st.Snapshot.Profiles, model.ProfileSnapshot{ProfileID: "x" + string(rune('a'+i)), Name: "DB", Enabled: false})
		}
		sw.update(st, true)
		last := p.shown[len(p.shown)-1]
		if p.visible() > cardSlots || !strings.Contains(last.Tip, i18n.T(i18n.KeyCardMoreHint, lang)) {
			t.Errorf("%s: %d places, last %+v", lang, p.visible(), last)
		}
		winapi.DestroyWindow(sw.hwnd)
	}
}
