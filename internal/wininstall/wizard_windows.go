//go:build windows

package wininstall

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Setup wizard window (docs/PHASE7.md §1.3, CHK-SETUP-10 / CHK-SETUP-14):
// one top-level window of class WizardClass with plain controls (no dialog
// templates), keyboard via IsDialogMessageW. Page logic is WizardState
// (wizard.go); this file renders the modern chrome. Never created in silent mode.

const (
	WizardClass    = "ExchangeCheckerSetup"
	UninstallClass = "ExchangeCheckerUninstall"

	// Control IDs: see wizard_layout.go (CHK-SETUP-14 contract).

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
	cfg    WizardConfig
	hwnd   uintptr
	ctl    map[int]uintptr
	out    WizardOutcome
	mu     sync.Mutex
	steps  int
	last   string
	res    Result
	chrome *setupChrome
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
				Cursor: winapi.LoadCursor(winapi.IDC_ARROW),
				// Background 0: we paint via WM_ERASEBKGND / WM_PAINT (not COLOR_BTNFACE).
				Background: 0,
				ClassName:  winapi.UTF16Ptr(name),
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
	w := &wizard{cfg: cfg, ctl: map[int]uintptr{}, chrome: newSetupChrome()}
	curWizard = w
	defer func() {
		curWizard = nil
		w.chrome.destroy()
	}()
	s := cfg.State
	style := uint32(winapi.WS_OVERLAPPED | winapi.WS_CAPTION | winapi.WS_SYSMENU | wsMinimizeBox)
	x, y, ww, hh := centered(WizardClientW, WizardClientH, style)
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
	cx, cw := ContentLeft(), ContentWidth()
	top := ContentTop()
	// Title / body text (welcome, folder label, finish)
	w.ctl[IDText] = child(h, "STATIC", "", winapi.SS_LEFT, IDText, cx, top, cw, 120)
	w.chrome.applyFont(w.ctl[IDText], true)
	// Folder row
	w.ctl[IDDir] = child(h, "EDIT", w.cfg.State.Dir, winapi.WS_TABSTOP|wsBorder|esAutoHScroll, IDDir, cx, top+36, cw-100, WizardEditH)
	w.chrome.applyFont(w.ctl[IDDir], false)
	w.ctl[IDBrowse] = child(h, "BUTTON", T(L, "setup.btn.browse"), winapi.WS_TABSTOP|bsPushButton, IDBrowse, cx+cw-96, top+34, 96, WizardBtnH)
	w.chrome.applyFont(w.ctl[IDBrowse], false)
	w.ctl[IDError] = child(h, "STATIC", "", winapi.SS_LEFT, IDError, cx, top+72, cw, 60)
	w.chrome.applyFont(w.ctl[IDError], false)
	// Options
	w.ctl[IDShortcut] = child(h, "BUTTON", T(L, "setup.options.shortcut"), winapi.WS_TABSTOP|bsAutoCheckbox, IDShortcut, cx, top+8, cw, 24)
	w.chrome.applyFont(w.ctl[IDShortcut], false)
	w.ctl[IDService] = child(h, "BUTTON", T(L, "setup.options.service"), winapi.WS_TABSTOP|bsAutoCheckbox, IDService, cx, top+8+32, cw, 24)
	w.chrome.applyFont(w.ctl[IDService], false)
	// Progress
	w.ctl[IDStatus] = child(h, "STATIC", "", winapi.SS_LEFT, IDStatus, cx, top+8, cw, 40)
	w.chrome.applyFont(w.ctl[IDStatus], false)
	w.ctl[IDProgress] = child(h, "msctls_progress32", "", 0, IDProgress, cx, top+56, cw, WizardProgH)
	winapi.SendMessage(w.ctl[IDProgress], pbmSetRange32, 0, 100)
	winapi.SendMessage(w.ctl[IDProgress], pbmSetBarColor, 0, uintptr(ColorAccent))
	// Finish tray checkbox
	w.ctl[IDTray] = child(h, "BUTTON", T(L, "setup.finish.tray"), winapi.WS_TABSTOP|bsAutoCheckbox, IDTray, cx, ContentBottom()-36, cw, 24)
	w.chrome.applyFont(w.ctl[IDTray], false)
	// Button bar (right-aligned primary)
	by := ButtonBarTop() + (WizardBtnBarH-WizardBtnH)/2
	right := int32(WizardClientW - WizardPad)
	bw, bh, gap := int32(WizardBtnW), int32(WizardBtnH), int32(WizardBtnGap)
	w.ctl[IDCancel] = child(h, "BUTTON", T(L, "setup.btn.cancel"), winapi.WS_TABSTOP|bsPushButton, IDCancel, right-bw, by, bw, bh)
	w.ctl[IDNext] = child(h, "BUTTON", T(L, "setup.btn.next"), winapi.WS_TABSTOP|winapi.BS_DEFPUSHBUTTON, IDNext, right-bw*2-gap, by, bw, bh)
	w.ctl[IDBack] = child(h, "BUTTON", T(L, "setup.btn.back"), winapi.WS_TABSTOP|bsPushButton, IDBack, right-bw*3-gap*2, by, bw, bh)
	for _, id := range []int{IDBack, IDNext, IDCancel} {
		w.chrome.applyFont(w.ctl[id], false)
	}
	setCheck(w.ctl[IDShortcut], w.cfg.State.Shortcut)
	setCheck(w.ctl[IDService], w.cfg.State.Service)
	setCheck(w.ctl[IDTray], w.cfg.State.LaunchTray)
	if w.cfg.State.DirLocked {
		winapi.SendMessage(w.ctl[IDDir], 0x00CF /*EM_SETREADONLY*/, 1, 0)
		winapi.EnableWindow(w.ctl[IDBrowse], false)
	}
	if w.cfg.State.ServiceLocked() {
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
	cx, cw := ContentLeft(), ContentWidth()
	top := ContentTop()
	w.show([]int{IDText, IDDir, IDBrowse, IDError, IDShortcut, IDService, IDStatus, IDProgress, IDTray}, false)
	winapi.SetWindowText(w.ctl[IDNext], T(L, "setup.btn.next"))
	switch s.Page {
	case PageWelcome:
		winapi.MoveWindow(w.ctl[IDText], cx, top, cw, ContentBottom()-top-8)
		w.chrome.applyFont(w.ctl[IDText], false)
		winapi.SetWindowText(w.ctl[IDText], s.WelcomeText())
		w.show([]int{IDText}, true)
	case PageFolder:
		lbl := T(L, "setup.folder.label")
		if s.DirLocked {
			lbl += " " + T(L, "setup.folder.upgrade")
		}
		winapi.MoveWindow(w.ctl[IDText], cx, top, cw, WizardTitleH)
		w.chrome.applyFont(w.ctl[IDText], true)
		winapi.SetWindowText(w.ctl[IDText], lbl)
		winapi.MoveWindow(w.ctl[IDDir], cx, top+36, cw-100, WizardEditH)
		winapi.MoveWindow(w.ctl[IDBrowse], cx+cw-96, top+34, 96, WizardBtnH)
		winapi.MoveWindow(w.ctl[IDError], cx, top+72, cw, 60)
		w.show([]int{IDText, IDDir, IDBrowse, IDError}, true)
	case PageOptions:
		winapi.MoveWindow(w.ctl[IDShortcut], cx, top+8, cw, 24)
		winapi.MoveWindow(w.ctl[IDService], cx, top+8+32, cw, 24)
		w.show([]int{IDShortcut, IDService}, true)
		winapi.SetWindowText(w.ctl[IDNext], T(L, "setup.btn.install"))
	case PageProgress:
		winapi.MoveWindow(w.ctl[IDStatus], cx, top+8, cw, 40)
		winapi.MoveWindow(w.ctl[IDProgress], cx, top+56, cw, WizardProgH)
		winapi.SetWindowText(w.ctl[IDStatus], T(L, "setup.progress.label"))
		w.show([]int{IDStatus, IDProgress}, true)
	case PageFinish:
		trayH := int32(0)
		if s.ShowTrayCheckbox() {
			trayH = 36
		}
		winapi.MoveWindow(w.ctl[IDText], cx, top, cw, ContentBottom()-top-trayH-8)
		w.chrome.applyFont(w.ctl[IDText], false)
		winapi.SetWindowText(w.ctl[IDText], s.FinishText(w.cfg.LogPath))
		w.show([]int{IDText}, true)
		if s.ShowTrayCheckbox() {
			winapi.MoveWindow(w.ctl[IDTray], cx, ContentBottom()-28, cw, 24)
			w.show([]int{IDTray}, true)
		}
		winapi.SetWindowText(w.ctl[IDNext], T(L, "setup.btn.finish"))
	}
	w.show([]int{IDBack}, s.Page != PageFinish)
	winapi.EnableWindow(w.ctl[IDBack], s.CanBack())
	winapi.EnableWindow(w.ctl[IDNext], s.Page != PageProgress && !s.Blocked())
	w.show([]int{IDCancel}, s.Page != PageFinish)
	winapi.EnableWindow(w.ctl[IDCancel], s.CanCancel())
	winapi.InvalidateRect(w.hwnd, false)
}

func (w *wizard) proc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	s := w.cfg.State
	switch msg {
	case MsgGetPage:
		return uintptr(s.Page), true
	case winapi.WM_ERASEBKGND:
		return 1, true
	case winapi.WM_PAINT:
		w.chrome.paintWizard(hwnd, s.Lang, s.Page)
		return 0, true
	case winapi.WM_CTLCOLORSTATIC, winapi.WM_CTLCOLOREDIT:
		return w.chrome.ctlColor(wp, true), true
	case winapi.WM_CTLCOLORBTN:
		// checkboxes live in the content area
		return w.chrome.ctlColor(wp, true), true
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
	chrome *setupChrome
}

// ConfirmUninstall shows the confirmation window; ok = false on Cancel.
func ConfirmUninstall(lang string) (ok, purge bool, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	winapi.InitCommonControls()
	if err := registerSetupClasses(); err != nil {
		return false, false, err
	}
	u := &uninstallDlg{chrome: newSetupChrome()}
	curUninstall = u
	defer func() {
		curUninstall = nil
		u.chrome.destroy()
	}()
	style := uint32(winapi.WS_OVERLAPPED | winapi.WS_CAPTION | winapi.WS_SYSMENU)
	x, y, ww, hh := centered(WizardUninstW, WizardUninstH, style)
	hwnd, err := winapi.CreateWindowEx(0, UninstallClass, T(lang, "setup.uninstall.title"), style, x, y, ww, hh, 0, 0, winapi.GetModuleHandle())
	if err != nil {
		return false, false, err
	}
	u.hwnd = hwnd
	pad := int32(WizardPad)
	txt := child(hwnd, "STATIC", T(lang, "setup.uninstall.text"), winapi.SS_LEFT, IDText, pad, pad+12, WizardUninstW-pad*2, 56)
	u.chrome.applyFont(txt, false)
	u.purge = child(hwnd, "BUTTON", T(lang, "setup.uninstall.purge"), winapi.WS_TABSTOP|bsAutoCheckbox, IDPurge, pad, pad+76, WizardUninstW-pad*2, 24)
	u.chrome.applyFont(u.purge, false)
	setCheck(u.purge, false)
	by := int32(WizardUninstH - WizardBtnBarH + (WizardBtnBarH-WizardBtnH)/2)
	right := int32(WizardUninstW) - pad
	bw, bh, gap := int32(WizardBtnW), int32(WizardBtnH), int32(WizardBtnGap)
	btnUn := child(hwnd, "BUTTON", T(lang, "setup.btn.uninstall"), winapi.WS_TABSTOP|winapi.BS_DEFPUSHBUTTON, IDUninstall, right-bw*2-gap, by, bw, bh)
	btnCa := child(hwnd, "BUTTON", T(lang, "setup.btn.cancel"), winapi.WS_TABSTOP|bsPushButton, IDCancel, right-bw, by, bw, bh)
	u.chrome.applyFont(btnUn, false)
	u.chrome.applyFont(btnCa, false)
	winapi.ShowWindow(hwnd, winapi.SW_SHOW)
	winapi.SetForegroundWindow(hwnd)
	runLoop(hwnd)
	return u.ok, u.purged, nil
}

func (u *uninstallDlg) proc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case winapi.WM_ERASEBKGND:
		return 1, true
	case winapi.WM_PAINT:
		u.chrome.paintUninstall(hwnd)
		return 0, true
	case winapi.WM_CTLCOLORSTATIC, winapi.WM_CTLCOLOREDIT, winapi.WM_CTLCOLORBTN:
		return u.chrome.ctlColor(wp, true), true
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
