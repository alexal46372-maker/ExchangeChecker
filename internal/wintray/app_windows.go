//go:build windows

package wintray

import (
	"context"
	"sync"
	"unsafe"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

const (
	mainClass = "ExchangeCheckerTray"
	// msgEvents: session events are queued (drained on the UI thread).
	msgEvents = winapi.WM_APP + 1
	// msgNotify: NotifyIcon callback message.
	msgNotify = winapi.WM_APP + 2
	trayID    = 1
)

// Options configure the tray application.
type Options struct {
	Lang    string
	Session trayipc.Options // Dial and Verify; OnEvent is set by the App
	// NoNotifyIcon skips Shell_NotifyIconW (tests without Explorer).
	NoNotifyIcon bool
}

// App is the tray application. All methods except the session callback
// run on the thread that called NewApp (a locked OS thread).
type App struct {
	opt     Options
	hwnd    uintptr
	session *trayipc.Session
	cancel  context.CancelFunc
	done    chan struct{}

	qmu     sync.Mutex
	queue   []trayipc.Event
	closing bool

	status         trayipc.Status
	ind            trayui.Indicator
	icons          icons
	squares        bitmaps
	nidAdded       bool
	nidV4          bool
	taskbarCreated uint32
	about          uintptr
}

// NewApp creates the hidden owner window, the tray icon and starts the
// service session. Call on a locked OS thread, then Loop.
func NewApp(opt Options) (*App, error) {
	a := &App{opt: opt, done: make(chan struct{})}
	a.opt.Lang = i18n.Normalize(opt.Lang)
	hwnd, err := createWindow(mainClass, i18n.T(i18n.KeyAppName, a.opt.Lang), 0, winapi.WS_OVERLAPPED, 0, 0, 0, 0, 0, a.wndProc)
	if err != nil {
		return nil, err
	}
	a.hwnd = hwnd
	a.taskbarCreated = winapi.RegisterWindowMessage("TaskbarCreated")
	a.ind = trayui.Evaluate(a.status, a.opt.Lang)
	a.addIcon()

	so := opt.Session
	so.OnEvent = a.onEvent
	a.session = trayipc.New(so)
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	go func() {
		defer close(a.done)
		a.session.Run(ctx)
	}()
	return a, nil
}

// onEvent runs on the session dispatcher goroutine.
func (a *App) onEvent(e trayipc.Event) {
	a.qmu.Lock()
	if a.closing {
		a.qmu.Unlock()
		return
	}
	first := len(a.queue) == 0
	a.queue = append(a.queue, e)
	a.qmu.Unlock()
	if first {
		winapi.PostMessage(a.hwnd, msgEvents, 0, 0)
	}
}

// Indicator is the current tray state (UI thread).
func (a *App) Indicator() trayui.Indicator { return a.ind }

// Status is the last session status applied on the UI thread.
func (a *App) Status() trayipc.Status { return a.status }

// Session is the service session.
func (a *App) Session() *trayipc.Session { return a.session }

// HasNotifyIcon reports whether the tray icon is shown.
func (a *App) HasNotifyIcon() bool { return a.nidAdded }

// Window is the hidden owner window.
func (a *App) Window() uintptr { return a.hwnd }

// Loop runs the message loop until Exit.
func (a *App) Loop() int {
	var m winapi.MSG
	for {
		r := winapi.GetMessage(&m)
		if r == 0 || r == -1 {
			break
		}
		winapi.TranslateMessage(&m)
		winapi.DispatchMessage(&m)
	}
	return int(m.WParam)
}

// Exit removes the icon, stops the session and ends Loop.
func (a *App) Exit() {
	a.qmu.Lock()
	already := a.closing
	a.closing = true
	a.qmu.Unlock()
	if already {
		return
	}
	a.removeIcon()
	a.cancel()
	<-a.done
	if a.about != 0 {
		winapi.DestroyWindow(a.about)
	}
	winapi.DestroyWindow(a.hwnd) // WM_DESTROY -> PostQuitMessage
	a.icons.free()
	a.squares.free()
}

func (a *App) wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch {
	case msg == msgEvents:
		a.drain()
		return 0, true
	case msg == msgNotify:
		a.onNotify(uint32(lp & 0xFFFF))
		return 0, true
	case a.taskbarCreated != 0 && msg == a.taskbarCreated:
		// Explorer restarted: the icon is gone, add it again
		a.nidAdded = false
		a.addIcon()
		return 0, true
	case msg == winapi.WM_DESTROY:
		winapi.PostQuitMessage(0)
		return 0, false
	}
	return 0, false
}

// drain applies queued session events (UI thread).
func (a *App) drain() {
	a.qmu.Lock()
	q := a.queue
	a.queue = nil
	a.qmu.Unlock()
	if len(q) == 0 {
		return
	}
	// every event carries the full status; the last one wins. Alerts are
	// handled from phase 6b (alert window, sound).
	a.status = q[len(q)-1].Status
	old := a.ind
	a.ind = trayui.Evaluate(a.status, a.opt.Lang)
	if !a.nidAdded {
		a.addIcon()
	} else if old.Icon != a.ind.Icon || old.Tooltip != a.ind.Tooltip {
		a.modifyIcon()
	}
}

func (a *App) onNotify(ev uint32) {
	switch ev {
	case winapi.NIN_SELECT, winapi.NIN_KEYSELECT:
		a.ShowLeftMenu()
	case winapi.WM_CONTEXTMENU:
		a.ShowRightMenu()
	case winapi.WM_LBUTTONUP:
		if !a.nidV4 {
			a.ShowLeftMenu()
		}
	case winapi.WM_RBUTTONUP:
		if !a.nidV4 {
			a.ShowRightMenu()
		}
	}
}

// LeftMenu / RightMenu are the current menu models.
func (a *App) LeftMenu() []trayui.Item  { return trayui.BuildLeftMenu(a.status, a.opt.Lang) }
func (a *App) RightMenu() []trayui.Item { return trayui.BuildRightMenu(a.opt.Lang) }

// ShowLeftMenu shows the databases menu at the cursor.
func (a *App) ShowLeftMenu() { a.Command(trackMenu(a.hwnd, a.LeftMenu(), &a.squares)) }

// ShowRightMenu shows Settings / About / Exit at the cursor.
func (a *App) ShowRightMenu() { a.Command(trackMenu(a.hwnd, a.RightMenu(), &a.squares)) }

// Command executes a menu command.
func (a *App) Command(cmd int) {
	switch cmd {
	case trayui.CmdAbout:
		a.ShowAbout(true)
	case trayui.CmdExit:
		a.Exit()
	}
}

// ShowAbout opens (or focuses) the About window; returns its handle.
func (a *App) ShowAbout(show bool) uintptr {
	if a.about != 0 && winapi.IsWindow(a.about) {
		if show {
			winapi.SetForegroundWindow(a.about)
		}
		return a.about
	}
	lang := a.opt.Lang
	h, err := newAboutWindow(a.hwnd, trayui.AboutTitle(lang), trayui.AboutText(a.session.Status(), lang),
		i18n.T(i18n.KeyAboutOK, lang), show, func() { a.about = 0 })
	if err != nil {
		return 0
	}
	a.about = h
	return h
}

func (a *App) nid() winapi.NOTIFYICONDATAW {
	var n winapi.NOTIFYICONDATAW
	n.Size = uint32(unsafe.Sizeof(n))
	n.Wnd = a.hwnd
	n.ID = trayID
	n.Flags = winapi.NIF_MESSAGE | winapi.NIF_ICON | winapi.NIF_TIP | winapi.NIF_SHOWTIP
	n.CallbackMessage = msgNotify
	n.Icon = a.icons.get(a.ind.Icon)
	tip, _ := utf16From(a.ind.Tooltip)
	copy(n.Tip[:len(n.Tip)-1], tip)
	return n
}

func (a *App) addIcon() {
	if a.opt.NoNotifyIcon || a.nidAdded {
		return
	}
	n := a.nid()
	if err := winapi.ShellNotifyIcon(winapi.NIM_ADD, &n); err != nil {
		// Explorer not ready yet (logon): retried on TaskbarCreated and on
		// the next status change
		return
	}
	a.nidAdded = true
	n.Version = winapi.NOTIFYICON_VERSION_4
	a.nidV4 = winapi.ShellNotifyIcon(winapi.NIM_SETVERSION, &n) == nil
}

func (a *App) modifyIcon() {
	n := a.nid()
	if winapi.ShellNotifyIcon(winapi.NIM_MODIFY, &n) != nil {
		a.nidAdded = false
		a.addIcon()
	}
}

func (a *App) removeIcon() {
	if !a.nidAdded {
		return
	}
	n := a.nid()
	n.Flags = 0
	winapi.ShellNotifyIcon(winapi.NIM_DELETE, &n)
	a.nidAdded = false
}
