//go:build windows

package wintray

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/version"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// pumpUntil dispatches messages of this (locked) thread until cond.
func pumpUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		PumpPending()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func menuItem(t *testing.T, hm uintptr, pos int) (text string, mii winapi.MENUITEMINFOW) {
	t.Helper()
	buf := make([]uint16, 256)
	mii.Mask = winapi.MIIM_FTYPE | winapi.MIIM_STRING | winapi.MIIM_ID | winapi.MIIM_STATE | winapi.MIIM_SUBMENU | winapi.MIIM_BITMAP
	mii.TypeData = &buf[0]
	mii.Cch = uint32(len(buf))
	if err := winapi.GetMenuItemInfo(hm, uint32(pos), &mii); err != nil {
		t.Fatalf("GetMenuItemInfo(%d): %v", pos, err)
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(utf16Decode(buf[:n])), mii
}

func utf16Decode(u []uint16) []rune {
	s := make([]rune, 0, len(u))
	for i := 0; i < len(u); i++ {
		r := rune(u[i])
		if r >= 0xD800 && r < 0xDC00 && i+1 < len(u) {
			r = (r-0xD800)<<10 + (rune(u[i+1]) - 0xDC00) + 0x10000
			i++
		}
		s = append(s, r)
	}
	return s
}

func testSnapshot(level model.AlertLevel) *model.StatusSnapshot {
	now := time.Now()
	return &model.StatusSnapshot{Seq: 3, Level: level, Profiles: []model.ProfileSnapshot{
		{ProfileID: "a", Name: "Торговля & склад", Enabled: true, Polled: true, Level: level, Points: []model.PointStatus{
			{ExchangePoint: model.ExchangePoint{Name: "Склад", Level: level, LastSend: &now, PlanTable: "_Node1"}, FreshAt: &now},
		}},
		{ProfileID: "b", Name: "Архив", Enabled: false, Points: []model.PointStatus{}},
	}}
}

// TestCHK_TRAY_01_NativeMenu — the trayui model becomes a real HMENU:
// status line disabled on top, databases with a colored square
// (hbmpItem) and status text, points in a submenu, '&' escaped; the right
// menu has the disabled Settings item, About and Exit.
func TestCHK_TRAY_01_NativeMenu(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	st := trayipc.Status{State: trayipc.StateConnected, Version: 2, Capabilities: []string{model.CapHeartbeat},
		Heartbeat: &model.Heartbeat{Protocol: 2}, Snapshot: testSnapshot(model.LevelWarning), EverConnected: true}
	items := trayui.BuildLeftMenu(st, i18n.RU)
	var bm bitmaps
	defer bm.free()
	hm, err := buildMenu(items, &bm)
	if err != nil {
		t.Fatal(err)
	}
	defer winapi.DestroyMenu(hm)
	if n := winapi.GetMenuItemCount(hm); n != len(items) {
		t.Fatalf("items %d want %d", n, len(items))
	}
	dbSeen := 0
	for i, it := range items {
		text, mii := menuItem(t, hm, i)
		if it.Separator {
			if mii.Type&winapi.MFT_SEPARATOR == 0 {
				t.Errorf("item %d must be a separator", i)
			}
			continue
		}
		if want := strings.ReplaceAll(it.Text, "&", "&&"); text != want {
			t.Errorf("item %d text %q want %q", i, text, want)
		}
		if it.Disabled != (mii.State&winapi.MFS_DISABLED != 0) {
			t.Errorf("item %d disabled state", i)
		}
		if (it.Color != trayui.ColorNone) != (mii.BmpItem != 0) {
			t.Errorf("item %d color square (hbmpItem) %x", i, mii.BmpItem)
		}
		if it.Sub != nil {
			dbSeen++
			if mii.SubMenu == 0 || winapi.GetMenuItemCount(mii.SubMenu) != len(it.Sub) {
				t.Errorf("item %d submenu", i)
			}
		}
	}
	if dbSeen != 2 {
		t.Errorf("databases %d", dbSeen)
	}
	if _, mii := menuItem(t, hm, 0); mii.State&winapi.MFS_DISABLED == 0 {
		t.Error("service status line must be a disabled item on top")
	}

	rm, err := buildMenu(trayui.BuildRightMenu(i18n.RU), &bm)
	if err != nil {
		t.Fatal(err)
	}
	defer winapi.DestroyMenu(rm)
	text, mii := menuItem(t, rm, 0)
	if mii.ID != trayui.CmdSettings || mii.State&winapi.MFS_DISABLED == 0 || !strings.Contains(text, "Настройки") {
		t.Errorf("settings item %q state %x", text, mii.State)
	}
	if _, mii := menuItem(t, rm, 1); mii.ID != trayui.CmdAbout || mii.State&winapi.MFS_DISABLED != 0 {
		t.Error("about item")
	}
}

// TestCHK_TRAY_01_AboutWindow — About shows product, version and service
// protocol; OK closes it.
func TestCHK_TRAY_01_AboutWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	closed := false
	text := trayui.AboutText(trayipc.Status{State: trayipc.StateConnected, Version: 2}, i18n.EN)
	h, err := newAboutWindow(0, trayui.AboutTitle(i18n.EN), text, "OK", false, func() { closed = true })
	if err != nil {
		t.Fatal(err)
	}
	got := winapi.GetWindowText(winapi.GetDlgItem(h, idAboutText))
	if !strings.Contains(got, version.Version) || !strings.Contains(got, version.ProductName) {
		t.Errorf("about text %q", got)
	}
	if winapi.GetDlgItem(h, winapi.IDOK) == 0 {
		t.Error("no OK button")
	}
	winapi.SendMessage(h, winapi.WM_COMMAND, winapi.IDOK, 0)
	if winapi.IsWindow(h) || !closed {
		t.Error("OK must close the About window")
	}
}

type fakeService struct {
	mu   sync.Mutex
	snap model.StatusSnapshot
}

func (f *fakeService) Snapshot() model.StatusSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap
}
func (f *fakeService) PollNow(string) (bool, error) { return false, nil }
func (f *fakeService) ReloadConfig() bool           { return false }
func (f *fakeService) Heartbeat() model.Heartbeat {
	return model.Heartbeat{Protocol: 2, ServiceStartedAt: time.Unix(100, 0)}
}

// TestCHK_TRAY_02_AppFollowsService — the App (hidden window, no
// NotifyIcon) connects to a fake service, its indicator follows pushes
// through the UI-thread queue, and goes to "not running" when the service
// disappears.
func TestCHK_TRAY_02_AppFollowsService(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &fakeService{snap: *testSnapshot(model.LevelOK)}
	l := ipc.NewMemListener()
	srv := ipc.NewServer(svc, nil)
	go srv.Serve(l)
	var mu sync.Mutex
	down := false
	dial := func(context.Context) (io.ReadWriteCloser, error) {
		mu.Lock()
		defer mu.Unlock()
		if down {
			return nil, fmt.Errorf("%w: test", trayipc.ErrServiceNotRunning)
		}
		return l.Dial()
	}
	app, err := NewApp(Options{Lang: i18n.RU, NoNotifyIcon: true, Session: trayipc.Options{Dial: dial,
		BackoffMin: 10 * time.Millisecond, BackoffMax: 20 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Exit()
	pumpUntil(t, "connected", func() bool {
		return app.Indicator().Service == trayui.SvcOK && app.Status().Snapshot != nil
	})
	if app.Indicator().Icon != trayui.IconOK {
		t.Errorf("icon %v", app.Indicator().Icon)
	}

	bad := testSnapshot(model.LevelError)
	bad.Seq = 4
	srv.PublishStatus(*bad)
	pumpUntil(t, "error icon", func() bool { return app.Indicator().Icon == trayui.IconError })
	if !strings.Contains(app.Indicator().Tooltip, "ошибка") {
		t.Errorf("tooltip %q", app.Indicator().Tooltip)
	}
	items := app.LeftMenu()
	if len(items) == 0 || !items[0].Disabled {
		t.Error("status line on top")
	}

	h := app.ShowAbout(false)
	if h == 0 || !strings.Contains(winapi.GetWindowText(winapi.GetDlgItem(h, idAboutText)), "2") {
		t.Error("about from the app")
	}
	winapi.SendMessage(h, winapi.WM_CLOSE, 0, 0)

	mu.Lock()
	down = true
	mu.Unlock()
	srv.Close()
	pumpUntil(t, "not running", func() bool { return app.Indicator().Service == trayui.SvcNotRunning })
	if app.Indicator().Icon == trayui.IconOK {
		t.Error("icon must not stay OK without the service")
	}
}

// TestCHK_TRAY_02_SingleInstance — the second tray of a session exits.
func TestCHK_TRAY_02_SingleInstance(t *testing.T) {
	name := fmt.Sprintf(`Local\ExchangeCheckerTray-test-%d`, os.Getpid())
	rel, ok, err := SingleInstance(name)
	if err != nil || !ok {
		t.Fatalf("first: ok=%v err=%v", ok, err)
	}
	if _, ok2, err := SingleInstance(name); err != nil || ok2 {
		t.Fatalf("second must be refused: ok=%v err=%v", ok2, err)
	}
	rel()
	rel3, ok3, err := SingleInstance(name)
	if err != nil || !ok3 {
		t.Fatalf("after release: ok=%v err=%v", ok3, err)
	}
	rel3()
}

// TestCHK_TRAY_02_Icons — every state icon and color square is created.
func TestCHK_TRAY_02_Icons(t *testing.T) {
	var ic icons
	defer ic.free()
	for _, k := range []trayui.Icon{trayui.IconOK, trayui.IconWarning, trayui.IconError, trayui.IconOffline, trayui.IconProblem} {
		if ic.get(k) == 0 {
			t.Errorf("icon %v not created", k)
		}
	}
	var bm bitmaps
	defer bm.free()
	for _, c := range []trayui.Color{trayui.ColorGreen, trayui.ColorYellow, trayui.ColorRed, trayui.ColorGrey} {
		if bm.get(c) == 0 {
			t.Errorf("square %v not created", c)
		}
	}
}

// TestCHK_TRAY_02_NotifyIconSmoke — Shell_NotifyIconW add/modify/delete
// with a real taskbar. CI runners may have no Explorer shell: SKIP then.
func TestCHK_TRAY_02_NotifyIconSmoke(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if winapi.FindWindow("Shell_TrayWnd") == 0 {
		t.Skip("SKIP: no Explorer taskbar (Shell_TrayWnd) in this session; Shell_NotifyIconW is checked manually (CHECKS.md, phase 6a)")
	}
	dial := func(context.Context) (io.ReadWriteCloser, error) {
		return nil, fmt.Errorf("%w: test", trayipc.ErrServiceNotRunning)
	}
	app, err := NewApp(Options{Lang: i18n.EN, Session: trayipc.Options{Dial: dial}})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Exit()
	if !app.HasNotifyIcon() {
		t.Skip("SKIP: taskbar present but Shell_NotifyIconW(NIM_ADD) failed (non-interactive session)")
	}
	pumpUntil(t, "not running", func() bool { return app.Indicator().Service == trayui.SvcNotRunning })
	if !app.HasNotifyIcon() {
		t.Error("icon lost after modify")
	}
}

// TestCHK_TRAY_10_StructSizes — Win32 struct layouts (x64).
func TestCHK_TRAY_10_StructSizes(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("SKIP: sizes are for amd64")
	}
	cases := []struct {
		name      string
		got, want uintptr
	}{
		{"NOTIFYICONDATAW", unsafe.Sizeof(winapi.NOTIFYICONDATAW{}), 976},
		{"MENUITEMINFOW", unsafe.Sizeof(winapi.MENUITEMINFOW{}), 80},
		{"WNDCLASSEXW", unsafe.Sizeof(winapi.WNDCLASSEXW{}), 80},
		{"MSG", unsafe.Sizeof(winapi.MSG{}), 48},
		{"BITMAPINFOHEADER", unsafe.Sizeof(winapi.BITMAPINFOHEADER{}), 40},
		{"FLASHWINFO", unsafe.Sizeof(winapi.FLASHWINFO{}), 32},
		{"ICONINFO", unsafe.Sizeof(winapi.ICONINFO{}), 32},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: %d bytes, want %d", c.name, c.got, c.want)
		}
	}
}
