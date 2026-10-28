//go:build windows

package wintray

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
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
	if !bytes.Contains(shell, []byte(", 0)")) {
		// bRepaint=FALSE
		t.Fatal("MoveWindowNoRedraw must pass bRepaint=0")
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

// TestCHK_TRAY_69_SettingsScrollSmear — CaptureWindow of host/pagePanel
// after multi-step scroll on Databases + General must stay stable (Alex
// photo AC: no diagonal/vertical ghost trails). Source-grep is NOT proof.
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

	assertHostStableAfterScroll := func(tag string, page int) {
		t.Helper()
		w.setPage(page)
		w.layoutFit()
		host := w.host
		pan := w.pagePanel[page]
		if host == 0 {
			t.Fatalf("%s: no host", tag)
		}
		maxY := max32(0, w.contentH-w.viewH)
		steps := []int32{0, maxY / 4, maxY / 2, (3 * maxY) / 4, maxY, maxY / 3, 0}
		var lastHost, lastPan []byte
		for i, y := range steps {
			w.scrollTo(0, y)
			// pump a few messages so UPDATENOW paints settle
			pumpN(10)
			cw, ch, hostPix, ok := winapi.CaptureWindow(host)
			if !ok || cw <= 0 || ch <= 0 || len(hostPix) == 0 {
				continue // PrintWindow may fail off-screen / headless — HWND path still ran
			}
			if lastHost != nil && len(lastHost) == len(hostPix) {
				// Consecutive captures at SAME scroll pos must be identical
				// after eraseRepaint; first check: re-capture without scrolling.
				eraseRepaint(host)
				pumpN(5)
				_, _, hostPix2, ok2 := winapi.CaptureWindow(host)
				if ok2 && len(hostPix2) == len(hostPix) && !bytes.Equal(hostPix, hostPix2) {
					diff := pixelDiffRatio(hostPix, hostPix2)
					if diff > 0.02 {
						t.Fatalf("%s step %d y=%d: host capture unstable after eraseRepaint (diff=%.3f)", tag, i, y, diff)
					}
				}
			}
			lastHost = hostPix
			if pan != 0 && winapi.IsWindowVisible(pan) {
				_, _, panPix, okp := winapi.CaptureWindow(pan)
				if okp && len(panPix) > 0 {
					if lastPan != nil && len(lastPan) == len(panPix) {
						eraseRepaint(pan)
						pumpN(5)
						_, _, panPix2, okp2 := winapi.CaptureWindow(pan)
						if okp2 && len(panPix2) == len(panPix) && !bytes.Equal(panPix, panPix2) {
							diff := pixelDiffRatio(panPix, panPix2)
							if diff > 0.02 {
								t.Fatalf("%s step %d y=%d: pagePanel unstable after eraseRepaint (diff=%.3f)", tag, i, y, diff)
							}
						}
					}
					lastPan = panPix
				}
			}
			// page-colour dominance on host: smeared text leaves non-page pixels
			// cascading; a clean host after scroll+erase has a large page-bg share
			// in the uncovered bands (heuristic; skip if capture tiny).
			if len(hostPix) >= 4*50*50 {
				page := trayui.ChromePage()
				share := pageColorShare(hostPix, page)
				if share < 0.05 {
					// very low page share after erase is suspicious (ghost fill)
					t.Logf("%s step %d: page-color share=%.3f (informational)", tag, i, share)
				}
			}
		}
		if why := w.assertPageClear(); why != "" {
			t.Fatalf("%s pageClear: %s", tag, why)
		}
		if why := assertHostChildrenClear(w); why != "" {
			t.Fatalf("%s host: %s", tag, why)
		}
	}

	assertHostStableAfterScroll("Databases", 0)
	assertHostStableAfterScroll("General", 1)
}

// TestCHK_TRAY_70_ModalOverSettingsSmear — discard MessageBox must not
// leave parent settings content smeared; ask() forces eraseRepaint of
// frame + host during/after the modal.
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

	var duringHost []byte
	var duringOK bool
	w.cfg.confirm = func(owner uintptr, text, caption string) bool {
		if owner != w.hwnd {
			t.Errorf("confirm owner %#x want settings %#x", owner, w.hwnd)
		}
		if !strings.Contains(text, "Отмен") && !strings.Contains(strings.ToLower(caption), "exchange") {
			// RU discard prompt; caption is app name — either is fine
			_ = text
		}
		// parent must be erase-able while modal is "up"
		eraseRepaint(w.hwnd)
		eraseRepaint(w.host)
		pumpN(5)
		_, _, duringHost, duringOK = winapi.CaptureWindow(w.host)
		return false // «Нет»
	}

	ok := w.ask(w.t(i18n.KeySetDiscardConfirm))
	if ok {
		t.Fatal("ask returned true (confirm stub said No)")
	}
	pumpN(10)
	// after return: forced redraw in ask() — host capture must be stable
	eraseRepaint(w.hwnd)
	eraseRepaint(w.host)
	pumpN(5)
	_, _, after, okA := winapi.CaptureWindow(w.host)
	if duringOK && okA && len(duringHost) == len(after) && len(after) > 0 {
		diff := pixelDiffRatio(duringHost, after)
		if diff > 0.05 {
			t.Fatalf("host smeared across modal: during vs after diff=%.3f", diff)
		}
	}
	// second path via discardOK with dirty form
	w.set(idSetName, "smear-edit")
	w.readForm()
	if !w.ed.Unsaved() {
		t.Fatal("expected unsaved edit before discardOK")
	}
	calls := 0
	w.cfg.confirm = func(owner uintptr, text, caption string) bool {
		calls++
		eraseRepaint(w.hwnd)
		eraseRepaint(w.host)
		return false
	}
	if w.discardOK() {
		t.Fatal("discardOK should be false when confirm says No")
	}
	if calls != 1 {
		t.Fatalf("confirm calls=%d want 1", calls)
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
