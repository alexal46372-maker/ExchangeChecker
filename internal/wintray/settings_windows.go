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
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// SettingsClass is the window class of the administrator settings window
// (FindWindow of a second `exchtray --settings`). The read-only window of
// the tray process has its own class, SettingsViewClass, so that search
// can never find a read-only window that is closing after «Изменить».
const (
	SettingsClass     = model.TraySettingsClass
	SettingsViewClass = model.TraySettingsViewClass
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
	idSetPage // navigation: «Общие настройки» (phase 11)
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
	// phase 8 (CHK-TRAY-12): server preset, own thresholds, timeouts
	idSetPreset
	idSetWarn
	idSetError
	idSetConnTO
	idSetQueryTO
	idSetUIMode // «Экспертный режим» / «Простой режим» (phase 10 D)
	idSetExport // «Экспорт…» (phase 10 E, administrator)
	idSetImport // «Импорт…»
	idSetClose  = winapi.IDCANCEL
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
	h, err := winapi.ShellExecuteEx(owner, "runas", exe, SettingsArg+" "+OwnerSIDArg+CurrentUserSID())
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
	// clipboard puts the grant script on the clipboard (nil =
	// winapi.SetClipboardText; tests use a fake)
	clipboard func(owner uintptr, text string) error
	// uiMode / uiStore: «Простой» / «Экспертный» (HKCU UIMode, phase 10
	// D, CHK-TRAY-40); nil store = not stored
	uiMode  traysettings.UIMode
	uiStore UIModeStore
	// phase 10 E (CHK-TRAY-44): the tray block stores, the file dialog
	// and the warning box (nil = GetSaveFileNameW / GetOpenFileNameW,
	// MessageBoxW)
	stores     trayStores
	fileDialog func(owner uintptr, save bool) (string, bool)
	notice     func(text string)
	// otherUser: elevated as another user than the tray owner: no tray
	// block in export / import (this HKCU is not the tray user's)
	otherUser bool
}

// UIModeStore keeps the settings window mode.
type UIModeStore interface {
	Load() (traysettings.UIMode, error)
	Save(traysettings.UIMode) error
}

// settingsWindow is the database settings window (CHK-TRAY-04/05/06): the
// same window for the read-only mode (ordinary tray, get_status data) and
// the administrator mode (elevated process, get_config / save_*).
// Requests run off the UI thread; their results come back through the
// window's own queue. Closing the window cancels a running test (cancel
// {ref}, G6).
type settingsWindow struct {
	// phase 10 D (CHK-TRAY-40/41): mode, field labels, required fields
	uiMode    traysettings.UIMode
	labels    map[int32]uintptr
	labelText map[int32]string
	missing   map[int32]bool
	reqBrush  uintptr
	brushOff  bool // closing: requiredColor no longer creates the brush
	// groups: the group boxes by caption key (phase 10 D3)
	groups map[string]uintptr
	// vbTestIDs: recipient ids of the «Тест для» items; vbTesting: the
	// running viber_test (phase 10 D4)
	vbTestIDs []string
	vbTesting *model.IPCRequest
	// phase 10 E: the scrolled page host, the px rectangles of its
	// controls (unscrolled), the view / content sizes, the bar buttons
	host                           uintptr
	hostRect                       map[uintptr]winapi.RECT
	scrollX, scrollY, viewW, viewH int32
	contentW, contentH             int32
	pageW, pageH                   int32 // laid out page content (px, phase 11)
	// phase 11 (CHK-TRAY-47): the laid out host content; visibility of
	// its controls is ui.Ctl.Hidden (show), applied by relayoutAll
	root              ui.Node
	rootM             *ui.Metrics
	ctlOf             map[uintptr]*ui.Ctl
	wrap              []*ui.Ctl // text boxes sized to their wrapped text (phase 11)
	checksG           *ui.Grid  // the thresholds / timeouts (stacked when too narrow)
	stackedRows       []ui.GridRow
	tlsG              *ui.Grid
	tlsStacked        ui.Node
	scrollSet, inBar  bool
	barLeft, barRight []barButton
	// expertShown: the expert-only controls are shown (test hook)
	expertShown bool
	// phase 10 E: the import dialog, the profiles of needPassword, the
	// status after the import
	imp         *importDialog
	notifyBtn   map[uintptr]bool // page buttons with BS_NOTIFY
	needPw      []string
	afterImport string
	cfg         settingsConfig
	ed          *traysettings.Editor
	hwnd        uintptr
	ctl         map[int32]uintptr

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
	page                             int          // 0 = databases, 1 = general settings
	pageCtl                          [3][]uintptr // profiles, «Общие», «Viber» (phase 10 D4)
	cur                              *[]uintptr   // page of the controls being created
	connected                        bool         // service session state seen by onSessionState
	loaded                           bool         // get_config (or a snapshot) loaded at least once
	disc                             *discoveryDialog
	wiz                              *wizardDialog // «Добавить базу» (CHK-TRAY-30)
	diagnoses                        int           // sql_diagnose replies shown (test hook)
	// exiting: the tray exit message (or App.Exit) is closing the window:
	// no questions any more, an open one answers «Нет» (CHK-TRAY-11)
	exiting bool
	updates int       // update calls (test hook)
	tips    *tooltips // CHK-TRAY-15
	// cues: cue banners set on the edit boxes (CHK-TRAY-17; EM_SETCUEBANNER
	// itself needs comctl32 v6 of the tray manifest)
	cues map[int32]string
}

func (w *settingsWindow) setCue(id int32, text string) {
	if w.cues == nil {
		w.cues = map[int32]string{}
	}
	w.cues[id] = text
	winapi.SetCueBanner(w.ctl[id], text)
}

// settingsTips are the tooltips of the settings window controls (phase 8,
// CHK-TRAY-15): every edit box, button, check box, combo and list box.
var settingsTips = map[int32]string{
	idSetList: i18n.KeyTipSetList, idSetAdd: i18n.KeyTipSetAdd, idSetDelete: i18n.KeyTipSetDelete,
	idSetDiscover: i18n.KeyTipSetDiscover, idSetExport: i18n.KeyTipXferExport, idSetImport: i18n.KeyTipXferImport, idSetName: i18n.KeyTipSetName, idSetHost: i18n.KeyTipSetHost,
	idSetInstance: i18n.KeyTipSetInstance, idSetPort: i18n.KeyTipSetPort, idSetDatabase: i18n.KeyTipSetDatabase,
	idSetAuth: i18n.KeyTipSetAuth, idSetLogin: i18n.KeyTipSetLogin, idSetPassword: i18n.KeyTipSetPassword,
	idSetClearPw: i18n.KeyTipSetClearPw, idSetUIMode: i18n.KeyTipSetUIMode, idSetEncrypt: i18n.KeyTipSetEncrypt, idSetPreset: i18n.KeyTipSetPreset,
	idSetTLS10: i18n.KeyTipSetTLS10, idSetTrust: i18n.KeyTipSetTrust, idSetEnabled: i18n.KeyTipSetEnabled,
	idSetWarn: i18n.KeyTipSetWarn, idSetError: i18n.KeyTipSetError, idSetConnTO: i18n.KeyTipSetConnTimeout,
	idSetQueryTO: i18n.KeyTipSetQueryTimeout, idSetPage: i18n.KeyTipSetPage, idSetNavProfiles: i18n.KeyTipSetNavProfiles, idSetStatus: i18n.KeyTipSetStatus,
	idSetTest: i18n.KeyTipSetTest, idSetCancelTest: i18n.KeyTipSetCancelTest, idSetSave: i18n.KeyTipSetSave,
	idSetPoll: i18n.KeyTipSetPoll, idSetReload: i18n.KeyTipSetReload, idSetElevate: i18n.KeyTipSetElevate,
	idSetClose: i18n.KeyTipSetClose,
	idGenPoll:  i18n.KeyTipGenPoll, idGenWarn: i18n.KeyTipGenWarn, idGenError: i18n.KeyTipGenError,
	idGenBeep: i18n.KeyTipGenBeep, idGenWarnFreq: i18n.KeyTipGenWarnFreq, idGenWarnDur: i18n.KeyTipGenWarnDur,
	idGenErrFreq: i18n.KeyTipGenErrFreq, idGenErrDur: i18n.KeyTipGenErrDur, idGenGap: i18n.KeyTipGenGap,
	idGenLang: i18n.KeyTipGenLang, idGenAutostart: i18n.KeyTipGenAutostart,
}

// settingsOutputs are the static texts of the window (no tooltip).
var settingsOutputs = map[int32]bool{idSetPwHint: true, idSetHints: true, idSetInfo: true, idGenNote: true, idGenAutostartErr: true}

// profNumbers are the phase 8 number boxes of the profile form.
var profNumbers = []int32{idSetWarn, idSetError, idSetConnTO, idSetQueryTO}

// profText returns the address of the Form string of a phase 8 box.
func profText(f *traysettings.Form, id int32) *string {
	switch id {
	case idSetWarn:
		return &f.WarnMin
	case idSetError:
		return &f.ErrorMin
	case idSetConnTO:
		return &f.ConnTimeout
	case idSetQueryTO:
		return &f.QueryTimeout
	}
	return nil
}

func newSettingsWindow(cfg settingsConfig) (*settingsWindow, error) {
	cfg.lang = i18n.Normalize(cfg.lang)
	w := &settingsWindow{cfg: cfg, ed: traysettings.NewEditor(cfg.lang, cfg.mode), ctl: map[int32]uintptr{},
		labels: map[int32]uintptr{}, labelText: map[int32]string{}, missing: map[int32]bool{}, groups: map[string]uintptr{}}
	// no mode given (tests, older callers): every field, as before
	w.uiMode = traysettings.UIExpert
	if cfg.uiMode != "" {
		w.uiMode = traysettings.NormalizeUIMode(string(cfg.uiMode))
	}
	if w.cfg.elevator == nil {
		w.cfg.elevator = ShellElevator{}
	}
	title := i18n.T(i18n.KeySetTitleRead, cfg.lang)
	if cfg.mode == traysettings.Admin {
		title = i18n.T(i18n.KeySetTitleAdmin, cfg.lang)
	}
	class := SettingsViewClass
	if cfg.mode == traysettings.Admin {
		class = SettingsClass
	}
	// client 774×706 (controls up to 755 / 694), clamped to the work area
	const style = winapi.WS_OVERLAPPED | winapi.WS_CAPTION | winapi.WS_SYSMENU | winapi.WS_MINIMIZEBOX
	x, y, ww, wh := settingsRect(style)
	hwnd, err := createWindowPx(class, title, 0, style, x, y, ww, wh, 0, w.wndProc)
	if err != nil {
		return nil, err
	}
	w.hwnd = hwnd
	if err := w.createHost(); err != nil {
		destroyWindow(hwnd)
		return nil, err
	}
	w.build()
	w.layoutFit()
	w.fill()
	if !cfg.hidden {
		winapi.ShowWindow(hwnd, winapi.SW_SHOWNORMAL)
		winapi.SetForegroundWindow(hwnd)
	}
	return w, nil
}

func (w *settingsWindow) t(key string) string { return i18n.T(key, w.cfg.lang) }

func (w *settingsWindow) add(class, text string, ex, style uint32, id, x, y, cw, ch int32) uintptr {
	x, y, cw, ch = pxRect(x, y, cw, ch)
	parent := w.host
	if w.inBar || parent == 0 {
		parent = w.hwnd
	}
	btn := class == "BUTTON" && style&0xF != winapi.BS_GROUPBOX && parent == w.host
	if btn {
		style |= winapi.BS_NOTIFY
	}
	h, err := winapi.CreateWindowEx(ex, class, text, winapi.WS_CHILD|winapi.WS_VISIBLE|style, x, y, cw, ch, parent, uintptr(id), winapi.GetModuleHandle())
	if err != nil {
		return 0
	}
	winapi.SendMessage(h, winapi.WM_SETFONT, uiFont(), 1)
	if id != 0 {
		w.ctl[id] = h
	}
	if w.cur != nil {
		*w.cur = append(*w.cur, h)
	}
	if btn {
		if w.notifyBtn == nil {
			w.notifyBtn = map[uintptr]bool{}
		}
		w.notifyBtn[h] = true
	}
	if parent == w.host { // a combo box keeps its drop-down height on MoveWindow
		w.hostRect[h] = winapi.RECT{Left: x, Top: y, Right: x + cw, Bottom: y + ch}
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
	// phase 11 (CHK-TRAY-45): the controls are created in the old order
	// (tab order) at 0,0; the framework lays the pages out (labels
	// measured, group boxes as wide as their content, the three pages
	// sharing one area right of the profile list) and layoutFit pins the
	// bar to the bottom.
	edit := uint32(winapi.WS_TABSTOP | winapi.ES_AUTOHSCROLL)
	add := func(class, text string, ex, style uint32, id int32, kind ui.Kind, wd, hg int32) *ui.Ctl {
		c := uiCtl(w.add(class, text, ex, style, id, 0, 0, 0, 0), kind, text)
		c.W, c.Hgt = wd, hg
		return c
	}
	label := func(text string, id int32) *ui.Ctl {
		h := w.add("STATIC", text, 0, winapi.SS_LEFT, 0, 0, 0, 0, 0)
		if id != 0 {
			w.labels[id] = h
			w.labelText[id] = text
		}
		measured := text
		if _, req := reqFields[id]; req {
			measured += " *" // updateRequired adds the asterisk
		}
		return &ui.Ctl{H: h, Kind: ui.Label, Text: measured, Lines: 1}
	}
	field := func(text string, id int32, class string, style uint32, width int32) ui.GridRow {
		l := label(text, id)
		ex, kind := uint32(0), ui.Combo
		if class == "EDIT" {
			ex, kind = winapi.WS_EX_CLIENTEDGE, ui.Edit
		}
		f := add(class, "", ex, style, id, kind, width, 0)
		// phase 11 (CHK-TRAY-48): the full-width fields take the width of
		// their group (no empty strip on the right at 144 dpi)
		f.Grow = width == 390 // cw
		return ui.GridRow{Label: l, Field: f}
	}
	check := func(id int32, key string) *ui.Ctl {
		return add("BUTTON", w.t(key), 0, winapi.WS_TABSTOP|winapi.BS_AUTOCHECKBOX, id, ui.Check, 0, 0)
	}
	group := func(key string, child ui.Node) *ui.GroupBox {
		g := add("BUTTON", w.t(key), 0, winapi.BS_GROUPBOX, 0, ui.Group, 0, 0)
		w.groups[key] = g.H
		return &ui.GroupBox{Box: g, Child: child}
	}
	const cw = 390 // edit / combo boxes
	// left navigation (phase 11, CHK-TRAY-47): one button per page, the
	// current one pressed (push-like check box, not disabled: Кончита); then the mode switch; then the profile list
	// column of the databases page
	w.cur = nil
	nav := &ui.Col{Gap: ui.P4, Items: []ui.Node{
		add("BUTTON", w.t(i18n.KeySetNavProfiles), 0, winapi.WS_TABSTOP|navStyle, idSetNavProfiles, ui.Button, 205, 0),
		add("BUTTON", w.t(i18n.KeySetNavGeneral), 0, winapi.WS_TABSTOP|navStyle, idSetPage, ui.Button, 205, 0),
		add("BUTTON", w.t(i18n.KeySetNavViber), 0, winapi.WS_TABSTOP|navStyle, idSetViber, ui.Button, 205, 0),
		add("BUTTON", longest(w.t(i18n.KeySetModeExpert), w.t(i18n.KeySetModeSimple)), 0, winapi.WS_TABSTOP, idSetUIMode, ui.Button, 205, 0),
	}}
	w.navIcons()
	winapi.SetWindowText(w.ctl[idSetUIMode], "")         // applyUIMode names it
	winapi.ShowWindow(w.ctl[idSetViber], winapi.SW_HIDE) // updateViber shows it
	w.cur = &w.pageCtl[0]
	list := add("LISTBOX", "", winapi.WS_EX_CLIENTEDGE, winapi.WS_TABSTOP|winapi.WS_VSCROLL|winapi.LBS_NOTIFY|0x0100 /* LBS_NOINTEGRALHEIGHT */, idSetList, ui.Box, 205, 250)
	// phase 11 (CHK-TRAY-48): the list as wide as the navigation column
	list.Grow, list.NoShrink = true, true
	left := []ui.Node{label(w.t(i18n.KeySetProfiles), 0), list,
		&ui.Row{Grow: true, Items: []ui.Node{add("BUTTON", w.t(i18n.KeySetAdd), 0, winapi.WS_TABSTOP, idSetAdd, ui.Button, 100, 0),
			add("BUTTON", w.t(i18n.KeySetDelete), 0, winapi.WS_TABSTOP, idSetDelete, ui.Button, 100, 0)}}}
	var xfer ui.Node
	if w.cfg.mode == traysettings.Admin {
		left = append(left, add("BUTTON", w.t(i18n.KeyDiscButton), 0, winapi.WS_TABSTOP, idSetDiscover, ui.Button, 205, 0))
		xfer = &ui.Row{Items: []ui.Node{add("BUTTON", w.t(i18n.KeyXferExport), 0, winapi.WS_TABSTOP, idSetExport, ui.Button, 100, 0),
			add("BUTTON", w.t(i18n.KeyXferImport), 0, winapi.WS_TABSTOP, idSetImport, ui.Button, 100, 0)}}
	}
	// profile page: four group boxes (phase 10 D3)
	// phase 11 (CHK-TRAY-47): one field column for the whole page
	// (ui.Share), «Порт» on its own row, the TLS boxes in one row,
	// hints and info under the groups, full width (the text wraps)
	nameRow := field(w.t(i18n.KeySetName), idSetName, "EDIT", edit, cw)
	hostRow := field(w.t(i18n.KeySetHost), idSetHost, "EDIT", edit, cw)
	instRow := field(w.t(i18n.KeySetInstance), idSetInstance, "EDIT", edit, 200)
	portL := label(w.t(i18n.KeySetPort), idSetPort)
	// phase 11: the port on its own row, wide enough for its whole cue
	// «1433 или пусто для SQL Browser» at 144 dpi (CHK-TRAY-17 wording)
	port := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idSetPort, ui.Edit, 200, 0)
	port.NoShrink = true
	connG := &ui.Grid{Gap: ui.P4, Rows: []ui.GridRow{nameRow, hostRow, instRow, {Label: portL, Field: port},
		field(w.t(i18n.KeySetDatabase), idSetDatabase, "EDIT", edit, cw)}}
	conn := group(i18n.KeySetGroupConn, connG)
	loginRows := []ui.GridRow{
		field(w.t(i18n.KeySetAuth), idSetAuth, "COMBOBOX", winapi.WS_TABSTOP|winapi.CBS_DROPDOWNLIST, cw),
		field(w.t(i18n.KeySetLogin), idSetLogin, "EDIT", edit, cw),
		field(w.t(i18n.KeySetPassword), idSetPassword, "EDIT", edit|winapi.ES_PASSWORD, cw),
	}
	loginRows = append(loginRows, ui.GridRow{Field: add("STATIC", "", 0, winapi.SS_LEFT, idSetPwHint, ui.Box, 120, 18)},
		ui.GridRow{Field: check(idSetClearPw, i18n.KeySetClearPassword)})
	loginG := &ui.Grid{Gap: ui.P4, Rows: loginRows}
	login := group(i18n.KeySetGroupLogin, loginG)
	tlsRows := []ui.GridRow{
		field(w.t(i18n.KeySetEncrypt), idSetEncrypt, "COMBOBOX", winapi.WS_TABSTOP|winapi.CBS_DROPDOWNLIST, cw),
		field(w.t(i18n.KeySetPreset), idSetPreset, "COMBOBOX", winapi.WS_TABSTOP|winapi.CBS_DROPDOWNLIST, cw),
	}
	// side by side (the page fits 1024×768 at 96 dpi); stacked by
	// stackPairs when the window is too narrow
	tls10, trust := check(idSetTLS10, i18n.KeySetTLS10), check(idSetTrust, i18n.KeySetTrust)
	tlsRows = append(tlsRows, ui.GridRow{Span: true, Field: &ui.Row{Gap: ui.P16, Items: []ui.Node{tls10, trust}}})
	w.tlsStacked = &ui.Col{Gap: ui.P4, Items: []ui.Node{tls10, trust}}
	tlsG := &ui.Grid{Gap: ui.P4, Rows: tlsRows}
	w.tlsG = tlsG
	tls := group(i18n.KeySetGroupTLS, tlsG)
	enabled := check(idSetEnabled, i18n.KeySetEnabled)
	// two number boxes per row: own thresholds, then timeouts (phase 8)
	// (phase 11: one per row when the window is too narrow, stackPairs)
	var stacked []ui.GridRow
	pair := func(k1 string, id1 int32, k2 string, id2 int32) ui.GridRow {
		l1 := label(w.t(k1), id1)
		e1 := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit|winapi.ES_NUMBER, id1, ui.Edit, 55, 0)
		l2 := label(w.t(k2), id2)
		e2 := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit|winapi.ES_NUMBER, id2, ui.Edit, 55, 0)
		stacked = append(stacked, ui.GridRow{Label: l1, Field: e1}, ui.GridRow{Label: l2, Field: e2})
		return ui.GridRow{Label: l1, Field: &ui.Row{Items: []ui.Node{e1, &ui.Space{W: ui.P8}, l2, e2}}}
	}
	checksG := &ui.Grid{Gap: ui.P4, Rows: []ui.GridRow{{Span: true, Field: enabled},
		pair(i18n.KeySetWarnMin, idSetWarn, i18n.KeySetErrorMin, idSetError),
		pair(i18n.KeySetConnTimeout, idSetConnTO, i18n.KeySetQueryTimeout, idSetQueryTO),
	}}
	w.checksG, w.stackedRows = checksG, append([]ui.GridRow{checksG.Rows[0]}, stacked...)
	checks := group(i18n.KeySetGroupChecks, checksG)
	ui.Share(connG, loginG, tlsG, checksG)
	hints := add("STATIC", "", 0, winapi.SS_LEFT, idSetHints, ui.Box, 120, 30)
	info := add("STATIC", "", 0, winapi.SS_LEFT, idSetInfo, ui.Box, 120, 16)
	hints.Grow, info.Grow = true, true
	pwHint := loginRows[len(loginRows)-2].Field.(*ui.Ctl)
	pwHint.Grow = true
	// phase 11: the wrapped text boxes are as high as their text
	w.wrap = []*ui.Ctl{hints, info, pwHint}
	profilePage := &ui.Col{Gap: ui.P8, Items: []ui.Node{conn, login, tls, checks, &ui.Col{Gap: ui.P4, Items: []ui.Node{hints, info}}}}

	// general settings page (hidden until «Общие настройки…»)
	w.cur = &w.pageCtl[1]
	gBox := add("BUTTON", w.t(i18n.KeySetGroupGeneral), 0, winapi.BS_GROUPBOX, 0, ui.Group, 0, 0)
	w.groups[i18n.KeySetGroupGeneral] = gBox.H
	var gRows []ui.GridRow
	for i, g := range genEdits {
		if i == 3 {
			gRows = append(gRows, ui.GridRow{Span: true, Field: check(idGenBeep, i18n.KeyGenBeepEnabled)})
		}
		gRows = append(gRows, field(w.t(g.key), g.id, "EDIT", edit|winapi.ES_NUMBER, 100))
	}
	gRows = append(gRows, field(w.t(i18n.KeyGenLanguage), idGenLang, "COMBOBOX", winapi.WS_TABSTOP|winapi.CBS_DROPDOWNLIST, 260))
	auto := add("BUTTON", w.t(i18n.KeyGenAutostart), 0, winapi.WS_TABSTOP|winapi.BS_AUTOCHECKBOX|winapi.BS_MULTILINE, idGenAutostart, ui.Box, 260, 34)
	// a wrapped check box: the page width, not its one-line text (phase 11)
	auto.Grow, auto.NoShrink = true, true
	autoErr := add("STATIC", "", 0, winapi.SS_LEFT, idGenAutostartErr, ui.Box, 300, 32)
	note := add("STATIC", w.t(i18n.KeyGenNote), 0, winapi.SS_LEFT, idGenNote, ui.Box, 300, 100)
	autoErr.Grow, note.Grow = true, true
	// phase 11 (CHK-TRAY-48): as high as their text, an empty error box
	// takes no room (no gap under the autostart check box)
	w.wrap = append(w.wrap, autoErr, note)
	gRows = append(gRows, ui.GridRow{Span: true, Field: auto}, ui.GridRow{Span: true, Field: autoErr}, ui.GridRow{Span: true, Field: note})
	generalPage := &ui.GroupBox{Box: gBox, Child: &ui.Grid{Gap: ui.P4, Rows: gRows}}
	w.cur = nil

	// the bar: children of the window, pinned to the bottom (layoutFit)
	w.inBar = true
	w.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, winapi.ES_MULTILINE|winapi.ES_READONLY|winapi.ES_AUTOVSCROLL|winapi.WS_VSCROLL,
		idSetStatus, 12, 628, 743, 40)
	m := uiMetrics()
	button := func(id int32, key string, width int32, right bool) {
		w.add("BUTTON", w.t(key), 0, winapi.WS_TABSTOP, id, 12, 674, width, gridButtonH)
		bw, _ := ui.Layout(&ui.Ctl{Kind: ui.Button, Text: w.t(key), W: width}, m, 0, 0, 0)
		width = max32(width, unpx(bw)) // at least its text in this language
		if right {
			w.barRight = append(w.barRight, barButton{id, width})
		} else {
			w.barLeft = append(w.barLeft, barButton{id, width})
		}
	}
	w.cur = &w.pageCtl[0]
	button(idSetTest, i18n.KeySetTest, 150, false)
	button(idSetCancelTest, i18n.KeySetCancelTest, 120, false)
	button(idSetPoll, i18n.KeySetPoll, 110, false)
	w.cur = nil
	button(idSetReload, i18n.KeySetReload, 90, false)
	if w.cfg.mode == traysettings.ReadOnly {
		button(idSetElevate, i18n.KeySetElevate, 165, false)
	}
	if w.cfg.mode == traysettings.Admin {
		button(idSetSave, i18n.KeySetSave, 90, true)
	}
	button(idSetClose, i18n.KeySetClose, 90, true)
	w.inBar = false
	viberPage := w.buildViber()
	if xfer != nil {
		left = append(left, xfer)
	}
	// grey hints of empty boxes (phase 8, CHK-TRAY-12); before the layout:
	// the boxes are at least as wide as their cues (phase 11, CHK-TRAY-48)
	w.setCue(idSetPort, w.t(i18n.KeySetPortCue))
	// phase 11 (PLAN §12 p.50): every edit box shows its cue
	w.setCue(idSetName, w.t(i18n.KeyCueName))
	w.setCue(idSetHost, w.t(i18n.KeyCueHost))
	w.setCue(idSetInstance, w.t(i18n.KeyCueInstance))
	w.setCue(idSetDatabase, w.t(i18n.KeyCueDatabase))
	w.setCue(idSetLogin, w.t(i18n.KeyCueLogin))
	w.setCue(idSetPassword, w.t(i18n.KeyCuePassword))
	if w.ctl[idVbToken] != 0 {
		w.setCue(idVbToken, w.t(i18n.KeyCueVbToken))
	}
	w.setCue(idSetWarn, w.t(i18n.KeySetThreshCue))
	w.setCue(idSetError, w.t(i18n.KeySetThreshCue))
	w.setCue(idSetConnTO, fmt.Sprintf(w.t(i18n.KeySetTimeoutCue), model.DefaultConnectTimeoutSec))
	w.setCue(idSetQueryTO, fmt.Sprintf(w.t(i18n.KeySetTimeoutCue), model.DefaultQueryTimeoutSec))
	w.layoutPages(m, &ui.Col{Gap: ui.P12, Items: []ui.Node{nav, &ui.Col{Gap: ui.P8 - 2, Items: left}}},
		[]ui.Node{profilePage, generalPage, viberPage})
	for _, k := range []string{i18n.KeySetAuthSQL, i18n.KeySetAuthWindows} {
		winapi.SendMessageString(w.ctl[idSetAuth], winapi.CB_ADDSTRING, 0, w.t(k))
	}
	for _, k := range traysettings.EncryptKeys() {
		winapi.SendMessageString(w.ctl[idSetEncrypt], winapi.CB_ADDSTRING, 0, w.t(k))
	}
	for _, k := range traysettings.PresetKeys() {
		winapi.SendMessageString(w.ctl[idSetPreset], winapi.CB_ADDSTRING, 0, w.t(k))
	}
	w.tips = newTooltips(w.hwnd)
	for id, h := range w.ctl {
		if key, ok := settingsTips[id]; ok {
			w.tips.set(h, w.t(key))
		}
	}
	for _, k := range []string{i18n.KeyGenLangAuto, i18n.KeyGenLangRU, i18n.KeyGenLangEN} {
		winapi.SendMessageString(w.ctl[idGenLang], winapi.CB_ADDSTRING, 0, w.t(k))
	}
	for _, h := range w.pageCtl[1] {
		w.show(h, false)
	}
	w.applyUIMode()
	if w.cfg.mode == traysettings.ReadOnly {
		w.show(w.ctl[idSetAdd], false)
		w.show(w.ctl[idSetDelete], false)
		w.show(w.ctl[idSetClearPw], false)
		w.relayoutAll()
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
	case winapi.WM_CTLCOLOREDIT:
		if br, ok := w.requiredColor(wp, lp); ok {
			return br, true
		}
	case winapi.WM_CLOSE:
		w.userClose()
		return 0, true
	}
	if isTrayExit(msg) {
		w.exitNow()
		return 0, true
	}
	return 0, false
}

func (w *settingsWindow) command(id int32, code uint32) {
	if w.viberCommand(id, code) {
		return
	}
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
		if w.page != 1 {
			w.setPage(1)
		}
	case idSetNavProfiles:
		if w.page != 0 {
			w.setPage(0)
		}
	case idSetUIMode:
		w.toggleUIMode()
	case idSetName, idSetHost, idSetInstance, idSetPort, idSetDatabase, idSetLogin, idSetWarn, idSetError, idSetConnTO, idSetQueryTO:
		if code == winapi.EN_CHANGE && !w.loading {
			w.readForm()
			w.update()
		}
	case idSetAuth, idSetEncrypt, idSetPreset:
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
		if w.wizardAvailable() {
			w.openWizard()
		} else {
			w.newProfile()
		}
	case idSetDelete:
		w.deleteProfile()
	case idSetExport:
		w.exportConfig()
	case idSetImport:
		w.importConfig()
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
	f.ClearPassword, f.TLS10, f.Enabled = w.check(idSetClearPw), w.check(idSetTLS10), w.check(idSetEnabled)
	// the trust box is shown cleared and disabled where it has no effect
	// (CHK-TRAY-13); its value is kept for «Весь сеанс (TLS)»
	// (read only while the box is enabled, i.e. before a mode change)
	if winapi.IsWindowEnabled(w.ctl[idSetTrust]) {
		f.TrustServerCert = w.check(idSetTrust)
	}
	if i := int(winapi.SendMessage(w.ctl[idSetPreset], winapi.CB_GETCURSEL, 0, 0)); i >= 0 && i < len(traysettings.PresetChoices) {
		f.Preset = traysettings.PresetChoices[i]
	}
	for _, id := range profNumbers {
		*profText(f, id) = w.text(id)
	}
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
		idSetDatabase: f.Database, idSetLogin: f.Login, idSetWarn: f.WarnMin, idSetError: f.ErrorMin,
		idSetConnTO: f.ConnTimeout, idSetQueryTO: f.QueryTimeout} {
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
	preset := traysettings.PresetIndex(f.Preset)
	if !w.ed.HasProfile() {
		preset = -1
	}
	winapi.SendMessage(w.ctl[idSetPreset], winapi.CB_SETCURSEL, uintptr(preset), 0)
	w.setCheck(idSetClearPw, f.ClearPassword)
	w.setCheck(idSetTLS10, f.TLS10)
	w.setCheck(idSetTrust, f.TrustServerCert && traysettings.TrustApplies(f.Encrypt))
	w.setCheck(idSetEnabled, f.Enabled)
	w.fillGeneral()
	w.fillViber()
	w.update()
}

// update refreshes hints, enabled states and the status line.
func (w *settingsWindow) update() {
	w.updates++
	ed := w.ed
	edit := ed.CanEdit()
	has := ed.HasProfile()
	sql := ed.Form.Auth != model.AuthWindows
	encrypted := ed.Form.Encrypt.Effective() != model.EncryptDisable
	for _, id := range []int32{idSetName, idSetHost, idSetInstance, idSetPort, idSetDatabase, idSetLogin, idSetPassword,
		idSetWarn, idSetError, idSetConnTO, idSetQueryTO} {
		winapi.SendMessage(w.ctl[id], winapi.EM_SETREADONLY, boolPtr(!edit), 0)
	}
	winapi.EnableWindow(w.ctl[idSetLogin], has && sql)
	winapi.EnableWindow(w.ctl[idSetPassword], edit && sql)
	winapi.EnableWindow(w.ctl[idSetClearPw], edit && sql && !ed.IsNew && ed.Base.HasPassword)
	winapi.EnableWindow(w.ctl[idSetAuth], edit)
	winapi.EnableWindow(w.ctl[idSetEncrypt], edit)
	winapi.EnableWindow(w.ctl[idSetPreset], edit)
	winapi.EnableWindow(w.ctl[idSetTLS10], edit && encrypted)
	// «Доверять сертификату» only where the driver uses it: encrypt on
	// (optional never validates, strict always does; CHK-TRAY-13)
	trust := traysettings.TrustApplies(ed.Form.Encrypt)
	winapi.EnableWindow(w.ctl[idSetTrust], edit && trust)
	if want := trust && ed.Form.TrustServerCert; w.check(idSetTrust) != want {
		w.setCheck(idSetTrust, want)
	}
	winapi.EnableWindow(w.ctl[idSetEnabled], edit)
	pw := ""
	if has {
		pw = ed.PasswordHint()
	}
	winapi.SetWindowText(w.ctl[idSetPwHint], pw)
	hints, info := strings.Join(ed.Hints(), "\r\n"), w.info()
	if winapi.GetWindowText(w.ctl[idSetHints]) != hints || winapi.GetWindowText(w.ctl[idSetInfo]) != info {
		winapi.SetWindowText(w.ctl[idSetHints], hints)
		winapi.SetWindowText(w.ctl[idSetInfo], info)
		w.relayoutAll() // the boxes are as high as their text
	}
	testing := w.testCancel != nil
	sess := w.cfg.session
	winapi.EnableWindow(w.ctl[idSetTest], has && !testing && sess.Supported(model.CmdTestProfile))
	winapi.EnableWindow(w.ctl[idSetCancelTest], testing)
	saved := has && !ed.IsNew
	winapi.EnableWindow(w.ctl[idSetPoll], saved && !w.busy)
	winapi.EnableWindow(w.ctl[idSetReload], !w.busy)
	if w.ctl[idSetExport] != 0 {
		x := w.transferAvailable()
		winapi.EnableWindow(w.ctl[idSetExport], x && !w.busy)
		winapi.EnableWindow(w.ctl[idSetImport], x && !w.busy)
	}
	if t := w.autostartError(); w.text(idGenAutostartErr) != t {
		winapi.SetWindowText(w.ctl[idGenAutostartErr], t)
		w.relayoutAll()
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
		if w.page == pageViber {
			canSave = ed.CanEditViber()
		}
		winapi.EnableWindow(w.ctl[idSetSave], canSave && !w.busy)
		winapi.EnableWindow(w.ctl[idSetDelete], saved && !w.busy)
		winapi.EnableWindow(w.ctl[idSetAdd], !w.busy && w.wiz == nil)
		winapi.EnableWindow(w.ctl[idSetDiscover], has && !w.busy && w.disc == nil && w.discoveryAvailable())
	}
	st := w.status
	if !has && st == "" && w.page == 0 {
		st = w.t(i18n.KeySetNoProfile)
	}
	if w.text(idSetStatus) != st {
		winapi.SetWindowText(w.ctl[idSetStatus], st)
	}
	w.updateViber()
	w.updateRequired()
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
	traysettings.FieldDiscoverButton: idSetDiscover, // mapping.* / mappings[i].*
	traysettings.FieldPreset:         idSetPreset, traysettings.FieldProfWarn: idSetWarn, traysettings.FieldProfError: idSetError,
	traysettings.FieldConnTimeout: idSetConnTO, traysettings.FieldQueryTimeout: idSetQueryTO,
}

// focus moves the keyboard focus to the control of f (lastFocus keeps it
// for tests: SetFocus of a hidden window is not reliable).
func (w *settingsWindow) focus(f traysettings.Field) {
	w.lastFocus = f
	page := map[bool]int{false: 0, true: 1}[traysettings.IsGeneral(f)]
	if traysettings.IsViber(f) {
		page = pageViber
	}
	if page != w.page {
		w.setPage(page)
	}
	if id, ok := fieldControl[f]; ok {
		w.scrollIntoView(w.ctl[id])
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
	if w.exiting {
		return false
	}
	ok := confirm(w.hwnd, text, w.t(i18n.KeyAppName))
	return ok && !w.exiting // the exit message came while it was open
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
			w.show(h, cmd == winapi.SW_SHOW)
		}
	}
	w.applyUIMode() // lays the page out (relayoutAll)
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
	if w.page == pageViber {
		w.saveViber()
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
			w.savedNeedPassword(w.ed.Base.ID)
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
			pr := w.ed.TestReply(m, err)
			w.problem(pr)
			if err == nil && pr != nil {
				w.testDiagnosis(m, pr.Text)
			}
		})
	}()
}

// testDiagnosis adds the code and «что делать» of a failed test
// (CHK-TRAY-31): the diagnosis of sql_diagnose in the administrator
// window (an admin-only command), else the local text.
func (w *settingsWindow) testDiagnosis(m model.IPCMessage, text string) {
	req, code, ok := traysettings.DiagnoseRequest(m)
	if !ok {
		return
	}
	detail := "" // TestText / Reply already show the technical error
	show := func(d *model.SQLDiagnosis) {
		w.setStatus(text + "\r\n" + traysettings.ErrorText(code, d, detail, w.cfg.lang))
	}
	if w.cfg.mode != traysettings.Admin || !w.cfg.session.Supported(model.CmdSQLDiagnose) {
		show(nil)
		return
	}
	seq := w.testSeq
	s := w.cfg.session
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), traysettings.CallTimeout)
		r, err := s.Do(ctx, req)
		cancel()
		w.post(func() {
			if seq != w.testSeq || w.testCancel != nil {
				return
			}
			w.diagnoses++
			if err == nil && r.Type == model.MsgSQLDiagnosis {
				show(r.SQLDiagnosis)
				return
			}
			show(nil)
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
// (read-only). Unsaved edits are dropped («Перечитать» asks first).
func (w *settingsWindow) Reload() { w.reload(false) }

// reload: keep = an automatic re-read (reconnection): edits made while
// get_config was in flight (e.g. a mapping applied in the Discovery
// dialog) are kept; only the revision is compared then (checkRevision).
func (w *settingsWindow) reload(keep bool) {
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
		case keep && w.loaded && w.unsavedNow():
			if m.Revision != w.ed.Revision {
				w.setStatus(w.t(i18n.KeySetChangedOffline))
			} else {
				w.setStatus("")
			}
		default:
			w.ed.LoadConfig(*m.Config, m.Revision)
			w.loaded = true
			note := w.afterImport
			w.status = note
			w.afterImport = ""
			w.fill()
			w.nextNeedPassword()
			if note != "" && w.status != note { // the import result stays visible
				w.setStatus(note + "\r\n" + w.status)
			}
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
	if w.cfg.mode == traysettings.ReadOnly || !w.loaded || !w.unsavedNow() {
		w.reload(true)
		return
	}
	w.checkRevision()
}

// unsavedNow: the editor (with the boxes read now) has unsaved edits.
func (w *settingsWindow) unsavedNow() bool {
	w.readForm()
	w.readGeneral()
	return w.ed.Unsaved()
}

// onSessionStatus follows get_status pushes: e.g. trayAutostartError of
// the get_status that follows a reconnection (EventState comes before it).
func (w *settingsWindow) onSessionStatus() {
	if winapi.IsWindow(w.hwnd) {
		w.update()
	}
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

// exitNow closes the window (and the Discovery dialog) at once without
// asking about unsaved edits (CHK-TRAY-11). Called from inside an open
// question (its modal loop dispatches the message), it destroys the owner,
// which ends the MessageBox owned by it; ask then answers «Нет».
func (w *settingsWindow) exitNow() {
	w.exiting = true
	w.close()
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
	w.brushOff = true
	if w.reqBrush != 0 {
		winapi.DeleteObject(w.reqBrush)
		w.reqBrush = 0
	}
	if w.disc != nil {
		w.disc.close()
	}
	if w.imp != nil {
		w.imp.close()
	}
	if w.wiz != nil {
		w.wiz.close()
	}
	if w.testCancel != nil {
		w.testCancel()
		w.testCancel = nil
	}
	destroyWindow(w.hwnd)
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
	// UIModeStore: «Простой» / «Экспертный» (HKCU; nil = every field).
	UIModeStore UIModeStore
	// tray block of export / import (phase 10 E; HKCU, nil = not stored)
	LangStore    LangStore
	BalloonStore BalloonStore
	SoundStore   SoundStore
	// OwnerSID: the user SID of the tray that started the elevated window
	// (--owner-sid=; "" = unknown, treated as the same user)
	OwnerSID string
}

// loadUIMode reads the stored mode ("" without a store).
func loadUIMode(s UIModeStore) traysettings.UIMode {
	if s == nil {
		return ""
	}
	if m, err := s.Load(); err == nil && m == traysettings.UIExpert {
		return m
	}
	return traysettings.UISimple // missing / unreadable = «Простой»
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
	if !ok { // the open Discovery dialog first (its owner is disabled)
		if h := winapi.FindWindow(WizardClass); h != 0 {
			frontWindow(h)
		} else if h := winapi.FindWindow(DiscoveryClass); h != 0 {
			frontWindow(h)
		} else if h := winapi.FindWindow(SettingsClass); h != 0 {
			frontWindow(h)
		}
		return 0
	}
	defer release()
	var w *settingsWindow
	so := opt.Session
	so.OnEvent = func(e trayipc.Event) {
		if w == nil {
			return
		}
		switch st := e.Status; e.Kind {
		case trayipc.EventState:
			w.post(func() { w.onSessionState(st) })
		case trayipc.EventStatus:
			w.post(w.onSessionStatus)
		}
	}
	s := trayipc.New(so)
	w, err = newSettingsWindow(settingsConfig{lang: opt.Lang, mode: traysettings.Admin, session: s,
		uiMode: loadUIMode(opt.UIModeStore), uiStore: opt.UIModeStore,
		stores:    trayStores{Lang: opt.LangStore, Balloon: opt.BalloonStore, Sound: opt.SoundStore},
		otherUser: OtherUser(opt.OwnerSID, CurrentUserSID()),
		onClose:   func() { winapi.PostQuitMessage(0) }})
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
		if z := w.wiz; z != nil && winapi.IsWindow(z.hwnd) && winapi.IsDialogMessage(z.hwnd, &m) {
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
