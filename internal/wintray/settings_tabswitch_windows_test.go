//go:build windows

package wintray

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// paintSpy subclasses windows (test-only) and counts the messages that
// reach their real window procedures.
type paintSpy struct {
	old   map[uintptr]uintptr
	count map[uintptr]map[uint32]int
	cb    uintptr
}

var (
	spyUser32      = syscall.NewLazyDLL("user32.dll")
	spyCallWndProc = spyUser32.NewProc("CallWindowProcW")
	spySetLong     = spyUser32.NewProc(spySetLongName())
	activeSpy      *paintSpy
)

func spySetLongName() string {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		return "SetWindowLongPtrW"
	}
	return "SetWindowLongW"
}

const gwlpWndProc = ^uintptr(3) // GWLP_WNDPROC = -4

func newPaintSpy() *paintSpy {
	s := &paintSpy{old: map[uintptr]uintptr{}, count: map[uintptr]map[uint32]int{}}
	s.cb = syscall.NewCallback(func(hwnd, msg, wp, lp uintptr) uintptr {
		sp := activeSpy
		if sp == nil {
			return 0
		}
		if m := sp.count[hwnd]; m != nil {
			m[uint32(msg)]++
		}
		r, _, _ := spyCallWndProc.Call(sp.old[hwnd], hwnd, msg, wp, lp)
		return r
	})
	activeSpy = s
	return s
}

func (s *paintSpy) watch(hwnd uintptr) {
	if hwnd == 0 || s.old[hwnd] != 0 {
		return
	}
	s.count[hwnd] = map[uint32]int{}
	old, _, _ := spySetLong.Call(hwnd, gwlpWndProc, s.cb)
	s.old[hwnd] = old
}

func (s *paintSpy) reset() {
	for h := range s.count {
		s.count[h] = map[uint32]int{}
	}
}

func (s *paintSpy) n(hwnd uintptr, msg uint32) int { return s.count[hwnd][msg] }

func (s *paintSpy) release() {
	for h, old := range s.old {
		if winapi.IsWindow(h) {
			spySetLong.Call(h, gwlpWndProc, old)
		}
	}
	activeSpy = nil
}

// TestCHK_TRAY_107_TabSwitchSingleRepaint — phase 16 F (Кончита
// 16.11.2026, flicker on a tab switch): every switch through the real
// setPage of a shown settings window suspends the frame's painting
// (WM_SETREDRAW FALSE … TRUE, exactly one pair) and repaints the frame, the
// host and the shown page panel exactly ONCE (one WM_ERASEBKGND each,
// nothing else after the messages settle); no hidden page panel is painted;
// the pages are not WS_CLIPCHILDREN (CHK-TRAY-71). A switch of a hidden
// window does not set WS_VISIBLE on it.
func TestCHK_TRAY_107_TabSwitchSingleRepaint(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _ := startChromeApp(t)
	defer app.Exit()
	defer func() { testDPI = 0 }()
	trayui.SetTheme(trayui.ThemeLight)
	w := openChromeSettings(t, app, i18n.RU, 96, false)
	defer w.close()
	pumpN(10)
	if !winapi.IsWindowVisible(w.hwnd) {
		t.Fatal("settings window not shown")
	}
	spy := newPaintSpy()
	defer spy.release()
	spy.watch(w.hwnd)
	spy.watch(w.host)
	for _, pan := range w.pagePanel {
		spy.watch(pan)
	}
	pages := chromePages(w)
	order := append(append([]int{}, pages[1:]...), pages[0])
	order = append(order, pages[1], pages[0])
	for _, p := range order {
		pumpN(10)
		spy.reset()
		w.setPage(p)
		if w.page != p {
			t.Fatalf("page %d not shown", p)
		}
		sync := map[string]int{
			"frame erase": spy.n(w.hwnd, winapi.WM_ERASEBKGND),
			"host erase":  spy.n(w.host, winapi.WM_ERASEBKGND),
			"setredraw":   spy.n(w.hwnd, winapi.WM_SETREDRAW),
		}
		pan := w.pagePanel[p]
		if pan == 0 {
			t.Fatalf("page %d has no panel", p)
		}
		sync["panel erase"] = spy.n(pan, winapi.WM_ERASEBKGND)
		pumpN(10)
		// the shown panel exactly once; frame and host at most once (an
		// update region fully covered by WS_CLIPCHILDREN children is empty)
		if sync["setredraw"] != 2 || sync["panel erase"] != 1 || sync["frame erase"] > 1 || sync["host erase"] > 1 {
			t.Errorf("page %d during setPage: %v (want setredraw 2, panel erase 1, frame/host erase ≤1)", p, sync)
		}
		for h, what := range map[uintptr]string{w.hwnd: "frame", w.host: "host", pan: "panel"} {
			if h == 0 {
				continue
			}
			want := 1
			if h != pan {
				want = sync[what+" erase"]
			}
			if e, pt := spy.n(h, winapi.WM_ERASEBKGND), spy.n(h, winapi.WM_PAINT); e != want || pt > 1 {
				t.Errorf("page %d: %s erased ×%d painted ×%d per switch (want %d / ≤1)", p, what, e, pt, want)
			}
		}
		for i, other := range w.pagePanel {
			if i == p || other == 0 {
				continue
			}
			if e, pt := spy.n(other, winapi.WM_ERASEBKGND), spy.n(other, winapi.WM_PAINT); e+pt != 0 {
				t.Errorf("page %d: hidden panel %d painted (erase %d paint %d)", p, i, e, pt)
			}
			if winapi.IsWindowVisible(other) {
				t.Errorf("page %d: panel %d visible", p, i)
			}
		}
		if pan != 0 && winapi.WindowStyle(pan)&winapi.WS_CLIPCHILDREN != 0 {
			t.Errorf("page %d: panel has WS_CLIPCHILDREN", p)
		}
		if why := w.assertPageClear(); why != "" {
			t.Errorf("page %d: %s", p, why)
		}
	}
	// a hidden window stays hidden through a switch
	winapi.ShowWindow(w.hwnd, winapi.SW_HIDE)
	pumpN(3)
	w.setPage(pages[1])
	if winapi.IsWindowVisible(w.hwnd) || w.page != pages[1] {
		t.Error("setPage on a hidden window made it visible")
	}
}
