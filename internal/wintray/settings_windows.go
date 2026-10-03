//go:build windows

package wintray

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// SettingsClass is the window class of the settings window (FindWindow
// of a second `exchtray --settings`).
const SettingsClass = "ExchangeCheckerTraySettings"

// msgSetTask: queued results of the settings window's requests.
const msgSetTask = winapi.WM_APP + 3

// Settings window control IDs.
const (
	idSetList = 400 + iota
	idSetName
	idSetHost
	idSetInstance
	idSetPort
	idSetDatabase
	idSetAuth
	idSetLogin
	idSetPassword
	idSetPwHint
	idSetClearPw
	idSetEncrypt
	idSetTLS10
	idSetTrust
	idSetEnabled
	idSetHints
	idSetInfo
	idSetStatus
	idSetTest
	idSetCancelTest
	idSetSave
	idSetDelete
	idSetAdd
	idSetPoll
	idSetReload
	idSetElevate
	idSetClose = winapi.IDCANCEL
)

// Elevator starts the elevated settings process («Изменить
// (администратор)»). The default runs this exe with --settings through
// ShellExecuteW "runas"; tests use a fake.
type Elevator interface {
	Elevate(owner uintptr) error
}

// ShellElevator is the default Elevator.
type ShellElevator struct{}

// SettingsArg is the command line switch of the elevated settings process.
const SettingsArg = "--settings"

// Elevate implements Elevator (UAC prompt; ERROR_CANCELLED when refused).
func (ShellElevator) Elevate(owner uintptr) error {
	exe, err := winapi.ModuleFileName()
	if err != nil {
		return err
	}
	return winapi.ShellExecute(owner, "runas", exe, SettingsArg)
}

// settingsSession is what the window needs of trayipc.Session.
type settingsSession interface {
	Do(ctx context.Context, req model.IPCRequest) (model.IPCMessage, error)
	DoCancel(ctx context.Context, req model.IPCRequest) (model.IPCMessage, error)
	Refresh(ctx context.Context) error
	Status() trayipc.Status
	Supported(cmd string) bool
}

// settingsConfig configures a settings window.
type settingsConfig struct {
	lang     string
	mode     traysettings.Mode
	session  settingsSession
	elevator Elevator
	// confirm asks before delete; nil = MessageBoxW.
	confirm func(owner uintptr, text, caption string) bool
	hidden  bool
	onClose func()
}

// settingsWindow is the database settings window (CHK-TRAY-04/05/06): the
// same window for the read-only mode (ordinary tray, get_status data) and
// the administrator mode (elevated process, get_config / save_*).
// Requests run off the UI thread; their results come back through the
// window's own queue. Closing the window cancels a running test (cancel
// {ref}, G6).
type settingsWindow struct {
	cfg  settingsConfig
	ed   *traysettings.Editor
	hwnd uintptr
	ctl  map[int32]uintptr

	qmu    sync.Mutex
	tasks  []func()
	closed bool

	loading    bool // filling controls: ignore change notifications
	busy       bool // get_config / save / delete in flight
	testCancel context.CancelFunc
	testSeq    int
	lastFocus  traysettings.Field
	status     string
}

func newSettingsWindow(cfg settingsConfig) (*settingsWindow, error) {
	cfg.lang = i18n.Normalize(cfg.lang)
	w := &settingsWindow{cfg: cfg, ed: traysettings.NewEditor(cfg.lang, cfg.mode), ctl: map[int32]uintptr{}}
	if w.cfg.elevator == nil {
		w.cfg.elevator = ShellElevator{}
	}
	const ww, wh = 780, 660
	x := (winapi.GetSystemMetrics(winapi.SM_CXSCREEN) - ww) / 2
	y := (winapi.GetSystemMetrics(winapi.SM_CYSCREEN) - wh) / 3
	title := i18n.T(i18n.KeySetTitleRead, cfg.lang)
	if cfg.mode == traysettings.Admin {
		title = i18n.T(i18n.KeySetTitleAdmin, cfg.lang)
	}
	hwnd, err := createWindow(SettingsClass, title, 0,
		winapi.WS_OVERLAPPED|winapi.WS_CAPTION|winapi.WS_SYSMENU|winapi.WS_MINIMIZEBOX, x, y, ww, wh, 0, w.wndProc)
	if err != nil {
		return nil, err
	}
	w.hwnd = hwnd
	w.build()
	w.fill()
	if !cfg.hidden {
		winapi.ShowWindow(hwnd, winapi.SW_SHOWNORMAL)
		winapi.SetForegroundWindow(hwnd)
	}
	return w, nil
}

func (w *settingsWindow) t(key string) string { return i18n.T(key, w.cfg.lang) }

func (w *settingsWindow) add(class, text string, ex, style uint32, id, x, y, cw, ch int32) uintptr {
	h, err := winapi.CreateWindowEx(ex, class, text, winapi.WS_CHILD|winapi.WS_VISIBLE|style, x, y, cw, ch, w.hwnd, uintptr(id), winapi.GetModuleHandle())
	if err != nil {
		return 0
	}
	winapi.SendMessage(h, winapi.WM_SETFONT, winapi.GetStockObject(winapi.DEFAULT_GUI_FONT), 1)
	if id != 0 {
		w.ctl[id] = h
	}
	return h
}

func (w *settingsWindow) build() {
	const (
		lx, lw = 230, 130 // labels
		cx, cw = 365, 390 // controls
		rh     = 27
	)
	edit := uint32(winapi.WS_TABSTOP | winapi.ES_AUTOHSCROLL)
	w.add("STATIC", w.t(i18n.KeySetProfiles), 0, winapi.SS_LEFT, 0, 12, 10, 200, 18)
	w.add("LISTBOX", "", winapi.WS_EX_CLIENTEDGE, winapi.WS_TABSTOP|winapi.WS_VSCROLL|winapi.LBS_NOTIFY, idSetList, 12, 30, 205, 400)
	w.add("BUTTON", w.t(i18n.KeySetAdd), 0, winapi.WS_TABSTOP, idSetAdd, 12, 436, 100, 26)
	w.add("BUTTON", w.t(i18n.KeySetDelete), 0, winapi.WS_TABSTOP, idSetDelete, 117, 436, 100, 26)
	row := int32(10)
	line := func(label string, id int32, class string, style uint32, width int32) {
		w.add("STATIC", label, 0, winapi.SS_LEFT, 0, lx, row+3, lw, 18)
		ex := uint32(0)
		if class == "EDIT" {
			ex = winapi.WS_EX_CLIENTEDGE
		}
		hgt := int32(23)
		if class == "COMBOBOX" {
			hgt = 200
		}
		w.add(class, "", ex, style, id, cx, row, width, hgt)
		row += rh
	}
	line(w.t(i18n.KeySetName), idSetName, "EDIT", edit, cw)
	line(w.t(i18n.KeySetHost), idSetHost, "EDIT", edit, cw)
	line(w.t(i18n.KeySetInstance), idSetInstance, "EDIT", edit, 200)
	line(w.t(i18n.KeySetPort), idSetPort, "EDIT", edit, 80)
	line(w.t(i18n.KeySetDatabase), idSetDatabase, "EDIT", edit, cw)
	line(w.t(i18n.KeySetAuth), idSetAuth, "COMBOBOX", winapi.WS_TABSTOP|winapi.CBS_DROPDOWNLIST, cw)
	line(w.t(i18n.KeySetLogin), idSetLogin, "EDIT", edit, cw)
	line(w.t(i18n.KeySetPassword), idSetPassword, "EDIT", edit|winapi.ES_PASSWORD, cw)
	w.add("STATIC", "", 0, winapi.SS_LEFT, idSetPwHint, cx, row-3, cw, 30)
	row += 28
	w.add("BUTTON", w.t(i18n.KeySetClearPassword), 0, winapi.WS_TABSTOP|winapi.BS_AUTOCHECKBOX, idSetClearPw, cx, row, cw, 20)
	row += 26
	line(w.t(i18n.KeySetEncrypt), idSetEncrypt, "COMBOBOX", winapi.WS_TABSTOP|winapi.CBS_DROPDOWNLIST, cw)
	for _, c := range []struct {
		id  int32
		key string
	}{{idSetTLS10, i18n.KeySetTLS10}, {idSetTrust, i18n.KeySetTrust}, {idSetEnabled, i18n.KeySetEnabled}} {
		w.add("BUTTON", w.t(c.key), 0, winapi.WS_TABSTOP|winapi.BS_AUTOCHECKBOX, c.id, cx, row, cw, 20)
		row += 24
	}
	w.add("STATIC", "", 0, winapi.SS_LEFT, idSetHints, lx, row+2, cx+cw-lx, 46)
	row += 50
	w.add("STATIC", "", 0, winapi.SS_LEFT, idSetInfo, lx, row, cx+cw-lx, 36)
	w.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, winapi.ES_MULTILINE|winapi.ES_READONLY|winapi.ES_AUTOVSCROLL|winapi.WS_VSCROLL,
		idSetStatus, 12, 476, 743, 100)
	bx := int32(12)
	button := func(id int32, key string, width int32) {
		w.add("BUTTON", w.t(key), 0, winapi.WS_TABSTOP, id, bx, 586, width, 28)
		bx += width + 6
	}
	button(idSetTest, i18n.KeySetTest, 150)
	button(idSetCancelTest, i18n.KeySetCancelTest, 120)
	if w.cfg.mode == traysettings.Admin {
		button(idSetSave, i18n.KeySetSave, 90)
	}
	button(idSetPoll, i18n.KeySetPoll, 110)
	button(idSetReload, i18n.KeySetReload, 90)
	if w.cfg.mode == traysettings.ReadOnly {
		button(idSetElevate, i18n.KeySetElevate, 165)
	}
	w.add("BUTTON", w.t(i18n.KeySetClose), 0, winapi.WS_TABSTOP, idSetClose, 665, 586, 90, 28)
	for _, k := range []string{i18n.KeySetAuthSQL, i18n.KeySetAuthWindows} {
		winapi.SendMessageString(w.ctl[idSetAuth], winapi.CB_ADDSTRING, 0, w.t(k))
	}
	for _, k := range []string{i18n.KeySetEncDisable, i18n.KeySetEncOptional, i18n.KeySetEncOn, i18n.KeySetEncStrict} {
		winapi.SendMessageString(w.ctl[idSetEncrypt], winapi.CB_ADDSTRING, 0, w.t(k))
	}
	if w.cfg.mode == traysettings.ReadOnly {
		winapi.ShowWindow(w.ctl[idSetAdd], winapi.SW_HIDE)
		winapi.ShowWindow(w.ctl[idSetDelete], winapi.SW_HIDE)
		winapi.ShowWindow(w.ctl[idSetClearPw], winapi.SW_HIDE)
		w.status = w.t(i18n.KeySetReadOnlyNote)
	} else {
		w.status = w.t(i18n.KeySetLoading)
	}
}

// post queues f for the UI thread (from any goroutine); dropped after
// the window is closed.
func (w *settingsWindow) post(f func()) {
	w.qmu.Lock()
	if w.closed {
		w.qmu.Unlock()
		return
	}
	first := len(w.tasks) == 0
	w.tasks = append(w.tasks, f)
	w.qmu.Unlock()
	if first {
		winapi.PostMessage(w.hwnd, msgSetTask, 0, 0)
	}
}

func (w *settingsWindow) wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case msgSetTask:
		w.qmu.Lock()
		tasks := w.tasks
		w.tasks = nil
		w.qmu.Unlock()
		for _, f := range tasks {
			f()
		}
		return 0, true
	case winapi.WM_COMMAND:
		w.command(int32(wp&0xFFFF), uint32(wp>>16&0xFFFF))
		return 0, true
	case winapi.WM_CLOSE:
		w.close()
		return 0, true
	}
	return 0, false
}

func (w *settingsWindow) command(id int32, code uint32) {
	switch id {
	case idSetName, idSetHost, idSetInstance, idSetPort, idSetDatabase, idSetLogin, idSetPassword:
		if code == winapi.EN_CHANGE && !w.loading {
			w.readForm()
			w.update()
		}
	case idSetAuth, idSetEncrypt:
		if code == winapi.CBN_SELCHANGE && !w.loading {
			w.readForm()
			w.update()
		}
	case idSetClearPw, idSetTLS10, idSetTrust, idSetEnabled:
		if code == winapi.BN_CLICKED && !w.loading {
			w.readForm()
			w.update()
		}
	case idSetList:
		if code == winapi.LBN_SELCHANGE {
			w.selectFromList()
		}
	case idSetAdd:
		w.newProfile()
	case idSetDelete:
		w.deleteProfile()
	case idSetSave:
		w.save()
	case idSetTest:
		w.test()
	case idSetCancelTest:
		w.cancelTest()
	case idSetPoll:
		w.poll()
	case idSetReload:
		w.Reload()
	case idSetElevate:
		w.elevate()
	case idSetClose:
		w.close()
	}
}

func (w *settingsWindow) check(id int32) bool {
	return winapi.SendMessage(w.ctl[id], winapi.BM_GETCHECK, 0, 0) == winapi.BST_CHECKED
}

func (w *settingsWindow) setCheck(id int32, on bool) {
	v := uintptr(winapi.BST_UNCHECKED)
	if on {
		v = winapi.BST_CHECKED
	}
	winapi.SendMessage(w.ctl[id], winapi.BM_SETCHECK, v, 0)
}

func (w *settingsWindow) text(id int32) string { return winapi.GetWindowText(w.ctl[id]) }

// readForm copies the controls into the editor form (admin mode).
func (w *settingsWindow) readForm() {
	if !w.ed.CanEdit() {
		return
	}
	f := &w.ed.Form
	f.Name, f.Host, f.Instance, f.Port = w.text(idSetName), w.text(idSetHost), w.text(idSetInstance), w.text(idSetPort)
	f.Database, f.Login, f.Password = w.text(idSetDatabase), w.text(idSetLogin), w.text(idSetPassword)
	f.Auth = model.AuthSQL
	if winapi.SendMessage(w.ctl[idSetAuth], winapi.CB_GETCURSEL, 0, 0) == 1 {
		f.Auth = model.AuthWindows
	}
	if i := int(winapi.SendMessage(w.ctl[idSetEncrypt], winapi.CB_GETCURSEL, 0, 0)); i >= 0 && i < len(traysettings.EncryptChoices) {
		f.Encrypt = traysettings.EncryptChoices[i]
	}
	f.ClearPassword, f.TLS10, f.TrustServerCert, f.Enabled = w.check(idSetClearPw), w.check(idSetTLS10), w.check(idSetTrust), w.check(idSetEnabled)
}

// fill shows the editor state: list, form, hints, buttons.
func (w *settingsWindow) fill() {
	w.loading = true
	defer func() { w.loading = false }()
	lb := w.ctl[idSetList]
	winapi.SendMessage(lb, winapi.LB_RESETCONTENT, 0, 0)
	for _, p := range w.ed.Profiles {
		winapi.SendMessageString(lb, winapi.LB_ADDSTRING, 0, p.DisplayName())
	}
	sel := w.ed.Sel
	if w.ed.IsNew {
		winapi.SendMessageString(lb, winapi.LB_ADDSTRING, 0, w.t(i18n.KeySetNewProfile))
		sel = len(w.ed.Profiles)
	}
	winapi.SendMessage(lb, winapi.LB_SETCURSEL, uintptr(sel), 0) // -1 clears
	f := w.ed.Form
	for id, s := range map[int32]string{idSetName: f.Name, idSetHost: f.Host, idSetInstance: f.Instance, idSetPort: f.Port,
		idSetDatabase: f.Database, idSetLogin: f.Login, idSetPassword: f.Password} {
		if w.text(id) != s {
			winapi.SetWindowText(w.ctl[id], s)
		}
	}
	auth := 0
	if f.Auth == model.AuthWindows {
		auth = 1
	}
	if !w.ed.HasProfile() {
		auth = -1
	}
	winapi.SendMessage(w.ctl[idSetAuth], winapi.CB_SETCURSEL, uintptr(auth), 0)
	enc := traysettings.EncryptIndex(f.Encrypt)
	if !w.ed.HasProfile() {
		enc = -1
	}
	winapi.SendMessage(w.ctl[idSetEncrypt], winapi.CB_SETCURSEL, uintptr(enc), 0)
	w.setCheck(idSetClearPw, f.ClearPassword)
	w.setCheck(idSetTLS10, f.TLS10)
	w.setCheck(idSetTrust, f.TrustServerCert)
	w.setCheck(idSetEnabled, f.Enabled)
	w.update()
}

// update refreshes hints, enabled states and the status line.
func (w *settingsWindow) update() {
	ed := w.ed
	edit := ed.CanEdit()
	has := ed.HasProfile()
	sql := ed.Form.Auth != model.AuthWindows
	encrypted := ed.Form.Encrypt.Effective() != model.EncryptDisable
	for _, id := range []int32{idSetName, idSetHost, idSetInstance, idSetPort, idSetDatabase, idSetLogin, idSetPassword} {
		winapi.SendMessage(w.ctl[id], winapi.EM_SETREADONLY, boolPtr(!edit), 0)
	}
	winapi.EnableWindow(w.ctl[idSetLogin], has && sql)
	winapi.EnableWindow(w.ctl[idSetPassword], edit && sql)
	winapi.EnableWindow(w.ctl[idSetClearPw], edit && sql && !ed.IsNew && ed.Base.HasPassword)
	winapi.EnableWindow(w.ctl[idSetAuth], edit)
	winapi.EnableWindow(w.ctl[idSetEncrypt], edit)
	winapi.EnableWindow(w.ctl[idSetTLS10], edit && encrypted)
	winapi.EnableWindow(w.ctl[idSetTrust], edit && encrypted)
	winapi.EnableWindow(w.ctl[idSetEnabled], edit)
	pw := ""
	if has {
		pw = ed.PasswordHint()
	}
	winapi.SetWindowText(w.ctl[idSetPwHint], pw)
	winapi.SetWindowText(w.ctl[idSetHints], strings.Join(ed.Hints(), "\r\n"))
	winapi.SetWindowText(w.ctl[idSetInfo], w.info())
	testing := w.testCancel != nil
	sess := w.cfg.session
	winapi.EnableWindow(w.ctl[idSetTest], has && !testing && sess.Supported(model.CmdTestProfile))
	winapi.EnableWindow(w.ctl[idSetCancelTest], testing)
	saved := has && !ed.IsNew
	winapi.EnableWindow(w.ctl[idSetPoll], saved && !w.busy)
	winapi.EnableWindow(w.ctl[idSetReload], !w.busy)
	if ed.Mode == traysettings.Admin {
		winapi.EnableWindow(w.ctl[idSetSave], edit && !w.busy)
		winapi.EnableWindow(w.ctl[idSetDelete], saved && !w.busy)
		winapi.EnableWindow(w.ctl[idSetAdd], !w.busy)
	}
	st := w.status
	if !has && st == "" {
		st = w.t(i18n.KeySetNoProfile)
	}
	if w.text(idSetStatus) != st {
		winapi.SetWindowText(w.ctl[idSetStatus], st)
	}
}

func boolPtr(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// info is the mapping / thresholds line.
func (w *settingsWindow) info() string {
	ed := w.ed
	if !ed.HasProfile() || ed.IsNew {
		return ""
	}
	var parts []string
	configured := ed.Base.Mapping.Configured()
	if s, ok := ed.MapSummary[ed.Base.ID]; ok && s != nil {
		configured = s.Configured
	}
	if configured {
		parts = append(parts, w.t(i18n.KeySetMapYes))
	} else {
		parts = append(parts, w.t(i18n.KeySetMapNo))
	}
	if ed.Base.WarnMinutes > 0 || ed.Base.ErrorMinutes > 0 {
		parts = append(parts, fmt.Sprintf(w.t(i18n.KeySetThresholds), ed.Base.WarnMinutes, ed.Base.ErrorMinutes))
	}
	return strings.Join(parts, "\r\n")
}

func (w *settingsWindow) setStatus(s string) {
	w.status = s
	w.update()
}

func (w *settingsWindow) problem(p *traysettings.Problem) {
	w.status = p.Text
	if p.Field != traysettings.FieldNone {
		w.focus(p.Field)
	}
	w.update()
}

var fieldControl = map[traysettings.Field]int32{
	traysettings.FieldName: idSetName, traysettings.FieldHost: idSetHost, traysettings.FieldInstance: idSetInstance,
	traysettings.FieldPort: idSetPort, traysettings.FieldDatabase: idSetDatabase, traysettings.FieldAuth: idSetAuth,
	traysettings.FieldLogin: idSetLogin, traysettings.FieldPassword: idSetPassword, traysettings.FieldClearPassword: idSetClearPw,
	traysettings.FieldEncrypt: idSetEncrypt, traysettings.FieldTLS10: idSetTLS10, traysettings.FieldTrust: idSetTrust,
	traysettings.FieldEnabled: idSetEnabled,
}

// focus moves the keyboard focus to the control of f (lastFocus keeps it
// for tests: SetFocus of a hidden window is not reliable).
func (w *settingsWindow) focus(f traysettings.Field) {
	w.lastFocus = f
	if id, ok := fieldControl[f]; ok {
		winapi.SetFocus(w.ctl[id])
	}
}

func (w *settingsWindow) selectFromList() {
	i := int(int32(winapi.SendMessage(w.ctl[idSetList], winapi.LB_GETCURSEL, 0, 0)))
	if w.ed.IsNew && i == len(w.ed.Profiles) {
		return
	}
	w.ed.Select(i)
	w.status = ""
	w.fill()
}

func (w *settingsWindow) newProfile() {
	if w.cfg.mode != traysettings.Admin || w.busy {
		return
	}
	w.ed.New()
	w.status = ""
	w.fill()
	w.focus(traysettings.FieldHost)
}

// call runs one request off the UI thread; done gets the reply on it.
func (w *settingsWindow) call(req model.IPCRequest, done func(model.IPCMessage, error)) {
	w.busy = true
	w.update()
	s := w.cfg.session
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), traysettings.CallTimeout)
		m, err := s.Do(ctx, req)
		cancel()
		w.post(func() {
			w.busy = false
			done(m, err)
		})
	}()
}

func (w *settingsWindow) save() {
	if w.busy {
		return
	}
	w.readForm()
	req, pr := w.ed.SaveRequest()
	if pr != nil {
		w.problem(pr)
		return
	}
	w.status = w.t(i18n.KeySetSaving)
	sent := req.Profile
	w.call(req, func(m model.IPCMessage, err error) {
		switch {
		case err != nil:
			w.problem(w.ed.ErrorProblem(err, traysettings.CallTimeout))
		case m.Type == model.MsgError:
			w.problem(w.ed.Reply(m, sent))
		default:
			w.status = w.ed.SaveDone(m)
			w.fill()
		}
	})
}

func (w *settingsWindow) deleteProfile() {
	if w.busy {
		return
	}
	req, pr := w.ed.DeleteRequest()
	if pr != nil {
		w.problem(pr)
		return
	}
	text := fmt.Sprintf(w.t(i18n.KeySetDeleteConfirm), w.ed.Base.DisplayName())
	confirm := w.cfg.confirm
	if confirm == nil {
		confirm = func(owner uintptr, text, caption string) bool {
			return winapi.MessageBox(owner, text, caption, winapi.MB_YESNO|winapi.MB_ICONWARNING|winapi.MB_DEFBUTTON2) == winapi.IDYES
		}
	}
	if !confirm(w.hwnd, text, w.t(i18n.KeyAppName)) {
		return
	}
	w.status = w.t(i18n.KeySetDeleting)
	w.call(req, func(m model.IPCMessage, err error) {
		switch {
		case err != nil:
			w.problem(w.ed.ErrorProblem(err, traysettings.CallTimeout))
		case m.Type == model.MsgError:
			w.problem(w.ed.Reply(m, nil))
		default:
			w.status = w.ed.DeleteDone(m)
			w.fill()
		}
	})
}

// test runs test_profile (cancelable: «Отменить проверку» or closing the
// window sends cancel {ref}).
func (w *settingsWindow) test() {
	if w.testCancel != nil {
		return
	}
	w.readForm()
	req, pr := w.ed.TestRequest()
	if pr != nil {
		w.problem(pr)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), traysettings.TestTimeout)
	w.testCancel = cancel
	w.testSeq++
	seq := w.testSeq
	w.status = w.t(i18n.KeySetTesting)
	w.update()
	s := w.cfg.session
	go func() {
		m, err := s.DoCancel(ctx, req)
		cancel()
		w.post(func() {
			if seq != w.testSeq {
				return
			}
			w.testCancel = nil
			w.problem(w.ed.TestReply(m, err))
		})
	}()
}

func (w *settingsWindow) cancelTest() {
	if w.testCancel != nil {
		w.testCancel()
	}
}

func (w *settingsWindow) poll() {
	req, pr := w.ed.PollRequest()
	if pr != nil {
		w.problem(pr)
		return
	}
	w.call(req, func(m model.IPCMessage, err error) {
		switch {
		case err != nil:
			w.problem(w.ed.ErrorProblem(err, traysettings.CallTimeout))
		case m.Type == model.MsgError:
			w.problem(w.ed.Reply(m, nil))
		default:
			w.setStatus(w.ed.PollText(m))
		}
	})
}

// Reload re-reads the configuration: get_config (admin) or get_status
// (read-only). Unsaved edits are dropped.
func (w *settingsWindow) Reload() {
	if w.busy {
		return
	}
	if w.cfg.mode == traysettings.ReadOnly {
		s := w.cfg.session
		w.busy = true
		w.update()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), traysettings.CallTimeout)
			err := s.Refresh(ctx)
			cancel()
			st := s.Status()
			w.post(func() {
				w.busy = false
				if st.Snapshot != nil {
					w.ed.LoadSnapshot(*st.Snapshot)
				}
				w.status = w.t(i18n.KeySetReadOnlyNote)
				w.fill()
				if err != nil {
					w.problem(w.ed.ErrorProblem(err, traysettings.CallTimeout))
				}
			})
		}()
		return
	}
	w.status = w.t(i18n.KeySetLoading)
	w.call(model.IPCRequest{Cmd: model.CmdGetConfig}, func(m model.IPCMessage, err error) {
		switch {
		case err != nil:
			w.problem(w.ed.ErrorProblem(err, traysettings.CallTimeout))
		case m.Type == model.MsgError:
			w.problem(w.ed.Reply(m, nil))
		case m.Config == nil:
			w.problem(&traysettings.Problem{Text: fmt.Sprintf(w.t(i18n.KeySetFailed), "get_config: "+m.Type)})
		default:
			w.ed.LoadConfig(*m.Config, m.Revision)
			w.status = ""
			w.fill()
		}
	})
}

// loadSnapshot fills the read-only window from a status snapshot.
func (w *settingsWindow) loadSnapshot(snap *model.StatusSnapshot) {
	if snap != nil {
		w.ed.LoadSnapshot(*snap)
	}
	w.fill()
}

func (w *settingsWindow) elevate() {
	err := w.cfg.elevator.Elevate(w.hwnd)
	switch {
	case err == nil:
		w.close() // the elevated window replaces this one
	case errors.Is(err, syscall.Errno(winapi.ERROR_CANCELLED)):
		w.setStatus(w.t(i18n.KeySetElevateCanceled))
	default:
		w.setStatus(fmt.Sprintf(w.t(i18n.KeySetElevateFailed), err.Error()))
	}
}

// close cancels a running test (cancel {ref}) and destroys the window.
func (w *settingsWindow) close() {
	w.qmu.Lock()
	already := w.closed
	w.closed = true
	w.tasks = nil
	w.qmu.Unlock()
	if already {
		return
	}
	if w.testCancel != nil {
		w.testCancel()
		w.testCancel = nil
	}
	winapi.DestroyWindow(w.hwnd)
	if w.cfg.onClose != nil {
		w.cfg.onClose()
	}
}

// SettingsOptions configure the elevated settings process (exchtray
// --settings).
type SettingsOptions struct {
	Lang string
	// Session: Dial and Verify (G7: the pipe server PID is checked
	// before any command, so no password goes to a foreign process).
	Session trayipc.Options
}

// SettingsMutexName keeps one settings window per logon session.
const SettingsMutexName = `Local\ExchangeCheckerTraySettings`

// RunSettings runs the administrator settings window in its own process
// with its own service connection until the window is closed; a second
// instance brings the first one to the front. Call on a locked OS thread.
func RunSettings(opt SettingsOptions) int {
	release, ok, err := SingleInstance(SettingsMutexName)
	if err != nil {
		return 1
	}
	if !ok {
		if h := winapi.FindWindow(SettingsClass); h != 0 {
			winapi.ShowWindow(h, winapi.SW_SHOWNORMAL)
			winapi.SetForegroundWindow(h)
		}
		return 0
	}
	defer release()
	var w *settingsWindow
	loaded := false
	so := opt.Session
	so.OnEvent = func(e trayipc.Event) {
		if e.Kind != trayipc.EventState || w == nil {
			return
		}
		st := e.Status
		w.post(func() {
			if st.State == trayipc.StateConnected {
				if !loaded {
					loaded = true
					w.Reload()
				}
				return
			}
			if !w.busy {
				w.setStatus(i18n.T(i18n.KeySetNotConnected, w.cfg.lang))
			}
		})
	}
	s := trayipc.New(so)
	w, err = newSettingsWindow(settingsConfig{lang: opt.Lang, mode: traysettings.Admin, session: s,
		onClose: func() { winapi.PostQuitMessage(0) }})
	if err != nil {
		return 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	code := w.loop()
	cancel()
	<-done
	return code
}

// loop is the message loop of the settings process (dialog keys).
func (w *settingsWindow) loop() int {
	var m winapi.MSG
	for {
		r := winapi.GetMessage(&m)
		if r == 0 || r == -1 {
			break
		}
		if winapi.IsWindow(w.hwnd) && winapi.IsDialogMessage(w.hwnd, &m) {
			continue
		}
		winapi.TranslateMessage(&m)
		winapi.DispatchMessage(&m)
	}
	return int(m.WParam)
}
