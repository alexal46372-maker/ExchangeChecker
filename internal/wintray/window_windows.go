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

func wndProc(hwnd, msg, wp, lp uintptr) uintptr {
	hmu.Lock()
	h := handlers[hwnd]
	hmu.Unlock()
	if h != nil {
		if r, ok := h(hwnd, uint32(msg), wp, lp); ok {
			return r
		}
	}
	if uint32(msg) == winapi.WM_DESTROY {
		hmu.Lock()
		delete(handlers, hwnd)
		hmu.Unlock()
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
		x, y, w, hgt = px(x), px(y), px(w), px(hgt)
	} else if w > 0 && hgt > 0 {
		sw, sh := px(w), px(hgt)
		x, y = x-(sw-w)/2, y-(sh-hgt)/3
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		w, hgt = sw, sh
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
	hmu.Lock()
	handlers[hwnd] = h
	hmu.Unlock()
	return hwnd, nil
}

// child creates a standard control (STATIC / BUTTON) with the GUI font.
func child(parent uintptr, class, text string, style uint32, id int32, x, y, w, h int32) uintptr {
	hwnd, err := winapi.CreateWindowEx(0, class, text, winapi.WS_CHILD|winapi.WS_VISIBLE|style, px(x), px(y), px(w), px(h), parent, uintptr(id), winapi.GetModuleHandle())
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
