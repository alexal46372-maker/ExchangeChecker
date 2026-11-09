//go:build windows

package wintray

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// enablerService adds set_enabled (ipc.ProfileEnabler, capability
// profile_enabled) to the fake config service: revision check (conflict),
// unknown profile (not_found), revision bump.
type enablerService struct {
	*cfgService
	calls []model.IPCRequest
}

func (e *enablerService) SetEnabled(id string, on bool, rev, account string) (string, error) {
	f := e.cfgService
	f.mu.Lock()
	defer f.mu.Unlock()
	e.calls = append(e.calls, model.IPCRequest{ProfileID: id, Enabled: &on, ExpectedRevision: rev})
	if rev != "" && rev != f.revision() {
		return "", &model.IPCError{Code: model.ErrCodeConflict, Key: model.MsgMonitorConfigConflict, Message: "conflict"}
	}
	for i := range f.profiles {
		if f.profiles[i].ID == id {
			f.profiles[i].Enabled = on
			f.rev++
			return f.revision(), nil
		}
	}
	return "", &model.IPCError{Code: model.ErrCodeNotFound, Key: model.MsgMonitorUnknownProfile, Message: "not found"}
}

func (e *enablerService) enabled(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range e.profiles {
		if p.ID == id {
			return p.Enabled
		}
	}
	return false
}

func (e *enablerService) nCalls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.calls)
}

func dbItem(app *App) trayui.Item {
	for _, it := range app.LeftMenu() {
		if it.Check {
			return it
		}
	}
	return trayui.Item{}
}

// TestCHK_TRAY_91_DatabaseCheckToggle — the bold check mark of a database
// item in the left menu: real ipc.Server + session; the menu command sends
// set_enabled, the profile_enabled push unchecks / checks the «Опрос»
// checkbox of the open settings list, and a click on that checkbox sends
// set_enabled at once (with expectedRevision) and updates the menu; a
// conflict reverts the checkbox. No mute item is left in the menu.
func TestCHK_TRAY_91_DatabaseCheckToggle(t *testing.T) {
	runtime.LockOSThread() // windows and the message pump on one thread
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	en := &enablerService{cfgService: svc}
	svc.handler = en
	app := startCfgApp(t, svc, true, nil)
	if !app.Status().Has(model.CapProfileEnabled) {
		t.Fatalf("capability profile_enabled missing: %v", app.Status().Capabilities)
	}
	it := dbItem(app)
	if !it.Check || !it.Checked || it.ID != trayui.ToggleCmd(0) || it.Sub == nil {
		t.Fatalf("database item %+v", it)
	}
	for _, s := range it.Sub {
		if s.Text == "Приглушить до восстановления" || s.Text == "Снять" {
			t.Fatalf("mute item left: %+v", it.Sub)
		}
	}
	w := openAdmin(t, app)
	lv := w.ctl[idSetList]
	if !winapi.ListViewChecked(lv, 0) {
		t.Fatal("list «Опрос» must start checked")
	}

	// menu → set_enabled(false) → push → list unchecked, menu unchecked
	app.Command(trayui.ToggleCmd(0))
	pumpUntil(t, "menu toggle off", func() bool {
		return !en.enabled("p1") && !winapi.ListViewChecked(lv, 0) && !dbItem(app).Checked && !w.ed.Profiles[0].Enabled
	})
	if w.ed.Revision != svc.revision() || w.ed.ProfileUnsaved() {
		t.Fatalf("revision %q / %q, unsaved %v", w.ed.Revision, svc.revision(), w.ed.ProfileUnsaved())
	}
	if c := dbItem(app).Color; c != trayui.ColorGrey {
		t.Errorf("disabled check color %v", c)
	}

	// list checkbox → set_enabled(true, expectedRevision) → menu checked
	n := en.nCalls()
	winapi.ListViewSetChecked(lv, 0, true)
	pumpUntil(t, "list toggle on", func() bool { return en.enabled("p1") && dbItem(app).Checked && !w.busy })
	en.mu.Lock()
	last := en.calls[n]
	en.mu.Unlock()
	if last.ExpectedRevision == "" || last.Enabled == nil || !*last.Enabled {
		t.Fatalf("list set_enabled %+v", last)
	}
	if w.ed.ProfileUnsaved() {
		t.Error("live toggle must not leave unsaved edits")
	}

	// conflict: stale revision reverts the checkbox
	svc.mu.Lock()
	svc.rev += 5
	svc.mu.Unlock()
	winapi.ListViewSetChecked(lv, 0, false)
	pumpUntil(t, "conflict revert", func() bool { return !w.busy && winapi.ListViewChecked(lv, 0) })
	if !en.enabled("p1") || w.statusText() == "" {
		t.Fatalf("conflict: enabled %v status %q", en.enabled("p1"), w.statusText())
	}
}

// TestCHK_TRAY_91_CheckBitmapAndHook — buildMenu gives a database item the
// check-mark bitmap (checked / empty), no MFS_CHECKED, and the menu hook
// maps a click on the item to its toggle command only for Check items with
// a command.
func TestCHK_TRAY_91_CheckBitmapAndHook(t *testing.T) {
	runtime.LockOSThread() // windows and the message pump on one thread
	defer runtime.UnlockOSThread()
	var bm bitmaps
	defer bm.free()
	items := []trayui.Item{
		{ID: trayui.ToggleCmd(0), Text: "A", Color: trayui.ColorRed, Check: true, Checked: true, Sub: []trayui.Item{{Text: "x", Disabled: true}}},
		{Text: "B", Color: trayui.ColorGrey, Check: true, Sub: []trayui.Item{{Text: "y", Disabled: true}}},
		{ID: 7, Text: "C", Color: trayui.ColorGreen},
	}
	hm, err := buildMenu(items, &bm)
	if err != nil {
		t.Fatal(err)
	}
	defer winapi.DestroyMenu(hm)
	_, m0 := menuItem(t, hm, 0)
	_, m1 := menuItem(t, hm, 1)
	_, m2 := menuItem(t, hm, 2)
	if m0.BmpItem == 0 || m0.BmpItem != bm.check(trayui.ColorRed, true) || m0.State&winapi.MFS_CHECKED != 0 {
		t.Errorf("checked item bitmap %x state %x", m0.BmpItem, m0.State)
	}
	if m1.BmpItem == 0 || m1.BmpItem != bm.check(trayui.ColorGrey, false) || m1.BmpItem == m0.BmpItem {
		t.Errorf("unchecked item bitmap %x", m1.BmpItem)
	}
	if m2.BmpItem != bm.get(trayui.ColorGreen) {
		t.Errorf("plain item keeps the square")
	}
	menuToggle.menu, menuToggle.items = hm, items
	defer func() { menuToggle.menu, menuToggle.items = 0, nil }()
	if toggleAt(0) != trayui.ToggleCmd(0) || toggleAt(1) != 0 || toggleAt(2) != 0 || toggleAt(-1) != 0 || toggleAt(3) != 0 {
		t.Errorf("toggleAt %d %d %d", toggleAt(0), toggleAt(1), toggleAt(2))
	}
	h, err := winapi.SetMsgFilterHook(msgFilterCallback)
	if err != nil || h == 0 {
		t.Fatalf("WH_MSGFILTER hook: %v", err)
	}
	winapi.UnhookWindowsHookEx(h)
}

// TestCHK_TRAY_91_NotElevatedNoToggle — without clientElevated (set_enabled
// is admin-only) the check mark is drawn but sends nothing.
func TestCHK_TRAY_91_NotElevatedNoToggle(t *testing.T) {
	runtime.LockOSThread() // windows and the message pump on one thread
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	en := &enablerService{cfgService: svc}
	svc.handler = en
	app := startCfgApp(t, svc, false, nil)
	it := dbItem(app)
	if !it.Check || !it.Checked || it.ID != 0 {
		t.Fatalf("non-elevated item %+v", it)
	}
	app.Command(trayui.ToggleCmd(0))
	pumpFor(100e6)
	if en.nCalls() != 0 || !en.enabled("p1") {
		t.Fatal("non-elevated toggle sent set_enabled")
	}
}

// TestCHK_TRAY_91_ToggleAfterListChange — the database list changes while
// the left menu is shown (a profile is inserted in front: every index
// moves); a click on the check mark of the shown item «B» sends set_enabled
// for B (its ProfileID kept in the menu item) with the inverse of the shown
// state, not for whatever profile now has that index. Real path: the shown
// menu (#32768), the WH_MSGFILTER hook, ShowLeftMenu → MenuCommand.
func TestCHK_TRAY_91_ToggleAfterListChange(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	p2 := model.NewProfile("p2")
	p2.Name, p2.Server, p2.Database = "Филиал", `srv\SQL2`, "db2"
	svc.profiles = append(svc.profiles, p2)
	en := &enablerService{cfgService: svc}
	svc.handler = en
	app := startCfgApp(t, svc, true, nil)
	items := app.LeftMenu()
	bi := -1
	for i, it := range items {
		if it.Check && it.ProfileID == "p2" {
			bi = i
		}
	}
	if bi < 0 || !items[bi].Checked || items[bi].ID == 0 {
		t.Fatalf("menu %+v", items)
	}
	shownCmd := items[bi].ID
	errc := make(chan string, 1)
	go func() {
		var mw uintptr
		for i := 0; i < 100 && mw == 0; i++ {
			time.Sleep(50 * time.Millisecond)
			if w := winapi.FindWindow("#32768"); w != 0 && winapi.IsWindowVisible(w) {
				mw = w
			}
		}
		if mw == 0 {
			errc <- "menu not shown"
			return
		}
		// the list changes while the menu is open: p0 in front
		p0 := model.NewProfile("p0")
		p0.Name, p0.Server, p0.Database = "Новая", `srv\SQL0`, "db0"
		svc.mu.Lock()
		svc.profiles = append([]model.Profile{p0}, svc.profiles...)
		svc.rev++
		svc.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = app.session.Refresh(ctx)
		cancel()
		for i := 0; i < 100; i++ {
			if st := app.session.Status(); st.Snapshot != nil && len(st.Snapshot.Profiles) == 3 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(200 * time.Millisecond)         // the UI thread applies the status inside the menu loop
		hm := winapi.SendMessage(mw, 0x01E1, 0, 0) // MN_GETHMENU
		rc, ok := winapi.GetMenuItemRect(mw, hm, bi)
		if !ok {
			errc <- "GetMenuItemRect"
			return
		}
		winapi.SetCursorPos((rc.Left+rc.Right)/2, (rc.Top+rc.Bottom)/2)
		time.Sleep(80 * time.Millisecond)
		winapi.PostMessage(mw, winapi.WM_LBUTTONDOWN, 0, 0)
		time.Sleep(80 * time.Millisecond)
		winapi.PostMessage(mw, winapi.WM_LBUTTONUP, 0, 0)
		time.Sleep(300 * time.Millisecond)
		if w := winapi.FindWindow("#32768"); w != 0 && winapi.IsWindowVisible(w) {
			winapi.PostMessage(w, winapi.WM_KEYDOWN, winapi.VK_ESCAPE, 0)
			time.Sleep(100 * time.Millisecond)
			winapi.PostMessage(w, winapi.WM_KEYDOWN, winapi.VK_ESCAPE, 0)
		}
		errc <- ""
	}()
	app.ShowLeftMenu()
	if e := <-errc; e != "" {
		t.Fatal(e)
	}
	// the menu really changed under the shown items: the shown command now
	// has another database at its index
	if now, _ := trayui.ToggleItem(app.LeftMenu(), shownCmd); now.ProfileID == "p2" {
		t.Fatalf("the list did not change while the menu was open: %+v", app.LeftMenu())
	}
	pumpUntil(t, "set_enabled", func() bool { return en.nCalls() == 1 && !en.enabled("p2") })
	en.mu.Lock()
	c := en.calls[0]
	en.mu.Unlock()
	if c.ProfileID != "p2" || c.Enabled == nil || *c.Enabled {
		t.Fatalf("set_enabled %+v, want p2 off", c)
	}
	if !en.enabled("p1") || !en.enabled("p0") {
		t.Error("another database was toggled")
	}
	pumpFor(100 * time.Millisecond)
	if en.nCalls() != 1 {
		t.Errorf("set_enabled calls %d", en.nCalls())
	}
}
