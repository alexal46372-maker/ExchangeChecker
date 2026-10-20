//go:build windows

package wintray

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

const wmGetFont = 0x0031

type laidCtl struct {
	h     uintptr
	class string
	text  string
	style uint32
	r     winapi.RECT
}

func (c laidCtl) group() bool {
	return c.class == "Button" && c.style&winapi.BS_TYPEMASK == winapi.BS_GROUPBOX
}

func (c laidCtl) String() string {
	return fmt.Sprintf("%s %q (%d,%d-%d,%d)", c.class, c.text, c.r.Left, c.r.Top, c.r.Right, c.r.Bottom)
}

func intersects(a, b winapi.RECT) bool {
	return a.Left < b.Right && b.Left < a.Right && a.Top < b.Bottom && b.Top < a.Bottom
}

func contains(o, i winapi.RECT) bool {
	return o.Left <= i.Left && o.Top <= i.Top && i.Right <= o.Right && i.Bottom <= o.Bottom
}

// checkLayout: the visible children of hwnd do not overlap (a control is
// fully inside or outside a group box, group boxes never cross) and every
// text fits its control in the control's own font.
func checkLayout(t *testing.T, what string, hwnd uintptr) {
	t.Helper()
	// phase 10 E: every visible control lies inside its parent's client
	// area (a scrolled page host: inside its content extent)
	ox, oy := winapi.ClientOrigin(hwnd)
	cr := winapi.GetClientRect(hwnd)
	bounds := winapi.RECT{Left: ox, Top: oy, Right: ox + cr.Right, Bottom: oy + cr.Bottom}
	if ext, ok := hostExtent[hwnd]; ok {
		if ox+ext[0] > bounds.Right {
			bounds.Right = ox + ext[0]
		}
		if oy+ext[1] > bounds.Bottom {
			bounds.Bottom = oy + ext[1]
		}
	}
	var cs []laidCtl
	for _, h := range winapi.Children(hwnd) {
		st := winapi.WindowStyle(h)
		if st&winapi.WS_VISIBLE == 0 {
			continue
		}
		cs = append(cs, laidCtl{h: h, class: winapi.GetClassName(h), text: winapi.GetWindowText(h), style: st, r: winapi.GetWindowRect(h)})
	}
	if len(cs) == 0 {
		t.Errorf("%s: no visible controls", what)
	}
	// phase 11 (CHK-TRAY-47): never a horizontal scroll bar; a top-level
	// window does not scroll at all (the button bar is pinned and visible,
	// only a scroll host child scrolls, vertically)
	if st := winapi.WindowStyle(hwnd); st&winapi.WS_HSCROLL != 0 {
		t.Errorf("%s: horizontal scroll bar", what)
	} else if !strings.HasSuffix(what, "(page)") && st&winapi.WS_VSCROLL != 0 {
		t.Errorf("%s: the window scrolls (the bar must stay visible)", what)
	}
	if ext, ok := hostExtent[hwnd]; ok && ext[0] > cr.Right {
		t.Errorf("%s: content %d px wider than the client %d px", what, ext[0], cr.Right)
	}
	// no vertical hole larger than the grid step (16 units): the bands
	// covered by the controls leave no gap between the first and the last
	if gap, at := maxGap(cs); gap > px(16) {
		t.Errorf("%s: a vertical gap of %d px (> %d) above %v", what, gap, px(16), at)
	}
	for _, c := range cs {
		if !contains(bounds, c.r) {
			t.Errorf("%s: %v outside the window %+v", what, c, bounds)
		}
		if c.class == SettingsPageClass || c.class == ScrollHostClass {
			checkLayout(t, what+" (page)", c.h)
		}
	}
	for i := range cs {
		for j := i + 1; j < len(cs); j++ {
			a, b := cs[i], cs[j]
			if !intersects(a.r, b.r) {
				continue
			}
			switch {
			case a.group() && b.group():
			case a.group() && contains(a.r, b.r), b.group() && contains(b.r, a.r):
				continue
			}
			t.Errorf("%s: %v overlaps %v", what, a, b)
		}
	}
	for _, c := range cs {
		if c.text == "" {
			continue
		}
		font := winapi.SendMessage(c.h, wmGetFont, 0, 0)
		w, h := c.r.Right-c.r.Left, c.r.Bottom-c.r.Top
		fit := true
		switch c.class {
		case "Static":
			if c.style&winapi.SS_TYPEMASK == winapi.SS_ICON {
				continue
			}
			_, th := winapi.TextSize(font, c.text, w)
			fit = th <= h
		case "Button":
			typ := c.style & winapi.BS_TYPEMASK
			pad := int32(px(8))
			switch typ {
			case winapi.BS_GROUPBOX:
				pad = px(16)
			case 2, 3, 4, 5, 6, 9: // check boxes and radio buttons
				pad = px(20)
			}
			if c.style&winapi.BS_MULTILINE != 0 {
				_, th := winapi.TextSize(font, c.text, w-pad)
				fit = th <= h
			} else {
				tw, _ := winapi.TextSize(font, c.text, 0)
				fit = tw <= w-pad
			}
		default:
			continue
		}
		if !fit {
			t.Errorf("%s: text does not fit %v", what, c)
		}
	}
}

// maxGap is the largest vertical band between the topmost and the
// bottommost control that no control covers.
func maxGap(cs []laidCtl) (int32, laidCtl) {
	type band struct {
		a, b int32
		c    laidCtl
	}
	bs := make([]band, 0, len(cs))
	for _, c := range cs {
		bs = append(bs, band{c.r.Top, c.r.Bottom, c})
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].a < bs[j].a })
	gap, end := int32(0), int32(0)
	var at laidCtl
	for i, b := range bs {
		if i > 0 && b.a-end > gap {
			gap, at = b.a-end, b.c
		}
		if i == 0 || b.b > end {
			end = b.b
		}
	}
	return gap, at
}

// TestCHK_TRAY_42_Font — Segoe UI 9 pt scaled to the DPI.
func TestCHK_TRAY_42_Font(t *testing.T) {
	defer func() { testDPI = 0 }()
	for _, c := range []struct{ dpi, in, out int32 }{{96, 100, 100}, {120, 100, 125}, {144, 100, 150}, {144, 7, 11}} {
		testDPI = c.dpi
		if px(c.in) != c.out {
			t.Errorf("px(%d) at %d dpi = %d, want %d", c.in, c.dpi, px(c.in), c.out)
		}
		f := uiFont()
		if f == 0 || f == winapi.GetStockObject(winapi.DEFAULT_GUI_FONT) {
			t.Errorf("%d dpi: no Segoe UI font", c.dpi)
		}
		_, h := winapi.TextSize(f, "Аб", 0)
		if want := c.dpi * 12 / 96; h < want || h > want*2 {
			t.Errorf("%d dpi: text height %d, want about %d", c.dpi, h, want)
		}
	}
}

// TestCHK_TRAY_42_LayoutOverlap — every tray window, RU and EN, at 96, 120
// and 144 dpi: no control overlaps another, every text fits.
func TestCHK_TRAY_42_LayoutOverlap(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() { testDPI = 0 }()
	defer func() { testWorkArea = nil }()
	small := winapi.RECT{Right: 1024, Bottom: 728} // 1024×768 minus a 40 px taskbar
	for _, dpi := range []int32{96, 120, 144} {
		for _, lang := range []string{i18n.RU, i18n.EN} {
			testDPI = dpi
			tag := fmt.Sprintf("%s %d dpi", lang, dpi)
			func() {
				testWorkArea = &small
				defer func() { testWorkArea = nil }()
				checkSettingsFit(t, "small "+tag, lang, small)
			}()
			uiScenes(t, lang, func(name string, hwnd uintptr) { checkLayout(t, name+" "+tag, hwnd) })
		}
	}
}

// checkSettingsFit — the settings window (admin expert / simple /
// «Общие», read-only) inside the work area, «Сохранить» / «Закрыть»
// inside the client, every page control reachable by scrolling.
func checkSettingsFit(t *testing.T, tag, lang string, work winapi.RECT) {
	t.Helper()
	ds := newDiscService()
	app := startCfgApp(t, ds.cfgService, true, nil)
	defer app.Exit()
	for _, mode := range []traysettings.Mode{traysettings.Admin, traysettings.ReadOnly} {
		w, err := newSettingsWindow(settingsConfig{lang: lang, mode: mode, session: app.session, hidden: true,
			uiMode: traysettings.UIExpert, elevator: &fakeElevator{}})
		if err != nil {
			t.Fatal(err)
		}
		r := winapi.GetWindowRect(w.hwnd)
		if !contains(work, r) {
			t.Errorf("%s: settings window %+v outside the work area %+v", tag, r, work)
		}
		ox, oy := winapi.ClientOrigin(w.hwnd)
		cr := winapi.GetClientRect(w.hwnd)
		client := winapi.RECT{Left: ox, Top: oy, Right: ox + cr.Right, Bottom: oy + cr.Bottom}
		for _, id := range []int32{idSetSave, idSetClose, idSetStatus} {
			if h := w.ctl[id]; h != 0 && !contains(client, winapi.GetWindowRect(h)) {
				t.Errorf("%s: bar control %d not visible", tag, id)
			}
		}
		w.Reload()
		pumpUntil(t, "loaded", func() bool { return !w.busy })
		for page := 0; page < 2; page++ {
			w.setPage(page)
			checkLayout(t, fmt.Sprintf("settings %v page %d %s", mode, page, tag), w.hwnd)
			// scrolled to the end: the last control of the page is in the view
			w.scrollTo(1<<20, 1<<20)
			last := w.ctl[idSetInfo]
			if page == 1 {
				last = w.ctl[idGenNote]
			}
			hx, hy := winapi.ClientOrigin(w.host)
			view := winapi.RECT{Left: hx, Top: hy, Right: hx + w.viewW, Bottom: hy + w.viewH}
			lr := winapi.GetWindowRect(last)
			if lr.Bottom > view.Bottom || lr.Right > view.Right {
				t.Errorf("%s page %d: last control %+v not reachable (view %+v)", tag, page, lr, view)
			}
			w.scrollTo(0, 0)
		}
		w.close()
	}
}

// uiScenes opens every tray window (settings expert / general / simple /
// read-only, import, discovery, sound, history, volume, panel, about, wizard,
// alert, status) in lang and visits it (layout test, CI screenshots).
func uiScenes(t *testing.T, lang string, visit func(name string, hwnd uintptr)) {
	t.Helper()
	func() { // import dialog after a dry run (phase 11)
		e := newXferEnv(t, lang)
		writeImportFile(t, e.path, &traysettings.TrayBlock{Language: "ru"}, e.x.profiles[0])
		e.w.click(t, idSetImport)
		d := e.w.imp
		if d == nil {
			t.Fatalf("import dialog: %q", e.w.statusText())
		}
		pumpUntil(t, "dry run", func() bool { return d.res != nil })
		visit("import", d.hwnd)
		d.close()
		e.w.close()
	}()
	func() {
		ds := newDiscService()
		app := startCfgApp(t, ds.cfgService, true, nil)
		defer app.Exit()
		w, err := newSettingsWindow(settingsConfig{lang: lang, mode: traysettings.Admin, session: app.session, hidden: true,
			uiMode: traysettings.UIExpert})
		if err != nil {
			t.Fatal(err)
		}
		defer w.close()
		w.Reload()
		pumpUntil(t, "loaded", func() bool { return !w.busy && w.ed.HasProfile() })
		visit("settings expert", w.hwnd)
		dd := openDisc(t, w)
		visit("discovery", dd.hwnd)
		dd.close()
		w.setPage(1)
		visit("settings general", w.hwnd)
		w.setPage(0)
		w.toggleUIMode()
		visit("settings simple", w.hwnd)

		ro, err := newSettingsWindow(settingsConfig{lang: lang, mode: traysettings.ReadOnly, session: app.session, hidden: true,
			uiMode: traysettings.UIExpert, elevator: &fakeElevator{}})
		if err != nil {
			t.Fatal(err)
		}
		visit("settings read-only", ro.hwnd)
		ro.close()

		app.opt.Lang = lang
		sw, err := newSoundWindow(app)
		if err != nil {
			t.Fatal(err)
		}
		visit("sound", sw.hwnd)
		winapi.DestroyWindow(sw.hwnd)
		hw, err := newHistWindow(app, trayui.StatusRow{Database: "db1", Point: "ЦБ"}, false)
		if err != nil {
			t.Fatal(err)
		}
		visit("history", hw.hwnd)
		winapi.DestroyWindow(hw.hwnd)
		app.opt.HiddenPanel = true
		if v := app.OpenVolume(); v != nil {
			visit("volume", v.hwnd)
			winapi.DestroyWindow(v.hwnd)
		} else {
			t.Error("volume window")
		}
		app.problems = []trayalert.Problem{{Kind: trayalert.ProblemError, Text: "Склад / Exchange — ЦБ: 45 мин без обмена"},
			{Kind: trayalert.ProblemNoConnection, Text: "Магазин 12: нет подключения к SQL Server"},
			{Kind: trayalert.ProblemWarning, Text: "Офис — Розница: 25 мин без обмена"}}
		app.updatePanel()
		if p := app.Panel(); p != nil {
			visit("panel", p.hwnd)
		} else {
			t.Error("panel")
		}
		ab, err := newAboutWindow(0, trayui.AboutTitle(lang), trayui.AboutText(app.session.Status(), lang),
			i18n.T(i18n.KeyAboutOK, lang), "", false, nil)
		if err == nil {
			visit("about", ab)
			winapi.DestroyWindow(ab)
		}
	}()
	func() {
		ws := newWizService()
		app := startCfgApp(t, ws.cfgService, true, nil)
		defer app.Exit()
		app.opt.Lang = lang
		w := openAdmin(t, app)
		w.click(t, idSetAdd)
		if w.wiz == nil {
			t.Fatal("wizard not opened")
		}
		visit("wizard", w.wiz.hwnd)
		// phase 11: steps 2 and 3 too
		wd := w.wiz
		wd.z.Server, wd.z.Login = "srv", "mon"
		wd.z.Databases = []model.SQLDatabaseInfo{{Name: "ut"}, {Name: "retail"}}
		wd.z.PickDatabase(0)
		wd.z.Step = traysettings.WizDatabase
		wd.show()
		visit("wizard step2", wd.hwnd)
		wd.z.Step = traysettings.WizFinish
		wd.show()
		visit("wizard step3", wd.hwnd)
	}()
	func() { // phase 11: the Viber page
		svc := newViberCfg()
		app := startCfgApp(t, svc.cfgService, true, nil)
		defer app.Exit()
		vw, err := newSettingsWindow(settingsConfig{lang: lang, mode: traysettings.Admin, session: app.session, hidden: true,
			uiMode: traysettings.UIExpert})
		if err != nil {
			t.Fatal(err)
		}
		defer vw.close()
		vw.Reload()
		pumpUntil(t, "loaded", func() bool { return !vw.busy && vw.ed.HasViber })
		vw.setPage(pageViber)
		visit("settings viber", vw.hwnd)
	}()
	aw, err := newAlertWindow(0, lang, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	aw.apply(sampleAlert(lang), false) // a sample alert: text and status filled
	visit("alert", aw.hwnd)
	winapi.DestroyWindow(aw.hwnd)
	st, err := newStatusWindow(lang, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	visit("status", st.hwnd)
	winapi.DestroyWindow(st.hwnd)
}

// sampleAlert is an alert view for the layout test and the screenshots.
func sampleAlert(lang string) trayalert.View {
	msg := "Служба: подтверждение отправлено"
	if lang == i18n.EN {
		msg = "Service: acknowledgement sent"
	}
	at := time.Date(2026, 10, 20, 9, 30, 0, 0, time.FixedZone("UTC+3", 3*3600))
	return trayalert.View{Visible: true, Level: model.LevelError, CanAck: true, Message: msg,
		Alerts: []model.AlertEvent{
			{ID: 1, ProfileID: "p1", ProfileName: "Склад", PointName: "ЦБ", PlanTable: "_Node123", From: model.LevelWarning, To: model.LevelError, At: at, FreshAt: &at, LagMinutes: 45},
			{ID: 2, ProfileID: "p2", ProfileName: "Магазин 12", From: model.LevelOK, To: model.LevelError, At: at},
		}}
}
