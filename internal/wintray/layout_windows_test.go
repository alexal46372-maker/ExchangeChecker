//go:build windows

package wintray

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
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
	for _, dpi := range []int32{96, 120, 144} {
		for _, lang := range []string{i18n.RU, i18n.EN} {
			testDPI = dpi
			tag := fmt.Sprintf("%s %d dpi", lang, dpi)
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
				checkLayout(t, "settings expert "+tag, w.hwnd)
				dd := openDisc(t, w)
				checkLayout(t, "discovery "+tag, dd.hwnd)
				dd.close()
				w.setPage(1)
				checkLayout(t, "settings general "+tag, w.hwnd)
				w.setPage(0)
				w.toggleUIMode()
				checkLayout(t, "settings simple "+tag, w.hwnd)

				ro, err := newSettingsWindow(settingsConfig{lang: lang, mode: traysettings.ReadOnly, session: app.session, hidden: true,
					uiMode: traysettings.UIExpert, elevator: &fakeElevator{}})
				if err != nil {
					t.Fatal(err)
				}
				checkLayout(t, "settings read-only "+tag, ro.hwnd)
				ro.close()

				app.opt.Lang = lang
				sw, err := newSoundWindow(app)
				if err != nil {
					t.Fatal(err)
				}
				checkLayout(t, "sound "+tag, sw.hwnd)
				winapi.DestroyWindow(sw.hwnd)
				hw, err := newHistWindow(app, trayui.StatusRow{Database: "db1", Point: "ЦБ"}, false)
				if err != nil {
					t.Fatal(err)
				}
				checkLayout(t, "history "+tag, hw.hwnd)
				winapi.DestroyWindow(hw.hwnd)
				ab, err := newAboutWindow(0, trayui.AboutTitle(lang), trayui.AboutText(app.session.Status(), lang),
					i18n.T(i18n.KeyAboutOK, lang), "", false, nil)
				if err == nil {
					checkLayout(t, "about "+tag, ab)
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
				checkLayout(t, "wizard "+tag, w.wiz.hwnd)
			}()
			aw, err := newAlertWindow(0, lang, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			checkLayout(t, "alert "+tag, aw.hwnd)
			winapi.DestroyWindow(aw.hwnd)
			st, err := newStatusWindow(lang, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			checkLayout(t, "status "+tag, st.hwnd)
			winapi.DestroyWindow(st.hwnd)
		}
	}
}
