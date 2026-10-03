//go:build windows

package wintray

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

type saveCall struct {
	p        model.Profile
	password string
	clear    bool
	rev      string
}

// cfgService is a fake version-2 service with get_config / save_* /
// test_profile (G5 / G6) and a counter-based revision.
type cfgService struct {
	mu       sync.Mutex
	profiles []model.Profile
	rev      int
	saves    []saveCall
	deletes  []string
	polls    []string
	tests    []model.IPCRequest
	canceled int
	gets     int
	// saveErr: reply to the next save_profile
	saveErr error
	// test: "ok", "busy", "rate", "timeout", "block"
	test string
}

func (f *cfgService) revision() string { return "r" + strconv.Itoa(f.rev) }

func (f *cfgService) Snapshot() model.StatusSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	snap := model.StatusSnapshot{Seq: uint64(f.rev + 1), ConfigRevision: f.revision()}
	for _, p := range f.profiles {
		snap.Profiles = append(snap.Profiles, model.ProfileSnapshot{ProfileID: p.ID, Name: p.Name, Enabled: p.Enabled,
			Connection: &model.ProfileConnection{Server: p.Server, Port: p.Port, Database: p.Database, AuthMode: p.AuthMode,
				Login: p.Login, HasPassword: p.HasPassword, Encrypt: p.Encrypt, TLSMinVersion: p.TLSMinVersion, ServerPreset: p.ServerPreset},
			MappingSummary: &model.MappingSummary{Configured: p.Mapping.Configured()}})
	}
	return snap
}
func (f *cfgService) PollNow(id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls = append(f.polls, id)
	return false, nil
}
func (f *cfgService) ReloadConfig() bool { return false }
func (f *cfgService) Heartbeat() model.Heartbeat {
	return model.Heartbeat{Protocol: 2, ServiceStartedAt: time.Unix(100, 0)}
}

func (f *cfgService) TestProfile(ctx context.Context, req model.IPCRequest) (model.TestProfileResult, error) {
	f.mu.Lock()
	f.tests = append(f.tests, req)
	mode := f.test
	f.mu.Unlock()
	switch mode {
	case "busy":
		return model.TestProfileResult{}, &model.IPCError{Code: model.ErrCodeBusy, Key: model.MsgTestBusy, Message: "busy"}
	case "rate":
		return model.TestProfileResult{}, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgMonitorRateLimited, Message: "rate"}
	case "timeout":
		return model.TestProfileResult{Category: model.TestTimeout, ErrorKey: model.MsgTestTimeout, Encrypt: "disable", DurationMs: 15000}, nil
	case "block":
		<-ctx.Done()
		f.mu.Lock()
		f.canceled++
		f.mu.Unlock()
		return model.TestProfileResult{}, ctx.Err()
	}
	return model.TestProfileResult{OK: true, Category: model.TestOK, ServerVersion: "10.50.1600.1", ProductLevel: "RTM",
		CompatLevel: 100, Encrypt: "disable", DurationMs: 12}, nil
}

func (f *cfgService) GetConfig() (model.ConfigView, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	return model.ConfigView{Settings: model.DefaultSettings(), Profiles: append([]model.Profile(nil), f.profiles...)}, f.revision(), nil
}

func (f *cfgService) SaveProfile(p model.Profile, password string, clear bool, rev, account string) (model.SavedProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves = append(f.saves, saveCall{p, password, clear, rev})
	if err := f.saveErr; err != nil {
		f.saveErr = nil
		return model.SavedProfile{}, err
	}
	if rev != f.revision() {
		return model.SavedProfile{}, &model.IPCError{Code: model.ErrCodeConflict, Key: model.MsgMonitorConfigConflict, Message: "conflict"}
	}
	if p.ID == "" {
		p.ID = "id" + strconv.Itoa(len(f.profiles)+1)
	}
	idx := -1
	for i := range f.profiles {
		if f.profiles[i].ID == p.ID {
			idx = i
		}
	}
	switch {
	case p.AuthMode == model.AuthWindows || clear:
		p.HasPassword = false
	case password != "":
		p.HasPassword = true
	case idx >= 0:
		p.HasPassword = f.profiles[idx].HasPassword
	}
	if idx < 0 {
		f.profiles = append(f.profiles, p)
	} else {
		f.profiles[idx] = p
	}
	f.rev++
	return model.SavedProfile{Profile: p, Revision: f.revision(), PollScheduled: p.Enabled}, nil
}

func (f *cfgService) DeleteProfile(id, rev, account string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, id)
	if rev != f.revision() {
		return "", &model.IPCError{Code: model.ErrCodeConflict, Key: model.MsgMonitorConfigConflict, Message: "conflict"}
	}
	for i := range f.profiles {
		if f.profiles[i].ID == id {
			f.profiles = append(f.profiles[:i], f.profiles[i+1:]...)
			break
		}
	}
	f.rev++
	return f.revision(), nil
}

func (f *cfgService) SaveSettings(model.Settings, string, string) (string, error) {
	return "", errors.New("not used")
}

func (f *cfgService) lastSave() saveCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saves[len(f.saves)-1]
}

func (f *cfgService) count(what string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch what {
	case "save":
		return len(f.saves)
	case "delete":
		return len(f.deletes)
	case "poll":
		return len(f.polls)
	case "test":
		return len(f.tests)
	case "canceled":
		return f.canceled
	case "get":
		return f.gets
	}
	return -1
}

func newCfgService() *cfgService {
	p := model.NewProfile("p1")
	p.Name, p.Server, p.Database, p.Login, p.HasPassword = "Склад", `srv\SQL1`, "db1", "mon", true
	return &cfgService{profiles: []model.Profile{p}, rev: 1}
}

type fakeElevator struct {
	err   error
	calls int
}

func (e *fakeElevator) Elevate(uintptr) error { e.calls++; return e.err }

// fakeConfirm answers the Yes/No questions of the settings window.
type fakeConfirm struct {
	no    bool
	texts []string
}

func (c *fakeConfirm) ask(_ uintptr, text, _ string) bool {
	c.texts = append(c.texts, text)
	return !c.no
}

func startCfgApp(t *testing.T, svc *cfgService, admin bool, el Elevator, conf ...*fakeConfirm) *App {
	t.Helper()
	l := ipc.NewMemListener()
	srv := ipc.NewServer(svc, nil)
	srv.Authorize = func(io.ReadWriteCloser) (ipc.Peer, error) { return ipc.Peer{Admin: admin, Account: `PC\admin`}, nil }
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	dial := func(context.Context) (io.ReadWriteCloser, error) { return l.Dial() }
	c := &fakeConfirm{}
	if len(conf) > 0 {
		c = conf[0]
	}
	app, err := NewApp(Options{Lang: i18n.RU, NoNotifyIcon: true, HiddenAlerts: true, HiddenSettings: true, Beeper: &testBeeper{},
		Elevator: el, Confirm: c.ask,
		Session: trayipc.Options{Dial: dial, BackoffMin: 10 * time.Millisecond, BackoffMax: 20 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Exit)
	pumpUntil(t, "connected with clientElevated", func() bool {
		st := app.Status()
		return st.State == trayipc.StateConnected && st.ClientElevated != nil && st.Snapshot != nil
	})
	return app
}

func (w *settingsWindow) set(id int32, s string) {
	winapi.SendMessageString(w.ctl[id], winapi.WM_SETTEXT, 0, s)
}

func (w *settingsWindow) click(t *testing.T, id int32) {
	t.Helper()
	h := w.ctl[id]
	if h == 0 || !winapi.IsWindowEnabled(h) {
		t.Fatalf("control %d missing or disabled", id)
	}
	winapi.SendMessage(h, winapi.BM_CLICK, 0, 0)
}

func (w *settingsWindow) statusText() string { return winapi.GetWindowText(w.ctl[idSetStatus]) }

func openAdmin(t *testing.T, app *App) *settingsWindow {
	t.Helper()
	if app.OpenSettings() == 0 || app.settings == nil || app.settings.ed.Mode != traysettings.Admin {
		t.Fatal("admin settings window not opened")
	}
	w := app.settings
	pumpUntil(t, "get_config loaded", func() bool { return !w.busy && w.ed.Revision != "" })
	return w
}

// TestCHK_TRAY_06_SettingsSave — elevated tray: get_config, edit,
// save_profile with expectedRevision (keep the password), the new
// revision of the reply is used next; conflict offers «Перечитать»;
// a service field error focuses the field; local validation does not
// send; clear password; new profile; delete.
func TestCHK_TRAY_06_SettingsSave(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	app := startCfgApp(t, svc, true, nil)
	w := openAdmin(t, app)
	if n := winapi.SendMessage(w.ctl[idSetList], winapi.LB_GETCOUNT, 0, 0); n != 1 || w.text(idSetHost) != "srv" || w.text(idSetInstance) != "SQL1" {
		t.Fatalf("list %d host %q inst %q", n, w.text(idSetHost), w.text(idSetInstance))
	}
	if !strings.Contains(winapi.GetWindowText(w.ctl[idSetHints]), "db_datareader") {
		t.Error("plain-text warning next to «no encryption»")
	}
	if winapi.GetWindowText(w.ctl[idSetPwHint]) != i18n.T(i18n.KeySetPwKeep, i18n.RU) {
		t.Errorf("password hint %q", winapi.GetWindowText(w.ctl[idSetPwHint]))
	}

	w.set(idSetDatabase, "db2")
	w.click(t, idSetSave)
	pumpUntil(t, "saved", func() bool { return svc.count("save") == 1 && !w.busy })
	sc := svc.lastSave()
	if sc.rev != "r1" || sc.password != "" || sc.clear || sc.p.Database != "db2" || sc.p.ID != "p1" {
		t.Errorf("save call %+v", sc)
	}
	if w.ed.Revision != "r2" || !strings.Contains(w.statusText(), "Сохранено") {
		t.Errorf("after save rev %q status %q", w.ed.Revision, w.statusText())
	}

	// another administrator changed the file
	svc.mu.Lock()
	svc.rev = 7
	svc.mu.Unlock()
	w.set(idSetName, "Склад-2")
	w.click(t, idSetSave)
	pumpUntil(t, "conflict", func() bool { return svc.count("save") == 2 && !w.busy })
	if sc = svc.lastSave(); sc.rev != "r2" || !strings.Contains(w.statusText(), "Перечитать") {
		t.Errorf("conflict: rev %q status %q", sc.rev, w.statusText())
	}
	w.click(t, idSetReload)
	pumpUntil(t, "reloaded", func() bool { return !w.busy && w.ed.Revision == "r7" })

	// field error of the service -> focus
	svc.mu.Lock()
	svc.saveErr = &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorProfileInvalid, Message: `profile "p1": port`,
		Args: []string{"port", model.ValRange, "0", "65535", "99999"}}
	svc.mu.Unlock()
	w.click(t, idSetSave)
	pumpUntil(t, "field error", func() bool { return svc.count("save") == 3 && !w.busy })
	if w.lastFocus != traysettings.FieldPort || !strings.Contains(w.statusText(), "«Порт»") {
		t.Errorf("focus %q status %q", w.lastFocus, w.statusText())
	}
	// service: the saved password cannot be reused -> hint at the password
	svc.mu.Lock()
	svc.saveErr = &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgTestPasswordRequired, Message: "enter the password",
		Args: []string{"password", model.ValRequired}}
	svc.mu.Unlock()
	w.click(t, idSetSave)
	pumpUntil(t, "password required", func() bool { return svc.count("save") == 4 && !w.busy })
	if w.lastFocus != traysettings.FieldPassword || w.statusText() != "Введите пароль." {
		t.Errorf("password required: focus %q status %q", w.lastFocus, w.statusText())
	}

	// local validation: nothing is sent
	w.set(idSetHost, "")
	w.click(t, idSetSave)
	PumpPending()
	if svc.count("save") != 4 || w.lastFocus != traysettings.FieldHost {
		t.Errorf("empty host: saves %d focus %q", svc.count("save"), w.lastFocus)
	}
	w.set(idSetHost, "srv")
	w.set(idSetPort, "1500")
	if !strings.Contains(winapi.GetWindowText(w.ctl[idSetHints]), "SQL Browser") {
		t.Error("port-over-instance hint")
	}

	// delete the saved password
	w.click(t, idSetClearPw)
	if winapi.GetWindowText(w.ctl[idSetPwHint]) != i18n.T(i18n.KeySetPwClear, i18n.RU) {
		t.Errorf("clear hint %q", winapi.GetWindowText(w.ctl[idSetPwHint]))
	}
	w.click(t, idSetSave)
	pumpUntil(t, "saved with clearPassword", func() bool { return svc.count("save") == 5 && !w.busy })
	if sc = svc.lastSave(); !sc.clear || sc.password != "" || sc.rev != "r7" || sc.p.Port != 1500 {
		t.Errorf("clear save %+v", sc)
	}
	if w.ed.Base.HasPassword || winapi.IsWindowEnabled(w.ctl[idSetClearPw]) {
		t.Error("password still shown as saved")
	}

	// new profile with a password
	w.click(t, idSetAdd)
	if !w.ed.IsNew || winapi.SendMessage(w.ctl[idSetList], winapi.LB_GETCOUNT, 0, 0) != 2 {
		t.Fatal("new profile row")
	}
	w.set(idSetHost, "h2")
	w.set(idSetDatabase, "d2")
	w.set(idSetLogin, "l2")
	w.click(t, idSetSave)
	PumpPending()
	if svc.count("save") != 5 || w.lastFocus != traysettings.FieldPassword {
		t.Errorf("new without password: saves %d focus %q", svc.count("save"), w.lastFocus)
	}
	w.set(idSetPassword, "secret")
	w.click(t, idSetSave)
	pumpUntil(t, "new saved", func() bool { return svc.count("save") == 6 && !w.busy })
	if sc = svc.lastSave(); sc.password != "secret" || sc.p.ID != "" || sc.p.Encrypt != model.EncryptDisable {
		t.Errorf("new save %+v", sc.p)
	}
	if w.ed.IsNew || w.ed.Base.ID != "id2" || w.text(idSetPassword) != "" || len(w.ed.Profiles) != 2 {
		t.Errorf("after new save: %+v pw %q", w.ed.Base, w.text(idSetPassword))
	}

	// Windows authentication drops the password (warned)
	winapi.SendMessage(w.ctl[idSetAuth], winapi.CB_SETCURSEL, 1, 0)
	w.command(idSetAuth, winapi.CBN_SELCHANGE)
	if winapi.GetWindowText(w.ctl[idSetPwHint]) != i18n.T(i18n.KeySetPwWindowsDrop, i18n.RU) || winapi.IsWindowEnabled(w.ctl[idSetPassword]) {
		t.Errorf("windows hint %q", winapi.GetWindowText(w.ctl[idSetPwHint]))
	}
	w.click(t, idSetSave)
	pumpUntil(t, "windows saved", func() bool { return svc.count("save") == 7 && !w.busy })
	if sc = svc.lastSave(); sc.p.AuthMode != model.AuthWindows || sc.p.Login != "" || sc.password != "" {
		t.Errorf("windows save %+v", sc)
	}

	// delete
	w.click(t, idSetDelete)
	pumpUntil(t, "deleted", func() bool { return svc.count("delete") == 1 && !w.busy })
	if len(w.ed.Profiles) != 1 || !strings.Contains(w.statusText(), "удалена") {
		t.Errorf("after delete %d %q", len(w.ed.Profiles), w.statusText())
	}

	// poll now
	w.click(t, idSetPoll)
	pumpUntil(t, "poll_now", func() bool { return svc.count("poll") == 1 && !w.busy })
	if !strings.Contains(w.statusText(), "Опрос") {
		t.Errorf("poll status %q", w.statusText())
	}
	winapi.SendMessage(w.hwnd, winapi.WM_CLOSE, 0, 0)
	if app.settings != nil || winapi.IsWindow(w.hwnd) {
		t.Error("window not closed")
	}
}

// TestCHK_TRAY_06_SettingsOtherProfileInvalid — another profile of the
// file fails validation: shown with its name, no focus.
func TestCHK_TRAY_06_SettingsOtherProfileInvalid(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	bad := model.NewProfile("bad")
	bad.Name, bad.Server, bad.Database, bad.Login = "Ручной", "x", "y", "z"
	svc.profiles = append(svc.profiles, bad)
	svc.saveErr = &model.IPCError{Code: model.ErrCodeInvalidRequest, Key: model.MsgMonitorProfileInvalid,
		Message: `monitor: profile "bad": mapping: point name/id column is required`, Args: []string{"mapping.pointNameColumn", model.ValRequired},
		ProfileID: "bad"}
	app := startCfgApp(t, svc, true, nil)
	w := openAdmin(t, app)
	w.click(t, idSetSave)
	pumpUntil(t, "reply", func() bool { return svc.count("save") == 1 && !w.busy })
	if st := w.statusText(); !strings.Contains(st, "Ручной") || w.lastFocus != traysettings.FieldNone {
		t.Errorf("status %q focus %q", st, w.lastFocus)
	}
}

// TestCHK_TRAY_06_SettingsAccessDenied — a non-elevated client gets
// access_denied for get_config: the window explains that administrator
// rights are needed.
func TestCHK_TRAY_06_SettingsAccessDenied(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	app := startCfgApp(t, svc, false, nil)
	w, err := newSettingsWindow(settingsConfig{lang: i18n.RU, mode: traysettings.Admin, session: app.session, hidden: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	w.Reload()
	pumpUntil(t, "access_denied", func() bool { return !w.busy && strings.Contains(w.statusText(), "администратора") })
	if w.ed.HasProfile() {
		t.Error("profiles without get_config")
	}
}

// TestCHK_TRAY_05_SettingsTestProfile — test_profile: success with
// version and compatibility level, busy, rate_limited, timeout result,
// «Отменить проверку» and closing the window cancel (cancel {ref}).
func TestCHK_TRAY_05_SettingsTestProfile(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	app := startCfgApp(t, svc, true, nil)
	w := openAdmin(t, app)
	run := func(mode string) string {
		t.Helper()
		svc.mu.Lock()
		svc.test = mode
		svc.mu.Unlock()
		n := svc.count("test")
		w.click(t, idSetTest)
		pumpUntil(t, "test "+mode, func() bool { return svc.count("test") == n+1 && w.testCancel == nil })
		return w.statusText()
	}
	if st := run("ok"); !strings.Contains(st, "10.50.1600.1 RTM") || !strings.Contains(st, "100") || !strings.Contains(st, "успешно") {
		t.Errorf("ok: %q", st)
	}
	svc.mu.Lock()
	req := svc.tests[0]
	svc.mu.Unlock()
	if req.ProfileID != "p1" || req.Profile != nil {
		t.Errorf("unchanged profile is tested by id: %+v", req)
	}
	if st := run("busy"); st != i18n.Message(model.MsgTestBusy, i18n.RU) {
		t.Errorf("busy: %q", st)
	}
	if st := run("rate"); !strings.Contains(st, "10 секунд") {
		t.Errorf("rate_limited: %q", st)
	}
	w.set(idSetPassword, "pw2")
	if st := run("timeout"); !strings.Contains(st, i18n.Message(model.MsgTestTimeout, i18n.RU)) || !strings.Contains(st, "15") {
		t.Errorf("timeout: %q", st)
	}
	svc.mu.Lock()
	req = svc.tests[len(svc.tests)-1]
	svc.mu.Unlock()
	if req.Profile == nil || req.Password != "pw2" {
		t.Errorf("edited profile is sent with the password: %+v", req)
	}

	svc.mu.Lock()
	svc.test = "block"
	svc.mu.Unlock()
	w.click(t, idSetTest)
	pumpUntil(t, "test running", func() bool { return svc.count("test") == 5 })
	if winapi.IsWindowEnabled(w.ctl[idSetTest]) {
		t.Error("test button enabled while testing")
	}
	w.click(t, idSetCancelTest)
	pumpUntil(t, "canceled by the button", func() bool { return svc.count("canceled") == 1 && w.testCancel == nil })
	if st := w.statusText(); st != i18n.T(i18n.KeySetTestCanceled, i18n.RU) {
		t.Errorf("cancel status %q", st)
	}

	w.click(t, idSetTest)
	pumpUntil(t, "test running again", func() bool { return svc.count("test") == 6 })
	winapi.SendMessage(w.hwnd, winapi.WM_CLOSE, 0, 0)
	pumpUntil(t, "canceled on close", func() bool { return svc.count("canceled") == 2 })
	if app.settings != nil {
		t.Error("settings window still referenced")
	}
}

// TestCHK_TRAY_04_SettingsReadOnly — an ordinary tray: the window is
// read-only from get_status (no get_config), no Save; test by id and
// poll_now work; «Изменить (администратор)» calls the elevator (UAC
// refused -> message; started -> the read-only window closes).
func TestCHK_TRAY_04_SettingsReadOnly(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	el := &fakeElevator{err: syscall.Errno(winapi.ERROR_CANCELLED)}
	app := startCfgApp(t, svc, false, el)
	app.Command(1001) // trayui.CmdSettings
	w := app.settings
	if w == nil || w.ed.Mode != traysettings.ReadOnly {
		t.Fatal("read-only window not opened")
	}
	if w.ctl[idSetSave] != 0 || winapi.IsWindowEnabled(w.ctl[idSetAuth]) || winapi.IsWindowEnabled(w.ctl[idSetPassword]) {
		t.Error("read-only window is editable")
	}
	if w.text(idSetHost) != "srv" || w.text(idSetDatabase) != "db1" || w.ed.Revision != "r1" {
		t.Errorf("read-only data host %q db %q rev %q", w.text(idSetHost), w.text(idSetDatabase), w.ed.Revision)
	}
	if winapi.GetWindowText(w.ctl[idSetPwHint]) != i18n.T(i18n.KeySetPwSaved, i18n.RU) {
		t.Errorf("password hint %q", winapi.GetWindowText(w.ctl[idSetPwHint]))
	}
	w.set(idSetHost, "typed") // read-only edit: ignored by the request
	w.click(t, idSetTest)
	pumpUntil(t, "test", func() bool { return svc.count("test") == 1 && w.testCancel == nil })
	svc.mu.Lock()
	req := svc.tests[0]
	svc.mu.Unlock()
	if req.Profile != nil || req.ProfileID != "p1" {
		t.Errorf("read-only test %+v", req)
	}
	w.click(t, idSetPoll)
	pumpUntil(t, "poll", func() bool { return svc.count("poll") == 1 && !w.busy })
	if svc.count("save") != 0 {
		t.Error("read-only window saved")
	}

	w.click(t, idSetElevate)
	if el.calls != 1 || w.statusText() != i18n.T(i18n.KeySetElevateCanceled, i18n.RU) {
		t.Errorf("elevate canceled: calls %d status %q", el.calls, w.statusText())
	}
	el.err = nil
	w.click(t, idSetElevate)
	if el.calls != 2 || app.settings != nil || winapi.IsWindow(w.hwnd) {
		t.Error("read-only window must close after the elevated one started")
	}
}

// TestCHK_TRAY_04_SettingsDiscardPrompt — unsaved edits: switching the
// profile, «Добавить», «Перечитать» and closing ask «Отменить
// изменения?» («Нет» keeps the edits); no question without edits.
func TestCHK_TRAY_04_SettingsDiscardPrompt(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	p2 := model.NewProfile("p2")
	p2.Name, p2.Server, p2.Database, p2.AuthMode = "Филиал", "h2", "d2", model.AuthWindows
	svc.profiles = append(svc.profiles, p2)
	conf := &fakeConfirm{}
	app := startCfgApp(t, svc, true, nil, conf)
	w := openAdmin(t, app)
	sel := func(i int) {
		winapi.SendMessage(w.ctl[idSetList], winapi.LB_SETCURSEL, uintptr(i), 0)
		w.command(idSetList, winapi.LBN_SELCHANGE)
	}
	want := func(n int, what string) {
		t.Helper()
		if len(conf.texts) != n {
			t.Fatalf("%s: %d questions %q", what, len(conf.texts), conf.texts)
		}
		if n > 0 && conf.texts[n-1] != "Отменить изменения?" {
			t.Fatalf("%s: question %q", what, conf.texts[n-1])
		}
	}
	sel(1) // no edits: no question
	sel(0)
	want(0, "switch without edits")

	w.set(idSetName, "правка")
	conf.no = true
	sel(1)
	want(1, "switch with edits")
	if w.ed.Sel != 0 || w.text(idSetName) != "правка" || int(winapi.SendMessage(w.ctl[idSetList], winapi.LB_GETCURSEL, 0, 0)) != 0 {
		t.Error("«Нет» must keep the edits and the selection")
	}
	w.click(t, idSetAdd)
	want(2, "add with edits")
	if w.ed.IsNew {
		t.Error("«Нет» on «Добавить» created a profile")
	}
	w.click(t, idSetReload)
	want(3, "reload with edits")
	if w.busy || w.text(idSetName) != "правка" {
		t.Error("«Нет» on «Перечитать» reloaded")
	}
	winapi.SendMessage(w.hwnd, winapi.WM_CLOSE, 0, 0)
	want(4, "close with edits")
	if !winapi.IsWindow(w.hwnd) {
		t.Fatal("«Нет» on close closed the window")
	}

	conf.no = false
	w.click(t, idSetAdd)
	want(5, "add, yes")
	if !w.ed.IsNew {
		t.Fatal("«Да» on «Добавить» must start a new profile")
	}
	sel(1) // untouched new profile: no question
	want(5, "untouched new profile")
	if w.ed.Base.ID != "p2" {
		t.Errorf("selected %q", w.ed.Base.ID)
	}
	w.click(t, idSetReload)
	pumpUntil(t, "reloaded", func() bool { return !w.busy })
	want(5, "reload without edits")
	w.set(idSetDatabase, "d3")
	w.click(t, idSetClose)
	want(6, "close button with edits")
	if winapi.IsWindow(w.hwnd) || app.settings != nil {
		t.Error("«Да» on close must close the window")
	}
}

// TestCHK_TRAY_06_SettingsPasswordReads — the password box is read only
// for save / test (readPassword, wiped UTF-16 buffer); editing other
// fields, readForm and EN_CHANGE of the password only look at its length.
func TestCHK_TRAY_06_SettingsPasswordReads(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	app := startCfgApp(t, svc, true, &fakeElevator{})
	w := openAdmin(t, app)
	w.set(idSetPassword, "s1")
	w.set(idSetHost, "srv2")
	w.readForm()
	w.command(idSetPassword, winapi.EN_CHANGE)
	if w.pwReads != 0 || !w.ed.Form.PasswordTyped || !w.ed.Unsaved() {
		t.Fatalf("password read outside save/test: reads %d typed %v", w.pwReads, w.ed.Form.PasswordTyped)
	}
	w.click(t, idSetSave)
	pumpUntil(t, "saved", func() bool { return svc.count("save") == 1 && !w.busy })
	if w.pwReads != 1 || svc.lastSave().password != "s1" {
		t.Errorf("save: reads %d password %q", w.pwReads, svc.lastSave().password)
	}
	if w.ed.Form.PasswordTyped || w.text(idSetPassword) != "" {
		t.Error("password box must be cleared after save")
	}
	w.set(idSetDatabase, "db3") // edit without a password (same server: reuse): no read
	w.click(t, idSetSave)
	pumpUntil(t, "saved 2", func() bool { return svc.count("save") == 2 && !w.busy })
	if w.pwReads != 1 || svc.lastSave().password != "" {
		t.Errorf("save without password: reads %d password %q", w.pwReads, svc.lastSave().password)
	}

	buf := winapi.GetWindowTextUTF16(w.ctl[idSetDatabase])
	if string(utf16.Decode(buf)) != "db3" {
		t.Errorf("GetWindowTextUTF16 %q", string(utf16.Decode(buf)))
	}
	winapi.ZeroUTF16(buf)
	for _, c := range buf {
		if c != 0 {
			t.Fatal("ZeroUTF16 left data")
		}
	}
}

// TestCHK_TRAY_04_SettingsWindowClasses — the read-only window has its own
// class, so FindWindow(SettingsClass) of `--settings` finds only the
// administrator window.
func TestCHK_TRAY_04_SettingsWindowClasses(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app := startCfgApp(t, newCfgService(), false, &fakeElevator{})
	app.Command(1001) // trayui.CmdSettings
	w := app.settings
	if w == nil || w.ed.Mode != traysettings.ReadOnly {
		t.Fatal("read-only window not opened")
	}
	// (hidden windows of earlier tests may still exist in this process, so
	// the class of each window is checked, not only the first FindWindow hit)
	if c := winapi.GetClassName(w.hwnd); c != SettingsViewClass {
		t.Errorf("read-only window class %q, want %q", c, SettingsViewClass)
	}
	if h := winapi.FindWindow(SettingsClass); h == w.hwnd {
		t.Error("FindWindow(SettingsClass) found the read-only window")
	}
	if h := winapi.FindWindow(SettingsViewClass); h == 0 || winapi.GetClassName(h) != SettingsViewClass {
		t.Errorf("FindWindow(SettingsViewClass) = %#x", h)
	}
	w.close()

	admin := startCfgApp(t, newCfgService(), true, &fakeElevator{})
	aw := openAdmin(t, admin)
	if c := winapi.GetClassName(aw.hwnd); c != SettingsClass {
		t.Errorf("admin window class %q, want %q", c, SettingsClass)
	}
	if h := winapi.FindWindow(SettingsClass); h == 0 || winapi.GetClassName(h) != SettingsClass {
		t.Errorf("FindWindow(SettingsClass) = %#x", h)
	}
}

// TestCHK_TRAY_06_SettingsReconnect — «Нет связи» while disconnected; on
// reconnection get_config is read again: without edits the form is
// reloaded, with edits the form and revision stay and a changed
// configuration is reported.
func TestCHK_TRAY_06_SettingsReconnect(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := newCfgService()
	app := startCfgApp(t, svc, true, &fakeElevator{})
	w := openAdmin(t, app)
	down := trayipc.Status{State: trayipc.StateUnavailable}
	up := trayipc.Status{State: trayipc.StateConnected}
	gets := svc.count("get")

	w.onSessionState(down)
	if w.statusText() != i18n.T(i18n.KeySetNotConnected, i18n.RU) {
		t.Errorf("disconnected status %q", w.statusText())
	}
	svc.mu.Lock()
	svc.profiles[0].Database, svc.rev = "db9", 2
	svc.mu.Unlock()
	w.onSessionState(up)
	pumpUntil(t, "reloaded", func() bool { return svc.count("get") == gets+1 && !w.busy })
	if w.statusText() != "" || w.ed.Revision != "r2" || w.text(idSetDatabase) != "db9" {
		t.Errorf("reload after reconnect: status %q rev %q db %q", w.statusText(), w.ed.Revision, w.text(idSetDatabase))
	}
	w.onSessionState(up) // no transition: nothing
	if svc.count("get") != gets+1 {
		t.Error("get_config without a reconnection")
	}

	w.set(idSetHost, "edited")
	w.onSessionState(down)
	w.onSessionState(up) // same revision: the edit stays, status cleared
	pumpUntil(t, "checked", func() bool { return svc.count("get") == gets+2 && !w.busy })
	if w.statusText() != "" || w.text(idSetHost) != "edited" || !w.ed.Unsaved() {
		t.Errorf("same revision: status %q host %q", w.statusText(), w.text(idSetHost))
	}
	w.onSessionState(down)
	svc.mu.Lock()
	svc.rev = 3
	svc.mu.Unlock()
	w.onSessionState(up)
	pumpUntil(t, "checked 2", func() bool { return svc.count("get") == gets+3 && !w.busy })
	if w.statusText() != i18n.T(i18n.KeySetChangedOffline, i18n.RU) || w.text(idSetHost) != "edited" || w.ed.Revision != "r2" {
		t.Errorf("changed revision: status %q host %q rev %q", w.statusText(), w.text(idSetHost), w.ed.Revision)
	}
}
