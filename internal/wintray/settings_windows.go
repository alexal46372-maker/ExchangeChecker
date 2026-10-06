//go:build windows

package wintray

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// SettingsClass is the window class of the administrator settings window
// (FindWindow of a second `exchtray --settings`). The read-only window of
// the tray process has its own class, SettingsViewClass, so that search
// can never find a read-only window that is closing after «Изменить».
const (
	SettingsClass     = "ExchangeCheckerTraySettings"
	SettingsViewClass = "ExchangeCheckerTraySettingsView"
)

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
	idSetPage // «Общие настройки…» / «Базы данных…»
	idGenPoll
	idGenWarn
	idGenError
	idGenBeep
	idGenWarnFreq
	idGenWarnDur
	idGenErrFreq
	idGenErrDur
	idGenGap
	idGenLang
	idGenAutostart
	idGenNote
	idGenAutostartErr
	idSetDiscover // «Таблицы обмена…» (phase 6d, administrator only)
	idSetClose    = winapi.IDCANCEL
)

// Elevator starts the elevated settings process («Изменить
// (администратор)»). The default runs this exe with --settings through
// ShellExecuteExW "runas"; tests use a fake.
type Elevator interface {
	Elevate(owner uintptr) error
}

// ShellElevator is the default Elevator.
type ShellElevator struct{}

// SettingsArg is the command line switch of the elevated settings process.
const SettingsArg = "--settings"

// Elevate implements Elevator: ShellExecuteExW "runas" with
// SEE_MASK_NOCLOSEPROCESS (UAC refused: ERROR_CANCELLED). The started
// process may take the foreground (AllowSetForegroundWindow), so an
// already open settings window of another `--settings` instance, which
// it activates, really comes to the front; the handle is closed.
func (ShellElevator) Elevate(owner uintptr) error {
	exe, err := winapi.ModuleFileName()
	if err != nil {
		return err
	}
	h, err := winapi.ShellExecuteEx(owner, "runas", exe, SettingsArg)
	if err != nil {
		return err
	}
	if h != 0 {
		winapi.AllowSetForegroundWindow(winapi.GetProcessId(h))
		syscall.CloseHandle(h)
	}
	return nil
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
	// confirm asks Yes/No (delete, discard changes); nil = MessageBoxW
	// with «Нет» as the default button.
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
	pwReads    int // readPassword calls (test hook)
	// statusRefreshes / statusRefreshed: get_status re-reads started /
	// done after save_settings (test hook)
	statusRefreshes, statusRefreshed int
	page                             int // 0 = databases, 1 = general settings
	pageCtl                          [2][]uintptr
	cur                              *[]uintptr // page of the controls being created
	connected                        bool       // service session state seen by onSessionState
	loaded                           bool       // get_config (or a snapshot) loaded at least once
	disc                             *discoveryDialog
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
	class := SettingsViewClass
	if cfg.mode == traysettings.Admin {
		class = SettingsClass
	}
	hwnd, err := createWindow(class, title, 0,
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
	if w.cur != nil {
		*w.cur = append(*w.cur, h)
	}
	return h
}

// genEdits are the number boxes of the general page and their fields.
var genEdits = []struct {
	id    int32
	field traysettings.Field
	key   string
}{
	{idGenPoll, traysettings.FieldPollInterval, i18n.KeyGenPoll},
	{idGenWarn, traysettings.FieldWarnMinutes, i18n.KeyGenWarn},
	{idGenError, traysettings.FieldErrorMinutes, i18n.KeyGenError},
	{idGenWarnFreq, traysettings.FieldWarnFreq, i18n.KeyGenWarnFreq},
	{idGenWarnDur, traysettings.FieldWarnDur, i18n.KeyGenWarnDur},
	{idGenErrFreq, traysettings.FieldErrorFreq, i18n.KeyGenErrorFreq},
	{idGenErrDur, traysettings.FieldErrorDur, i18n.KeyGenErrorDur},
	{idGenGap, traysettings.FieldGap, i18n.KeyGenGap},
}

// genText returns the address of the GeneralForm string of an edit box.
func genText(g *traysettings.GeneralForm, id int32) *string {
	switch id {
	case idGenPoll:
		return &g.PollSec
	case idGenWarn:
		return &g.WarnMin
	case idGenError:
		return &g.ErrorMin
	case idGenWarnFreq:
		return &g.WarnFreq
	case idGenWarnDur:
		return &g.WarnDur
	case idGenErrFreq:
		return &g.ErrorFreq
	case idGenErrDur:
		return &g.ErrorDur
	case idGenGap:
		return &g.Gap
	}
	return nil
}

func (w *settingsWindow) build() {
	const (
		lx, lw = 230, 130 // labels
		cx, cw = 365, 390 // controls
		rh     = 27
	)
	edit := uint32(winapi.WS_TABSTOP | winapi.ES_AUTOHSCROLL)
	w.cur = &w.pageCtl[0]
	w.add("STATIC", w.t(i18n.KeySetProfiles), 0, winapi.SS_LEFT, 0, 12, 10, 200, 18)
	w.add("LISTBOX", "", winapi.WS_EX_CLIENTEDGE, winapi.WS_TABSTOP|winapi.WS_VSCROLL|winapi.LBS_NOTIFY, idSetList, 12, 30, 205, 340)
	w.add("BUTTON", w.t(i18n.KeySetAdd), 0, winapi.WS_TABSTOP, idSetAdd, 12, 376, 100, 26)
	w.add("BUTTON", w.t(i18n.KeySetDelete), 0, winapi.WS_TABSTOP, idSetDelete, 117, 376, 100, 26)
	if w.cfg.mode == traysettings.Admin {
		w.add("BUTTON", w.t(i18n.KeyDiscButton), 0, winapi.WS_TABSTOP, idSetDiscover, 12, 408, 205, 26)
	}
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

	// general settings page (hidden until «Общие настройки…»)
	w.cur = &w.pageCtl[1]
	row = 10
	for i, g := range genEdits {
		if i == 3 {
			w.add("BUTTON", w.t(i18n.KeyGenBeepEnabled), 0, winapi.WS_TABSTOP|winapi.BS_AUTOCHECKBOX, idGenBeep, cx, row+4, cw, 20)
			row += rh + 4
		}
		line(w.t(g.key), g.id, "EDIT", edit|winapi.ES_NUMBER, 100)
	}
	row += 6
	line(w.t(i18n.KeyGenLanguage), idGenLang, "COMBOBOX", winapi.WS_TABSTOP|winapi.CBS_DROPDOWNLIST, 200)
	w.add("BUTTON", w.t(i18n.KeyGenAutostart), 0, winapi.WS_TABSTOP|winapi.BS_AUTOCHECKBOX, idGenAutostart, cx, row+4, cw, 20)
	row += rh
	w.add("STATIC", "", 0, winapi.SS_LEFT, idGenAutostartErr, cx, row, cw, 32)
	row += 38
	w.add("STATIC", w.t(i18n.KeyGenNote), 0, winapi.SS_LEFT, idGenNote, lx, row, cx+cw-lx, 80)
	w.cur = nil

	w.add("BUTTON", w.t(i18n.KeyGenPage), 0, winapi.WS_TABSTOP, idSetPage, 12, 440, 205, 26)
	w.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, winapi.ES_MULTILINE|winapi.ES_READONLY|winapi.ES_AUTOVSCROLL|winapi.WS_VSCROLL,
		idSetStatus, 12, 476, 743, 100)
	bx := int32(12)
	button := func(id int32, key string, width int32) {
		w.add("BUTTON", w.t(key), 0, winapi.WS_TABSTOP, id, bx, 586, width, 28)
		bx += width + 6
	}
	w.cur = &w.pageCtl[0]
	button(idSetTest, i18n.KeySetTest, 150)
	button(idSetCancelTest, i18n.KeySetCancelTest, 120)
	w.cur = nil
	if w.cfg.mode == traysettings.Admin {
		button(idSetSave, i18n.KeySetSave, 90)
	}
	w.cur = &w.pageCtl[0]
	button(idSetPoll, i18n.KeySetPoll, 110)
	w.cur = nil
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
	for _, k := range []string{i18n.KeyGenLangAuto, i18n.KeyGenLangRU, i18n.KeyGenLangEN} {
		winapi.SendMessageString(w.ctl[idGenLang], winapi.CB_ADDSTRING, 0, w.t(k))
	}
	for _, h := range w.pageCtl[1] {
		winapi.ShowWindow(h, winapi.SW_HIDE)
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
		w.userClose()
		return 0, true
	}
	return 0, false
}

func (w *settingsWindow) command(id int32, code uint32) {
	switch id {
	case idSetPassword:
		// Dirty tracking by a flag, not by content: only the length is asked.
		if code == winapi.EN_CHANGE && !w.loading && w.ed.CanEdit() {
			w.ed.Form.PasswordTyped = winapi.GetWindowTextLength(w.ctl[idSetPassword]) > 0
			w.update()
		}
	case idGenPoll, idGenWarn, idGenError, idGenWarnFreq, idGenWarnDur, idGenErrFreq, idGenErrDur, idGenGap:
		if code == winapi.EN_CHANGE && !w.loading {
			w.readGeneral()
		}
	case idGenLang:
		if code == winapi.CBN_SELCHANGE && !w.loading {
			w.readGeneral()
		}
	case idGenBeep, idGenAutostart:
		if code == winapi.BN_CLICKED && !w.loading {
			w.readGeneral()
		}
	case idSetPage:
		w.setPage(1 - w.page)
	case idSetName, idSetHost, idSetInstance, idSetPort, idSetDatabase, idSetLogin:
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
		if w.discardOK() {
			w.Reload()
		}
	case idSetElevate:
		w.elevate()
	case idSetDiscover:
		w.openDiscovery()
	case idSetClose:
		w.userClose()
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

// readPassword returns the password box text for one save / test request.
// The UTF-16 copy is wiped right away; the returned string lives only as
// long as the request (Go strings cannot be wiped).
func (w *settingsWindow) readPassword() string {
	if !w.ed.Form.PasswordTyped {
		return ""
	}
	w.pwReads++
	buf := winapi.GetWindowTextUTF16(w.ctl[idSetPassword])
	defer winapi.ZeroUTF16(buf)
	r := utf16.Decode(buf)
	s := string(r)
	for i := range r {
		r[i] = 0
	}
	return s
}

// readForm copies the controls into the editor form (admin mode).
func (w *settingsWindow) readForm() {
	if !w.ed.CanEdit() {
		return
	}
	f := &w.ed.Form
	f.Name, f.Host, f.Instance, f.Port = w.text(idSetName), w.text(idSetHost), w.text(idSetInstance), w.text(idSetPort)
	f.Database, f.Login = w.text(idSetDatabase), w.text(idSetLogin)
	// The password box is never read here (only its length on EN_CHANGE,
	// see command); its text is taken by readPassword for save / test.
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
		idSetDatabase: f.Database, idSetLogin: f.Login} {
		if w.text(id) != s {
			winapi.SetWindowText(w.ctl[id], s)
		}
	}
	if !f.PasswordTyped && winapi.GetWindowTextLength(w.ctl[idSetPassword]) > 0 {
		winapi.SetWindowText(w.ctl[idSetPassword], "") // the box is only ever cleared
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
	w.fillGeneral()
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
	if t := w.autostartError(); w.text(idGenAutostartErr) != t {
		winapi.SetWindowText(w.ctl[idGenAutostartErr], t)
	}
	gen := ed.CanEditSettings()
	for _, e := range genEdits {
		winapi.SendMessage(w.ctl[e.id], winapi.EM_SETREADONLY, boolPtr(!gen), 0)
	}
	for _, id := range []int32{idGenBeep, idGenAutostart, idGenLang} {
		winapi.EnableWindow(w.ctl[id], gen)
	}
	if ed.Mode == traysettings.Admin {
		canSave := edit
		if w.page == 1 {
			canSave = gen
		}
		winapi.EnableWindow(w.ctl[idSetSave], canSave && !w.busy)
		winapi.EnableWindow(w.ctl[idSetDelete], saved && !w.busy)
		winapi.EnableWindow(w.ctl[idSetAdd], !w.busy)
		winapi.EnableWindow(w.ctl[idSetDiscover], has && !w.busy && w.disc == nil && w.discoveryAvailable())
	}
	st := w.status
	if !has && st == "" && w.page == 0 {
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
	if ed.MapEdit != nil {
		configured = ed.MapEdit.Configured()
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
	traysettings.FieldEnabled:      idSetEnabled,
	traysettings.FieldPollInterval: idGenPoll, traysettings.FieldWarnMinutes: idGenWarn, traysettings.FieldErrorMinutes: idGenError,
	traysettings.FieldBeepEnabled: idGenBeep, traysettings.FieldWarnFreq: idGenWarnFreq, traysettings.FieldWarnDur: idGenWarnDur,
	traysettings.FieldErrorFreq: idGenErrFreq, traysettings.FieldErrorDur: idGenErrDur, traysettings.FieldGap: idGenGap,
	traysettings.FieldLanguage: idGenLang, traysettings.FieldAutostart: idGenAutostart,
}

// focus moves the keyboard focus to the control of f (lastFocus keeps it
// for tests: SetFocus of a hidden window is not reliable).
func (w *settingsWindow) focus(f traysettings.Field) {
	w.lastFocus = f
	if page := map[bool]int{false: 0, true: 1}[traysettings.IsGeneral(f)]; page != w.page {
		w.setPage(page)
	}
	if id, ok := fieldControl[f]; ok {
		winapi.SetFocus(w.ctl[id])
	}
}

// ask shows a Yes/No question («Нет» is the default button).
func (w *settingsWindow) ask(text string) bool {
	confirm := w.cfg.confirm
	if confirm == nil {
		confirm = func(owner uintptr, text, caption string) bool {
			return winapi.MessageBox(owner, text, caption, winapi.MB_YESNO|winapi.MB_ICONWARNING|winapi.MB_DEFBUTTON2) == winapi.IDYES
		}
	}
	return confirm(w.hwnd, text, w.t(i18n.KeyAppName))
}

// discardOK asks «Отменить изменения?» when the form (either page) has
// unsaved edits; true = go on (no edits or the user agreed).
func (w *settingsWindow) discardOK() bool {
	w.readForm()
	w.readGeneral()
	if !w.ed.Unsaved() {
		return true
	}
	return w.ask(w.t(i18n.KeySetDiscardConfirm))
}

// profileDiscardOK is discardOK for switching / adding a profile: the
// general page keeps its edits then.
func (w *settingsWindow) profileDiscardOK() bool {
	w.readForm()
	if !w.ed.ProfileUnsaved() {
		return true
	}
	return w.ask(w.t(i18n.KeySetDiscardConfirm))
}

// setPage shows the databases (0) or the general settings (1) page.
func (w *settingsWindow) setPage(p int) {
	w.page = p
	for i, hs := range w.pageCtl {
		cmd := int32(winapi.SW_HIDE)
		if i == p {
			cmd = winapi.SW_SHOW
		}
		for _, h := range hs {
			winapi.ShowWindow(h, cmd)
		}
	}
	key := i18n.KeyGenPage
	if p == 1 {
		key = i18n.KeyGenProfilesPage
	}
	winapi.SetWindowText(w.ctl[idSetPage], w.t(key))
	w.update()
}

// readGeneral copies the general page into the editor (administrator).
func (w *settingsWindow) readGeneral() {
	if !w.ed.CanEditSettings() {
		return
	}
	g := &w.ed.General
	for _, e := range genEdits {
		*genText(g, e.id) = w.text(e.id)
	}
	g.BeepEnabled, g.Autostart = w.check(idGenBeep), w.check(idGenAutostart)
	if i := int(winapi.SendMessage(w.ctl[idGenLang], winapi.CB_GETCURSEL, 0, 0)); i >= 0 && i < len(traysettings.LanguageChoices) {
		g.Language = traysettings.LanguageChoices[i]
	}
}

// fillGeneral shows the general page of the editor.
func (w *settingsWindow) fillGeneral() {
	g := w.ed.General
	for _, e := range genEdits {
		if s := *genText(&g, e.id); w.text(e.id) != s {
			winapi.SetWindowText(w.ctl[e.id], s)
		}
	}
	w.setCheck(idGenBeep, g.BeepEnabled)
	w.setCheck(idGenAutostart, g.Autostart)
	lang := traysettings.LanguageIndex(g.Language)
	if !w.ed.HasSettings {
		lang = -1
	}
	winapi.SendMessage(w.ctl[idGenLang], winapi.CB_SETCURSEL, uintptr(lang), 0)
}

// saveSettings sends save_settings of the general page.
func (w *settingsWindow) saveSettings() {
	if w.busy {
		return
	}
	w.readGeneral()
	req, pr := w.ed.SettingsRequest()
	if pr != nil {
		w.problem(pr)
		return
	}
	w.status = w.t(i18n.KeySetSaving)
	sent := *req.Settings
	w.call(req, func(m model.IPCMessage, err error) {
		switch {
		case err != nil:
			w.problem(w.ed.ErrorProblem(err, traysettings.CallTimeout))
		case m.Type == model.MsgError:
			w.problem(w.ed.Reply(m))
		default:
			w.status = w.ed.SettingsDone(m, sent)
			w.fill()
			w.refreshStatus() // trayAutostartError of the new value
		}
	})
}

// refreshStatus re-reads get_status in the background (the service
// applies trayAutostart on save_settings; the result comes only with
// get_status) and shows it.
func (w *settingsWindow) refreshStatus() {
	s := w.cfg.session
	w.statusRefreshes++
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), traysettings.CallTimeout)
		s.Refresh(ctx)
		cancel()
		w.post(func() {
			w.statusRefreshed++
			w.update()
		})
	}()
}

// autostartError is the text of trayAutostartError of the last get_status
// ("" = applied, not reported or not supported by the service).
func (w *settingsWindow) autostartError() string {
	st := w.cfg.session.Status()
	if !st.Has(model.CapTrayAutostartStatus) {
		return ""
	}
	return traysettings.AutostartErrorText(st.TrayAutostartError, w.cfg.lang)
}

// listIndex is the list row of the edited profile (-1 = none).
func (w *settingsWindow) listIndex() int {
	if w.ed.IsNew {
		return len(w.ed.Profiles)
	}
	return w.ed.Sel
}

func (w *settingsWindow) selectFromList() {
	i := int(int32(winapi.SendMessage(w.ctl[idSetList], winapi.LB_GETCURSEL, 0, 0)))
	if i == w.listIndex() {
		return
	}
	if !w.profileDiscardOK() {
		winapi.SendMessage(w.ctl[idSetList], winapi.LB_SETCURSEL, uintptr(w.listIndex()), 0)
		return
	}
	if w.ed.IsNew && i == len(w.ed.Profiles) {
		return
	}
	w.ed.Select(i)
	w.status = ""
	w.fill()
}

func (w *settingsWindow) newProfile() {
	if w.cfg.mode != traysettings.Admin || w.busy || !w.profileDiscardOK() {
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
	if w.page == 1 {
		w.saveSettings()
		return
	}
	if w.busy {
		return
	}
	w.readForm()
	req, pr := w.ed.SaveRequest(w.readPassword())
	if pr != nil {
		w.problem(pr)
		return
	}
	w.status = w.t(i18n.KeySetSaving)
	w.call(req, func(m model.IPCMessage, err error) {
		switch {
		case err != nil:
			w.problem(w.ed.ErrorProblem(err, traysettings.CallTimeout))
		case m.Type == model.MsgError:
			w.problem(w.ed.Reply(m))
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
	if !w.ask(fmt.Sprintf(w.t(i18n.KeySetDeleteConfirm), w.ed.Base.DisplayName())) {
		return
	}
	w.status = w.t(i18n.KeySetDeleting)
	w.call(req, func(m model.IPCMessage, err error) {
		switch {
		case err != nil:
			w.problem(w.ed.ErrorProblem(err, traysettings.CallTimeout))
		case m.Type == model.MsgError:
			w.problem(w.ed.Reply(m))
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
	req, pr := w.ed.TestRequest(w.readPassword())
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
			w.problem(w.ed.Reply(m))
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
			w.problem(w.ed.Reply(m))
		case m.Config == nil:
			w.problem(&traysettings.Problem{Text: fmt.Sprintf(w.t(i18n.KeySetFailed), "get_config: "+m.Type)})
		default:
			w.ed.LoadConfig(*m.Config, m.Revision)
			w.loaded = true
			w.status = ""
			w.fill()
		}
	})
}

// onSessionState follows the service connection: «Нет связи» while it is
// lost, and on (re)connection the data is loaded again. Unsaved edits are
// never dropped: with edits only the revision is checked in the
// background and a changed configuration is reported (the user decides
// with «Перечитать», which asks before discarding).
func (w *settingsWindow) onSessionState(st trayipc.Status) {
	up := st.State == trayipc.StateConnected
	was := w.connected
	w.connected = up
	if !up {
		if !w.busy {
			w.setStatus(w.t(i18n.KeySetNotConnected))
		}
		return
	}
	if was {
		w.update() // e.g. trayAutostartError of a new get_status
		return
	}
	if w.cfg.mode == traysettings.ReadOnly || !w.loaded || !w.ed.Unsaved() {
		w.Reload()
		return
	}
	w.checkRevision()
}

// checkRevision reads get_config without touching the form or the
// revision of the edit (used after a reconnection with unsaved edits).
func (w *settingsWindow) checkRevision() {
	if w.busy {
		return
	}
	rev := w.ed.Revision
	w.call(model.IPCRequest{Cmd: model.CmdGetConfig}, func(m model.IPCMessage, err error) {
		switch {
		case err != nil:
			w.problem(w.ed.ErrorProblem(err, traysettings.CallTimeout))
		case m.Type == model.MsgError:
			w.problem(w.ed.Reply(m))
		case m.Revision != rev:
			w.setStatus(w.t(i18n.KeySetChangedOffline))
		default:
			w.setStatus("")
		}
	})
}

// loadSnapshot fills the read-only window from a status snapshot.
func (w *settingsWindow) loadSnapshot(snap *model.StatusSnapshot) {
	if snap != nil {
		w.ed.LoadSnapshot(*snap)
		w.loaded = true
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

// userClose: the close box, «Закрыть» or Esc — asks before dropping
// unsaved edits.
func (w *settingsWindow) userClose() {
	if w.discardOK() {
		w.close()
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
	if w.disc != nil {
		w.disc.close()
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
	so := opt.Session
	so.OnEvent = func(e trayipc.Event) {
		if e.Kind != trayipc.EventState || w == nil {
			return
		}
		st := e.Status
		w.post(func() { w.onSessionState(st) })
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
		if d := w.disc; d != nil && winapi.IsWindow(d.hwnd) && winapi.IsDialogMessage(d.hwnd, &m) {
			continue
		}
		if winapi.IsWindow(w.hwnd) && winapi.IsDialogMessage(w.hwnd, &m) {
			continue
		}
		winapi.TranslateMessage(&m)
		winapi.DispatchMessage(&m)
	}
	return int(m.WParam)
}
