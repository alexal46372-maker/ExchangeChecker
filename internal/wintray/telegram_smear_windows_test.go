//go:build windows

package wintray

import (
	"context"
	"runtime"
	"strconv"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// tgChromeCfg is the chrome fixture of CHK-TRAY-66/67/72/73 (Viber +
// e-mail + alarm_test) plus capability telegram (settings.telegram,
// telegram_test), so the settings window has the 5th tab «Telegram».
type tgChromeCfg struct {
	*chromeCfg
	tg model.TelegramSettings
}

func newTgChromeCfg() *tgChromeCfg {
	tg := model.DefaultTelegramSettings()
	tg.Enabled, tg.ChatID, tg.HasToken = true, "-100123", true
	tg.Status = &model.ViberStatus{Sent: 2}
	f := &tgChromeCfg{chromeCfg: newChromeCfg(), tg: tg}
	f.chromeCfg.cfgService.handler = f
	return f
}

func (f *tgChromeCfg) withTelegram(s *model.Settings) {
	t := f.tg
	s.Telegram = &t
}

func (f *tgChromeCfg) GetConfig() (model.ConfigView, string, error) {
	cv, rev, err := f.chromeCfg.GetConfig()
	f.withTelegram(&cv.Settings)
	return cv, rev, err
}

func (f *tgChromeCfg) Snapshot() model.StatusSnapshot {
	snap := f.chromeCfg.Snapshot()
	if snap.Settings != nil {
		f.withTelegram(snap.Settings)
	}
	return snap
}

func (f *tgChromeCfg) TelegramTest(ctx context.Context, token, chat string) (model.TelegramTestResult, error) {
	return model.TelegramTestResult{OK: true}, nil
}

// TestCHK_TRAY_94_TelegramScrollSmear — the «Telegram» tab (exception to
// the frozen tab list, settings_tabs_windows.go: exactly the 3 lines that
// add the tab) goes through the same erase/redraw proofs as the other tabs
// in CHK-TRAY-71/72/73, with the same helpers (CaptureScreen BitBlt, not
// only PrintWindow): (1) scroll to 1/4, 1/2, end and back — page colour
// share, PrintWindow vs screen, round-trip diff ≤ 1 %; (2) opened while a
// foreign TOPMOST window covers the host, then the cover is destroyed — the
// screen equals the tab shown uncovered; (3) cover + DestroyWindow at a
// scrolled position. Fixture: chrome caps + capability telegram.
func TestCHK_TRAY_94_TelegramScrollSmear(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newTgChromeCfg()
	app := startCfgApp(t, svc.chromeCfg.cfgService, true, nil)
	defer app.Exit()
	if !app.Status().Has(model.CapTelegram) {
		t.Fatalf("capability telegram: %v", app.Status().Capabilities)
	}
	defer func() { testDPI = 0 }()
	testDPI = 96

	w := openChromeSettings(t, app, i18n.RU, 96, false)
	defer w.close()
	pumpUntil(t, "telegram tab", func() bool {
		w.update()
		for _, pg := range w.tabPages {
			if pg == pageTelegram {
				return true
			}
		}
		return false
	})
	if n := winapi.TabItemCount(w.ctl[idSetTabs]); n != 5 {
		t.Fatalf("need 5 tabs (Общие/Базы/Viber/e-mail/Telegram), got %d %v", n, w.tabPages)
	}
	undo := bringSettingsOnScreen(t, w.hwnd)
	defer undo()
	pageCol := trayui.ChromePage()

	show := func() {
		w.setPage(pageTelegram)
		w.layoutFit()
		pumpN(8)
		if w.page != pageTelegram || !visible(w.ctl[idTgChat]) || visible(w.ctl[idSetName]) {
			t.Fatal("telegram page not shown")
		}
	}
	show()
	// The tab must really scroll: when the page fits at the default size,
	// make the frame lower (the user can resize it the same way).
	for i := 0; i < 6 && max32(0, w.contentH-w.viewH) < w.contentH/3; i++ {
		fr := winapi.GetWindowRect(w.hwnd)
		h := (fr.Bottom - fr.Top) - (w.viewH - w.contentH/2)
		if h < 120 {
			h = 120
		}
		winapi.SetWindowPos(w.hwnd, 0, 0, 0, fr.Right-fr.Left, h,
			winapi.SWP_NOMOVE|winapi.SWP_NOZORDER|winapi.SWP_NOACTIVATE)
		pumpN(6)
		w.layoutFit()
		pumpN(6)
	}
	maxY := max32(0, w.contentH-w.viewH)
	if maxY < 8 {
		t.Fatalf("telegram page does not scroll (content %d, view %d)", w.contentH, w.viewH)
	}

	// (1) scroll smear — CHK-TRAY-72 method.
	host := w.host
	winapi.SetFocus(host)
	pumpN(5)
	w.scrollTo(0, 0)
	pumpN(8)
	winapi.DwmFlush()
	baseScreen := mustCaptureScreen(t, host, "telegram screen baseline")
	for _, y := range []int32{maxY / 4, maxY / 2, maxY} {
		w.scrollTo(0, y)
		pumpN(10)
		winapi.DwmFlush()
		if w.scrollY != y {
			t.Fatalf("scrollTo(%d) → scrollY=%d", y, w.scrollY)
		}
		tag := "telegram y=" + strconv.Itoa(int(y))
		scr := mustCaptureScreen(t, host, tag)
		share := pageColorShare(scr, pageCol)
		if share < 0.05 {
			t.Fatalf("%s: CaptureScreen pageColorShare=%.3f (ghost/uncovered band under controls)", tag, share)
		}
		cw, ch, pwPix, pwOK := winapi.CaptureWindow(host)
		if pwOK && cw > 0 && ch > 0 && len(pwPix) == len(scr) {
			if pwShare := pageColorShare(pwPix, pageCol); pwShare >= 0.10 && share < pwShare*0.55 {
				t.Fatalf("%s: PrintWindow clean (share=%.3f) but CaptureScreen dirty (%.3f)", tag, pwShare, share)
			}
		}
	}
	w.scrollTo(0, 0)
	pumpN(10)
	winapi.DwmFlush()
	after := mustCaptureScreen(t, host, "telegram screen return")
	if len(after) != len(baseScreen) {
		t.Fatalf("return CaptureScreen size %d != baseline %d", len(after), len(baseScreen))
	}
	shareBase, shareAfter := pageColorShare(baseScreen, pageCol), pageColorShare(after, pageCol)
	if shareAfter < 0.05 || (shareBase >= 0.10 && shareAfter < shareBase*0.70) {
		t.Fatalf("return: screen pageColorShare after=%.3f base=%.3f (smear)", shareAfter, shareBase)
	}
	if diff := pixelDiffRatio(baseScreen, after); diff > 0.01 {
		t.Fatalf("return: CaptureScreen round-trip diff=%.3f (smear/ghost)", diff)
	}
	if why := w.assertPageClear(); why != "" {
		t.Fatalf("pageClear after scroll: %s", why)
	}

	newCover := func(tag string) uintptr {
		t.Helper()
		over, err := winapi.CreateWindowEx(winapi.WS_EX_TOPMOST, "STATIC", "cover",
			winapi.WS_POPUP|winapi.WS_VISIBLE, 0, 0, 40, 40, 0, 0, winapi.GetModuleHandle())
		if err != nil || over == 0 {
			t.Fatalf("%s: overlay create: %v", tag, err)
		}
		hr := winapi.GetWindowRect(w.host)
		winapi.SetWindowPos(over, winapi.HWND_TOPMOST, hr.Left, hr.Top, hr.Right-hr.Left, hr.Bottom-hr.Top,
			winapi.SWP_SHOWWINDOW)
		winapi.ShowWindow(over, winapi.SW_SHOW)
		pumpN(6)
		winapi.DwmFlush()
		return over
	}
	checkUncovered := func(tag string, want []byte) {
		t.Helper()
		got := mustCaptureScreen(t, w.host, tag)
		if len(got) != len(want) {
			t.Fatalf("%s: CaptureScreen size %d != baseline %d", tag, len(got), len(want))
		}
		sb, sa := pageColorShare(want, pageCol), pageColorShare(got, pageCol)
		if sa < 0.05 || (sb >= 0.10 && sa < sb*0.70) {
			t.Fatalf("%s: pageColorShare after=%.3f base=%.3f (ghost after uncover)", tag, sa, sb)
		}
		if diff := pixelDiffRatio(want, got); diff > 0.01 {
			t.Fatalf("%s: CaptureScreen diff=%.3f after DestroyWindow uncover (ghost; test did not eraseRepaint)", tag, diff)
		}
		if why := w.assertPageClear(); why != "" {
			t.Fatalf("%s pageClear: %s", tag, why)
		}
	}

	// (2) open the tab while covered: General shown, foreign TOPMOST cover,
	// switch to Telegram under it, destroy the cover.
	w.setPage(1)
	w.layoutFit()
	pumpN(8)
	over := newCover("open-covered")
	show()
	w.scrollTo(0, 0)
	winapi.SetFocus(host)
	pumpN(8)
	winapi.DestroyWindow(over)
	pumpN(12)
	winapi.DwmFlush()
	checkUncovered("open-covered", baseScreen)

	// (3) uncover at a scrolled position — CHK-TRAY-73 method.
	w.scrollTo(0, maxY/3)
	pumpN(8)
	winapi.DwmFlush()
	pre := mustCaptureScreen(t, w.host, "uncover pre")
	over = newCover("uncover")
	winapi.DestroyWindow(over)
	pumpN(12)
	winapi.DwmFlush()
	checkUncovered("uncover y="+strconv.Itoa(int(maxY/3)), pre)
}
