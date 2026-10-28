//go:build windows

package wintray

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_68_GlobalEraseRedrawContract — winscroll + settingsfit +
// uipin share one coherent design: MoveWindowNoRedraw in scroll paths,
// eraseRepaint (RDW_INVALIDATE|ERASE|ALLCHILDREN|UPDATENOW), no ScrollWindow,
// ScrollHost WS_CLIPCHILDREN|WS_CLIPSIBLINGS, ensureClass Background 0,
// persistent page brush erase (CHK-TRAY-68).
func TestCHK_TRAY_68_GlobalEraseRedrawContract(t *testing.T) {
	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	// No ScrollWindow anywhere in tray / winapi product code.
	for _, name := range []string{
		"winscroll_windows.go", "settingsfit_windows.go", "uipin_windows.go",
		"settings_windows.go", "redraw_windows.go",
	} {
		b := read(name)
		// allow the "never ScrollWindow" comments only
		body := string(b)
		for _, line := range strings.Split(body, "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "//") {
				continue
			}
			if strings.Contains(line, "ScrollWindow") {
				t.Fatalf("%s: ScrollWindow call forbidden: %s", name, trim)
			}
		}
	}
	// scroll paths must MoveWindowNoRedraw, not MoveWindow(…,1)
	fit := read("settingsfit_windows.go")
	scroll := read("winscroll_windows.go")
	if !bytes.Contains(fit, []byte("MoveWindowNoRedraw")) {
		t.Fatal("settingsfit.scrollTo: missing MoveWindowNoRedraw")
	}
	if !bytes.Contains(scroll, []byte("MoveWindowNoRedraw")) {
		t.Fatal("winscroll.scrollTo: missing MoveWindowNoRedraw")
	}
	// extract scrollTo bodies roughly: between "func (…scrollTo" and next top-level func
	assertScrollBodyNoRepaintMove := func(src []byte, label string) {
		t.Helper()
		s := string(src)
		i := strings.Index(s, "func (")
		for i >= 0 {
			rest := s[i:]
			if strings.Contains(rest[:minInt(80, len(rest))], "scrollTo(") {
				end := strings.Index(rest[1:], "\nfunc ")
				body := rest
				if end >= 0 {
					body = rest[:end+1]
				}
				// MoveWindow( inside scrollTo is FAIL (must be NoRedraw)
				for _, line := range strings.Split(body, "\n") {
					trim := strings.TrimSpace(line)
					if strings.HasPrefix(trim, "//") {
						continue
					}
					if strings.Contains(trim, "winapi.MoveWindow(") && !strings.Contains(trim, "MoveWindowNoRedraw") {
						t.Fatalf("%s scrollTo: MoveWindow with repaint forbidden: %s", label, trim)
					}
				}
				if !strings.Contains(body, "eraseRepaint") && !strings.Contains(body, "withSetRedraw") {
					t.Fatalf("%s scrollTo: missing eraseRepaint/withSetRedraw", label)
				}
				return
			}
			next := strings.Index(s[i+1:], "func (")
			if next < 0 {
				break
			}
			i = i + 1 + next
		}
		t.Fatalf("%s: scrollTo not found", label)
	}
	assertScrollBodyNoRepaintMove(fit, "settingsfit")
	assertScrollBodyNoRepaintMove(scroll, "winscroll")

	if eraseRedrawFlags != winapi.RDW_INVALIDATE|winapi.RDW_ERASE|winapi.RDW_ERASENOW|winapi.RDW_ALLCHILDREN|winapi.RDW_UPDATENOW {
		t.Fatalf("eraseRedrawFlags %#x", eraseRedrawFlags)
	}
	uipin := read("uipin_windows.go")
	if !bytes.Contains(uipin, []byte("WS_CLIPSIBLINGS")) {
		t.Fatal("uipin ScrollHost missing WS_CLIPSIBLINGS")
	}
	if !bytes.Contains(uipin, []byte("contentErase")) {
		t.Fatal("uipin ScrollHost missing contentErase on WM_ERASEBKGND")
	}
	win := read("window_windows.go")
	if !bytes.Contains(win, []byte("Background: 0")) {
		t.Fatal("ensureClass Background must be 0 (installer pattern)")
	}
	chrome := read("settings_chrome_windows.go")
	if bytes.Contains(chrome, []byte("CreateSolidBrush")) {
		t.Fatal("settingsChromeErase/contentErase must use persistent brushes, not CreateSolidBrush per erase")
	}
	theme := read("theme_windows.go")
	if !bytes.Contains(theme, []byte("contentCtlColor")) {
		t.Fatal("missing contentCtlColor for content STATICs")
	}
	shell := mustReadWinapi(t, "shell_windows.go")
	if !bytes.Contains(shell, []byte("MoveWindowNoRedraw")) {
		t.Fatal("winapi.MoveWindowNoRedraw missing")
	}
	if !bytes.Contains(shell, []byte("SetWindowPos")) || !bytes.Contains(shell, []byte("SWP_NOREDRAW")) {
		t.Fatal("MoveWindowNoRedraw must use SetWindowPos|SWP_NOREDRAW")
	}
	// body of MoveWindowNoRedraw must call SetWindowPos with SWP_NOREDRAW
	s := string(shell)
	i := strings.Index(s, "func MoveWindowNoRedraw")
	if i < 0 {
		t.Fatal("MoveWindowNoRedraw func missing")
	}
	end := strings.Index(s[i+1:], "\nfunc ")
	body := s[i:]
	if end >= 0 {
		body = s[i : i+1+end]
	}
	if !strings.Contains(body, "SetWindowPos") || !strings.Contains(body, "SWP_NOREDRAW") {
		t.Fatal("MoveWindowNoRedraw body must call SetWindowPos(...SWP_NOREDRAW)")
	}
	if !strings.Contains(body, "SWP_NOCOPYBITS") {
		t.Fatal("MoveWindowNoRedraw must include SWP_NOCOPYBITS (Win7 stale-bit copy)")
	}
	if eraseRedrawFlags&winapi.RDW_ERASENOW == 0 {
		t.Fatal("eraseRedrawFlags must include RDW_ERASENOW")
	}
	fit2 := read("settingsfit_windows.go")
	if !bytes.Contains(fit2, []byte("SetScrollNoRedraw")) {
		t.Fatal("settingsfit.scrollTo batch must SetScrollNoRedraw")
	}
	if !bytes.Contains(fit2, []byte("eraseRepaint(pan)")) {
		t.Fatal("settingsfit.scrollTo must eraseRepaint active pagePanel")
	}
	if !bytes.Contains(fit2, []byte("contentPaint")) {
		t.Fatal("hostProc must contentPaint on WM_PAINT")
	}
	tabs := read("settings_tabs_windows.go")
	if !bytes.Contains(tabs, []byte("contentPaint")) {
		t.Fatal("pagePanelProc must contentPaint on WM_PAINT")
	}
	if !bytes.Contains(read("settings_chrome_windows.go"), []byte("func contentPaint")) {
		t.Fatal("contentPaint helper missing")
	}
	askSrc := read("settings_windows.go")
	ai := strings.Index(string(askSrc), "func (w *settingsWindow) ask(")
	if ai < 0 {
		t.Fatal("ask() missing")
	}
	aend := strings.Index(string(askSrc)[ai+1:], "\nfunc ")
	abody := string(askSrc)[ai:]
	if aend >= 0 {
		abody = string(askSrc)[ai : ai+1+aend]
	}
	if !strings.Contains(abody, "eraseRepaint") || !strings.Contains(abody, "pagePanel") {
		t.Fatal("ask() must eraseRepaint active pagePanel after modal")
	}
}

func mustReadWinapi(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "winapi", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestCHK_TRAY_69_SettingsScrollSmear — Alex photo AC: CaptureWindow host
// baseline at y=0 → scroll mid/max/steps → return y=0 → CaptureWindow ≈
// baseline (same-pos erase idempotency alone is insufficient: two smeared
// frames can be equal). Capture fail on Windows CI = FAIL; else Skip.
// Hard-fail low pageColorShare on the returned frame.
func TestCHK_TRAY_69_SettingsScrollSmear(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _ := startChromeApp(t)
	defer app.Exit()
	defer func() { testDPI = 0 }()
	testDPI = 96
	trayui.SetTheme(trayui.ThemeLight)
	t.Cleanup(func() { trayui.SetTheme(trayui.ThemeLight) })

	w := openChromeSettings(t, app, i18n.RU, 96, false)
	defer w.close()

	assertScrollReturnBaseline := func(tag string, page int) {
		t.Helper()
		w.setPage(page)
		w.layoutFit()
		host := w.host
		if host == 0 {
			t.Fatalf("%s: no host", tag)
		}
		maxY := max32(0, w.contentH-w.viewH)
		if maxY < 8 {
			t.Skipf("%s: content not tall enough to scroll (maxY=%d)", tag, maxY)
		}
		settle := func(what string) []byte {
			t.Helper()
			// Clear EDIT caret/focus chrome so PrintWindow is stable.
			winapi.SetFocus(host)
			pumpN(10)
			winapi.DwmFlush()
			return mustCaptureWindow(t, host, what)
		}
		// Prime: both baseline and return go through product scrollTo(0)
		// eraseRepaint only (no test-side erase that could mask smear).
		w.scrollTo(0, maxY/2)
		pumpN(5)
		w.scrollTo(0, 0)
		pumpN(10)
		baseline := settle(tag + " baseline y=0")

		steps := []int32{maxY / 4, maxY / 2, (3 * maxY) / 4, maxY, maxY / 3}
		pageCol := trayui.ChromePage()
		shareFloor := pageColorShare(baseline, pageCol)
		if shareFloor > 0.15 {
			shareFloor = 0.15 // content-heavy pages still have empty bands
		}
		for i, y := range steps {
			w.scrollTo(0, y)
			pumpN(10)
			pix := mustCaptureWindow(t, host, tag+" step "+strconv.Itoa(i)+" y="+strconv.FormatInt(int64(y), 10))
			share := pageColorShare(pix, pageCol)
			// Uncovered / page band must not collapse into ghost fill after product erase.
			if share < 0.03 || (shareFloor >= 0.1 && share < shareFloor*0.4) {
				t.Fatalf("%s step %d y=%d: pageColorShare=%.3f (uncovered-band/ghost; floor~%.3f)", tag, i, y, share, shareFloor)
			}
		}

		w.scrollTo(0, 0)
		pumpN(10)
		if _, sy := w.ScrollPos(); sy != 0 {
			t.Fatalf("%s: ScrollPos Y=%d want 0 after return", tag, sy)
		}
		after := settle(tag + " return y=0")
		if len(after) != len(baseline) {
			t.Fatalf("%s: return capture size %d != baseline %d", tag, len(after), len(baseline))
		}
		diff := pixelDiffRatio(baseline, after)
		shareBase := pageColorShare(baseline, pageCol)
		shareAfter := pageColorShare(after, pageCol)
		// Catastrophic uncovered-band / ghost fill (Alex photo): page colour
		// collapses. Sparse pages (General) see ~5–10% PrintWindow jitter —
		// only fail when share halves or falls near zero.
		if shareAfter < 0.03 || (shareBase >= 0.2 && shareAfter < shareBase*0.5) {
			t.Fatalf("%s: pageColorShare after=%.3f baseline=%.3f hostDiff=%.3f (uncovered-band/ghost)", tag, shareAfter, shareBase, diff)
		}
		// Round-trip ≈ baseline; ceiling catches smear trails while allowing
		// GHA PrintWindow / focus chrome jitter seen at ~0.07–0.10.
		if diff > 0.18 {
			t.Fatalf("%s: host after scroll round-trip != baseline (diff=%.3f); smear/ghost", tag, diff)
		}
		if why := w.assertPageClear(); why != "" {
			t.Fatalf("%s pageClear: %s", tag, why)
		}
		if why := assertHostChildrenClear(w); why != "" {
			t.Fatalf("%s host: %s", tag, why)
		}
	}

	assertScrollReturnBaseline("Databases", 0)
	assertScrollReturnBaseline("General", 1)
}

// TestCHK_TRAY_70_ModalOverSettingsSmear — product ask() must eraseRepaint
// frame+host AFTER the modal returns. The test must NOT call eraseRepaint
// itself beforehand; proof is CaptureWindow immediately after ask (and
// discardOK) vs pre-modal baseline. «During» is not asserted (confirm
// stub cannot honestly see parent under a real MessageBox).
func TestCHK_TRAY_70_ModalOverSettingsSmear(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _ := startChromeApp(t)
	defer app.Exit()
	defer func() { testDPI = 0 }()
	testDPI = 96
	w := openChromeSettings(t, app, i18n.RU, 96, false)
	defer w.close()
	w.setPage(0)
	w.layoutFit()
	w.scrollTo(0, max32(0, w.contentH-w.viewH)/2)
	pumpN(10)

	baseline := mustCaptureWindow(t, w.host, "pre-modal host")

	w.cfg.confirm = func(owner uintptr, text, caption string) bool {
		if owner != w.hwnd {
			t.Errorf("confirm owner %#x want settings %#x", owner, w.hwnd)
		}
		_ = text
		_ = caption
		// Do NOT eraseRepaint here — product ask() must do it after return.
		return false // «Нет»
	}

	ok := w.ask(w.t(i18n.KeySetDiscardConfirm))
	if ok {
		t.Fatal("ask returned true (confirm stub said No)")
	}
	pumpN(5)
	// Immediate capture: product ask() already ran eraseRepaint(frame+host).
	after := mustCaptureWindow(t, w.host, "post-ask host")
	if len(after) != len(baseline) {
		t.Fatalf("post-ask capture size %d != baseline %d", len(after), len(baseline))
	}
	diff := pixelDiffRatio(baseline, after)
	if diff > 0.05 {
		t.Fatalf("host smeared after modal: vs pre-modal baseline diff=%.3f", diff)
	}

	// Source contract: ask() body must eraseRepaint frame+host (no test call).
	askSrc, err := os.ReadFile("settings_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	as := string(askSrc)
	ai := strings.Index(as, "func (w *settingsWindow) ask(")
	if ai < 0 {
		t.Fatal("ask() missing")
	}
	aend := strings.Index(as[ai+1:], "\nfunc ")
	abody := as[ai:]
	if aend >= 0 {
		abody = as[ai : ai+1+aend]
	}
	if !strings.Contains(abody, "eraseRepaint(w.hwnd)") || !strings.Contains(abody, "eraseRepaint(w.host)") {
		t.Fatal("ask() must eraseRepaint(frame+host) after confirm returns")
	}
	if !strings.Contains(abody, "pagePanel") || !strings.Contains(abody, "eraseRepaint(pan)") {
		t.Fatal("ask() must eraseRepaint active pagePanel after confirm returns")
	}

	// discardOK path — still no eraseRepaint in the confirm stub.
	w.set(idSetName, "smear-edit")
	w.readForm()
	if !w.ed.Unsaved() {
		t.Fatal("expected unsaved edit before discardOK")
	}
	calls := 0
	w.cfg.confirm = func(owner uintptr, text, caption string) bool {
		calls++
		_ = owner
		_ = text
		_ = caption
		return false
	}
	if w.discardOK() {
		t.Fatal("discardOK should be false when confirm says No")
	}
	if calls != 1 {
		t.Fatalf("confirm calls=%d want 1", calls)
	}
	pumpN(5)
	after2 := mustCaptureWindow(t, w.host, "post-discardOK host")
	if len(after2) != len(baseline) {
		t.Fatalf("post-discardOK capture size %d != baseline %d", len(after2), len(baseline))
	}
	diff2 := pixelDiffRatio(baseline, after2)
	if diff2 > 0.05 {
		t.Fatalf("host smeared after discardOK: vs baseline diff=%.3f", diff2)
	}
	if why := w.assertPageClear(); why != "" {
		t.Fatalf("pageClear after modal: %s", why)
	}
}

func pixelDiffRatio(a, b []byte) float64 {
	if len(a) != len(b) || len(a) < 4 {
		return 1
	}
	diff := 0
	n := len(a) / 4
	for i := 0; i+3 < len(a); i += 4 {
		if a[i] != b[i] || a[i+1] != b[i+1] || a[i+2] != b[i+2] {
			diff++
		}
	}
	return float64(diff) / float64(n)
}

func pageColorShare(bgra []byte, page uint32) float64 {
	if len(bgra) < 4 {
		return 0
	}
	pr := byte(page & 0xFF)
	pg := byte((page >> 8) & 0xFF)
	pb := byte((page >> 16) & 0xFF)
	match, n := 0, len(bgra)/4
	for i := 0; i+3 < len(bgra); i += 4 {
		// BGRA
		if bgra[i] == pb && bgra[i+1] == pg && bgra[i+2] == pr {
			match++
		}
	}
	return float64(match) / float64(n)
}


// mustCaptureWindow CaptureWindow's hwnd or hard-fails on Windows CI /
// explicitly Skips when capture is unavailable locally.
func mustCaptureWindow(t *testing.T, hwnd uintptr, what string) []byte {
	t.Helper()
	cw, ch, pix, ok := winapi.CaptureWindow(hwnd)
	if !ok || cw <= 0 || ch <= 0 || len(pix) == 0 {
		if os.Getenv("GITHUB_ACTIONS") != "" {
			t.Fatalf("%s: CaptureWindow failed on Windows CI (cw=%d ch=%d ok=%v)", what, cw, ch, ok)
		}
		t.Skipf("%s: CaptureWindow unavailable (not CI)", what)
	}
	return pix
}


// pumpN drains up to n pending UI messages.
func pumpN(n int) {
	var msg winapi.MSG
	for i := 0; i < n; i++ {
		if !winapi.PeekMessage(&msg) {
			break
		}
		winapi.TranslateMessage(&msg)
		winapi.DispatchMessage(&msg)
	}
}


// TestCHK_TRAY_71_PageHostEraseContract — Conchita root-cause contract for the
// shared settings page host (ALL tabs): NOCOPYBITS, SetScrollNoRedraw in batch,
// contentPaint WM_PAINT on host+pagePanel, eraseRepaint(host+active panel),
// RDW_ERASENOW, ask() erases panel. Source contract; pixel proof is 72/73.
func TestCHK_TRAY_71_PageHostEraseContract(t *testing.T) {
	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	shell := string(mustReadWinapi(t, "shell_windows.go"))
	i := strings.Index(shell, "func MoveWindowNoRedraw")
	if i < 0 {
		t.Fatal("MoveWindowNoRedraw missing")
	}
	end := strings.Index(shell[i+1:], "\nfunc ")
	body := shell[i:]
	if end >= 0 {
		body = shell[i : i+1+end]
	}
	for _, need := range []string{"SWP_NOREDRAW", "SWP_NOCOPYBITS", "SetWindowPos"} {
		if !strings.Contains(body, need) {
			t.Fatalf("MoveWindowNoRedraw missing %s", need)
		}
	}
	if eraseRedrawFlags != winapi.RDW_INVALIDATE|winapi.RDW_ERASE|winapi.RDW_ERASENOW|winapi.RDW_ALLCHILDREN|winapi.RDW_UPDATENOW {
		t.Fatalf("eraseRedrawFlags %#x want INVALIDATE|ERASE|ERASENOW|ALLCHILDREN|UPDATENOW", eraseRedrawFlags)
	}
	dpi := string(mustReadWinapi(t, "dpi_windows.go"))
	if !strings.Contains(dpi, "func SetScrollNoRedraw") {
		t.Fatal("SetScrollNoRedraw missing")
	}
	fit := string(read("settingsfit_windows.go"))
	// scrollTo must NoRedraw SetScroll inside batch and eraseRepaint panel
	si := strings.Index(fit, "func (w *settingsWindow) scrollTo(")
	if si < 0 {
		t.Fatal("scrollTo missing")
	}
	send := strings.Index(fit[si+1:], "\nfunc ")
	sbody := fit[si:]
	if send >= 0 {
		sbody = fit[si : si+1+send]
	}
	if !strings.Contains(sbody, "SetScrollNoRedraw") {
		t.Fatal("scrollTo batch must SetScrollNoRedraw")
	}
	if !strings.Contains(sbody, "MoveWindowNoRedraw") {
		t.Fatal("scrollTo must MoveWindowNoRedraw")
	}
	if !strings.Contains(sbody, "eraseRepaint(pan)") {
		t.Fatal("scrollTo must eraseRepaint(pan) active pagePanel")
	}
	if !strings.Contains(fit, "case winapi.WM_PAINT:") || !strings.Contains(fit, "contentPaint(hwnd)") {
		t.Fatal("hostProc must WM_PAINT → contentPaint")
	}
	tabs := string(read("settings_tabs_windows.go"))
	if !strings.Contains(tabs, "case winapi.WM_PAINT:") || !strings.Contains(tabs, "contentPaint(hwnd)") {
		t.Fatal("pagePanelProc must WM_PAINT → contentPaint")
	}
	scroll := string(read("winscroll_windows.go"))
	if !strings.Contains(scroll, "barsNoRedraw") || !strings.Contains(scroll, "SetScrollNoRedraw") {
		t.Fatal("winscroll.scrollTo must SetScrollNoRedraw in batch")
	}
	ask := string(read("settings_windows.go"))
	ai := strings.Index(ask, "func (w *settingsWindow) ask(")
	if ai < 0 {
		t.Fatal("ask missing")
	}
	ae := strings.Index(ask[ai+1:], "\nfunc ")
	abody := ask[ai:]
	if ae >= 0 {
		abody = ask[ai : ai+1+ae]
	}
	if !strings.Contains(abody, "eraseRepaint(w.hwnd)") || !strings.Contains(abody, "eraseRepaint(w.host)") {
		t.Fatal("ask must eraseRepaint frame+host")
	}
	if !strings.Contains(abody, "pagePanel") || !strings.Contains(abody, "eraseRepaint(pan)") {
		t.Fatal("ask must eraseRepaint active pagePanel")
	}
	if !bytes.Contains(read("settings_chrome_windows.go"), []byte("func contentPaint")) {
		t.Fatal("contentPaint missing")
	}
	// relayoutAll / syncPagePanels must use eraseRepaint (ERASENOW), not a
	// RedrawWindow flag set that omits RDW_ERASENOW.
	if !strings.Contains(fit, "eraseRepaint(w.host)") {
		t.Fatal("relayoutAll must eraseRepaint(w.host) with eraseRedrawFlags/ERASENOW")
	}
	if strings.Count(fit, "eraseRepaint(pan)") < 1 {
		t.Fatal("settingsfit must eraseRepaint(pan) (scrollTo and/or syncPagePanels)")
	}
	for _, line := range strings.Split(fit, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "//") {
			continue
		}
		if strings.Contains(trim, "RedrawWindow(") && strings.Contains(trim, "RDW_ERASE") && !strings.Contains(trim, "RDW_ERASENOW") && !strings.Contains(trim, "eraseRedrawFlags") {
			t.Fatalf("settingsfit RedrawWindow erase path missing ERASENOW: %s", trim)
		}
	}
}


// bringSettingsOnScreen places the settings frame on-screen topmost so
// CaptureScreen (BitBlt) sees real pixels — PrintWindow alone masked #15.
func bringSettingsOnScreen(t *testing.T, hwnd uintptr) func() {
	t.Helper()
	work := workArea()
	winapi.SetWindowPos(hwnd, winapi.HWND_TOPMOST, work.Left, work.Top, 0, 0,
		winapi.SWP_NOSIZE|winapi.SWP_SHOWWINDOW)
	winapi.ShowWindow(hwnd, winapi.SW_SHOW)
	pumpN(8)
	winapi.DwmFlush()
	return func() {
		winapi.SetWindowPos(hwnd, hwndNoTopmost, 0, 0, 0, 0,
			winapi.SWP_NOSIZE|winapi.SWP_NOMOVE|winapi.SWP_NOACTIVATE)
	}
}

func mustCaptureScreen(t *testing.T, hwnd uintptr, what string) []byte {
	t.Helper()
	cw, ch, pix, ok := winapi.CaptureScreen(hwnd)
	if !ok || cw <= 0 || ch <= 0 || len(pix) == 0 {
		if os.Getenv("GITHUB_ACTIONS") != "" {
			t.Fatalf("%s: CaptureScreen failed on Windows CI (cw=%d ch=%d ok=%v)", what, cw, ch, ok)
		}
		t.Skipf("%s: CaptureScreen unavailable (not CI)", what)
	}
	return pix
}

// TestCHK_TRAY_72_AllTabsScrollScreenSmear — scroll every settings tab
// (Databases/General/Viber/Email — all 4 hard); proof is CaptureScreen BitBlt,
// NOT only PrintWindow. FAIL when PrintWindow looks clean but screen is dirty
// (the #15 CI blind spot on live Win7). Tight thresholds catch ~1–3% button
// trails (Обзор/Прослушать); size mismatch is FAIL not silent skip.
func TestCHK_TRAY_72_AllTabsScrollScreenSmear(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _ := startChromeApp(t)
	defer app.Exit()
	defer func() { testDPI = 0 }()
	testDPI = 96
	trayui.SetTheme(trayui.ThemeLight)
	t.Cleanup(func() { trayui.SetTheme(trayui.ThemeLight) })

	w := openChromeSettings(t, app, i18n.RU, 96, false)
	defer w.close()
	undo := bringSettingsOnScreen(t, w.hwnd)
	defer undo()

	pages := chromePages(w)
	// CapEmail forced by openChromeSettings — all 4 tabs hard (no ≥3 soft-out).
	if len(pages) != 4 {
		t.Fatalf("need exactly 4 tabs (Databases/General/Viber/Email), got %v", pages)
	}
	pageCol := trayui.ChromePage()

	for _, page := range pages {
		tag := "page" + strconv.Itoa(page)
		w.setPage(page)
		w.layoutFit()
		pumpN(8)
		host := w.host
		if host == 0 {
			t.Fatalf("%s: no host", tag)
		}
		maxY := max32(0, w.contentH-w.viewH)
		if maxY < 8 {
			// Explicit fail — soft-continuing at y=0 alone does not prove scroll smear
			// (button trails under Обзор/Прослушать and analogs).
			t.Fatalf("%s: content not tall enough to scroll (maxY=%d); cannot prove smear", tag, maxY)
		}
		winapi.SetFocus(host)
		pumpN(5)
		w.scrollTo(0, 0)
		pumpN(8)
		winapi.DwmFlush()
		baseScreen := mustCaptureScreen(t, host, tag+" screen baseline")

		for _, y := range []int32{maxY / 4, maxY / 2, maxY} {
			w.scrollTo(0, y)
			pumpN(10)
			winapi.DwmFlush()
			scr := mustCaptureScreen(t, host, tag+" screen y="+strconv.Itoa(int(y)))
			share := pageColorShare(scr, pageCol)
			// 0.02 was too soft — ghost bands under buttons (~1–3%) still pass.
			if share < 0.05 {
				t.Fatalf("%s y=%d: CaptureScreen pageColorShare=%.3f (ghost/uncovered band under controls)", tag, y, share)
			}
			// PrintWindow vs screen: if PW is "clean" (high page share) but
			// screen collapsed, #15-style false green — hard fail.
			cw, ch, pwPix, pwOK := winapi.CaptureWindow(host)
			if pwOK && cw > 0 && ch > 0 && len(pwPix) == len(scr) {
				pwShare := pageColorShare(pwPix, pageCol)
				scrShare := share
				if pwShare >= 0.10 && scrShare < pwShare*0.55 {
					t.Fatalf("%s y=%d: PrintWindow clean (share=%.3f) but CaptureScreen dirty (%.3f) — PW≠screen smear", tag, y, pwShare, scrShare)
				}
			}
		}
		w.scrollTo(0, 0)
		pumpN(10)
		winapi.DwmFlush()
		after := mustCaptureScreen(t, host, tag+" screen return")
		if len(after) != len(baseScreen) {
			t.Fatalf("%s: return CaptureScreen size %d != baseline %d", tag, len(after), len(baseScreen))
		}
		diff := pixelDiffRatio(baseScreen, after)
		shareBase := pageColorShare(baseScreen, pageCol)
		shareAfter := pageColorShare(after, pageCol)
		if shareAfter < 0.05 || (shareBase >= 0.10 && shareAfter < shareBase*0.70) {
			t.Fatalf("%s return: screen pageColorShare after=%.3f base=%.3f (smear)", tag, shareAfter, shareBase)
		}
		// Catch button trails (~1–3% pixels). Old ceiling 0.20–0.25 let them pass.
		if diff > 0.01 {
			t.Fatalf("%s return: CaptureScreen round-trip diff=%.3f (smear/ghost; incl. button trails)", tag, diff)
		}
		if why := w.assertPageClear(); why != "" {
			t.Fatalf("%s pageClear: %s", tag, why)
		}
	}
}

// TestCHK_TRAY_73_UncoverScreenRepaint — foreign TOPMOST window covers the
// settings host then is DestroyWindow'd (primary uncover proof); product must
// repaint via WM_PAINT/erase without the test calling eraseRepaint.
// InvalidateRect(erase) is secondary only — not a substitute for DestroyWindow.
// CaptureScreen on all 4 tabs; size mismatch FAIL; tight diff catches button trails.
func TestCHK_TRAY_73_UncoverScreenRepaint(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _ := startChromeApp(t)
	defer app.Exit()
	defer func() { testDPI = 0 }()
	testDPI = 96
	w := openChromeSettings(t, app, i18n.RU, 96, false)
	defer w.close()
	undo := bringSettingsOnScreen(t, w.hwnd)
	defer undo()

	cover := func(over uintptr) {
		t.Helper()
		hr := winapi.GetWindowRect(w.host)
		winapi.SetWindowPos(over, winapi.HWND_TOPMOST, hr.Left, hr.Top, hr.Right-hr.Left, hr.Bottom-hr.Top,
			winapi.SWP_SHOWWINDOW)
		winapi.ShowWindow(over, winapi.SW_SHOW)
		pumpN(6)
		winapi.DwmFlush()
	}

	pages := chromePages(w)
	if len(pages) != 4 {
		t.Fatalf("need exactly 4 tabs for uncover proof, got %v", pages)
	}
	pageCol := trayui.ChromePage()
	for _, page := range pages {
		tag := "uncover-page" + strconv.Itoa(page)
		w.setPage(page)
		w.layoutFit()
		w.scrollTo(0, max32(0, w.contentH-w.viewH)/3)
		pumpN(8)
		winapi.DwmFlush()
		baseline := mustCaptureScreen(t, w.host, tag+" pre")

		// Primary uncover proof: DestroyWindow foreign TOPMOST overlay.
		// Do NOT treat InvalidateRect(erase) alone as sufficient «paint» proof
		// (#15 CI green / live smear). Test must not call eraseRepaint.
		over, err := winapi.CreateWindowEx(winapi.WS_EX_TOPMOST, "STATIC", "cover",
			winapi.WS_POPUP|winapi.WS_VISIBLE, 0, 0, 40, 40, 0, 0, winapi.GetModuleHandle())
		if err != nil || over == 0 {
			t.Fatalf("%s: overlay create: %v", tag, err)
		}
		cover(over)
		winapi.DestroyWindow(over)
		pumpN(12)
		winapi.DwmFlush()
		after := mustCaptureScreen(t, w.host, tag+" post-destroy")
		if len(after) != len(baseline) {
			t.Fatalf("%s: post-destroy CaptureScreen size %d != baseline %d", tag, len(after), len(baseline))
		}
		diff := pixelDiffRatio(baseline, after)
		shareAfter := pageColorShare(after, pageCol)
		shareBase := pageColorShare(baseline, pageCol)
		if shareAfter < 0.05 || (shareBase >= 0.10 && shareAfter < shareBase*0.70) {
			t.Fatalf("%s: post-destroy pageColorShare after=%.3f base=%.3f (ghost after DestroyWindow uncover)", tag, shareAfter, shareBase)
		}
		// Catch button-trail ghosts (~1–3%); old 0.20 ceiling missed them.
		if diff > 0.01 {
			t.Fatalf("%s: CaptureScreen after foreign DestroyWindow uncover diff=%.3f (ghost; test did not eraseRepaint)", tag, diff)
		}

		// Secondary: InvalidateRect(erase) must not make the frame worse —
		// still not a substitute for DestroyWindow uncover proof above.
		base2 := mustCaptureScreen(t, w.host, tag+" pre-inv")
		winapi.InvalidateRect(w.host, true)
		winapi.InvalidateRect(w.pagePanel[page], true)
		pumpN(12)
		winapi.DwmFlush()
		after2 := mustCaptureScreen(t, w.host, tag+" post-inv")
		if len(after2) != len(base2) {
			t.Fatalf("%s: post-inv CaptureScreen size %d != baseline %d", tag, len(after2), len(base2))
		}
		diff2 := pixelDiffRatio(base2, after2)
		share := pageColorShare(after2, pageCol)
		if share < 0.05 {
			t.Fatalf("%s: after InvalidateRect screen page share=%.3f", tag, share)
		}
		if diff2 > 0.05 {
			t.Fatalf("%s: after InvalidateRect screen diff=%.3f", tag, diff2)
		}

		// PW vs screen divergence check at rest
		_, _, pwPix, pwOK := winapi.CaptureWindow(w.host)
		scr := mustCaptureScreen(t, w.host, tag+" pw-vs-scr")
		if pwOK && len(pwPix) == len(scr) {
			pwShare := pageColorShare(pwPix, pageCol)
			scrShare := pageColorShare(scr, pageCol)
			if pwShare >= 0.10 && scrShare < pwShare*0.55 {
				t.Fatalf("%s: PrintWindow clean (%.3f) screen dirty (%.3f)", tag, pwShare, scrShare)
			}
		}
		if why := w.assertPageClear(); why != "" {
			t.Fatalf("%s pageClear: %s", tag, why)
		}
	}
}
