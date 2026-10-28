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

	if eraseRedrawFlags != winapi.RDW_INVALIDATE|winapi.RDW_ERASE|winapi.RDW_ALLCHILDREN|winapi.RDW_UPDATENOW {
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
		pan := w.pagePanel[page]
		if host == 0 {
			t.Fatalf("%s: no host", tag)
		}
		maxY := max32(0, w.contentH-w.viewH)
		if maxY < 8 {
			t.Skipf("%s: content not tall enough to scroll (maxY=%d)", tag, maxY)
		}
		// Prime scroll + erase so baseline and return captures share the same
		// paint path (fresh layoutFit y=0 alone skipped eraseRepaint / SB paint).
		w.scrollTo(0, maxY/2)
		pumpN(5)
		w.scrollTo(0, 0)
		eraseRepaint(host)
		if pan != 0 {
			eraseRepaint(pan)
		}
		pumpN(15)
		winapi.DwmFlush()
		baseHost := mustCaptureWindow(t, host, tag+" baseline host y=0")
		var basePan []byte
		if pan != 0 && winapi.IsWindowVisible(pan) {
			basePan = mustCaptureWindow(t, pan, tag+" baseline pan y=0")
		}

		steps := []int32{maxY / 4, maxY / 2, (3 * maxY) / 4, maxY, maxY / 3}
		for i, y := range steps {
			w.scrollTo(0, y)
			pumpN(10)
			// mid-scroll capture must succeed on CI (proves path ran); smear
			// proof is return-to-zero vs baseline below.
			_ = mustCaptureWindow(t, host, tag+" step "+strconv.Itoa(i)+" y="+strconv.FormatInt(int64(y), 10))
		}

		w.scrollTo(0, 0) // product eraseRepaint via withSetRedraw — do NOT re-erase in test
		pumpN(15)
		winapi.DwmFlush()
		afterHost := mustCaptureWindow(t, host, tag+" return host y=0")
		if len(afterHost) != len(baseHost) {
			t.Fatalf("%s: return host size %d != baseline %d", tag, len(afterHost), len(baseHost))
		}
		// Primary Alex AC: pagePanel (content) round-trip — host includes SB
		// chrome that can differ by a few % without content smear.
		if len(basePan) > 0 {
			afterPan := mustCaptureWindow(t, pan, tag+" return pan y=0")
			if len(afterPan) != len(basePan) {
				t.Fatalf("%s: return pan size %d != baseline %d", tag, len(afterPan), len(basePan))
			}
			diffPan := pixelDiffRatio(basePan, afterPan)
			if diffPan > 0.02 {
				t.Fatalf("%s: pagePanel after scroll round-trip != baseline (diff=%.3f); smear/ghost", tag, diffPan)
			}
		}
		diffHost := pixelDiffRatio(baseHost, afterHost)
		pageCol := trayui.ChromePage()
		shareBase := pageColorShare(baseHost, pageCol)
		shareAfter := pageColorShare(afterHost, pageCol)
		// Uncovered host band is page-coloured; collapse vs baseline = ghost fill.
		if shareAfter+0.05 < shareBase || shareAfter < 0.05 {
			t.Fatalf("%s: pageColorShare after=%.3f baseline=%.3f (uncovered-band/ghost) hostDiff=%.3f", tag, shareAfter, shareBase, diffHost)
		}
		// Host still compared, but allow SB/focus chrome jitter; content AC above.
		if diffHost > 0.12 {
			t.Fatalf("%s: host after scroll round-trip != baseline (diff=%.3f); smear/ghost", tag, diffHost)
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
	if len(after2) == len(baseline) {
		diff2 := pixelDiffRatio(baseline, after2)
		if diff2 > 0.05 {
			t.Fatalf("host smeared after discardOK: vs baseline diff=%.3f", diff2)
		}
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
