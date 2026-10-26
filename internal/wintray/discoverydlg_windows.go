//go:build windows

package wintray

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// DiscoveryClass is the window class of the Discovery dialog (phase 6d,
// CHK-TRAY-07): «Таблицы обмена…» of the administrator settings window.
const DiscoveryClass = model.TrayDiscoveryClass

// Discovery dialog control IDs.
const (
	idDiscFind = 600 + iota
	idDiscPoints
	idDiscCancel
	idDiscState
	idDiscPoint
	idDiscTime
	idDiscTol
	idDiscByValue
	idDiscList
	idDiscDetails
	idDiscMode
	idDiscSchema
	idDiscTable
	idDiscPointName
	idDiscPointID
	idDiscPointKey
	idDiscTimesTable
	idDiscTimesLink
	idDiscSend
	idDiscRecv
	idDiscPlanLabel
	idDiscTemplate
	idDiscYear
	idDiscKeep
	idDiscAllPlans
	idDiscSource
	idDiscSQL
	idDiscResult
	idDiscCheck
	idDiscApply
	idDiscClose = winapi.IDCANCEL
)

// discMapEdits are the text boxes of the mapping editor.
var discMapEdits = []struct {
	id    int32
	key   string
	field string // mapping.* of the service errorArgs[0]
}{
	{idDiscSchema, i18n.KeyDiscSchema, "mapping.schema"},
	{idDiscTable, i18n.KeyDiscTable, "mapping.table"},
	{idDiscPointName, i18n.KeyDiscPointName, "mapping.pointNameColumn"},
	{idDiscPointID, i18n.KeyDiscPointID, "mapping.pointIdColumn"},
	{idDiscPointKey, i18n.KeyDiscPointKey, "mapping.pointKeyColumn"},
	{idDiscTimesTable, i18n.KeyDiscTimesTable, "mapping.timesTable"},
	{idDiscTimesLink, i18n.KeyDiscTimesLink, "mapping.timesLinkColumn"},
	{idDiscSend, i18n.KeyDiscSend, "mapping.lastSendColumn"},
	{idDiscRecv, i18n.KeyDiscRecv, "mapping.lastRecvColumn"},
	{idDiscPlanLabel, i18n.KeyDiscPlanLabel, "mapping.planLabel"},
}

// discoveryTips are the tooltips of the dialog controls (phase 8,
// CHK-TRAY-15); the table / column boxes share KeyTipDiscField.
var discoveryTips = map[int32]string{
	idDiscFind: i18n.KeyTipDiscFind, idDiscPoints: i18n.KeyTipDiscPoints, idDiscCancel: i18n.KeyTipDiscCancel,
	idDiscPoint: i18n.KeyTipDiscPoint, idDiscTime: i18n.KeyTipDiscTime, idDiscTol: i18n.KeyTipDiscTol,
	idDiscByValue: i18n.KeyTipDiscByValue, idDiscList: i18n.KeyTipDiscList, idDiscDetails: i18n.KeyTipDiscDetails,
	idDiscMode: i18n.KeyTipDiscMode, idDiscSchema: i18n.KeyTipDiscField, idDiscTable: i18n.KeyTipDiscField,
	idDiscPointName: i18n.KeyTipDiscField, idDiscPointID: i18n.KeyTipDiscField, idDiscPointKey: i18n.KeyTipDiscField,
	idDiscTimesTable: i18n.KeyTipDiscField, idDiscTimesLink: i18n.KeyTipDiscField, idDiscSend: i18n.KeyTipDiscField,
	idDiscRecv: i18n.KeyTipDiscField, idDiscPlanLabel: i18n.KeyTipDiscPlanLabel, idDiscTemplate: i18n.KeyTipDiscTemplate,
	idDiscYear: i18n.KeyTipDiscYear, idDiscKeep: i18n.KeyTipDiscKeep, idDiscAllPlans: i18n.KeyTipDiscAllPlans,
	idDiscSQL: i18n.KeyTipDiscSQL, idDiscResult: i18n.KeyTipDiscResult, idDiscCheck: i18n.KeyTipDiscCheck,
	idDiscApply: i18n.KeyTipDiscApply, idDiscClose: i18n.KeyTipDiscClose,
}

// discoveryOutputs are the static texts of the dialog (no tooltip).
var discoveryOutputs = map[int32]bool{idDiscState: true, idDiscSource: true}

// discFieldControl maps a mapping.* field (and the query field) to the
// control highlighted by a check / request error.
// mappings / mappings[i].* (additional plans) highlight «все планы
// обмена»; the hidden link fields of a candidate highlight the times
// table; limit the button of the request (no input box).
var discFieldControl = map[string]int32{
	"mapping.mode": idDiscMode, "mapping.sentNoColumn": idDiscSend, "mapping.receivedNoColumn": idDiscRecv,
	"mapping.manualSql": idDiscSQL, "mapping.yearOffset": idDiscYear, "mapping.keepUnlinkedPoints": idDiscKeep,
	"mapping.timesSchema": idDiscTimesTable, "mapping.timesLinkTypeColumn": idDiscTimesLink,
	"mapping.timesLinkRTRefColumn": idDiscTimesLink, "mapping.timesLinkRTRef": idDiscTimesLink,
	"mappings":                          idDiscAllPlans,
	string(traysettings.FieldDiscPoint): idDiscPoint, string(traysettings.FieldDiscTime): idDiscTime,
	string(traysettings.FieldDiscTolerance): idDiscTol,
}

// discControl is the control of a highlighted field (mappings[i].* →
// «все планы обмена», limit → the button of the last request).
func (dd *discoveryDialog) discControl(field string) (int32, bool) {
	if strings.HasPrefix(field, "mappings[") {
		return idDiscAllPlans, true
	}
	if field == string(traysettings.FieldDiscLimit) {
		if len(dd.cmds) > 0 && dd.cmds[len(dd.cmds)-1] == model.CmdListNodePoints {
			return idDiscPoints, true
		}
		return idDiscCheck, true
	}
	id, ok := discFieldControl[field]
	return id, ok
}

func init() {
	for _, e := range discMapEdits {
		discFieldControl[e.field] = e.id
	}
}

// discoveryDialog is the Discovery dialog: discover / discover_by_value /
// list_node_points / check_mapping of the profile being edited (the
// test_profile rules: saved and unchanged connection -> profileId, else
// profile + password; the mapping is never part of these requests), the
// mapping editor, manual SQL templates; «Применить» validates the mapping
// and puts it into the profile as an unsaved edit (save_profile with
// expectedRevision by «Сохранить»). The owner window is disabled while
// the dialog is open; closing it cancels a running request (cancel {ref}).
// Errors and check results go to the multi-line result box (full text).
type discoveryDialog struct {
	inCommand int // nesting of WM_COMMAND (deferred destroy)
	w         *settingsWindow
	hwnd      uintptr
	ctl       map[int32]uintptr
	tips      *tooltips // CHK-TRAY-15
	d         *traysettings.Discovery

	profileID string // profile at opening (Apply refuses another one)
	isNew     bool

	cancel  context.CancelFunc
	seq     int
	started time.Time
	running string // KeyDiscRunning / KeyDiscChecking while a request runs
	cmds    []string
	loading bool
	closed  bool
	state   string // short state line (count of candidates, running)
	result  string // result box: errors, check result, incomplete notes
	// lastField: the last highlighted field (test hook)
	lastField string
}

// Dialog style and client size: fits a 1024×768 screen with a taskbar
// (Windows 7); windowRect clamps it to the work area.
const (
	discStyle          = winapi.WS_OVERLAPPED | winapi.WS_CAPTION | winapi.WS_SYSMENU
	discCW, discCH     = 860, 684
	discCol2, discEdit = 435, 548
)

// discoveryAvailable: «Таблицы обмена…» needs the administrator window
// and the four capabilities (discover, discover_by_value, node_points,
// check_mapping).
func (w *settingsWindow) discoveryAvailable() bool {
	return traysettings.DiscoveryAvailable(w.cfg.mode, w.cfg.session.Status().Has)
}

// openDiscovery opens the dialog for the profile being edited (or brings
// the open one to the front).
func (w *settingsWindow) openDiscovery() {
	if dd := w.disc; dd != nil {
		if !w.cfg.hidden && winapi.IsWindow(dd.hwnd) {
			frontWindow(dd.hwnd)
		}
		return
	}
	if w.busy || !w.discoveryAvailable() || !w.ed.HasProfile() {
		return
	}
	w.readForm()
	dd := &discoveryDialog{w: w, ctl: map[int32]uintptr{}, d: traysettings.NewDiscovery(w.cfg.lang, w.ed.EffectiveMapping()),
		profileID: w.ed.Base.ID, isNew: w.ed.IsNew}
	title := w.t(i18n.KeyDiscTitle)
	if n := w.ed.Base.DisplayName(); n != "" && !w.ed.IsNew {
		title += " — " + n
	}
	hwnd, err := createWindow(DiscoveryClass, title, 0, discStyle, 0, 0, discCW, discCH, w.hwnd, dd.wndProc)
	if err != nil {
		w.setStatus(fmt.Sprintf(w.t(i18n.KeySetFailed), err.Error()))
		return
	}
	dd.hwnd = hwnd
	w.disc = dd
	dd.build()
	dd.fill()
	winapi.EnableWindow(w.hwnd, false)
	if !w.cfg.hidden {
		winapi.ShowWindow(hwnd, winapi.SW_SHOWNORMAL)
		winapi.SetForegroundWindow(hwnd)
	}
	w.update()
}

func (dd *discoveryDialog) t(key string) string { return dd.w.t(key) }

func (dd *discoveryDialog) add(class, text string, ex, style uint32, id, x, y, cw, ch int32) uintptr {
	x, y, cw, ch = pxRect(x, y, cw, ch)
	h, err := winapi.CreateWindowEx(ex, class, text, winapi.WS_CHILD|winapi.WS_VISIBLE|style, x, y, cw, ch, dd.hwnd, uintptr(id), winapi.GetModuleHandle())
	if err != nil {
		return 0
	}
	winapi.SendMessage(h, winapi.WM_SETFONT, uiFont(), 1)
	if id != 0 {
		dd.ctl[id] = h
	}
	return h
}

func (dd *discoveryDialog) build() {
	edit := uint32(winapi.WS_TABSTOP | winapi.ES_AUTOHSCROLL)
	ro := uint32(winapi.WS_TABSTOP | winapi.ES_MULTILINE | winapi.ES_READONLY | winapi.ES_AUTOVSCROLL | winapi.WS_VSCROLL)
	// phase 11 (CHK-TRAY-45): controls created at 0,0 and laid out by
	// the framework (labels measured, rows on a common baseline)
	label := func(key string) *ui.Ctl {
		return uiCtl(dd.add("STATIC", dd.t(key), 0, winapi.SS_LEFT, 0, 0, 0, 0, 0), ui.Label, dd.t(key))
	}
	c := func(class, text string, ex, style uint32, id int32, kind ui.Kind, w, h int32) *ui.Ctl {
		x := uiCtl(dd.add(class, text, ex, style, id, 0, 0, 0, 0), kind, text)
		x.W, x.Hgt = w, h
		return x
	}
	btn := func(key string, id int32) *ui.Ctl {
		return c("BUTTON", dd.t(key), 0, winapi.WS_TABSTOP, id, ui.Button, 0, 0)
	}
	grow := func(x *ui.Ctl) *ui.Ctl { x.Grow = true; return x }
	// search buttons and the short state line (count / «Поиск… N с»)
	top := &ui.Row{Grow: true, Items: []ui.Node{btn(i18n.KeyDiscFind, idDiscFind), btn(i18n.KeyDiscListPoints, idDiscPoints),
		btn(i18n.KeyDiscCancel, idDiscCancel), grow(c("STATIC", "", 0, winapi.SS_LEFT, idDiscState, ui.Box, 200, 18))}}
	// search by a known point and time (discover_by_value)
	pointL := label(i18n.KeyDiscPoint)
	point := c("COMBOBOX", "", 0, winapi.WS_TABSTOP|winapi.WS_VSCROLL|winapi.CBS_DROPDOWN|winapi.CBS_AUTOHSCROLL, idDiscPoint, ui.Combo, 230, 0)
	point.NoShrink = true // phase 11 (CHK-TRAY-49): not narrowed at 144 dpi
	timeL := label(i18n.KeyDiscTime)
	tm := c("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idDiscTime, ui.Edit, 100, 0)
	tolL := label(i18n.KeyDiscTolerance)
	tol := c("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit|winapi.ES_NUMBER, idDiscTol, ui.Edit, 45, 0)
	// the hint «0 или пусто…» under its own field (phase 11)
	tolG := &ui.Grid{ColGap: ui.P8, Rows: []ui.GridRow{{Label: tolL, Field: &ui.Col{Gap: 2, Items: []ui.Node{tol, label(i18n.KeyDiscToleranceHint)}}}}}
	// two rows so that the window fits 1024 px at 144 dpi (phase 11)
	byValue := &ui.Col{Gap: 6, Items: []ui.Node{&ui.Row{Items: []ui.Node{pointL, point, timeL, tm}},
		&ui.Row{Grow: true, Align: ui.Top, Items: []ui.Node{tolG, &ui.Row{Right: true, Items: []ui.Node{btn(i18n.KeyDiscByValue, idDiscByValue)}}}}}}
	// candidates and the details of the selected one (no horizontal
	// scrolling: the details box shows the whole candidate)
	// every multi-line box has its label (phase 11)
	candL := label(i18n.KeyDiscCandidates)
	cand := c("LISTBOX", "", winapi.WS_EX_CLIENTEDGE, winapi.WS_TABSTOP|winapi.WS_VSCROLL|winapi.LBS_NOTIFY|0x0100 /* LBS_NOINTEGRALHEIGHT */, idDiscList, ui.Box, 520, 132)
	detL := label(i18n.KeyDiscDetails)
	det := c("EDIT", "", winapi.WS_EX_CLIENTEDGE, ro, idDiscDetails, ui.Box, 308, 132)
	det.Grow = true
	lists := &ui.Row{Align: ui.Top, Grow: true, Items: []ui.Node{
		&ui.Col{Gap: 2, Items: []ui.Node{candL, cand}}, &ui.Col{Gap: 2, Items: []ui.Node{detL, det}}}}
	// mapping editor: the kind, its source label, two columns of boxes
	modeL := label(i18n.KeyDiscMode)
	mode := c("COMBOBOX", "", 0, winapi.WS_TABSTOP|winapi.CBS_DROPDOWNLIST, idDiscMode, ui.Combo, 290, 0)
	modeRow := &ui.Row{Grow: true, Items: []ui.Node{modeL, mode, &ui.Space{W: ui.P8}, grow(c("STATIC", "", 0, winapi.SS_LEFT, idDiscSource, ui.Box, 240, 18))}}
	g1, g2 := ui.Grid{Gap: ui.P4}, ui.Grid{Gap: ui.P4}
	for i, e := range discMapEdits {
		l := label(e.key)
		g, cw := &g1, int32(290)
		if i >= 5 {
			g, cw = &g2, 300
		}
		g.Rows = append(g.Rows, ui.GridRow{Label: l, Field: c("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, e.id, ui.Edit, cw, 0)})
	}
	maps := &ui.Row{Align: ui.Top, Gap: ui.P16, Items: []ui.Node{&g1, &g2}}
	cb := uint32(winapi.WS_TABSTOP | winapi.BS_AUTOCHECKBOX)
	// two rows: the window fits 1024 px at 144 dpi (phase 11)
	checks := &ui.Col{Gap: ui.P4, Items: []ui.Node{&ui.Row{Gap: ui.P16, Items: []ui.Node{c("BUTTON", dd.t(i18n.KeyDiscYearOffset), 0, cb, idDiscYear, ui.Check, 0, 0),
		c("BUTTON", dd.t(i18n.KeyDiscKeepUnlinked), 0, cb, idDiscKeep, ui.Check, 0, 0)}},
		c("BUTTON", dd.t(i18n.KeyDiscAllPlans), 0, cb, idDiscAllPlans, ui.Check, 0, 0)}}
	sqlHead := &ui.Row{Grow: true, Items: []ui.Node{label(i18n.KeyDiscManualSQL), &ui.Row{Right: true, Items: []ui.Node{label(i18n.KeyDiscTemplate),
		c("COMBOBOX", "", 0, winapi.WS_TABSTOP|winapi.WS_VSCROLL|winapi.CBS_DROPDOWNLIST, idDiscTemplate, ui.Combo, 300, 0)}}}}
	sql := c("EDIT", "", winapi.WS_EX_CLIENTEDGE, winapi.WS_TABSTOP|winapi.ES_MULTILINE|winapi.ES_WANTRETURN|winapi.ES_AUTOVSCROLL|winapi.WS_VSCROLL,
		idDiscSQL, ui.Box, 836, 64)
	sql.Grow = true
	sql.Hgt = 64
	resL := label(i18n.KeyDiscResultLabel)
	res := c("EDIT", "", winapi.WS_EX_CLIENTEDGE, ro, idDiscResult, ui.Box, 836, 56)
	res.Grow = true
	buttons := &ui.Row{Grow: true, Items: []ui.Node{btn(i18n.KeyDiscCheck, idDiscCheck), btn(i18n.KeyDiscApply, idDiscApply),
		&ui.Row{Right: true, Items: []ui.Node{btn(i18n.KeyDiscClose, idDiscClose)}}}}
	// phase 11: the buttons pinned to the bottom, the rest scrolls when
	// the work area is too small (1024×768 at 144 dpi)
	content := &ui.Pad{Left: ui.P12, Top: ui.P12, Right: ui.P12, Child: &ui.Col{Gap: 6, Items: []ui.Node{
		top, byValue, lists, modeRow, maps, checks, sqlHead, sql, &ui.Col{Gap: 2, Items: []ui.Node{resL, res}},
	}}}
	// cues of the edit boxes (phase 11, PLAN §12 p.50), before the layout: the boxes are as wide as their cues (CHK-TRAY-48)
	for _, f := range traysettings.UIFields {
		switch f.ID {
		case traysettings.FieldDiscTime:
			winapi.SetCueBanner(dd.ctl[idDiscTime], f.CueText(dd.w.cfg.lang))
		case traysettings.FieldDiscTolerance:
			winapi.SetCueBanner(dd.ctl[idDiscTol], f.CueText(dd.w.cfg.lang))
		}
	}
	for _, e := range discMapEdits {
		cue := i18n.KeyCueDiscColumn
		if e.id == idDiscSchema {
			cue = i18n.KeyCueDiscSchema
		}
		winapi.SetCueBanner(dd.ctl[e.id], dd.t(cue))
	}
	fitForm(dd.hwnd, content, &ui.Pad{All: ui.P12, Child: buttons}, 0, false)
	for _, k := range []string{i18n.KeyDiscModeAuto, i18n.KeyDiscModeManual, i18n.KeyDiscModeCounters} {
		winapi.SendMessageString(dd.ctl[idDiscMode], winapi.CB_ADDSTRING, 0, dd.t(k))
	}
	for _, s := range traysettings.TemplateTitles(dd.w.cfg.lang) {
		winapi.SendMessageString(dd.ctl[idDiscTemplate], winapi.CB_ADDSTRING, 0, s)
	}
	dd.tips = newTooltips(dd.hwnd)
	for id, h := range dd.ctl {
		if key, ok := discoveryTips[id]; ok {
			dd.tips.set(h, dd.t(key))
		}
	}
}

const msgDiscDestroy = winapi.WM_APP + 9

func (dd *discoveryDialog) wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case winapi.WM_COMMAND:
		// a button is still inside BM_CLICK: the dialog must not be
		// destroyed under it (an access violation in user32)
		dd.inCommand++
		dd.command(int32(wp&0xFFFF), uint32(wp>>16&0xFFFF))
		dd.inCommand--
		return 0, true
	case msgDiscDestroy:
		winapi.DestroyWindow(hwnd)
		return 0, true
	case winapi.WM_CLOSE:
		dd.userClose()
		return 0, true
	}
	if isTrayExit(msg) { // CHK-TRAY-11: the dialog and its settings window
		dd.w.exitNow()
		return 0, true
	}
	return 0, false
}

func (dd *discoveryDialog) command(id int32, code uint32) {
	switch id {
	case idDiscFind:
		dd.find()
	case idDiscPoints:
		dd.listPoints()
	case idDiscByValue:
		dd.byValue()
	case idDiscCancel:
		if dd.cancel != nil {
			dd.cancel()
		}
	case idDiscList:
		if code == winapi.LBN_SELCHANGE {
			dd.selectCandidate(int(int32(winapi.SendMessage(dd.ctl[idDiscList], winapi.LB_GETCURSEL, 0, 0))))
		}
	case idDiscMode:
		if code == winapi.CBN_SELCHANGE && !dd.loading {
			dd.setKind(int(int32(winapi.SendMessage(dd.ctl[idDiscMode], winapi.CB_GETCURSEL, 0, 0))))
		}
	case idDiscTemplate:
		if code == winapi.CBN_SELCHANGE && !dd.loading {
			dd.template(int(int32(winapi.SendMessage(dd.ctl[idDiscTemplate], winapi.CB_GETCURSEL, 0, 0))))
		}
	case idDiscSchema, idDiscTable, idDiscPointName, idDiscPointID, idDiscPointKey, idDiscTimesTable, idDiscTimesLink,
		idDiscSend, idDiscRecv, idDiscPlanLabel, idDiscSQL:
		if code == winapi.EN_CHANGE && !dd.loading {
			dd.readMap()
			dd.update() // source label: manual once a column is edited
		}
	case idDiscYear, idDiscKeep:
		if code == winapi.BN_CLICKED && !dd.loading {
			dd.readMap()
			dd.update()
		}
	case idDiscCheck:
		dd.check()
	case idDiscApply:
		dd.apply()
	case idDiscClose:
		dd.userClose()
	}
}

func (dd *discoveryDialog) text(id int32) string { return winapi.GetWindowText(dd.ctl[id]) }

func (dd *discoveryDialog) setText(id int32, s string) {
	if dd.text(id) != s {
		winapi.SetWindowText(dd.ctl[id], s)
	}
}

func (dd *discoveryDialog) checked(id int32) bool {
	return winapi.SendMessage(dd.ctl[id], winapi.BM_GETCHECK, 0, 0) == winapi.BST_CHECKED
}

func (dd *discoveryDialog) setChecked(id int32, on bool) {
	v := uintptr(winapi.BST_UNCHECKED)
	if on {
		v = winapi.BST_CHECKED
	}
	winapi.SendMessage(dd.ctl[id], winapi.BM_SETCHECK, v, 0)
}

// toEdit / fromEdit convert the line ends of the multi-line boxes.
func toEdit(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

func fromEdit(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// mapText returns the address of the MappingForm string of a text box.
func mapText(f *traysettings.MappingForm, id int32) *string {
	switch id {
	case idDiscSchema:
		return &f.Schema
	case idDiscTable:
		return &f.Table
	case idDiscPointName:
		return &f.PointName
	case idDiscPointID:
		return &f.PointID
	case idDiscPointKey:
		return &f.PointKey
	case idDiscTimesTable:
		return &f.TimesTable
	case idDiscTimesLink:
		return &f.TimesLink
	case idDiscSend:
		return &f.Send
	case idDiscRecv:
		return &f.Recv
	case idDiscPlanLabel:
		return &f.PlanLabel
	case idDiscSQL:
		return &f.ManualSQL
	}
	return nil
}

// discEditField is the JSON name of the mapping field of a text box in
// kind k (send / receive columns are the counters in counters mode).
func discEditField(id int32, k traysettings.MappingKind) string {
	for _, e := range discMapEdits {
		if e.id == id {
			f := strings.TrimPrefix(e.field, "mapping.")
			if k == traysettings.KindCounters && id == idDiscSend {
				f = "sentNoColumn"
			} else if k == traysettings.KindCounters && id == idDiscRecv {
				f = "receivedNoColumn"
			}
			return f
		}
	}
	return ""
}

// readMap copies the editable boxes of the current kind into the form
// (read-only boxes hold nothing the mapping would use).
func (dd *discoveryDialog) readMap() {
	f := &dd.d.Map
	for _, e := range discMapEdits {
		if traysettings.FieldEditable(f.Kind, discEditField(e.id, f.Kind)) {
			*mapText(f, e.id) = dd.text(e.id)
		}
	}
	if traysettings.FieldEditable(f.Kind, "manualSql") {
		f.ManualSQL = fromEdit(dd.text(idDiscSQL))
	}
	if traysettings.FieldEditable(f.Kind, "yearOffset") {
		f.YearOffset2000 = dd.checked(idDiscYear)
	}
	if traysettings.FieldEditable(f.Kind, "keepUnlinkedPoints") {
		f.KeepUnlinked = dd.checked(idDiscKeep)
	}
}

// fill shows the lists and the mapping form.
func (dd *discoveryDialog) fill() {
	dd.loading = true
	defer func() { dd.loading = false }()
	lb := dd.ctl[idDiscList]
	winapi.SendMessage(lb, winapi.LB_RESETCONTENT, 0, 0)
	for _, c := range dd.d.Candidates {
		winapi.SendMessageString(lb, winapi.LB_ADDSTRING, 0, dd.d.CandidateLine(c))
	}
	winapi.SendMessage(lb, winapi.LB_SETCURSEL, uintptr(dd.d.Sel), 0)
	det := ""
	if dd.d.Sel >= 0 {
		det = dd.d.CandidateLine(dd.d.Candidates[dd.d.Sel]) + "\r\n" + dd.d.CandidateDetails(dd.d.Candidates[dd.d.Sel])
	}
	dd.setText(idDiscDetails, det)
	dd.fillMap()
}

// fillMap shows the mapping form (the boxes of other kinds are empty).
func (dd *discoveryDialog) fillMap() {
	dd.loading = true
	defer func() { dd.loading = false }()
	f := dd.d.Map
	for _, e := range discMapEdits {
		dd.setText(e.id, *mapText(&f, e.id))
	}
	dd.setText(idDiscSQL, toEdit(f.ManualSQL))
	winapi.SendMessage(dd.ctl[idDiscMode], winapi.CB_SETCURSEL, uintptr(f.Kind), 0)
	dd.setChecked(idDiscYear, f.YearOffset2000)
	dd.setChecked(idDiscKeep, f.KeepUnlinked)
	dd.update()
}

// fillPoints shows the exchange points in the point box (the typed text
// is kept).
func (dd *discoveryDialog) fillPoints() {
	cb := dd.ctl[idDiscPoint]
	typed := dd.text(idDiscPoint)
	winapi.SendMessage(cb, winapi.CB_RESETCONTENT, 0, 0)
	for _, p := range dd.d.NodePoints {
		winapi.SendMessageString(cb, winapi.CB_ADDSTRING, 0, traysettings.PointLine(p))
	}
	dd.setText(idDiscPoint, typed)
}

// sourceText is the source label of the mapping in the form.
func (dd *discoveryDialog) sourceText() string {
	m := dd.d.Map.Mapping()
	switch {
	case m.Mode == model.MappingCounters:
		return dd.t(i18n.KeyDiscSourceCounters)
	case !m.Configured() && m.Mode == model.MappingNone:
		return dd.t(i18n.KeyDiscSourceNone)
	case m.Mode == model.MappingAuto:
		return dd.t(i18n.KeyDiscSourceAuto)
	}
	return dd.t(i18n.KeyDiscSourceManual)
}

// update refreshes the enabled states, the state line and the result box;
// only the boxes of the current kind are editable.
func (dd *discoveryDialog) update() {
	run := dd.cancel != nil
	for _, id := range []int32{idDiscFind, idDiscPoints, idDiscByValue, idDiscCheck, idDiscApply} {
		winapi.EnableWindow(dd.ctl[id], !run)
	}
	winapi.EnableWindow(dd.ctl[idDiscCancel], run)
	k := dd.d.Map.Kind
	for _, e := range discMapEdits {
		winapi.SendMessage(dd.ctl[e.id], winapi.EM_SETREADONLY, boolPtr(!traysettings.FieldEditable(k, discEditField(e.id, k))), 0)
	}
	winapi.SendMessage(dd.ctl[idDiscSQL], winapi.EM_SETREADONLY, boolPtr(!traysettings.FieldEditable(k, "manualSql")), 0)
	winapi.EnableWindow(dd.ctl[idDiscYear], traysettings.FieldEditable(k, "yearOffset"))
	winapi.EnableWindow(dd.ctl[idDiscKeep], traysettings.FieldEditable(k, "keepUnlinkedPoints"))
	winapi.EnableWindow(dd.ctl[idDiscAllPlans], len(dd.d.PlanMappings(dd.d.Map.Mapping())) > 0)
	dd.setText(idDiscSource, dd.sourceText())
	st := dd.state
	if run {
		st = fmt.Sprintf(dd.t(dd.running), int(time.Since(dd.started)/time.Second))
	}
	dd.setText(idDiscState, st)
	dd.setText(idDiscResult, toEdit(dd.result))
}

// ask is a Yes/No question owned by the dialog («Нет» by default).
func (dd *discoveryDialog) ask(text string) bool {
	confirm := dd.w.cfg.confirm
	if confirm == nil {
		confirm = func(owner uintptr, text, caption string) bool {
			return winapi.MessageBox(owner, text, caption, winapi.MB_YESNO|winapi.MB_ICONWARNING|winapi.MB_DEFBUTTON2) == winapi.IDYES
		}
	}
	if dd.w.exiting {
		return false
	}
	ok := confirm(dd.hwnd, text, dd.t(i18n.KeyAppName))
	return ok && !dd.w.exiting // the exit message came while it was open
}

// overwriteOK asks before a new search / another candidate / a template
// replaces the fields edited by hand.
func (dd *discoveryDialog) overwriteOK() bool {
	dd.readMap()
	if !dd.d.Edited() {
		return true
	}
	if !dd.ask(dd.t(i18n.KeyDiscConfirmOverwrite)) {
		return false
	}
	dd.d.AcceptOverwrite()
	return true
}

// password is the password of the edited profile for one request (the
// test_profile rules: only an edited profile carries it).
func (dd *discoveryDialog) password() string { return dd.w.readPassword() }

// run sends one interactive request: «Поиск… N с» with «Отменить»
// (cancel {ref}); the tray waits DiscoveryTimeout (the service answers
// within 100 s). A reply after the dialog was closed or after a newer
// request is ignored.
func (dd *discoveryDialog) run(req model.IPCRequest, running string, done func(model.IPCMessage)) {
	if dd.cancel != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), traysettings.DiscoveryTimeout)
	dd.cancel = cancel
	dd.seq++
	seq := dd.seq
	dd.started, dd.running = time.Now(), running
	dd.cmds = append(dd.cmds, req.Cmd)
	dd.update()
	s, w := dd.w.cfg.session, dd.w
	go func() { // the seconds counter
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				w.post(func() {
					if !dd.closed && seq == dd.seq && dd.cancel != nil {
						dd.update()
					}
				})
			}
		}
	}()
	go func() {
		m, err := s.DoCancel(ctx, req)
		cancel()
		w.post(func() {
			if dd.closed || seq != dd.seq {
				return
			}
			dd.cancel = nil
			if pr := w.ed.InteractiveProblem(m, err); pr != nil {
				dd.problem(pr)
				return
			}
			done(m)
		})
	}()
}

// problem shows an error in the result box (full text) and highlights its
// field.
func (dd *discoveryDialog) problem(p *traysettings.Problem) {
	dd.result = p.Text
	dd.state = ""
	if p.Field != traysettings.FieldNone {
		dd.highlight(string(p.Field))
	}
	dd.update()
}

// highlight moves the focus to the control of a mapping.* / query / limit
// field.
func (dd *discoveryDialog) highlight(field string) {
	dd.lastField = field
	if id, ok := dd.discControl(field); ok {
		winapi.SetFocus(dd.ctl[id])
	}
}

func (dd *discoveryDialog) find() {
	if !dd.overwriteOK() {
		return
	}
	req, pr := dd.w.ed.InteractiveRequest(model.CmdDiscover, dd.password())
	if pr != nil {
		dd.problem(pr)
		return
	}
	dd.run(req, i18n.KeyDiscRunning, dd.discovered)
}

func (dd *discoveryDialog) discovered(m model.IPCMessage) {
	dd.readMap() // edits typed while the search ran are kept
	dd.d.DiscoveryDone(m)
	dd.state, dd.result = dd.d.CountText(), dd.d.IncompleteNote()
	dd.fill()
}

func (dd *discoveryDialog) listPoints() {
	req, pr := dd.w.ed.InteractiveRequest(model.CmdListNodePoints, dd.password())
	if pr != nil {
		dd.problem(pr)
		return
	}
	req.Limit = traysettings.DefaultNodePointsLimit
	dd.run(req, i18n.KeyDiscRunning, func(m model.IPCMessage) {
		dd.d.NodePointsDone(m)
		dd.fillPoints()
		dd.state = fmt.Sprintf(dd.t(i18n.KeyDiscPointsFound), len(dd.d.NodePoints))
		dd.result = dd.d.PointsNote()
		dd.update()
	})
}

func (dd *discoveryDialog) byValue() {
	f := traysettings.ValueForm{Point: dd.text(idDiscPoint), ApproxTime: dd.text(idDiscTime), Tolerance: dd.text(idDiscTol),
		PointIndex: int(int32(winapi.SendMessage(dd.ctl[idDiscPoint], winapi.CB_GETCURSEL, 0, 0)))}
	q, pr := dd.d.Query(f)
	if pr != nil {
		dd.problem(pr)
		return
	}
	if !dd.overwriteOK() {
		return
	}
	req, pr := dd.w.ed.InteractiveRequest(model.CmdDiscoverByValue, dd.password())
	if pr != nil {
		dd.problem(pr)
		return
	}
	req.Query = q
	dd.run(req, i18n.KeyDiscRunning, dd.discovered)
}

// selectCandidate takes candidate i into the form (asks over hand edits;
// «Нет» restores the list selection). A list replaced by a search reply
// while the question was open voids the choice (Discovery.Gen).
func (dd *discoveryDialog) selectCandidate(i int) {
	if i == dd.d.Sel {
		return
	}
	gen := dd.d.Gen
	if !dd.overwriteOK() {
		winapi.SendMessage(dd.ctl[idDiscList], winapi.LB_SETCURSEL, uintptr(dd.d.Sel), 0)
		return
	}
	if dd.closed {
		return
	}
	if gen != dd.d.Gen { // a search reply replaced the list during the question
		dd.result = dd.t(i18n.KeyDiscListChanged)
		dd.fill()
		return
	}
	dd.d.Select(i)
	dd.result = ""
	dd.fill()
}

// setKind switches the editor kind; the fields the kind does not use are
// cleared (never validated or checked hidden).
func (dd *discoveryDialog) setKind(i int) {
	if i < 0 || i >= len(traysettings.MappingKinds) {
		return
	}
	dd.readMap()
	dd.d.Map.SetKind(traysettings.MappingKinds[i])
	dd.fillMap()
}

// template puts manual SQL template i into the editor (manual SQL kind).
func (dd *discoveryDialog) template(i int) {
	ts := model.SQLManualTemplates()
	if i < 0 || i >= len(ts) {
		return
	}
	if !dd.overwriteOK() {
		return
	}
	dd.d.Map.SetKind(traysettings.KindSQL)
	dd.d.Map.ManualSQL = ts[i].SQL
	dd.fillMap()
}

func (dd *discoveryDialog) check() {
	dd.readMap()
	req, pr := dd.w.ed.CheckRequest(dd.d.Map.Mapping(), dd.password(), 0)
	if pr != nil {
		dd.problem(pr)
		return
	}
	dd.run(req, i18n.KeyDiscChecking, func(m model.IPCMessage) {
		if m.MappingCheck == nil {
			dd.problem(&traysettings.Problem{Text: fmt.Sprintf(dd.t(i18n.KeySetFailed), "check_mapping: "+m.Type)})
			return
		}
		mc := *m.MappingCheck
		dd.d.Check = &mc
		text, field := dd.d.CheckResult(mc)
		dd.result, dd.state = text, ""
		if field != "" {
			dd.highlight(field)
		}
		dd.update()
	})
}

func sameMappings(a, b []model.Mapping) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// apply validates the mapping (and «все планы обмена», merged with the
// profile's additional plans without duplicates) and puts it into the
// edited profile as an unsaved edit; anything the merge would remove is
// confirmed first. «Сохранить» sends save_profile with expectedRevision.
func (dd *discoveryDialog) apply() {
	w, ed := dd.w, dd.w.ed
	if !ed.HasProfile() || ed.IsNew != dd.isNew || (!dd.isNew && ed.Base.ID != dd.profileID) {
		dd.problem(&traysettings.Problem{Text: dd.t(i18n.KeyDiscProfileChanged)})
		return
	}
	if w.busy { // a get_config / save reply would replace the edit
		dd.problem(&traysettings.Problem{Text: dd.t(i18n.KeyDiscBusy)})
		return
	}
	dd.readMap()
	// no edits: the profile's own mapping as it is (Apply never changes
	// what the service polls without an edit, e.g. table + manual SQL)
	m, _ := dd.d.Result()
	var sugg []model.Mapping
	if dd.checked(idDiscAllPlans) {
		sugg = dd.d.PlanMappings(m)
	}
	oldMain, oldMaps, rev := ed.EffectiveMapping(), ed.EffectiveMappings(), ed.Revision
	merged, removed := traysettings.MergeMappings(oldMain, oldMaps, m, sugg)
	if fe := traysettings.ValidateMappings(m, merged); fe != nil {
		dd.problem(dd.d.ValidationProblem(fe))
		return
	}
	if len(removed) > 0 {
		if !dd.ask(fmt.Sprintf(dd.t(i18n.KeyDiscConfirmRemove), strings.Join(removed, ", "))) {
			return
		}
		// the question pumps messages: a get_config / save reply may have
		// replaced the profile meanwhile — merged would overwrite it
		if dd.closed {
			return
		}
		if w.busy || ed.Revision != rev || !ed.HasProfile() || ed.IsNew != dd.isNew || (!dd.isNew && ed.Base.ID != dd.profileID) ||
			ed.EffectiveMapping() != oldMain || !sameMappings(ed.EffectiveMappings(), oldMaps) {
			dd.problem(&traysettings.Problem{Text: dd.t(i18n.KeyDiscConfigChanged)})
			return
		}
	}
	changed := m != oldMain || !sameMappings(merged, oldMaps)
	if changed {
		ed.SetMapping(m, merged)
	}
	dd.close()
	if changed {
		w.status = w.t(i18n.KeyDiscApplied)
	}
	w.fill()
}

// userClose: the close box, «Закрыть» or Esc — asks before dropping
// changes not applied (like the settings window, §12 p.67).
func (dd *discoveryDialog) userClose() {
	dd.readMap()
	if dd.d.Unapplied() && !dd.ask(dd.t(i18n.KeyDiscConfirmClose)) {
		return
	}
	dd.close()
}

// close cancels a running request and destroys the dialog; the owner is
// enabled again.
func (dd *discoveryDialog) close() {
	if dd.closed {
		return
	}
	dd.closed = true
	if dd.cancel != nil {
		dd.cancel()
		dd.cancel = nil
	}
	w := dd.w
	winapi.EnableWindow(w.hwnd, true)
	if dd.inCommand > 0 {
		winapi.ShowWindow(dd.hwnd, winapi.SW_HIDE)
		winapi.PostMessage(dd.hwnd, msgDiscDestroy, 0, 0)
	} else {
		winapi.DestroyWindow(dd.hwnd)
	}
	if w.disc == dd {
		w.disc = nil
	}
	if w.exiting { // the whole settings window goes away (CHK-TRAY-11)
		return
	}
	if !w.cfg.hidden && winapi.IsWindow(w.hwnd) {
		winapi.SetForegroundWindow(w.hwnd)
	}
	if winapi.IsWindow(w.hwnd) {
		w.update()
	}
}
