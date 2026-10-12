//go:build windows

package wintray

import (
	"context"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// postExit posts the registered model.TrayExitMessage to hwnd (as the
// installer does; wParam / lParam must be ignored).
func postExit(t *testing.T, hwnd uintptr) {
	t.Helper()
	m := winapi.RegisterWindowMessage(model.TrayExitMessage)
	if m == 0 || m != trayExitMsg() {
		t.Fatalf("registered message %#x / %#x", m, trayExitMsg())
	}
	winapi.PostMessage(hwnd, m, 0x1234, 0x5678)
}

func appClosed(app *App) bool {
	app.qmu.Lock()
	defer app.qmu.Unlock()
	return app.closing && !winapi.IsWindow(app.hwnd)
}

// TestCHK_TRAY_11_ExitMessage — CHK-TRAY-11: the tray exit message on the
// hidden main window runs App.Exit: the icon is removed (NIM_DELETE), the
// window is destroyed; a real notify icon too when Explorer is there.
func TestCHK_TRAY_11_ExitMessage(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app := startCfgApp(t, newCfgService(), false, nil)
	if winapi.GetClassName(app.hwnd) != mainClass {
		t.Fatalf("main class %q", winapi.GetClassName(app.hwnd))
	}
	app.nidAdded = true // as if the icon was added (no Explorer in tests)
	postExit(t, app.hwnd)
	pumpUntil(t, "tray exit", func() bool { return appClosed(app) })
	if app.nidAdded || app.iconRemoves != 1 {
		t.Errorf("icon not removed: added %v removes %d", app.nidAdded, app.iconRemoves)
	}
	app.Command(0) // after the exit: nothing happens
}

// TestCHK_TRAY_11_ExitMessageRealIcon — the same with Shell_NotifyIconW
// when the session has an Explorer taskbar (SKIP otherwise).
func TestCHK_TRAY_11_ExitMessageRealIcon(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if winapi.FindWindow("Shell_TrayWnd") == 0 {
		t.Skip("no Explorer taskbar in this session")
	}
	app := startCfgApp(t, newCfgService(), false, nil)
	app.opt.NoNotifyIcon = false
	app.addIcon()
	if !app.nidAdded {
		t.Skip("Shell_NotifyIconW refused the icon")
	}
	postExit(t, app.hwnd)
	pumpUntil(t, "tray exit", func() bool { return appClosed(app) })
	if app.nidAdded || app.iconRemoves != 1 {
		t.Errorf("icon not removed: added %v removes %d", app.nidAdded, app.iconRemoves)
	}
}

// TestCHK_TRAY_11_WMClose — WM_CLOSE on the hidden main window also runs
// App.Exit (the icon used to linger after DefWindowProc).
func TestCHK_TRAY_11_WMClose(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app := startCfgApp(t, newCfgService(), false, nil)
	app.nidAdded = true
	winapi.PostMessage(app.hwnd, winapi.WM_CLOSE, 0, 0)
	pumpUntil(t, "tray exit on WM_CLOSE", func() bool { return appClosed(app) })
	if app.nidAdded || app.iconRemoves != 1 {
		t.Errorf("icon not removed: added %v removes %d", app.nidAdded, app.iconRemoves)
	}
}

// TestCHK_TRAY_11_SettingsUnsaved — the settings window with unsaved edits
// closes on the message without a question.
func TestCHK_TRAY_11_SettingsUnsaved(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	conf := &fakeConfirm{no: true}
	app := startCfgApp(t, newCfgService(), true, nil, conf)
	w := openAdmin(t, app)
	w.set(idSetName, "правка")
	w.readForm()
	if !w.ed.Unsaved() {
		t.Fatal("the edit is not unsaved")
	}
	postExit(t, w.hwnd)
	pumpUntil(t, "settings closed", func() bool { return !winapi.IsWindow(w.hwnd) })
	if len(conf.texts) != 0 || app.settings != nil {
		t.Errorf("questions %q, settings %v", conf.texts, app.settings != nil)
	}
	if appClosed(app) {
		t.Error("the settings window's message must not end the tray")
	}
}

// openDiscEdited opens the Discovery dialog and makes an unapplied edit.
func openDiscEdited(t *testing.T, conf *fakeConfirm) (*App, *settingsWindow, *discoveryDialog, *discService) {
	t.Helper()
	ds := newDiscService()
	app := startCfgApp(t, ds.cfgService, true, nil, conf)
	w := openAdmin(t, app)
	pumpUntil(t, "discovery button enabled", func() bool {
		w.update()
		return winapi.IsWindowEnabled(w.ctl[idSetDiscover])
	})
	w.click(t, idSetDiscover)
	dd := w.disc
	if dd == nil || winapi.GetClassName(dd.hwnd) != DiscoveryClass {
		t.Fatal("dialog not opened")
	}
	dd.click(t, idDiscFind)
	pumpUntil(t, "discover", func() bool { n, _ := ds.n(); return n == 1 && dd.cancel == nil })
	dd.set(idDiscTable, "_Node99")
	dd.readMap()
	if !dd.d.Unapplied() {
		t.Fatal("the edit is not unapplied")
	}
	return app, w, dd, ds
}

// TestCHK_TRAY_11_DiscoveryUnsaved — the Discovery dialog with unapplied
// edits closes (with its settings window) on the message, no question.
func TestCHK_TRAY_11_DiscoveryUnsaved(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	conf := &fakeConfirm{no: true}
	app, w, dd, _ := openDiscEdited(t, conf)
	ups := w.updates
	postExit(t, dd.hwnd)
	pumpUntil(t, "dialog closed", func() bool { return !winapi.IsWindow(dd.hwnd) && !winapi.IsWindow(w.hwnd) })
	if len(conf.texts) != 0 || app.settings != nil {
		t.Errorf("questions %q", conf.texts)
	}
	if w.updates != ups {
		t.Errorf("dd.close refreshed the exiting settings window (%d updates)", w.updates-ups)
	}
}

// TestCHK_TRAY_11_PendingConfirm — the message arriving while a question
// is open (the MessageBox modal loop dispatches it) closes the windows;
// the question answers «Нет» even if «Да» was clicked, nothing is done
// after it and no further question is shown.
func TestCHK_TRAY_11_PendingConfirm(t *testing.T) {
	t.Run("settings", func(t *testing.T) {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		conf := &fakeConfirm{}
		svc := newCfgService()
		app := startCfgApp(t, svc, true, nil, conf)
		w := openAdmin(t, app)
		w.set(idSetName, "правка")
		conf.hook = func() { postExit(t, w.hwnd); PumpPending() }
		w.click(t, idSetDelete) // «Удалить …?» is open, the message arrives
		pumpUntil(t, "settings closed", func() bool { return !winapi.IsWindow(w.hwnd) })
		if len(conf.texts) != 1 || svc.count("delete") != 0 {
			t.Errorf("questions %q, deletes %d", conf.texts, svc.count("delete"))
		}
		if w.ask("again?") || len(conf.texts) != 1 {
			t.Error("a question after the exit")
		}
	})
	t.Run("main", func(t *testing.T) {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		conf := &fakeConfirm{}
		svc := newCfgService()
		app := startCfgApp(t, svc, true, nil, conf)
		w := openAdmin(t, app)
		w.set(idSetName, "правка")
		conf.hook = func() { postExit(t, app.hwnd); PumpPending() }
		winapi.SendMessage(w.hwnd, winapi.WM_CLOSE, 0, 0) // «Отменить изменения?» open
		pumpUntil(t, "tray exit", func() bool { return appClosed(app) && !winapi.IsWindow(w.hwnd) })
		if len(conf.texts) != 1 || svc.count("save") != 0 || svc.count("settings") != 0 || svc.count("delete") != 0 {
			t.Errorf("questions %q, saves %d/%d, deletes %d", conf.texts, svc.count("save"), svc.count("settings"), svc.count("delete"))
		}
	})
	t.Run("discovery", func(t *testing.T) {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		conf := &fakeConfirm{}
		_, w, dd, ds := openDiscEdited(t, conf)
		conf.hook = func() { postExit(t, dd.hwnd); PumpPending() }
		winapi.SendMessage(dd.hwnd, winapi.WM_CLOSE, 0, 0) // «Закрыть без применения?» open
		pumpUntil(t, "dialog closed", func() bool { return !winapi.IsWindow(dd.hwnd) && !winapi.IsWindow(w.hwnd) })
		if len(conf.texts) != 1 || w.ed.MapEdit != nil || ds.count("save") != 0 {
			t.Errorf("questions %q, applied %v, saves %d", conf.texts, w.ed.MapEdit != nil, ds.count("save"))
		}
	})
}

// TestCHK_TRAY_11_ExitHungSession — a session goroutine that never ends
// (Dial ignores ctx) does not keep the tray: the icon is removed before
// the wait and Exit returns within exitWait (under the installer's 5 s).
func TestCHK_TRAY_11_ExitHungSession(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hang, dialed := make(chan struct{}), make(chan struct{}, 1)
	defer close(hang)
	dial := func(context.Context) (io.ReadWriteCloser, error) {
		dialed <- struct{}{}
		<-hang
		return nil, io.EOF
	}
	app, err := NewApp(Options{Lang: i18n.RU, NoNotifyIcon: true, HiddenAlerts: true, HiddenSettings: true, Beeper: &testBeeper{},
		Session: trayipc.Options{Dial: dial}})
	if err != nil {
		t.Fatal(err)
	}
	<-dialed
	app.nidAdded = true
	atWait := -1
	exitWaitHook = func() { atWait = app.iconRemoves }
	defer func() { exitWaitHook = nil }()
	start := time.Now()
	app.Exit()
	el := time.Since(start)
	if el < exitWait || el > exitWait+time.Second {
		t.Errorf("Exit took %v (bound %v)", el, exitWait)
	}
	if exitWait >= 5*time.Second {
		t.Errorf("exitWait %v is not under the installer's 5 s", exitWait)
	}
	if atWait != 1 {
		t.Errorf("icon removes when the wait started: %d", atWait)
	}
	if app.iconRemoves != 1 || app.nidAdded || winapi.IsWindow(app.hwnd) {
		t.Errorf("icon removes %d, window %v", app.iconRemoves, winapi.IsWindow(app.hwnd))
	}
}

// TestCHK_TRAY_11_WindowClasses — the classes the installer looks for
// (model) are the classes of the tray windows.
func TestCHK_TRAY_11_WindowClasses(t *testing.T) {
	for got, want := range map[string]string{
		mainClass: "ExchangeCheckerTray", SettingsClass: "ExchangeCheckerTraySettings",
		SettingsViewClass: "ExchangeCheckerTraySettingsView", DiscoveryClass: "ExchangeCheckerTrayDiscovery",
		model.TrayWindowClass + "|": mainClass + "|",
	} {
		if got != want {
			t.Errorf("class %q, want %q", got, want)
		}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	conf := &fakeConfirm{}
	app, w, dd, _ := openDiscEdited(t, conf)
	for h, c := range map[uintptr]string{app.hwnd: model.TrayWindowClass, w.hwnd: model.TraySettingsClass, dd.hwnd: model.TrayDiscoveryClass} {
		if g := winapi.GetClassName(h); g != c {
			t.Errorf("window class %q, want %q", g, c)
		}
	}
}
