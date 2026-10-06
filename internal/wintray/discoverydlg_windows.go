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
)

// DiscoveryClass is the window class of the Discovery dialog (phase 6d,
// CHK-TRAY-07): «Таблицы обмена…» of the administrator settings window.
const DiscoveryClass = "ExchangeCheckerTrayDiscovery"

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

// discFieldControl maps a mapping.* field (and the query field) to the
// control highlighted by a check / request error.
var discFieldControl = map[string]int32{
	"mapping.mode": idDiscMode, "mapping.sentNoColumn": idDiscSend, "mapping.receivedNoColumn": idDiscRecv,
	"mapping.manualSql": idDiscSQL, "mapping.yearOffset": idDiscYear, "mapping.keepUnlinkedPoints": idDiscKeep,
	string(traysettings.FieldDiscPoint): idDiscPoint, string(traysettings.FieldDiscTime): idDiscTime,
	string(traysettings.FieldDiscTolerance): idDiscTol,
}

func init() {
	for _, e := range discMapEdits {
		discFieldControl[e.field] = e.id
	}
}

// discoveryDialog is the Discovery dialog: discover / discover_by_value /
// list_node_points / check_mapping of the profile being edited (the
// test_profile rules: saved and unchanged -> profileId, else profile +
// password), the mapping editor, manual SQL templates; «Применить» puts
// the mapping into the profile as an unsaved edit (save_profile with
// expectedRevision by «Сохранить»). The owner window is disabled while
// the dialog is open; closing it cancels a running request (cancel {ref}).
type discoveryDialog struct {
	w    *settingsWindow
	hwnd uintptr
	ctl  map[int32]uintptr
	d    *traysettings.Discovery

	profileID string // profile at opening (Apply refuses another one)
	isNew     bool

	cancel  context.CancelFunc
	seq     int
	started time.Time
	running string // KeyDiscRunning / KeyDiscChecking while a request runs
	cmds    []string
	loading bool
	closed  bool
	state   string
	result  string
	// lastField: the last highlighted field (test hook)
	lastField string
}

// discoveryAvailable: «Таблицы обмена…» needs the administrator window
// and the four capabilities (discover, discover_by_value, node_points,
// check_mapping).
func (w *settingsWindow) discoveryAvailable() bool {
	return traysettings.DiscoveryAvailable(w.cfg.mode, w.cfg.session.Status().Has)
}

// openDiscovery opens the dialog for the profile being edited.
func (w *settingsWindow) openDiscovery() {
	if w.disc != nil || w.busy || !w.discoveryAvailable() || !w.ed.HasProfile() {
		return
	}
	w.readForm()
	dd := &discoveryDialog{w: w, ctl: map[int32]uintptr{}, d: traysettings.NewDiscovery(w.cfg.lang, w.ed.EffectiveMapping()),
		profileID: w.ed.Base.ID, isNew: w.ed.IsNew}
	const ww, wh = 870, 720
	x := (winapi.GetSystemMetrics(winapi.SM_CXSCREEN) - ww) / 2
	y := (winapi.GetSystemMetrics(winapi.SM_CYSCREEN) - wh) / 3
	title := w.t(i18n.KeyDiscTitle)
	if n := w.ed.Base.DisplayName(); n != "" && !w.ed.IsNew {
		title += " — " + n
	}
	hwnd, err := createWindow(DiscoveryClass, title, 0, winapi.WS_OVERLAPPED|winapi.WS_CAPTION|winapi.WS_SYSMENU,
		x, y, ww, wh, w.hwnd, dd.wndProc)
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
	h, err := winapi.CreateWindowEx(ex, class, text, winapi.WS_CHILD|winapi.WS_VISIBLE|style, x, y, cw, ch, dd.hwnd, uintptr(id), winapi.GetModuleHandle())
	if err != nil {
		return 0
	}
	winapi.SendMessage(h, winapi.WM_SETFONT, winapi.GetStockObject(winapi.DEFAULT_GUI_FONT), 1)
	if id != 0 {
		dd.ctl[id] = h
	}
	return h
}

func (dd *discoveryDialog) build() {
	edit := uint32(winapi.WS_TABSTOP | winapi.ES_AUTOHSCROLL)
	ro := uint32(winapi.ES_MULTILINE | winapi.ES_READONLY | winapi.ES_AUTOVSCROLL | winapi.WS_VSCROLL)
	label := func(key string, x, y, cw int32) { dd.add("STATIC", dd.t(key), 0, winapi.SS_LEFT, 0, x, y+3, cw, 18) }
	// search buttons and the running state
	dd.add("BUTTON", dd.t(i18n.KeyDiscFind), 0, winapi.WS_TABSTOP, idDiscFind, 12, 10, 150, 26)
	dd.add("BUTTON", dd.t(i18n.KeyDiscListPoints), 0, winapi.WS_TABSTOP, idDiscPoints, 168, 10, 150, 26)
	dd.add("BUTTON", dd.t(i18n.KeyDiscCancel), 0, winapi.WS_TABSTOP, idDiscCancel, 324, 10, 100, 26)
	dd.add("STATIC", "", 0, winapi.SS_LEFT, idDiscState, 432, 10, 420, 30)
	// search by a known point and time (discover_by_value)
	label(i18n.KeyDiscPoint, 12, 46, 60)
	dd.add("COMBOBOX", "", 0, winapi.WS_TABSTOP|winapi.WS_VSCROLL|winapi.CBS_DROPDOWN|winapi.CBS_AUTOHSCROLL, idDiscPoint, 75, 46, 250, 220)
	label(i18n.KeyDiscTime, 335, 46, 120)
	dd.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idDiscTime, 458, 46, 120, 23)
	label(i18n.KeyDiscTolerance, 586, 46, 90)
	dd.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit|winapi.ES_NUMBER, idDiscTol, 678, 46, 50, 23)
	dd.add("BUTTON", dd.t(i18n.KeyDiscByValue), 0, winapi.WS_TABSTOP, idDiscByValue, 736, 45, 116, 26)
	// candidates and the details of the selected one
	dd.add("LISTBOX", "", winapi.WS_EX_CLIENTEDGE, winapi.WS_TABSTOP|winapi.WS_VSCROLL|winapi.WS_HSCROLL|winapi.LBS_NOTIFY, idDiscList, 12, 80, 520, 170)
	dd.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, ro, idDiscDetails, 540, 80, 312, 166)
	// mapping editor: two columns
	row := int32(258)
	label(i18n.KeyDiscMode, 12, row, 110)
	dd.add("COMBOBOX", "", 0, winapi.WS_TABSTOP|winapi.CBS_DROPDOWNLIST, idDiscMode, 125, row, 290, 120)
	label(i18n.KeyDiscTemplate, 435, row, 110)
	dd.add("COMBOBOX", "", 0, winapi.WS_TABSTOP|winapi.WS_VSCROLL|winapi.CBS_DROPDOWNLIST, idDiscTemplate, 548, row, 304, 220)
	row += 27
	for i, e := range discMapEdits {
		x, ex, cw := int32(12), int32(125), int32(290)
		if i >= 5 {
			x, ex, cw = 435, 548, 304
		}
		r := row + int32(i%5)*27
		label(e.key, x, r, 110)
		dd.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, e.id, ex, r, cw, 23)
	}
	row += 5*27 + 2
	cb := uint32(winapi.WS_TABSTOP | winapi.BS_AUTOCHECKBOX)
	dd.add("BUTTON", dd.t(i18n.KeyDiscYearOffset), 0, cb, idDiscYear, 12, row, 270, 20)
	dd.add("BUTTON", dd.t(i18n.KeyDiscKeepUnlinked), 0, cb, idDiscKeep, 290, row, 270, 20)
	dd.add("BUTTON", dd.t(i18n.KeyDiscAllPlans), 0, cb, idDiscAllPlans, 568, row, 284, 20)
	row += 26
	label(i18n.KeyDiscManualSQL, 12, row-3, 300)
	row += 18
	dd.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, winapi.WS_TABSTOP|winapi.ES_MULTILINE|winapi.ES_WANTRETURN|winapi.ES_AUTOVSCROLL|winapi.WS_VSCROLL,
		idDiscSQL, 12, row, 840, 86)
	row += 92
	dd.add("EDIT", "", winapi.WS_EX_CLIENTEDGE, ro, idDiscResult, 12, row, 840, 80)
	row += 88
	dd.add("BUTTON", dd.t(i18n.KeyDiscCheck), 0, winapi.WS_TABSTOP, idDiscCheck, 12, row, 160, 28)
	dd.add("BUTTON", dd.t(i18n.KeyDiscApply), 0, winapi.WS_TABSTOP, idDiscApply, 178, row, 220, 28)
	dd.add("BUTTON", dd.t(i18n.KeyDiscClose), 0, winapi.WS_TABSTOP, idDiscClose, 762, row, 90, 28)
	for _, k := range []string{i18n.KeyDiscModeAuto, i18n.KeyDiscModeManual, i18n.KeyDiscModeCounters} {
		winapi.SendMessageString(dd.ctl[idDiscMode], winapi.CB_ADDSTRING, 0, dd.t(k))
	}
	for _, s := range traysettings.TemplateTitles(dd.w.cfg.lang) {
		winapi.SendMessageString(dd.ctl[idDiscTemplate], winapi.CB_ADDSTRING, 0, s)
	}
}

func (dd *discoveryDialog) wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case winapi.WM_COMMAND:
		dd.command(int32(wp&0xFFFF), uint32(wp>>16&0xFFFF))
		return 0, true
	case winapi.WM_CLOSE:
		dd.close()
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
			dd.readMap()
			dd.d.Select(int(int32(winapi.SendMessage(dd.ctl[idDiscList], winapi.LB_GETCURSEL, 0, 0))))
			dd.result = ""
			dd.fill()
		}
	case idDiscMode:
		if code == winapi.CBN_SELCHANGE && !dd.loading {
			dd.readMap()
			dd.update()
		}
	case idDiscTemplate:
		if code == winapi.CBN_SELCHANGE && !dd.loading {
			dd.template(int(int32(winapi.SendMessage(dd.ctl[idDiscTemplate], winapi.CB_GETCURSEL, 0, 0))))
		}
	case idDiscCheck:
		dd.check()
	case idDiscApply:
		dd.apply()
	case idDiscClose:
		dd.close()
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

// toEdit / fromEdit convert the line ends of the multi-line SQL box.
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

// readMap copies the mapping editor into the form.
func (dd *discoveryDialog) readMap() {
	f := &dd.d.Map
	for _, e := range discMapEdits {
		*mapText(f, e.id) = dd.text(e.id)
	}
	f.ManualSQL = fromEdit(dd.text(idDiscSQL))
	if i := int(int32(winapi.SendMessage(dd.ctl[idDiscMode], winapi.CB_GETCURSEL, 0, 0))); i >= 0 && i < len(traysettings.MappingModes) {
		f.Mode = traysettings.MappingModes[i]
	}
	f.YearOffset2000, f.KeepUnlinked = dd.checked(idDiscYear), dd.checked(idDiscKeep)
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
		det = dd.d.CandidateDetails(dd.d.Candidates[dd.d.Sel])
	}
	dd.setText(idDiscDetails, det)
	f := dd.d.Map
	for _, e := range discMapEdits {
		dd.setText(e.id, *mapText(&f, e.id))
	}
	dd.setText(idDiscSQL, toEdit(f.ManualSQL))
	winapi.SendMessage(dd.ctl[idDiscMode], winapi.CB_SETCURSEL, uintptr(traysettings.MappingModeIndex(f.Mode)), 0)
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

// update refreshes the enabled states, the running state and the result.
func (dd *discoveryDialog) update() {
	run := dd.cancel != nil
	for _, id := range []int32{idDiscFind, idDiscPoints, idDiscByValue, idDiscCheck, idDiscApply} {
		winapi.EnableWindow(dd.ctl[id], !run)
	}
	winapi.EnableWindow(dd.ctl[idDiscCancel], run)
	mode := dd.d.Map.Mode
	for _, e := range discMapEdits {
		winapi.SendMessage(dd.ctl[e.id], winapi.EM_SETREADONLY, boolPtr(mode == model.MappingManual && e.id != idDiscPlanLabel), 0)
	}
	winapi.SendMessage(dd.ctl[idDiscSQL], winapi.EM_SETREADONLY, boolPtr(mode != model.MappingManual), 0)
	winapi.EnableWindow(dd.ctl[idDiscYear], mode != model.MappingCounters)
	winapi.EnableWindow(dd.ctl[idDiscAllPlans], len(dd.d.PlanMappings(dd.d.Map.Mapping())) > 0)
	st := dd.state
	if run {
		st = fmt.Sprintf(dd.t(dd.running), int(time.Since(dd.started)/time.Second))
	}
	dd.setText(idDiscState, st)
	dd.setText(idDiscResult, dd.result)
}

// password is the password of the edited profile for one request (the
// test_profile rules: only an edited profile carries it).
func (dd *discoveryDialog) password() string { return dd.w.readPassword() }

// run sends one interactive request: «Поиск… N с» with «Отменить»
// (cancel {ref}), the service answers within 100 s.
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

// problem shows an error of a request and highlights its field.
func (dd *discoveryDialog) problem(p *traysettings.Problem) {
	dd.state = p.Text
	if p.Field != traysettings.FieldNone {
		dd.highlight(string(p.Field))
	}
	dd.update()
}

// highlight moves the focus to the control of a mapping.* / query field.
func (dd *discoveryDialog) highlight(field string) {
	dd.lastField = field
	if id, ok := discFieldControl[field]; ok {
		winapi.SetFocus(dd.ctl[id])
	}
}

func (dd *discoveryDialog) find() {
	req, pr := dd.w.ed.InteractiveRequest(model.CmdDiscover, dd.password())
	if pr != nil {
		dd.problem(pr)
		return
	}
	dd.run(req, i18n.KeyDiscRunning, dd.discovered)
}

func (dd *discoveryDialog) discovered(m model.IPCMessage) {
	dd.d.DiscoveryDone(m)
	dd.state, dd.result = dd.d.Status(), ""
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
		dd.state = dd.d.PointsStatus()
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
	req, pr := dd.w.ed.InteractiveRequest(model.CmdDiscoverByValue, dd.password())
	if pr != nil {
		dd.problem(pr)
		return
	}
	req.Query = q
	dd.run(req, i18n.KeyDiscRunning, dd.discovered)
}

// template puts manual SQL template i into the editor (manual mode).
func (dd *discoveryDialog) template(i int) {
	ts := model.SQLManualTemplates()
	if i < 0 || i >= len(ts) {
		return
	}
	dd.readMap()
	dd.d.Map.Mode, dd.d.Map.ManualSQL = model.MappingManual, ts[i].SQL
	dd.fill()
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

// apply puts the mapping (and «все планы обмена») into the edited profile
// as an unsaved edit; «Сохранить» sends save_profile with
// expectedRevision.
func (dd *discoveryDialog) apply() {
	ed := dd.w.ed
	if !ed.HasProfile() || ed.IsNew != dd.isNew || (!dd.isNew && ed.Base.ID != dd.profileID) {
		dd.problem(&traysettings.Problem{Text: dd.t(i18n.KeyDiscProfileChanged)})
		return
	}
	dd.readMap()
	m := dd.d.Map.Mapping()
	all := dd.checked(idDiscAllPlans)
	var others []model.Mapping
	if all {
		others = dd.d.PlanMappings(m)
	}
	ed.SetMapping(m, others, all)
	w := dd.w
	dd.close()
	w.status = w.t(i18n.KeyDiscApplied)
	w.fill()
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
	winapi.DestroyWindow(dd.hwnd)
	if w.disc == dd {
		w.disc = nil
	}
	if !w.cfg.hidden && winapi.IsWindow(w.hwnd) {
		winapi.SetForegroundWindow(w.hwnd)
	}
	if winapi.IsWindow(w.hwnd) {
		w.update()
	}
}
