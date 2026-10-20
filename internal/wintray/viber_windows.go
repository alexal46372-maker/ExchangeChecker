//go:build windows

package wintray

import (
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// Phase 10 D4 (CHK-TRAY-43, PLAN §12 p.123): the «Уведомления Viber»
// page (page 2) of the settings window. The button opening it is shown
// only with capability viber in «Экспертный»; the token box is
// write-only (the saved token is never read back, only hasToken).

const (
	idVbBase = 600 + iota
	idSetViber
	idSetNavProfiles // navigation: «Базы данных» (phase 11)
	idVbEnabled
	idVbToken
	idVbClearToken
	idVbTokenState
	idVbRecipients
	idVbFrom
	idVbTo
	idVbReminder
	idVbReminderHint
	idVbTestTo
	idVbTest
	idVbResult
	idVbStatus
	idVbDay1                // .. idVbDay1+6
	idVbEv1  = idVbDay1 + 7 // .. idVbEv1+3, model.ViberEventKinds order
)

// pageViber is the page index of the Viber page.
const pageViber = 2

var vbDayKeys = []string{i18n.KeyVbDay1, i18n.KeyVbDay2, i18n.KeyVbDay3, i18n.KeyVbDay4, i18n.KeyVbDay5, i18n.KeyVbDay6, i18n.KeyVbDay7}
var vbEventKeys = []string{i18n.KeyVbEvError, i18n.KeyVbEvWarning, i18n.KeyVbEvRecovered, i18n.KeyVbEvNoConn}

// viberTips: tooltips of the page (CHK-TRAY-15).
func viberTips() map[int32]string {
	m := map[int32]string{idSetViber: i18n.KeyTipVbPage, idVbEnabled: i18n.KeyTipVbEnabled, idVbToken: i18n.KeyTipVbToken,
		idVbClearToken: i18n.KeyTipVbClearToken, idVbRecipients: i18n.KeyTipVbRecipients, idVbFrom: i18n.KeyTipVbFrom,
		idVbTo: i18n.KeyTipVbTo, idVbReminder: i18n.KeyTipVbReminder, idVbTestTo: i18n.KeyTipVbTestTo, idVbTest: i18n.KeyTipVbTest}
	for i := int32(0); i < 7; i++ {
		m[idVbDay1+i] = i18n.KeyTipVbDay
	}
	for i := int32(0); i < 4; i++ {
		m[idVbEv1+i] = i18n.KeyTipVbEvent
	}
	return m
}

// viberOutputs are the read-only texts of the page.
var viberOutputs = map[int32]bool{idVbTokenState: true, idVbReminderHint: true, idVbResult: true, idVbStatus: true}

var viberFieldControl = map[traysettings.Field]int32{
	traysettings.FieldViberToken: idVbToken, traysettings.FieldViberRecipients: idVbRecipients, traysettings.FieldViberDays: idVbDay1,
	traysettings.FieldViberFrom: idVbFrom, traysettings.FieldViberTo: idVbTo, traysettings.FieldViberEvents: idVbEv1,
	traysettings.FieldViberReminder: idVbReminder, traysettings.FieldViberTest: idVbTest,
}

func init() {
	for f, id := range viberFieldControl {
		fieldControl[f] = id
	}
	for id, k := range viberTips() {
		settingsTips[id] = k
	}
	for id := range viberOutputs {
		settingsOutputs[id] = true
	}
}

// viberAvailable: the service offers capability viber.
func (w *settingsWindow) viberAvailable() bool {
	return w.cfg.session != nil && w.cfg.session.Status().Has(model.CapViber)
}

// buildViber creates page 2 (hidden) and the button opening it.
func (w *settingsWindow) buildViber() (page ui.Node) {
	// phase 11 (CHK-TRAY-45): laid out by the framework (layoutPages)
	edit := uint32(winapi.WS_TABSTOP | winapi.ES_AUTOHSCROLL)
	add := func(class, text string, ex, style uint32, id int32, kind ui.Kind, wd, hg int32) *ui.Ctl {
		c := uiCtl(w.add(class, text, ex, style, id, 0, 0, 0, 0), kind, text)
		c.W, c.Hgt = wd, hg
		return c
	}
	label := func(key string, id int32) *ui.Ctl {
		h := w.add("STATIC", w.t(key), 0, winapi.SS_LEFT, 0, 0, 0, 0, 0)
		w.labels[id] = h
		w.labelText[id] = w.t(key)
		return uiCtl(h, ui.Label, w.t(key))
	}
	check := func(key string, id int32) *ui.Ctl {
		return add("BUTTON", w.t(key), 0, winapi.WS_TABSTOP|winapi.BS_AUTOCHECKBOX, id, ui.Check, 0, 0)
	}
	w.cur = &w.pageCtl[pageViber]
	box := add("BUTTON", w.t(i18n.KeyVbGroup), 0, winapi.BS_GROUPBOX, 0, ui.Group, 0, 0)
	w.groups[i18n.KeyVbGroup] = box.H
	enabled := check(i18n.KeyVbEnabled, idVbEnabled)
	tokenL := label(i18n.KeyVbToken, idVbToken)
	token := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit|winapi.ES_PASSWORD, idVbToken, ui.Edit, 390, 0)
	tokenState := add("STATIC", "", 0, winapi.SS_LEFT, idVbTokenState, ui.Box, 200, 18)
	clear := check(i18n.KeyVbClearToken, idVbClearToken)
	recL := label(i18n.KeyVbRecipients, idVbRecipients)
	rec := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, winapi.WS_TABSTOP|winapi.ES_MULTILINE|winapi.ES_AUTOVSCROLL|winapi.WS_VSCROLL|winapi.ES_WANTRETURN,
		idVbRecipients, ui.Box, 525, 120)
	rec.Grow = true
	daysL := label(i18n.KeyVbDays, idVbDay1)
	var days []ui.Node
	for i := int32(0); i < 7; i++ {
		days = append(days, check(vbDayKeys[i], idVbDay1+i))
	}
	fromL := label(i18n.KeyVbFrom, idVbFrom)
	from := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idVbFrom, ui.Edit, 60, 0)
	toL := label(i18n.KeyVbTo, idVbTo)
	to := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idVbTo, ui.Edit, 60, 0)
	evL := label(i18n.KeyVbEvents, idVbEv1)
	var ev [4]ui.Node
	for i := int32(0); i < 4; i++ {
		ev[i] = check(vbEventKeys[i], idVbEv1+i)
	}
	remL := label(i18n.KeyVbReminder, idVbReminder)
	rem := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit|winapi.ES_NUMBER, idVbReminder, ui.Edit, 60, 0)
	remHint := add("STATIC", w.t(i18n.KeyVbReminderHint), 0, winapi.SS_LEFT, idVbReminderHint, ui.Label, 0, 0)
	testL := label(i18n.KeyVbTestTo, idVbTestTo)
	testTo := add("COMBOBOX", "", 0, winapi.WS_TABSTOP|winapi.WS_VSCROLL|winapi.CBS_DROPDOWNLIST, idVbTestTo, ui.Combo, 270, 0)
	test := add("BUTTON", w.t(i18n.KeyVbTest), 0, winapi.WS_TABSTOP, idVbTest, ui.Button, 110, 0)
	result := add("STATIC", "", 0, winapi.SS_LEFT, idVbResult, ui.Box, 300, 64)
	status := add("STATIC", "", 0, winapi.SS_LEFT, idVbStatus, ui.Box, 300, 54)
	result.Grow, status.Grow = true, true
	w.cur = nil
	page = &ui.GroupBox{Box: box, Child: &ui.Col{Items: []ui.Node{
		enabled,
		&ui.Grid{Gap: ui.P4, Rows: []ui.GridRow{{Label: tokenL, Field: token}, {Field: tokenState}, {Field: clear}}},
		recL, rec,
		&ui.Grid{Gap: ui.P4, Rows: []ui.GridRow{
			{Label: daysL, Field: &ui.Col{Gap: ui.P4, Items: []ui.Node{&ui.Row{Gap: ui.P4, Items: days[:4]}, &ui.Row{Gap: ui.P4, Items: days[4:]}}}},
			{Label: fromL, Field: &ui.Row{Items: []ui.Node{from, toL, to}}},
			{Label: evL, Field: &ui.Grid{Gap: ui.P4, Rows: []ui.GridRow{{Label: nil, Field: &ui.Row{Gap: ui.P16, Items: []ui.Node{ev[0], ev[1]}}},
				{Field: &ui.Row{Gap: ui.P16, Items: []ui.Node{ev[2], ev[3]}}}}}},
			{Label: remL, Field: &ui.Col{Gap: 2, Items: []ui.Node{rem, remHint}}},
			{Label: testL, Field: &ui.Row{Items: []ui.Node{testTo, test}}},
		}},
		result, status,
	}}}
	for _, h := range w.pageCtl[pageViber] {
		winapi.ShowWindow(h, winapi.SW_HIDE)
	}
	return page
}

// fillViber shows the editor's page state.
func (w *settingsWindow) fillViber() {
	f := w.ed.Viber
	w.setCheck(idVbEnabled, f.Enabled)
	w.setCheck(idVbClearToken, f.ClearToken)
	if f.Token == "" && winapi.GetWindowTextLength(w.ctl[idVbToken]) > 0 {
		winapi.SetWindowText(w.ctl[idVbToken], "") // the box is only ever cleared
	}
	for id, s := range map[int32]string{idVbRecipients: f.Recipients, idVbFrom: f.From, idVbTo: f.To, idVbReminder: f.Reminder} {
		if w.text(id) != s {
			winapi.SetWindowText(w.ctl[id], s)
		}
	}
	for i := int32(0); i < 7; i++ {
		w.setCheck(idVbDay1+i, f.Days[i])
	}
	for i := int32(0); i < 4; i++ {
		w.setCheck(idVbEv1+i, f.Events[i])
	}
	w.fillViberTestTo()
}

// fillViberTestTo: «Все получатели» and the saved recipients.
func (w *settingsWindow) fillViberTestTo() {
	cb := w.ctl[idVbTestTo]
	sel := int(winapi.SendMessage(cb, winapi.CB_GETCURSEL, 0, 0))
	winapi.SendMessage(cb, winapi.CB_RESETCONTENT, 0, 0)
	winapi.SendMessageString(cb, winapi.CB_ADDSTRING, 0, w.t(i18n.KeyVbTestAll))
	w.vbTestIDs = []string{""}
	if v := w.ed.Settings.Viber; v != nil {
		for _, r := range v.Recipients {
			t := r.ID
			if r.Name != "" {
				t = r.Name + " (" + r.ID + ")"
			}
			winapi.SendMessageString(cb, winapi.CB_ADDSTRING, 0, t)
			w.vbTestIDs = append(w.vbTestIDs, r.ID)
		}
	}
	if sel < 0 || sel >= len(w.vbTestIDs) {
		sel = 0
	}
	winapi.SendMessage(cb, winapi.CB_SETCURSEL, uintptr(sel), 0)
}

// readViber takes the page into the editor.
func (w *settingsWindow) readViber() {
	if !w.ed.CanEditViber() {
		return
	}
	f := &w.ed.Viber
	f.Enabled, f.ClearToken = w.check(idVbEnabled), w.check(idVbClearToken)
	f.Token = w.text(idVbToken)
	f.Recipients, f.From, f.To, f.Reminder = w.text(idVbRecipients), w.text(idVbFrom), w.text(idVbTo), w.text(idVbReminder)
	for i := int32(0); i < 7; i++ {
		f.Days[i] = w.check(idVbDay1 + i)
	}
	for i := int32(0); i < 4; i++ {
		f.Events[i] = w.check(idVbEv1 + i)
	}
}

// viberCommand handles WM_COMMAND of the page; false = not a page id.
func (w *settingsWindow) viberCommand(id int32, code uint32) bool {
	switch {
	case id == idSetViber:
		if w.page != pageViber {
			w.setPage(pageViber)
		}
	case id == idVbTest:
		w.testViber()
	case id == idVbToken || id == idVbRecipients || id == idVbFrom || id == idVbTo || id == idVbReminder:
		if code == winapi.EN_CHANGE && !w.loading {
			w.readViber()
			w.update()
		}
	case id == idVbEnabled || id == idVbClearToken || id >= idVbDay1 && id < idVbEv1+4:
		if code == winapi.BN_CLICKED && !w.loading {
			w.readViber()
			w.update()
		}
	case id == idVbTestTo:
	default:
		return false
	}
	return true
}

// updateViber enables the page and the button (capability viber,
// «Экспертный»).
func (w *settingsWindow) updateViber() {
	avail := w.viberAvailable() && w.uiMode != traysettings.UISimple
	if !avail && w.page == pageViber {
		w.setPage(0)
		return
	}
	if b := w.ctl[idSetViber]; b != 0 && (winapi.WindowStyle(b)&winapi.WS_VISIBLE != 0) != avail {
		cmd := int32(winapi.SW_HIDE)
		if avail {
			cmd = winapi.SW_SHOW
		}
		w.show(b, cmd == winapi.SW_SHOW)
		w.relayoutAll()
	}
	winapi.EnableWindow(w.ctl[idSetViber], avail && w.page != pageViber)
	can := w.ed.CanEditViber()
	for _, id := range []int32{idVbToken, idVbRecipients, idVbFrom, idVbTo, idVbReminder} {
		winapi.SendMessage(w.ctl[id], winapi.EM_SETREADONLY, boolPtr(!can), 0)
	}
	for _, id := range []int32{idVbEnabled, idVbClearToken} {
		winapi.EnableWindow(w.ctl[id], can)
	}
	if can && !(w.ed.Settings.Viber != nil && w.ed.Settings.Viber.HasToken) {
		winapi.EnableWindow(w.ctl[idVbClearToken], false)
	}
	for i := int32(0); i < 11; i++ {
		winapi.EnableWindow(w.ctl[idVbDay1+i], can)
	}
	admin := w.cfg.mode == traysettings.Admin
	winapi.EnableWindow(w.ctl[idVbTestTo], admin && w.ed.HasViber)
	winapi.EnableWindow(w.ctl[idVbTest], admin && w.ed.HasViber && !w.busy && w.vbTesting == nil)
	if t := w.ed.TokenStateText(); w.text(idVbTokenState) != t {
		winapi.SetWindowText(w.ctl[idVbTokenState], t)
	}
	if t := w.viberStatusText(); w.text(idVbStatus) != t {
		winapi.SetWindowText(w.ctl[idVbStatus], t)
	}
}

// viberStatusText: delivery state of the running service (get_status,
// else get_config).
func (w *settingsWindow) viberStatusText() string {
	v := w.ed.Settings.Viber
	if st := w.cfg.session.Status(); st.Snapshot != nil && st.Snapshot.Settings != nil && st.Snapshot.Settings.Viber != nil &&
		st.Snapshot.Settings.Viber.Status != nil {
		v = st.Snapshot.Settings.Viber
	}
	return traysettings.ViberStatusText(v, w.cfg.lang)
}

// saveViber sends save_settings of the page.
func (w *settingsWindow) saveViber() {
	if w.busy {
		return
	}
	w.readViber()
	req, pr := w.ed.ViberRequest()
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
			w.status = w.ed.ViberDone(m, req)
			w.fill()
		}
	})
}

// testViber sends viber_test to the chosen recipient with the typed
// token (once, not saved).
func (w *settingsWindow) testViber() {
	if w.busy || w.vbTesting != nil {
		return
	}
	w.readViber()
	id := ""
	if i := int(winapi.SendMessage(w.ctl[idVbTestTo], winapi.CB_GETCURSEL, 0, 0)); i > 0 && i < len(w.vbTestIDs) {
		id = w.vbTestIDs[i]
	}
	req, pr := w.ed.ViberTestRequest(id)
	if pr != nil {
		w.problem(pr)
		return
	}
	w.vbTesting = &req
	winapi.SetWindowText(w.ctl[idVbResult], w.t(i18n.KeyVbTesting))
	w.call(req, func(m model.IPCMessage, err error) {
		w.vbTesting = nil
		var t string
		switch {
		case err != nil:
			t = w.ed.ErrorProblem(err, traysettings.CallTimeout).Text
		case m.Type == model.MsgError:
			pr := w.ed.Reply(m)
			t = pr.Text
			if pr.Field != traysettings.FieldNone {
				w.focus(pr.Field)
			}
		default:
			t = w.ed.ViberTestText(m.ViberTest)
		}
		winapi.SetWindowText(w.ctl[idVbResult], strings.TrimSpace(t))
		w.update()
	})
}
