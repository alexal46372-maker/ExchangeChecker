//go:build windows

package wintray

import (
	"sync"
	"syscall"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// msgHandler handles messages of one window; handled == false falls back
// to DefWindowProc.
type msgHandler func(hwnd uintptr, msg uint32, wp, lp uintptr) (result uintptr, handled bool)

var (
	// one callback for every window class (syscall.NewCallback slots are
	// limited and never freed)
	wndProcCallback = syscall.NewCallback(wndProc)

	hmu      sync.Mutex
	handlers = map[uintptr]msgHandler{}
	classes  = map[string]bool{}
)

// msgDeferDestroy destroys the window that receives it. A window closed
// from a control's WM_COMMAND (a button still inside BM_CLICK) is hidden
// and destroyed after the click returns: destroying it under the button
// is an access violation in user32 (CI #309, #316).
const msgDeferDestroy = winapi.WM_APP + 10

var ctlDepth int // nesting of control WM_COMMAND on the UI thread

// destroyWindow destroys hwnd now, or after the current control command.
func destroyWindow(hwnd uintptr) {
	if ctlDepth > 0 && winapi.IsWindow(hwnd) {
		winapi.ShowWindow(hwnd, winapi.SW_HIDE)
		winapi.PostMessage(hwnd, msgDeferDestroy, 0, 0)
		return
	}
	winapi.DestroyWindow(hwnd)
}

func wndProc(hwnd, msg, wp, lp uintptr) uintptr {
	hmu.Lock()
	h := handlers[hwnd]
	sc := scrollers[hwnd]
	hmu.Unlock()
	if sc != nil && sc.handle(uint32(msg), wp, lp) {
		return 0
	}
	if uint32(msg) == msgDeferDestroy {
		winapi.DestroyWindow(hwnd)
		return 0
	}
	if h != nil {
		ctl := uint32(msg) == winapi.WM_COMMAND && lp != 0
		if ctl {
			ctlDepth++
		}
		r, ok := h(hwnd, uint32(msg), wp, lp)
		if ctl {
			ctlDepth--
		}
		if ok {
			return r
		}
	}
	// phase 12 (CHK-TRAY-55): client chrome when the window did not handle it
	if uint32(msg) == winapi.WM_ERASEBKGND {
		return themeErase(wp, hwnd)
	}
	if r, ok := themeCtlColor(uint32(msg), wp, lp); ok {
		return r
	}
	if uint32(msg) == winapi.WM_DESTROY {
		hmu.Lock()
		delete(handlers, hwnd)
		delete(scrollers, hwnd)
		hmu.Unlock()
		delete(hostExtent, hwnd)
	}
	return winapi.DefWindowProc(hwnd, uint32(msg), wp, lp)
}

// ensureClass registers a window class once.
func ensureClass(name string) error {
	hmu.Lock()
	defer hmu.Unlock()
	if classes[name] {
		return nil
	}
	wc := winapi.WNDCLASSEXW{
		WndProc:    wndProcCallback,
		Instance:   winapi.GetModuleHandle(),
		Cursor:     winapi.LoadCursor(winapi.IDC_ARROW),
		Background: 16, // COLOR_BTNFACE + 1
		ClassName:  winapi.UTF16Ptr(name),
	}
	if _, err := winapi.RegisterClassEx(&wc); err != nil {
		return err
	}
	classes[name] = true
	return nil
}

// createWindow creates a window of class (registered on demand) and
// attaches h.
// x, y, w, hgt are 96-dpi grid values: a child is scaled as a whole, a
// top-level window keeps its center (createWindowPx: already pixels).
func createWindow(class, title string, exStyle, style uint32, x, y, w, hgt int32, parent uintptr, h msgHandler) (uintptr, error) {
	if style&winapi.WS_CHILD != 0 {
		x, y, w, hgt = pxRect(x, y, w, hgt)
	} else if w > 0 && hgt > 0 {
		// w × hgt is the client of the grid; centered in the work area,
		// clamped to it with scroll bars when larger (phase 10 E)
		cw, ch := px(w), px(hgt)
		var scroll bool
		x, y, w, hgt, style, scroll = fitClient(cw, ch, style, exStyle)
		hwnd, err := createWindowPx(class, title, exStyle, style, x, y, w, hgt, parent, h)
		if err == nil && scroll {
			attachScroller(hwnd, cw, ch)
		}
		return hwnd, err
	}
	return createWindowPx(class, title, exStyle, style, x, y, w, hgt, parent, h)
}

// createWindowPx creates a window at pixel coordinates (windowRect).
func createWindowPx(class, title string, exStyle, style uint32, x, y, w, hgt int32, parent uintptr, h msgHandler) (uintptr, error) {
	if err := ensureClass(class); err != nil {
		return 0, err
	}
	hwnd, err := winapi.CreateWindowEx(exStyle, class, title, style, x, y, w, hgt, parent, 0, winapi.GetModuleHandle())
	if err != nil {
		return 0, err
	}
	if style&winapi.WS_CHILD == 0 && style&winapi.WS_CAPTION != 0 {
		setWindowIcons(hwnd) // phase 11 (CHK-TRAY-49): the app icon in the title bar
		ApplyWindowTheme(hwnd) // phase 12 (CHK-TRAY-55): immersive dark + chrome
	}
	hmu.Lock()
	handlers[hwnd] = h
	hmu.Unlock()
	return hwnd, nil
}

// child creates a standard control (STATIC / BUTTON) with the GUI font.
func child(parent uintptr, class, text string, style uint32, id int32, x, y, w, h int32) uintptr {
	x, y, w, h = pxRect(x, y, w, h)
	hwnd, err := winapi.CreateWindowEx(0, class, text, winapi.WS_CHILD|winapi.WS_VISIBLE|style, x, y, w, h, parent, uintptr(id), winapi.GetModuleHandle())
	if err != nil {
		return 0
	}
	winapi.SendMessage(hwnd, winapi.WM_SETFONT, uiFont(), 1)
	return hwnd
}

// PumpPending dispatches the queued messages of the calling thread
// (tests, modal-free waits).
func PumpPending() {
	var m winapi.MSG
	for winapi.PeekMessage(&m) {
		winapi.TranslateMessage(&m)
		winapi.DispatchMessage(&m)
	}
}
