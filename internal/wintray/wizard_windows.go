//go:build windows

package wintray

import (
	"context"
	"fmt"
	"os"
	"time"
	"unicode/utf16"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// WizardClass is the window class of the «Добавить базу» wizard (phase 9b
// part 2, CHK-TRAY-30): «Добавить» of the administrator settings window
// when the service has the wizard commands.
const WizardClass = "ExchangeCheckerTrayWizard"

// Wizard control IDs.
const (
	idWizStep = 700 + iota
	idWizServer
	idWizFind
	idWizInstances
	idWizPort
	idWizAuth
	idWizLogin
	idWizPassword
	idWizEncrypt
	idWizDBs
	idWizRefresh
	idWizCands
	idWizResult
	idWizBack
	idWizNext
	idWizSave
	idWizGrant
	idWizAdvanced
	idWizCancel = winapi.IDCANCEL
)

// wizardTips: a tooltip on every input and button (CHK-TRAY-15 rule).
var wizardTips = map[int32]string{
	idWizServer: i18n.KeyTipWizServer, idWizFind: i18n.KeyTipWizFind, idWizInstances: i18n.KeyTipWizInstances,
	idWizPort: i18n.KeyTipWizPort, idWizAuth: i18n.KeyTipWizAuth, idWizLogin: i18n.KeyTipWizLogin,
	idWizPassword: i18n.KeyTipWizPassword, idWizEncrypt: i18n.KeyTipWizEncrypt, idWizDBs: i18n.KeyTipWizDBs,
	idWizRefresh: i18n.KeyTipWizRefresh, idWizCands: i18n.KeyTipWizCands, idWizResult: i18n.KeyTipWizResult,
	idWizBack: i18n.KeyTipWizBack, idWizNext: i18n.KeyTipWizNext, idWizSave: i18n.KeyTipWizSave,
	idWizGrant: i18n.KeyTipWizGrant, idWizAdvanced: i18n.KeyTipWizAdvanced, idWizCancel: i18n.KeyTipWizCancel,
}

// wizardOutputs are the static texts (no tooltip).
var wizardOutputs = map[int32]bool{idWizStep: true}

// wizField: the wizard control and step of a form field of an error.
var wizField = map[traysettings.Field]struct {
	id   int32
	step traysettings.WizardStep
}{
	traysettings.FieldHost: {idWizServer, traysettings.WizServer}, traysettings.FieldInstance: {idWizServer, traysettings.WizServer},
	traysettings.FieldPort: {idWizPort, traysettings.WizServer}, traysettings.FieldAuth: {idWizAuth, traysettings.WizServer},
	traysettings.FieldLogin: {idWizLogin, traysettings.WizServer}, traysettings.FieldPassword: {idWizPassword, traysettings.WizServer},
	traysettings.FieldEncrypt: {idWizEncrypt, traysettings.WizServer}, traysettings.FieldDatabase: {idWizDBs, traysettings.WizDatabase},
}

const (
	wizStyle     = winapi.WS_OVERLAPPED | winapi.WS_CAPTION | winapi.WS_SYSMENU
	wizCW, wizCH = 760, 560
)

// wizardDialog is the «Добавить базу» wizard. It edits the settings
// window's editor as a new profile (ToForm), so test_profile / discover /
// save_profile are the requests of the form; sql_* commands are admin
// only and this window exists only in the elevated settings process
// (no elevation of its own; access_denied gets a clear text).
type wizardDialog struct {
	w    *settingsWindow
	hwnd uintptr
	ctl  map[int32]uintptr
	tips *tooltips
	z    *traysettings.Wizard

	prevSel  int
	stepCtl  [3][]uintptr
	cur      *[]uintptr
	cancel   context.CancelFunc
	seq      int
	loading  bool
	closed   bool
	pwTyped  bool
	result   string
	lastCmds []string
	// lastField: the last highlighted field (test hook)
	lastField traysettings.Field
}

// wizardAvailable: «Добавить» opens the wizard (else the old form).
func (w *settingsWindow) wizardAvailable() bool {
	return traysettings.WizardAvailable(w.cfg.mode, w.cfg.session.Supported) && w.discoveryAvailable()
}

// openWizard opens the wizard (or brings the open one to the front).
func (w *settingsWindow) openWizard() {
	if wd := w.wiz; wd != nil {
		if !w.cfg.hidden && winapi.IsWindow(wd.hwnd) {
			frontWindow(wd.hwnd)
		}
		return
	}
	if w.busy || w.disc != nil || !w.profileDiscardOK() {
		return
	}
	wd := &wizardDialog{w: w, ctl: map[int32]uintptr{}, z: traysettings.NewWizard(w.cfg.lang), prevSel: w.ed.Sel}
	x, y, ww, wh := windowRect(wizCW, wizCH, wizStyle, 0)
	hwnd, err := createWindow(WizardClass, w.t(i18n.KeyWizTitle), 0, wizStyle, x, y, ww, wh, w.hwnd, wd.wndProc)
	if err != nil {
		w.setStatus(fmt.Sprintf(w.t(i18n.KeySetFailed), err.Error()))
		return
	}
	w.ed.New()
	w.status = ""
	w.fill()
	wd.hwnd = hwnd
	w.wiz = wd
	wd.build()
	wd.show()
	winapi.EnableWindow(w.hwnd, false)
	if !w.cfg.hidden {
		winapi.ShowWindow(hwnd, winapi.SW_SHOWNORMAL)
		winapi.SetForegroundWindow(hwnd)
	}
	winapi.SetFocus(wd.ctl[idWizServer])
}

func (wd *wizardDialog) t(key string) string { return wd.w.t(key) }

func (wd *wizardDialog) add(class, text string, ex, style uint32, id, x, y, cw, ch int32) uintptr {
	h, err := winapi.CreateWindowEx(ex, class, text, winapi.WS_CHILD|winapi.WS_VISIBLE|style, x, y, cw, ch, wd.hwnd, uintptr(id), winapi.GetModuleHandle())
	if err != nil {
		return 0
	}
	winapi.SendMessage(h, winapi.WM_SETFONT, winapi.GetStockObject(winapi.DEFAULT_GUI_FONT), 1)
	if id != 0 {
		wd.ctl[id] = h
	}
	if wd.cur != nil {
		*wd.cur = append(*wd.cur, h)
	}
	return h
}

func (wd *wizardDialog) build() {
	edit := uint32(winapi.WS_TABSTOP | winapi.ES_AUTOHSCROLL)
	list := uint32(winapi.WS_TABSTOP | winapi.WS_VSCROLL | winapi.LBS_NOTIFY)
	combo := uint32(winapi.WS_TABSTOP | winapi.CBS_DROPDOWNLIST)
	label := func(key string, x, y, cw int32) { wd.add("STATIC", wd.t(key), 0, winapi.SS_LEFT, 0, x, y+3, cw, 18) }
	wd.add("STATIC", "", 0, winapi.SS_LEFT, idWizStep, 12, 10, 736, 18)
	// step 1: server and login
	wd.cur = &wd.stepCtl[traysettings.WizServer]
	row := int32(40)
	label(i18n.KeyWizServer, 12, row, 120)
	wd.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idWizServer, 140, row, 450, 23)
	wd.add("BUTTON", wd.t(i18n.KeyWizFind), 0, winapi.WS_TABSTOP, idWizFind, 598, row-1, 150, 26)
	row += 30
	wd.add("LISTBOX", "", winapi.WS_EX_CLIENTEDGE, list, idWizInstances, 140, row, 608, 120)
	row += 126
	label(i18n.KeyWizPort, 12, row, 120)
	wd.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit|winapi.ES_NUMBER, idWizPort, 140, row, 80, 23)
	row += 28
	label(i18n.KeyWizAuth, 12, row, 120)
	wd.add("COMBOBOX", "", 0, combo, idWizAuth, 140, row, 450, 120)
	row += 28
	label(i18n.KeyWizLogin, 12, row, 120)
	wd.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idWizLogin, 140, row, 450, 23)
	row += 28
	label(i18n.KeyWizPassword, 12, row, 120)
	wd.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit|winapi.ES_PASSWORD, idWizPassword, 140, row, 450, 23)
	row += 28
	label(i18n.KeyWizEncrypt, 12, row, 120)
	wd.add("COMBOBOX", "", 0, combo, idWizEncrypt, 140, row, 450, 160)
	// step 2: database
	wd.cur = &wd.stepCtl[traysettings.WizDatabase]
	label(i18n.KeyWizDBs, 12, 40, 400)
	wd.add("LISTBOX", "", winapi.WS_EX_CLIENTEDGE, list, idWizDBs, 12, 64, 736, 270)
	wd.add("BUTTON", wd.t(i18n.KeyWizRefresh), 0, winapi.WS_TABSTOP, idWizRefresh, 12, 338, 180, 26)
	// step 3: test, exchange tables, save
	wd.cur = &wd.stepCtl[traysettings.WizFinish]
	label(i18n.KeyWizCands, 12, 40, 400)
	wd.add("LISTBOX", "", winapi.WS_EX_CLIENTEDGE, list, idWizCands, 12, 64, 736, 300)
	wd.cur = nil
	wd.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, winapi.WS_TABSTOP|winapi.ES_MULTILINE|winapi.ES_READONLY|winapi.ES_AUTOVSCROLL|winapi.WS_VSCROLL,
		idWizResult, 12, 372, 736, 140)
	bx := int32(12)
	button := func(id int32, key string, width int32) {
		wd.add("BUTTON", wd.t(key), 0, winapi.WS_TABSTOP, id, bx, 522, width, 28)
		bx += width + 6
	}
	button(idWizBack, i18n.KeyWizBack, 90)
	button(idWizNext, i18n.KeyWizNext, 90)
	button(idWizSave, i18n.KeyWizSave, 100)
	button(idWizGrant, i18n.KeyWizGrant, 210)
	button(idWizAdvanced, i18n.KeyWizAdvanced, 150)
	wd.add("BUTTON", wd.t(i18n.KeyWizCancel), 0, winapi.WS_TABSTOP, idWizCancel, 668, 522, 80, 28)
	for _, k := range []string{i18n.KeyWizAuthSQL, i18n.KeyWizAuthWindows} {
		winapi.SendMessageString(wd.ctl[idWizAuth], winapi.CB_ADDSTRING, 0, wd.t(k))
	}
	for _, k := range traysettings.EncryptKeys() {
		winapi.SendMessageString(wd.ctl[idWizEncrypt], winapi.CB_ADDSTRING, 0, wd.t(k))
	}
	winapi.SendMessage(wd.ctl[idWizAuth], winapi.CB_SETCURSEL, 0, 0)
	winapi.SendMessage(wd.ctl[idWizEncrypt], winapi.CB_SETCURSEL, uintptr(traysettings.EncryptIndex(wd.z.Encrypt)), 0)
	wd.tips = newTooltips(wd.hwnd)
	for id, h := range wd.ctl {
		if key, ok := wizardTips[id]; ok {
			wd.tips.set(h, wd.t(key))
		}
	}
}

func (wd *wizardDialog) wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case winapi.WM_COMMAND:
		wd.command(int32(wp&0xFFFF), uint32(wp>>16&0xFFFF))
		return 0, true
	case winapi.WM_CLOSE:
		wd.abort()
		return 0, true
	}
	if isTrayExit(msg) {
		wd.w.exitNow()
		return 0, true
	}
	return 0, false
}

func (wd *wizardDialog) command(id int32, code uint32) {
	switch id {
	case idWizPassword:
		if code == winapi.EN_CHANGE {
			wd.pwTyped = winapi.GetWindowTextLength(wd.ctl[idWizPassword]) > 0
		}
	case idWizAuth:
		if code == winapi.CBN_SELCHANGE {
			wd.read()
			wd.show()
		}
	case idWizInstances:
		if code == winapi.LBN_SELCHANGE && wd.z.PickInstance(wd.sel(idWizInstances)) {
			winapi.SetWindowText(wd.ctl[idWizServer], wd.z.Server)
			winapi.SetWindowText(wd.ctl[idWizPort], wd.z.Port)
		}
	case idWizDBs:
		if code == winapi.LBN_SELCHANGE && wd.z.PickDatabase(wd.sel(idWizDBs)) {
			wd.w.ed.ClearMapping()
			wd.show()
		}
	case idWizCands:
		if code == winapi.LBN_SELCHANGE {
			if i := wd.sel(idWizCands); wd.z.Choose(wd.w.ed, i) {
				wd.setResult(fmt.Sprintf(wd.t(i18n.KeyWizChosen), wd.z.CandidateLine(i)))
			}
		}
	case idWizFind:
		wd.browse()
	case idWizRefresh:
		wd.loadDatabases()
	case idWizBack:
		wd.back()
	case idWizNext:
		wd.next()
	case idWizSave:
		wd.save()
	case idWizGrant:
		wd.grant()
	case idWizAdvanced:
		wd.advanced()
	case idWizCancel:
		wd.abort()
	}
}

func (wd *wizardDialog) sel(id int32) int {
	return int(int32(winapi.SendMessage(wd.ctl[id], winapi.LB_GETCURSEL, 0, 0)))
}

// read copies the step 1 boxes into the wizard (never the password).
func (wd *wizardDialog) read() {
	z := wd.z
	z.Server = winapi.GetWindowText(wd.ctl[idWizServer])
	z.Port = winapi.GetWindowText(wd.ctl[idWizPort])
	z.Login = winapi.GetWindowText(wd.ctl[idWizLogin])
	z.Windows = winapi.SendMessage(wd.ctl[idWizAuth], winapi.CB_GETCURSEL, 0, 0) == 1
	if i := int(winapi.SendMessage(wd.ctl[idWizEncrypt], winapi.CB_GETCURSEL, 0, 0)); i >= 0 && i < len(traysettings.EncryptChoices) {
		z.Encrypt = traysettings.EncryptChoices[i]
	}
}

// password returns the password box text for one request (the UTF-16
// copy is wiped; nothing is kept).
func (wd *wizardDialog) password() string {
	if !wd.pwTyped || wd.z.Windows {
		return ""
	}
	buf := winapi.GetWindowTextUTF16(wd.ctl[idWizPassword])
	defer winapi.ZeroUTF16(buf)
	r := utf16.Decode(buf)
	s := string(r)
	for i := range r {
		r[i] = 0
	}
	return s
}

// form puts the wizard input into the editor; returns the password.
func (wd *wizardDialog) form() string {
	wd.read()
	wd.z.ToForm(wd.w.ed, wd.pwTyped)
	return wd.password()
}

// show refreshes the step: visible controls, header, enabled buttons.
func (wd *wizardDialog) show() {
	z := wd.z
	for i, hs := range wd.stepCtl {
		cmd := int32(winapi.SW_HIDE)
		if traysettings.WizardStep(i) == z.Step {
			cmd = winapi.SW_SHOW
		}
		for _, h := range hs {
			winapi.ShowWindow(h, cmd)
		}
	}
	names := []string{i18n.KeyWizStepServer, i18n.KeyWizStepDB, i18n.KeyWizStepFinish}
	winapi.SetWindowText(wd.ctl[idWizStep], fmt.Sprintf(wd.t(i18n.KeyWizStep), int(z.Step)+1, wd.t(names[z.Step])))
	running := wd.cancel != nil
	sess := wd.w.cfg.session
	winapi.EnableWindow(wd.ctl[idWizLogin], !z.Windows)
	winapi.EnableWindow(wd.ctl[idWizPassword], !z.Windows)
	winapi.EnableWindow(wd.ctl[idWizFind], !running)
	winapi.EnableWindow(wd.ctl[idWizRefresh], !running)
	winapi.EnableWindow(wd.ctl[idWizBack], z.Step > traysettings.WizServer)
	winapi.EnableWindow(wd.ctl[idWizNext], z.Step < traysettings.WizFinish && !running)
	winapi.EnableWindow(wd.ctl[idWizSave], z.Step == traysettings.WizFinish && z.Chosen >= 0 && !running)
	winapi.EnableWindow(wd.ctl[idWizGrant], z.Database != "" && !running && sess.Supported(model.CmdSQLGrantScript))
	if winapi.GetWindowText(wd.ctl[idWizResult]) != wd.result {
		winapi.SetWindowText(wd.ctl[idWizResult], wd.result)
	}
}

func (wd *wizardDialog) setResult(s string) {
	wd.result = s
	wd.show()
}

// problem shows a problem and highlights its field (going to its step).
func (wd *wizardDialog) problem(p *traysettings.Problem) {
	if p == nil {
		return
	}
	wd.result = p.Text
	if f, ok := wizField[p.Field]; ok {
		wd.lastField = p.Field
		if wd.z.Step != f.step {
			wd.z.Step = f.step
		}
		wd.show()
		winapi.SetFocus(wd.ctl[f.id])
		return
	}
	wd.show()
}

// run sends one request off the UI thread (cancel {ref} on DoCancel for
// the async commands); done gets the reply on the UI thread unless the
// wizard was closed or another request replaced it.
func (wd *wizardDialog) run(req model.IPCRequest, timeout time.Duration, done func(model.IPCMessage, error)) {
	if wd.cancel != nil {
		wd.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	wd.cancel = cancel
	wd.seq++
	seq := wd.seq
	wd.lastCmds = append(wd.lastCmds, req.Cmd)
	wd.show()
	s := wd.w.cfg.session
	go func() {
		m, err := s.DoCancel(ctx, req)
		cancel()
		wd.w.post(func() {
			if wd.closed || seq != wd.seq {
				return
			}
			wd.cancel = nil
			done(m, err)
			if !wd.closed {
				wd.show()
			}
		})
	}()
}

// fail shows an error reply: with errorInfo (or a failed test) the code,
// the diagnosis of sql_diagnose (local text without it) and the detail;
// else the clear texts of WizardProblem (access_denied, rate_limited…).
func (wd *wizardDialog) fail(m model.IPCMessage, err error, prefix string) {
	if err == nil && m.Code != model.ErrCodeAccessDenied {
		if req, code, ok := traysettings.DiagnoseRequest(m); ok {
			detail := m.Error
			if m.Test != nil {
				detail = m.Test.Error
			}
			wd.diagnose(req, code, detail, prefix)
			return
		}
	}
	pr := wd.w.ed.WizardProblem(m, err)
	if prefix != "" {
		pr.Text = prefix + "\r\n" + pr.Text
	}
	wd.problem(pr)
}

func (wd *wizardDialog) diagnose(req model.IPCRequest, code, detail, prefix string) {
	lang := wd.w.cfg.lang
	join := func(s string) string {
		if prefix != "" {
			return prefix + "\r\n" + s
		}
		return s
	}
	if !wd.w.cfg.session.Supported(model.CmdSQLDiagnose) {
		wd.setResult(join(traysettings.ErrorText(code, nil, detail, lang)))
		return
	}
	wd.run(req, traysettings.CallTimeout, func(m model.IPCMessage, err error) {
		var d *model.SQLDiagnosis
		if err == nil && m.Type == model.MsgSQLDiagnosis {
			d = m.SQLDiagnosis
		}
		wd.setResult(join(traysettings.ErrorText(code, d, detail, lang)))
	})
}

// browse: «Найти» (sql_browse).
func (wd *wizardDialog) browse() {
	wd.read()
	wd.setResult(wd.t(i18n.KeyWizBrowsing))
	wd.run(wd.z.BrowseRequest(), traysettings.WizardTimeout, func(m model.IPCMessage, err error) {
		if err != nil || m.Type != model.MsgSQLBrowse || m.SQLBrowse == nil {
			wd.fail(m, err, "")
			return
		}
		text := wd.z.BrowseDone(*m.SQLBrowse)
		lb := wd.ctl[idWizInstances]
		winapi.SendMessage(lb, winapi.LB_RESETCONTENT, 0, 0)
		for _, in := range wd.z.Instances {
			winapi.SendMessageString(lb, winapi.LB_ADDSTRING, 0, traysettings.InstanceLine(in))
		}
		wd.setResult(text)
	})
}

// next: server → database (sql_databases) → finish (test + discover).
func (wd *wizardDialog) next() {
	switch wd.z.Step {
	case traysettings.WizServer:
		wd.read()
		if pr := wd.z.ServerProblem(wd.w.ed); pr != nil {
			wd.problem(pr)
			return
		}
		wd.z.Step = traysettings.WizDatabase
		wd.loadDatabases()
	case traysettings.WizDatabase:
		if wd.z.Database == "" {
			wd.setResult(wd.t(i18n.KeyWizDBNone))
			return
		}
		wd.z.Step = traysettings.WizFinish
		wd.finish()
	}
}

func (wd *wizardDialog) back() {
	if wd.z.Step == traysettings.WizServer {
		return
	}
	if wd.cancel != nil {
		wd.cancel()
		wd.cancel = nil
		wd.seq++
	}
	wd.z.Step--
	wd.setResult("")
}

func (wd *wizardDialog) loadDatabases() {
	pw := wd.form()
	req, pr := wd.w.ed.InteractiveRequest(model.CmdSQLDatabases, pw)
	if pr != nil {
		wd.problem(pr)
		return
	}
	winapi.SendMessage(wd.ctl[idWizDBs], winapi.LB_RESETCONTENT, 0, 0)
	wd.z.Databases, wd.z.Database = nil, ""
	wd.setResult(wd.t(i18n.KeyWizDBLoading))
	wd.run(req, traysettings.WizardTimeout, func(m model.IPCMessage, err error) {
		if err != nil || m.Type != model.MsgSQLDatabases || m.SQLDatabases == nil {
			wd.fail(m, err, "")
			return
		}
		text := wd.z.DatabasesDone(*m.SQLDatabases)
		lb := wd.ctl[idWizDBs]
		for _, d := range wd.z.Databases {
			winapi.SendMessageString(lb, winapi.LB_ADDSTRING, 0, wd.z.DatabaseLine(d))
		}
		wd.setResult(text)
	})
}

// finish: test_profile, then discover; autoPick decides (autoApply or a
// single candidate ≥ autoPickMin), else the user picks.
func (wd *wizardDialog) finish() {
	pw := wd.form()
	winapi.SendMessage(wd.ctl[idWizCands], winapi.LB_RESETCONTENT, 0, 0)
	req, pr := wd.w.ed.TestRequest(pw)
	if pr != nil {
		wd.problem(pr)
		return
	}
	wd.setResult(wd.t(i18n.KeyWizTesting))
	wd.run(req, traysettings.TestTimeout, func(m model.IPCMessage, err error) {
		if err != nil || m.Type != model.MsgTestResult || m.Test == nil || !m.Test.OK {
			wd.fail(m, err, "")
			return
		}
		wd.z.Tested = true
		wd.discover(wd.t(i18n.KeyWizTestOK))
	})
}

func (wd *wizardDialog) discover(prefix string) {
	pw := wd.form()
	req, pr := wd.w.ed.InteractiveRequest(model.CmdDiscover, pw)
	if pr != nil {
		wd.problem(pr)
		return
	}
	wd.setResult(prefix + "\r\n" + wd.t(i18n.KeyWizDiscovering))
	wd.run(req, traysettings.DiscoveryTimeout, func(m model.IPCMessage, err error) {
		if err != nil || m.Type != model.MsgDiscovery {
			wd.fail(m, err, prefix)
			return
		}
		idx, why := wd.z.DiscoveryDone(m)
		lb := wd.ctl[idWizCands]
		for i := range wd.z.Candidates {
			winapi.SendMessageString(lb, winapi.LB_ADDSTRING, 0, wd.z.CandidateLine(i))
		}
		text := prefix + "\r\n" + why
		if idx >= 0 && wd.z.Choose(wd.w.ed, idx) {
			winapi.SendMessage(lb, winapi.LB_SETCURSEL, uintptr(idx), 0)
			text = prefix + "\r\n" + fmt.Sprintf(wd.t(i18n.KeyWizApplied), wd.z.CandidateLine(idx))
			if why != "" {
				text += "\r\n" + why
			}
		}
		wd.setResult(text)
	})
}

// save: save_profile of the new profile with expectedRevision.
func (wd *wizardDialog) save() {
	if wd.z.Chosen < 0 {
		wd.setResult(wd.t(i18n.KeyWizChoose))
		return
	}
	pw := wd.form()
	req, pr := wd.w.ed.SaveRequest(pw)
	if pr != nil {
		wd.problem(pr)
		return
	}
	wd.setResult(wd.t(i18n.KeyWizSaving))
	wd.run(req, traysettings.CallTimeout, func(m model.IPCMessage, err error) {
		if err != nil || m.Type == model.MsgError {
			wd.fail(m, err, "")
			return
		}
		w := wd.w
		w.status = w.ed.SaveDone(m)
		wd.close()
		w.fill()
	})
}

// grant: sql_grant_script → the clipboard (never run, never logged).
func (wd *wizardDialog) grant() {
	wd.read()
	computer, domain := os.Getenv("COMPUTERNAME"), os.Getenv("USERDOMAIN")
	wd.run(wd.z.GrantRequest(computer, domain), traysettings.CallTimeout, func(m model.IPCMessage, err error) {
		if err != nil || m.Type != model.MsgSQLGrantScript || m.SQLGrantScript == nil {
			wd.fail(m, err, "")
			return
		}
		clip := wd.w.cfg.clipboard
		if clip == nil {
			clip = winapi.SetClipboardText
		}
		if cerr := clip(wd.hwnd, m.SQLGrantScript.SQL); cerr != nil {
			wd.setResult(fmt.Sprintf(wd.t(i18n.KeyWizClipFailed), cerr.Error()))
			return
		}
		wd.setResult(wd.z.GrantText(*m.SQLGrantScript))
	})
}

// advanced: continue in the old form with what was entered (the typed
// password is not carried over: the form asks for it again).
func (wd *wizardDialog) advanced() {
	wd.read()
	wd.z.ToForm(wd.w.ed, false)
	w := wd.w
	wd.close()
	w.status = ""
	w.fill()
	w.focus(traysettings.FieldHost)
}

// abort: «Отмена» / the close box — back to the profile shown before.
func (wd *wizardDialog) abort() {
	w := wd.w
	wd.close()
	w.ed.Select(wd.prevSel)
	w.fill()
}

// close cancels a running request and destroys the wizard.
func (wd *wizardDialog) close() {
	if wd.closed {
		return
	}
	wd.closed = true
	if wd.cancel != nil {
		wd.cancel()
		wd.cancel = nil
	}
	w := wd.w
	w.wiz = nil
	w.qmu.Lock()
	owner := !w.closed // the settings window itself is being destroyed
	w.qmu.Unlock()
	if owner {
		winapi.EnableWindow(w.hwnd, true)
	}
	winapi.DestroyWindow(wd.hwnd)
	if !owner {
		return
	}
	if !w.cfg.hidden && winapi.IsWindow(w.hwnd) {
		winapi.SetForegroundWindow(w.hwnd)
	}
	w.update()
}
