//go:build windows

package wintray

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// openDisc opens the Discovery dialog of the administrator window.
func openDisc(t *testing.T, w *settingsWindow) *discoveryDialog {
	t.Helper()
	pumpUntil(t, "discovery button enabled", func() bool {
		w.update()
		return winapi.IsWindowEnabled(w.ctl[idSetDiscover])
	})
	w.click(t, idSetDiscover)
	if w.disc == nil {
		t.Fatal("dialog not opened")
	}
	return w.disc
}

// selKind picks an item of the mode box like the user (CB_SETCURSEL does
// not notify).
func (dd *discoveryDialog) selKind(k traysettings.MappingKind) {
	winapi.SendMessage(dd.ctl[idDiscMode], winapi.CB_SETCURSEL, uintptr(k), 0)
	dd.command(idDiscMode, winapi.CBN_SELCHANGE)
}

// TestCHK_TRAY_07_ApplyInvalidMapping — the blocker of the 6d review:
// Apply refuses an invalid mapping inside the dialog (label + field);
// an invalid mapping edit never blocks test_profile / the Discovery
// commands; Save names the mapping field and focuses «Таблицы обмена…».
func TestCHK_TRAY_07_ApplyInvalidMapping(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ds := newDiscService()
	app := startCfgApp(t, ds.cfgService, true, nil)
	w := openAdmin(t, app)
	dd := openDisc(t, w)

	dd.selKind(traysettings.KindSQL) // manual SQL left empty
	dd.click(t, idDiscApply)
	if w.disc == nil || w.ed.MapEdit != nil || dd.lastField != "mapping.manualSql" ||
		!strings.Contains(dd.text(idDiscResult), "Ручной SQL") || strings.Contains(dd.text(idDiscResult), "manualSql") {
		t.Fatalf("empty manual SQL applied: open %v field %q result %q", w.disc != nil, dd.lastField, dd.text(idDiscResult))
	}
	// tables: a column without a table — the table box is highlighted
	dd.selKind(traysettings.KindTables)
	dd.set(idDiscSend, "_Fld90")
	if dd.text(idDiscSource) != i18n.T(i18n.KeyDiscSourceManual, i18n.RU) {
		t.Errorf("hand-typed column: source %q", dd.text(idDiscSource))
	}
	dd.click(t, idDiscApply)
	if w.disc == nil || w.ed.MapEdit != nil || dd.lastField != "mapping.table" {
		t.Fatalf("column without a table applied: field %q result %q", dd.lastField, dd.text(idDiscResult))
	}
	// the searches work while the form is invalid (no mapping in them)
	dd.set(idDiscSend, "")
	dd.click(t, idDiscFind)
	pumpUntil(t, "discover", func() bool { n, _ := ds.n(); return n == 1 && dd.cancel == nil })
	if r := ds.last(); r.ProfileID != "p1" || r.Profile != nil || r.Mapping != nil {
		t.Errorf("discover %+v", r)
	}
	dd.close()

	// an invalid mapping edit (e.g. from a configuration) never blocks test_profile
	w.ed.SetMapping(model.Mapping{Mode: model.MappingManual}, nil)
	tests := ds.count("test")
	w.click(t, idSetTest)
	pumpUntil(t, "test_profile", func() bool { return ds.count("test") == tests+1 && w.testCancel == nil })
	w.set(idSetDatabase, "db2") // edited connection: the form without the mapping
	w.click(t, idSetTest)
	pumpUntil(t, "test_profile 2", func() bool { return ds.count("test") == tests+2 && w.testCancel == nil })
	ds.mu.Lock()
	tr := ds.tests[len(ds.tests)-1]
	ds.mu.Unlock()
	if tr.Profile == nil || tr.Profile.Database != "db2" || tr.Profile.Mapping.Mode == model.MappingManual {
		t.Errorf("test_profile of the edited form: %+v", tr.Profile)
	}
	saves := ds.count("save")
	w.click(t, idSetSave)
	if ds.count("save") != saves || w.lastFocus != traysettings.FieldDiscoverButton ||
		!strings.Contains(w.statusText(), "Таблицы обмена — Ручной SQL") {
		t.Errorf("save with an invalid mapping: focus %q status %q", w.lastFocus, w.statusText())
	}
}

// TestCHK_TRAY_07_DialogQuestions — over hand edits a new search / another
// candidate asks; «Нет» keeps the edits; Esc / the close box ask with
// unapplied changes; a reply while the user typed keeps the edits.
func TestCHK_TRAY_07_DialogQuestions(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ds := newDiscService()
	conf := &fakeConfirm{no: true}
	app := startCfgApp(t, ds.cfgService, true, nil, conf)
	w := openAdmin(t, app)
	dd := openDisc(t, w)
	winapi.SendMessage(dd.hwnd, winapi.WM_COMMAND, winapi.IDCANCEL, 0) // Esc: nothing changed, no question
	if w.disc != nil || len(conf.texts) != 0 {
		t.Fatalf("unchanged dialog: open %v questions %q", w.disc != nil, conf.texts)
	}
	dd = openDisc(t, w)
	dd.click(t, idDiscFind)
	pumpUntil(t, "discover", func() bool { n, _ := ds.n(); return n == 1 && dd.cancel == nil })
	dd.set(idDiscSend, "_FldX")
	dd.click(t, idDiscFind)
	if n, _ := ds.n(); n != 1 || len(conf.texts) != 1 || dd.text(idDiscSend) != "_FldX" {
		t.Errorf("search over edits: reqs %d questions %q send %q", n, conf.texts, dd.text(idDiscSend))
	}
	winapi.SendMessage(dd.ctl[idDiscList], winapi.LB_SETCURSEL, 1, 0)
	dd.command(idDiscList, winapi.LBN_SELCHANGE)
	if dd.d.Sel != 0 || winapi.SendMessage(dd.ctl[idDiscList], winapi.LB_GETCURSEL, 0, 0) != 0 || dd.text(idDiscSend) != "_FldX" {
		t.Errorf("candidate switch refused: sel %d send %q", dd.d.Sel, dd.text(idDiscSend))
	}
	winapi.SendMessage(dd.hwnd, winapi.WM_COMMAND, winapi.IDCANCEL, 0)
	winapi.SendMessage(dd.hwnd, winapi.WM_CLOSE, 0, 0)
	if w.disc == nil || len(conf.texts) != 4 || conf.texts[3] != i18n.T(i18n.KeyDiscConfirmClose, i18n.RU) {
		t.Fatalf("close refused: open %v questions %q", w.disc != nil, conf.texts)
	}
	// a search reply after the user typed: edits kept, nothing selected
	conf.no = false
	ds.setBlock(false)
	ds.dmu.Lock()
	ds.hold = make(chan struct{})
	hold := ds.hold
	ds.dmu.Unlock()
	dd.click(t, idDiscFind) // asks (yes)
	pumpUntil(t, "discover held", func() bool { n, _ := ds.n(); return n == 2 })
	dd.set(idDiscPlanLabel, "typed while searching")
	ds.dmu.Lock()
	ds.hold = nil
	ds.dmu.Unlock()
	close(hold)
	pumpUntil(t, "discover 2", func() bool { return dd.cancel == nil })
	if dd.text(idDiscPlanLabel) != "typed while searching" || dd.d.Sel != -1 || len(dd.d.Candidates) != 2 {
		t.Errorf("reply over edits: label %q sel %d", dd.text(idDiscPlanLabel), dd.d.Sel)
	}
	winapi.SendMessage(dd.hwnd, winapi.WM_CLOSE, 0, 0)
	if w.disc != nil {
		t.Error("yes closes")
	}
}

// TestCHK_TRAY_07_MergeConfirm — «все планы обмена» with the profile's
// own plans: no duplicates, the replaced main mapping is confirmed first.
func TestCHK_TRAY_07_MergeConfirm(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ds := newDiscService()
	old := model.Mapping{Mode: model.MappingManual, Table: "_NODE40", PointNameColumn: "_Description", LastSendColumn: "_Fld1", PlanLabel: "Старый"}
	extra := model.Mapping{Mode: model.MappingManual, Schema: "DBO", Table: "_NODE20", PointNameColumn: "_Description", LastSendColumn: "_Fld2"}
	ds.profiles[0].Mapping, ds.profiles[0].Mappings = old, []model.Mapping{extra}
	conf := &fakeConfirm{no: true}
	app := startCfgApp(t, ds.cfgService, true, nil, conf)
	w := openAdmin(t, app)
	dd := openDisc(t, w)
	if dd.text(idDiscTable) != "_NODE40" || dd.text(idDiscSource) != i18n.T(i18n.KeyDiscSourceManual, i18n.RU) {
		t.Errorf("own mapping: %q %q", dd.text(idDiscTable), dd.text(idDiscSource))
	}
	dd.click(t, idDiscFind)
	pumpUntil(t, "discover", func() bool { n, _ := ds.n(); return n == 1 && dd.cancel == nil })
	winapi.SendMessage(dd.ctl[idDiscAllPlans], winapi.BM_SETCHECK, winapi.BST_CHECKED, 0)
	dd.click(t, idDiscApply)
	if w.disc == nil || w.ed.MapEdit != nil || len(conf.texts) != 1 || !strings.Contains(conf.texts[0], "Старый (_NODE40)") {
		t.Fatalf("removal not confirmed: %q", conf.texts)
	}
	conf.no = false
	dd.click(t, idDiscApply)
	if w.disc != nil || w.ed.MapEdit == nil || w.ed.MapEdit.Table != "_Node10" {
		t.Fatal("not applied after yes")
	}
	if ms := w.ed.MapsEdit; len(ms) != 1 || ms[0] != extra {
		t.Errorf("merged plans (the own _NODE20 kept, the found _Node20 not added): %+v", ms)
	}
}

// TestCHK_TRAY_07_ReconnectKeepsMapping — a get_config reply after a
// reconnection never wipes edits made while it was in flight (incl. the
// applied mapping); Apply waits while the window is busy.
func TestCHK_TRAY_07_ReconnectKeepsMapping(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ds := newDiscService()
	app := startCfgApp(t, ds.cfgService, true, nil)
	w := openAdmin(t, app)
	dd := openDisc(t, w)
	dd.click(t, idDiscFind)
	pumpUntil(t, "discover", func() bool { n, _ := ds.n(); return n == 1 && dd.cancel == nil })

	gets := ds.count("get")
	hold := make(chan struct{})
	ds.mu.Lock()
	ds.getHold, ds.rev = hold, 5
	ds.mu.Unlock()
	w.onSessionState(trayipc.Status{State: trayipc.StateUnavailable})
	w.onSessionState(trayipc.Status{State: trayipc.StateConnected}) // no edits yet: reload
	if !w.busy {
		t.Fatal("reload not started")
	}
	dd.click(t, idDiscApply) // busy: refused, the dialog stays
	if w.disc == nil || w.ed.MapEdit != nil || dd.text(idDiscResult) != i18n.T(i18n.KeyDiscBusy, i18n.RU) {
		t.Fatalf("apply while busy: %q", dd.text(idDiscResult))
	}
	// an edit made while get_config is in flight (as if applied)
	m := dd.d.Map.Mapping()
	w.ed.SetMapping(m, nil)
	ds.mu.Lock()
	ds.getHold = nil
	ds.mu.Unlock()
	close(hold)
	pumpUntil(t, "get_config", func() bool { return ds.count("get") == gets+1 && !w.busy })
	if w.ed.MapEdit == nil || *w.ed.MapEdit != m || w.ed.Revision != "r1" || w.statusText() != i18n.T(i18n.KeySetChangedOffline, i18n.RU) {
		t.Errorf("edit lost: mapEdit %v rev %q status %q", w.ed.MapEdit != nil, w.ed.Revision, w.statusText())
	}
	// Apply now works; a further reconnection only checks the revision
	w.ed.MapEdit = nil
	dd.click(t, idDiscApply)
	if w.disc != nil || w.ed.MapEdit == nil {
		t.Fatal("apply after the reply")
	}
	w.onSessionState(trayipc.Status{State: trayipc.StateUnavailable})
	w.onSessionState(trayipc.Status{State: trayipc.StateConnected})
	pumpUntil(t, "checkRevision", func() bool { return ds.count("get") == gets+2 && !w.busy })
	if w.ed.MapEdit == nil || w.ed.MapEdit.Table != "_Node10" {
		t.Error("the applied mapping must survive a reconnection")
	}
}

// TestCHK_TRAY_09_AutostartAfterReconnect — after a reconnection the
// state event comes before get_status: the autostart label follows the
// status push that brings trayAutostartError.
func TestCHK_TRAY_09_AutostartAfterReconnect(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	code := model.AutostartErrAccessDenied
	svc.autostart = &code
	app := startCfgApp(t, svc, true, &fakeElevator{})
	w := openAdmin(t, app)
	w.setPage(1)
	if got := w.text(idGenAutostartErr); got != i18n.T(i18n.KeyAutostartErrAccess, i18n.RU) {
		t.Fatalf("autostart error %q", got)
	}
	gets := svc.count("get")
	w.onSessionState(trayipc.Status{State: trayipc.StateUnavailable})
	svc.mu.Lock()
	code = ""
	svc.mu.Unlock()
	w.onSessionState(trayipc.Status{State: trayipc.StateConnected}) // state first
	pumpUntil(t, "reloaded", func() bool { return svc.count("get") == gets+1 && !w.busy })
	if w.text(idGenAutostartErr) == "" {
		t.Fatal("test premise: no get_status yet, the old value is shown")
	}
	go func() { // get_status of the session (status push, EventStatus)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		app.session.Refresh(ctx)
	}()
	pumpUntil(t, "label updated by the status push", func() bool { return w.text(idGenAutostartErr) == "" })
}

// TestCHK_TRAY_07_TimeoutLateReply — the dialog's own timeout
// (DiscoveryTimeout) shows the timeout text; a reply that comes after the
// dialog was closed is ignored; a failed command shows the full text
// (TLS 1.0 hint) in the result box.
func TestCHK_TRAY_07_TimeoutLateReply(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	old := traysettings.DiscoveryTimeout
	traysettings.DiscoveryTimeout = 300 * time.Millisecond
	defer func() { traysettings.DiscoveryTimeout = old }()
	ds := newDiscService()
	app := startCfgApp(t, ds.cfgService, true, nil)
	w := openAdmin(t, app)
	dd := openDisc(t, w)

	ds.setBlock(true)
	dd.click(t, idDiscFind)
	pumpUntil(t, "timeout", func() bool { return dd.cancel == nil && dd.text(idDiscResult) != "" })
	if dd.text(idDiscResult) != i18n.Message(model.MsgDiscoverTimeout, i18n.RU) {
		t.Errorf("timeout text %q", dd.text(idDiscResult))
	}
	ds.setBlock(false)
	traysettings.DiscoveryTimeout = old

	// failed with a long key: the whole text in the multi-line box
	ds.dmu.Lock()
	ds.failErr = &model.IPCError{Code: model.ErrCodeFailed, Key: model.MsgSQLTLSEnableTLS10, Message: "tls handshake"}
	ds.dmu.Unlock()
	dd.click(t, idDiscFind)
	pumpUntil(t, "failed", func() bool { n, _ := ds.n(); return n == 2 && dd.cancel == nil })
	if got := dd.text(idDiscResult); got != i18n.Message(model.MsgSQLTLSEnableTLS10, i18n.RU) || !strings.Contains(got, "TLS 1.0") {
		t.Errorf("failed text %q", got)
	}

	// a late reply: the request outlives the dialog
	hold := make(chan struct{})
	ds.dmu.Lock()
	ds.hold = hold
	ds.dmu.Unlock()
	dd.click(t, idDiscFind)
	pumpUntil(t, "held", func() bool { n, _ := ds.n(); return n == 3 })
	dd.close()
	dd2 := openDisc(t, w)
	ds.dmu.Lock()
	ds.hold = nil
	ds.dmu.Unlock()
	close(hold)
	time.Sleep(100 * time.Millisecond)
	PumpPending()
	if len(dd2.d.Candidates) != 0 || dd2.text(idDiscResult) != "" || w.ed.MapEdit != nil || !dd.closed {
		t.Errorf("late reply reached the new dialog: %d candidates, result %q", len(dd2.d.Candidates), dd2.text(idDiscResult))
	}
	dd2.close()
}

// TestCHK_TRAY_07_SecondSettingsAndLayout — a second «Настройки» while
// the dialog is open brings the dialog (not the disabled owner) to the
// front; both windows fit the work area (Windows 7 at 1024×768).
func TestCHK_TRAY_07_SecondSettingsAndLayout(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ds := newDiscService()
	app := startCfgApp(t, ds.cfgService, true, nil)
	w := openAdmin(t, app)
	dd := openDisc(t, w)
	if h := app.OpenSettings(); h != dd.hwnd {
		t.Errorf("second Settings returned %#x, want the dialog %#x", h, dd.hwnd)
	}
	if winapi.FindWindow(DiscoveryClass) != dd.hwnd {
		t.Error("the settings process finds the dialog by its class")
	}
	w.openDiscovery() // e.g. a stray command: the open dialog stays the only one
	if w.disc != dd || winapi.IsWindowEnabled(w.ctl[idSetDiscover]) || winapi.IsWindowEnabled(w.hwnd) {
		t.Error("one dialog, owner disabled")
	}
	dd.close()

	work := winapi.RECT{Right: 1024, Bottom: 728} // 1024×768 minus a 40 px taskbar
	r := winapi.RECT{Right: discCW, Bottom: discCH}
	winapi.AdjustWindowRectEx(&r, discStyle, 0)
	x, y, cw, ch := fitRect(r.Right-r.Left, r.Bottom-r.Top, work)
	if cw != r.Right-r.Left || ch != r.Bottom-r.Top || x < 0 || y < 0 || x+cw > 1024 || y+ch > 728 {
		t.Errorf("dialog does not fit 1024×728: %d,%d %dx%d", x, y, cw, ch)
	}
	if x, y, cw, ch := fitRect(1200, 900, work); x != 0 || y != 0 || cw != 1024 || ch != 728 {
		t.Errorf("clamp: %d,%d %dx%d", x, y, cw, ch)
	}
	if x, y, _, _ := fitRect(400, 300, winapi.RECT{Left: 100, Top: 50, Right: 900, Bottom: 650}); x != 300 || y != 150 {
		t.Errorf("offset work area: %d,%d", x, y)
	}
	wx, wy, ww, wh := windowRect(discCW, discCH, discStyle, 0)
	if real, ok := winapi.WorkArea(); ok && (wx < real.Left || wy < real.Top || wx+ww > real.Right || wy+wh > real.Bottom) {
		t.Errorf("dialog outside the work area %+v: %d,%d %dx%d", real, wx, wy, ww, wh)
	}
	// «Время обмена (UTC+3):» / «Exchange time (UTC+3):» fit their label (≈6 px a character)
	for _, lang := range []string{i18n.RU, i18n.EN} {
		if n := len([]rune(i18n.T(i18n.KeyDiscTime, lang))); n*6 > 147 {
			t.Errorf("%s time label too long: %d characters", lang, n)
		}
	}
}
