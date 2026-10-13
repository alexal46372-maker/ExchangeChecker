//go:build windows

package wininstall

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Setup wizard window (docs/PHASE7.md §1.3, CHK-SETUP-10): one top-level
// window of class WizardClass with plain controls (no dialog templates),
// keyboard via IsDialogMessageW. The page logic is WizardState
// (wizard.go); this file only renders it. Never created in silent mode.

const (
	WizardClass    = "ExchangeCheckerSetup"
	UninstallClass = "ExchangeCheckerUninstall"

	// control IDs (tests drive them with SendMessage)
	IDBack      = 101
	IDNext      = 102
	IDCancel    = 103
	IDBrowse    = 104
	IDDir       = 105
	IDShortcut  = 106
	IDService   = 107
	IDTray      = 108
	IDText      = 109
	IDProgress  = 110
	IDStatus    = 111
	IDError     = 112
	IDPurge     = 113
	IDUninstall = 114

	// MsgGetPage: SendMessage(hwnd, MsgGetPage, 0, 0) returns the Page.
	MsgGetPage   = winapi.WM_APP + 20
	msgProgress  = winapi.WM_APP + 1
	msgDone      = winapi.WM_APP + 2
	wmSetText    = 0x000C
	wmGetTextLen = 0x000E

	BM_GETCHECK = 0x00F0
	BM_SETCHECK = 0x00F1
	BM_CLICK    = 0x00F5

	bsPushButton   = 0x0
	bsAutoCheckbox = 0x3
	esAutoHScroll  = 0x80
	esReadOnly     = 0x800
	wsBorder       = 0x00800000
	wsMinimizeBox  = 0x00020000
	pbmSetRange32  = winapi.WM_USER + 6
	pbmSetPos      = winapi.WM_USER + 2
	mbYesNo        = 0x4
	mbIconQuestion = 0x20
	idYes          = 6
)

// WizardConfig wires the wizard to the installer.
type WizardConfig struct {
	State   *WizardState
	Base    Options
	LogPath string
	// Run performs the install (worker goroutine); progress reports a step.
	Run func(o Options, progress func(string)) Result
	// NoCancelConfirm skips the "Cancel the installation?" box (tests).
	NoCancelConfirm bool
}

// WizardOutcome is what the user ended with.
type WizardOutcome struct {
	Result     Result
	Cancelled  bool
	LaunchTray bool
}

type wizard struct {
	cfg   WizardConfig
	hwnd  uintptr
	ctl   map[int]uintptr
	out   WizardOutcome
	mu    sync.Mutex
	steps int
	last  string
	res   Result
}

var (
	curWizard     *wizard
	curUninstall  *uninstallDlg
	setupProcCB   = syscall.NewCallback(setupWndProc)
	classesOnce   sync.Once
	classesErr    error
	expectedSteps = 14 // approximate log lines of an install, for the bar
)

func registerSetupClasses() error {
	classesOnce.Do(func() {
		for _, name := range []string{WizardClass, UninstallClass} {
			wc := winapi.WNDCLASSEXW{
				WndProc: setupProcCB, Instance: winapi.GetModuleHandle(),
				Cursor: winapi.LoadCursor(winapi.IDC_ARROW), Background: 16, // COLOR_BTNFACE+1
				ClassName: winapi.UTF16Ptr(name),
			}
			wc.Size = uint32(unsafe.Sizeof(wc))
			if _, err := winapi.RegisterClassEx(&wc); err != nil {
				classesErr = err
				return
			}
		}
	})
	return classesErr
}

func setupWndProc(hwnd, msg, wp, lp uintptr) uintptr {
	if w := curWizard; w != nil && (w.hwnd == hwnd || w.hwnd == 0) {
		if r, ok := w.proc(hwnd, uint32(msg), wp, lp); ok {
			return r
		}
	} else if u := curUninstall; u != nil && (u.hwnd == hwnd || u.hwnd == 0) {
		if r, ok := u.proc(hwnd, uint32(msg), wp, lp); ok {
			return r
		}
	}
	return winapi.DefWindowProc(hwnd, uint32(msg), wp, lp)
}

func child(parent uintptr, class, text string, style uint32, id int, x, y, w, h int32) uintptr {
	hc, _ := winapi.CreateWindowEx(0, class, text, winapi.WS_CHILD|winapi.WS_VISIBLE|style,
		x, y, w, h, parent, uintptr(id), winapi.GetModuleHandle())
	winapi.SendMessage(hc, winapi.WM_SETFONT, winapi.GetStockObject(winapi.DEFAULT_GUI_FONT), 1)
	return hc
}

func centered(cw, ch int32, style uint32) (x, y, w, h int32) {
	r := winapi.RECT{Right: cw, Bottom: ch}
	winapi.AdjustWindowRectEx(&r, style, 0)
	w, h = r.Right-r.Left, r.Bottom-r.Top
	if wa, ok := winapi.WorkArea(); ok {
		x = wa.Left + (wa.Right-wa.Left-w)/2
		y = wa.Top + (wa.Bottom-wa.Top-h)/2
	}
	return
}

func runLoop(hwnd uintptr) {
	var m winapi.MSG
	for winapi.GetMessage(&m) > 0 {
		if winapi.IsDialogMessage(hwnd, &m) {
			continue
		}
		winapi.TranslateMessage(&m)
		winapi.DispatchMessage(&m)
	}
}

// RunWizard shows the wizard and blocks until it is closed. It locks the
// calling goroutine to its thread (message loop, OLE for Browse).
func RunWizard(cfg WizardConfig) (WizardOutcome, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	winapi.InitCommonControls()
	if err := winapi.OleInitialize(); err == nil {
		defer winapi.OleUninitialize()
	}
	if err := registerSetupClasses(); err != nil {
		return WizardOutcome{}, err
	}
	w := &wizard{cfg: cfg, ctl: map[int]uintptr{}}
	curWizard = w
	defer func() { curWizard = nil }()
	s := cfg.State
	style := uint32(winapi.WS_OVERLAPPED | winapi.WS_CAPTION | winapi.WS_SYSMENU | wsMinimizeBox)
	x, y, ww, hh := centered(500, 330, style)
	hwnd, err := winapi.CreateWindowEx(0, WizardClass, T(s.Lang, "setup.title", s.Version), style,
		x, y, ww, hh, 0, 0, winapi.GetModuleHandle())
	if err != nil {
		return WizardOutcome{}, err
	}
	w.hwnd = hwnd
	w.build()
	w.render()
	winapi.ShowWindow(hwnd, winapi.SW_SHOW)
	winapi.SetForegroundWindow(hwnd)
	runLoop(hwnd)
	return w.out, nil
}

func (w *wizard) build() {
	h, L := w.hwnd, w.cfg.State.Lang
	w.ctl[IDText] = child(h, "STATIC", "", winapi.SS_LEFT, IDText, 20, 20, 460, 120)
	w.ctl[IDDir] = child(h, "EDIT", w.cfg.State.Dir, winapi.WS_TABSTOP|wsBorder|esAutoHScroll, IDDir, 20, 50, 360, 24)
	w.ctl[IDBrowse] = child(h, "BUTTON", T(L, "setup.btn.browse"), winapi.WS_TABSTOP|bsPushButton, IDBrowse, 390, 49, 90, 26)
	w.ctl[IDError] = child(h, "STATIC", "", winapi.SS_LEFT, IDError, 20, 90, 460, 60)
	w.ctl[IDShortcut] = child(h, "BUTTON", T(L, "setup.options.shortcut"), winapi.WS_TABSTOP|bsAutoCheckbox, IDShortcut, 20, 30, 460, 24)
	w.ctl[IDService] = child(h, "BUTTON", T(L, "setup.options.service"), winapi.WS_TABSTOP|bsAutoCheckbox, IDService, 20, 60, 460, 24)
	w.ctl[IDStatus] = child(h, "STATIC", "", winapi.SS_LEFT, IDStatus, 20, 50, 460, 40)
	w.ctl[IDProgress] = child(h, "msctls_progress32", "", 0, IDProgress, 20, 100, 460, 20)
	winapi.SendMessage(w.ctl[IDProgress], pbmSetRange32, 0, 100)
	w.ctl[IDTray] = child(h, "BUTTON", T(L, "setup.finish.tray"), winapi.WS_TABSTOP|bsAutoCheckbox, IDTray, 20, 160, 460, 24)
	w.ctl[IDBack] = child(h, "BUTTON", T(L, "setup.btn.back"), winapi.WS_TABSTOP|bsPushButton, IDBack, 200, 280, 88, 28)
	w.ctl[IDNext] = child(h, "BUTTON", T(L, "setup.btn.next"), winapi.WS_TABSTOP|winapi.BS_DEFPUSHBUTTON, IDNext, 292, 280, 88, 28)
	w.ctl[IDCancel] = child(h, "BUTTON", T(L, "setup.btn.cancel"), winapi.WS_TABSTOP|bsPushButton, IDCancel, 392, 280, 88, 28)
	setCheck(w.ctl[IDShortcut], w.cfg.State.Shortcut)
	setCheck(w.ctl[IDService], w.cfg.State.Service)
	setCheck(w.ctl[IDTray], w.cfg.State.LaunchTray)
	if w.cfg.State.DirLocked {
		winapi.SendMessage(w.ctl[IDDir], 0x00CF /*EM_SETREADONLY*/, 1, 0)
		winapi.EnableWindow(w.ctl[IDBrowse], false)
	}
	if w.cfg.State.ServiceLocked() {
		// upgrade/reinstall keep the old service state (§7.5): the checkbox
		// only shows it and cannot be changed
		winapi.EnableWindow(w.ctl[IDService], false)
	}
}

func setCheck(h uintptr, on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	winapi.SendMessage(h, BM_SETCHECK, v, 0)
}

func checked(h uintptr) bool { return winapi.SendMessage(h, BM_GETCHECK, 0, 0) == 1 }

func (w *wizard) show(ids []int, on bool) {
	cmd := int32(winapi.SW_HIDE)
	if on {
		cmd = winapi.SW_SHOW
	}
	for _, id := range ids {
		winapi.ShowWindow(w.ctl[id], cmd)
	}
}

func (w *wizard) render() {
	s, L := w.cfg.State, w.cfg.State.Lang
	w.show([]int{IDText, IDDir, IDBrowse, IDError, IDShortcut, IDService, IDStatus, IDProgress, IDTray}, false)
	winapi.SetWindowText(w.ctl[IDNext], T(L, "setup.btn.next"))
	switch s.Page {
	case PageWelcome:
		winapi.SetWindowText(w.ctl[IDText], s.WelcomeText())
		w.show([]int{IDText}, true)
	case PageFolder:
		lbl := T(L, "setup.folder.label")
		if s.DirLocked {
			lbl += " " + T(L, "setup.folder.upgrade")
		}
		winapi.SetWindowText(w.ctl[IDText], lbl)
		w.show([]int{IDText, IDDir, IDBrowse, IDError}, true)
		winapi.MoveWindow(w.ctl[IDText], 20, 20, 460, 24)
	case PageOptions:
		w.show([]int{IDShortcut, IDService}, true)
		winapi.SetWindowText(w.ctl[IDNext], T(L, "setup.btn.install"))
	case PageProgress:
		winapi.SetWindowText(w.ctl[IDStatus], T(L, "setup.progress.label"))
		w.show([]int{IDStatus, IDProgress}, true)
	case PageFinish:
		winapi.MoveWindow(w.ctl[IDText], 20, 20, 460, 130)
		winapi.SetWindowText(w.ctl[IDText], s.FinishText(w.cfg.LogPath))
		w.show([]int{IDText}, true)
		w.show([]int{IDTray}, s.ShowTrayCheckbox())
		winapi.SetWindowText(w.ctl[IDNext], T(L, "setup.btn.finish"))
	}
	if s.Page == PageWelcome {
		winapi.MoveWindow(w.ctl[IDText], 20, 20, 460, 130)
	}
	w.show([]int{IDBack}, s.Page != PageFinish)
	winapi.EnableWindow(w.ctl[IDBack], s.CanBack())
	winapi.EnableWindow(w.ctl[IDNext], s.Page != PageProgress && !s.Blocked())
	w.show([]int{IDCancel}, s.Page != PageFinish)
	winapi.EnableWindow(w.ctl[IDCancel], s.CanCancel())
}

func (w *wizard) proc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	s := w.cfg.State
	switch msg {
	case MsgGetPage:
		return uintptr(s.Page), true
	case winapi.WM_COMMAND:
		if wp>>16 != winapi.BN_CLICKED {
			return 0, false
		}
		switch int(wp & 0xFFFF) {
		case IDNext:
			w.onNext()
		case IDBack:
			s.Back()
			w.render()
		case IDCancel:
			w.onCancel()
		case IDBrowse:
			dir, ok, err := winapi.BrowseForFolder(w.hwnd, T(s.Lang, "setup.folder.browse"))
			switch {
			case err != nil:
				winapi.SetWindowText(w.ctl[IDError], T(s.Lang, "setup.folder.browsefail", err.Error()))
			case ok:
				winapi.SetWindowText(w.ctl[IDDir], dir)
				winapi.SetWindowText(w.ctl[IDError], "")
			}
		}
		return 0, true
	case msgProgress:
		w.mu.Lock()
		text, n := w.last, w.steps
		w.mu.Unlock()
		winapi.SetWindowText(w.ctl[IDStatus], text)
		pos := n * 100 / expectedSteps
		if pos > 95 {
			pos = 95
		}
		winapi.SendMessage(w.ctl[IDProgress], pbmSetPos, uintptr(pos), 0)
		return 0, true
	case msgDone:
		winapi.SendMessage(w.ctl[IDProgress], pbmSetPos, 100, 0)
		w.mu.Lock()
		r := w.res
		w.mu.Unlock()
		s.Result = &r
		w.out.Result = r
		_ = s.Next()
		w.render()
		return 0, true
	case winapi.WM_CLOSE:
		switch {
		case s.Page == PageFinish:
			w.finish()
		case s.CanCancel():
			w.onCancel()
		}
		return 0, true
	case winapi.WM_DESTROY:
		winapi.PostQuitMessage(0)
		return 0, true
	}
	return 0, false
}

func (w *wizard) onNext() {
	s := w.cfg.State
	switch s.Page {
	case PageFolder:
		s.Dir = winapi.GetWindowText(w.ctl[IDDir])
		if err := s.Next(); err != nil {
			winapi.SetWindowText(w.ctl[IDError], err.Error())
			return
		}
		winapi.SetWindowText(w.ctl[IDError], "")
	case PageOptions:
		s.Shortcut = checked(w.ctl[IDShortcut])
		if !s.ServiceLocked() {
			s.Service = checked(w.ctl[IDService])
		}
		_ = s.Next()
		w.render()
		w.start()
		return
	case PageFinish:
		w.finish()
		return
	default:
		_ = s.Next()
	}
	w.render()
}

func (w *wizard) start() {
	opts := w.cfg.State.Options(w.cfg.Base)
	hwnd := w.hwnd
	go func() {
		r := w.cfg.Run(opts, func(step string) {
			w.mu.Lock()
			w.last, w.steps = step, w.steps+1
			w.mu.Unlock()
			winapi.PostMessage(hwnd, msgProgress, 0, 0)
		})
		w.mu.Lock()
		w.res = r
		w.mu.Unlock()
		winapi.PostMessage(hwnd, msgDone, 0, 0)
	}()
}

func (w *wizard) onCancel() {
	if !w.cfg.State.CanCancel() {
		return
	}
	if w.cfg.State.Blocked() {
		// downgrade refused on the Welcome page: nothing changed, exit 3
		w.out.Result = Result{Code: ExitDowngrade, Err: ErrDowngradeRefused}
		winapi.DestroyWindow(w.hwnd)
		return
	}
	if !w.cfg.NoCancelConfirm {
		L := w.cfg.State.Lang
		if winapi.MessageBox(w.hwnd, T(L, "setup.cancel.confirm"), T(L, "setup.title", w.cfg.State.Version), mbYesNo|mbIconQuestion) != idYes {
			return
		}
	}
	w.out.Cancelled = true
	w.out.Result = Result{Code: ExitCancelled}
	winapi.DestroyWindow(w.hwnd)
}

func (w *wizard) finish() {
	w.out.LaunchTray = w.cfg.State.ShowTrayCheckbox() && checked(w.ctl[IDTray])
	winapi.DestroyWindow(w.hwnd)
}

// --- uninstall confirmation (§6.2 step 1): checkbox "remove data", off.

type uninstallDlg struct {
	hwnd   uintptr
	purge  uintptr
	ok     bool
	purged bool
}

// ConfirmUninstall shows the confirmation window; ok = false on Cancel.
func ConfirmUninstall(lang string) (ok, purge bool, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	winapi.InitCommonControls()
	if err := registerSetupClasses(); err != nil {
		return false, false, err
	}
	u := &uninstallDlg{}
	curUninstall = u
	defer func() { curUninstall = nil }()
	style := uint32(winapi.WS_OVERLAPPED | winapi.WS_CAPTION | winapi.WS_SYSMENU)
	x, y, ww, hh := centered(460, 170, style)
	hwnd, err := winapi.CreateWindowEx(0, UninstallClass, T(lang, "setup.uninstall.title"), style, x, y, ww, hh, 0, 0, winapi.GetModuleHandle())
	if err != nil {
		return false, false, err
	}
	u.hwnd = hwnd
	child(hwnd, "STATIC", T(lang, "setup.uninstall.text"), winapi.SS_LEFT, IDText, 20, 16, 420, 48)
	u.purge = child(hwnd, "BUTTON", T(lang, "setup.uninstall.purge"), winapi.WS_TABSTOP|bsAutoCheckbox, IDPurge, 20, 72, 420, 24)
	setCheck(u.purge, false) // default off (PLAN §9)
	child(hwnd, "BUTTON", T(lang, "setup.btn.uninstall"), winapi.WS_TABSTOP|winapi.BS_DEFPUSHBUTTON, IDUninstall, 252, 124, 92, 28)
	child(hwnd, "BUTTON", T(lang, "setup.btn.cancel"), winapi.WS_TABSTOP|bsPushButton, IDCancel, 352, 124, 88, 28)
	winapi.ShowWindow(hwnd, winapi.SW_SHOW)
	winapi.SetForegroundWindow(hwnd)
	runLoop(hwnd)
	return u.ok, u.purged, nil
}

func (u *uninstallDlg) proc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case winapi.WM_COMMAND:
		if wp>>16 != winapi.BN_CLICKED {
			return 0, false
		}
		switch int(wp & 0xFFFF) {
		case IDUninstall:
			u.ok, u.purged = true, checked(u.purge)
			winapi.DestroyWindow(hwnd)
		case IDCancel:
			winapi.DestroyWindow(hwnd)
		}
		return 0, true
	case winapi.WM_CLOSE:
		winapi.DestroyWindow(hwnd)
		return 0, true
	case winapi.WM_DESTROY:
		winapi.PostQuitMessage(0)
		return 0, true
	}
	return 0, false
}
