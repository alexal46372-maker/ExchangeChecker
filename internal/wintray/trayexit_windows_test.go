//go:build windows

package wintray

import (
	"runtime"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
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
func openDiscEdited(t *testing.T, conf *fakeConfirm) (*App, *settingsWindow, *discoveryDialog) {
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
	return app, w, dd
}

// TestCHK_TRAY_11_DiscoveryUnsaved — the Discovery dialog with unapplied
// edits closes (with its settings window) on the message, no question.
func TestCHK_TRAY_11_DiscoveryUnsaved(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	conf := &fakeConfirm{no: true}
	app, w, dd := openDiscEdited(t, conf)
	postExit(t, dd.hwnd)
	pumpUntil(t, "dialog closed", func() bool { return !winapi.IsWindow(dd.hwnd) && !winapi.IsWindow(w.hwnd) })
	if len(conf.texts) != 0 || app.settings != nil {
		t.Errorf("questions %q", conf.texts)
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
		app := startCfgApp(t, newCfgService(), true, nil, conf)
		w := openAdmin(t, app)
		w.set(idSetName, "правка")
		conf.hook = func() { postExit(t, app.hwnd); PumpPending() }
		winapi.SendMessage(w.hwnd, winapi.WM_CLOSE, 0, 0) // «Отменить изменения?» open
		pumpUntil(t, "tray exit", func() bool { return appClosed(app) && !winapi.IsWindow(w.hwnd) })
		if len(conf.texts) != 1 {
			t.Errorf("questions %q", conf.texts)
		}
	})
	t.Run("discovery", func(t *testing.T) {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		conf := &fakeConfirm{}
		_, w, dd := openDiscEdited(t, conf)
		conf.hook = func() { postExit(t, dd.hwnd); PumpPending() }
		winapi.SendMessage(dd.hwnd, winapi.WM_CLOSE, 0, 0) // «Закрыть без применения?» open
		pumpUntil(t, "dialog closed", func() bool { return !winapi.IsWindow(dd.hwnd) && !winapi.IsWindow(w.hwnd) })
		if len(conf.texts) != 1 {
			t.Errorf("questions %q", conf.texts)
		}
	})
}
