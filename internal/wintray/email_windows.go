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

// Phase 13a (CHK-TRAY-64): the «Уведомления e-mail» page (page 3) of the
// settings window. Shown only with capability email in «Экспертный»;
// the SMTP password box is write-only (hasPassword). tlsMode none hides
// / disables the password (no AUTH); starttls/tls require it when enabled.

const (
	idEmBase = 700 + iota
	idSetEmail
	idEmEnabled
	idEmHost
	idEmPort
	idEmTLSMode
	idEmFrom
	idEmPassword
	idEmClearPassword
	idEmPasswordState
	idEmRecipients
	idEmFromTime
	idEmToTime
	idEmTestTo
	idEmTest
	idEmResult
	idEmStatus
	idEmDay1                   // .. idEmDay1+6
	idEmEv1  = idEmDay1 + 7    // .. idEmEv1+3
)

// pageEmail is the page index of the e-mail page.
const pageEmail = 3

var emDayKeys = []string{i18n.KeyVbDay1, i18n.KeyVbDay2, i18n.KeyVbDay3, i18n.KeyVbDay4, i18n.KeyVbDay5, i18n.KeyVbDay6, i18n.KeyVbDay7}
var emEventKeys = []string{i18n.KeyVbEvError, i18n.KeyVbEvWarning, i18n.KeyVbEvRecovered, i18n.KeyVbEvNoConn}
var emTLSKeys = []string{i18n.KeyEmTLSNone, i18n.KeyEmTLSStartTLS, i18n.KeyEmTLSImplicit}

func emailTips() map[int32]string {
	m := map[int32]string{idSetEmail: i18n.KeyTipEmPage, idEmEnabled: i18n.KeyTipEmEnabled, idEmHost: i18n.KeyTipEmHost,
		idEmPort: i18n.KeyTipEmPort, idEmTLSMode: i18n.KeyTipEmTLSMode, idEmFrom: i18n.KeyTipEmFromAddr,
		idEmPassword: i18n.KeyTipEmPassword, idEmClearPassword: i18n.KeyTipEmClearPassword,
		idEmRecipients: i18n.KeyTipEmRecipients, idEmFromTime: i18n.KeyTipEmFrom, idEmToTime: i18n.KeyTipEmTo,
		idEmTestTo: i18n.KeyTipEmTestTo, idEmTest: i18n.KeyTipEmTest}
	for i := int32(0); i < 7; i++ {
		m[idEmDay1+i] = i18n.KeyTipEmDay
	}
	for i := int32(0); i < 4; i++ {
		m[idEmEv1+i] = i18n.KeyTipEmEvent
	}
	return m
}

var emailOutputs = map[int32]bool{idEmPasswordState: true, idEmResult: true, idEmStatus: true}

var emailFieldControl = map[traysettings.Field]int32{
	traysettings.FieldEmailPassword: idEmPassword, traysettings.FieldEmailHost: idEmHost,
	traysettings.FieldEmailPort: idEmPort, traysettings.FieldEmailTLSMode: idEmTLSMode,
	traysettings.FieldEmailFrom: idEmFrom, traysettings.FieldEmailRecipients: idEmRecipients,
	traysettings.FieldEmailDays: idEmDay1, traysettings.FieldEmailSchedFrom: idEmFromTime,
	traysettings.FieldEmailSchedTo: idEmToTime, traysettings.FieldEmailEvents: idEmEv1,
	traysettings.FieldEmailTest: idEmTest,
}

func init() {
	for f, id := range emailFieldControl {
		fieldControl[f] = id
	}
	for id, k := range emailTips() {
		settingsTips[id] = k
	}
	for id := range emailOutputs {
		settingsOutputs[id] = true
	}
}

func (w *settingsWindow) emailAvailable() bool {
	return w.cfg.session != nil && w.cfg.session.Status().Has(model.CapEmail)
}

func (w *settingsWindow) buildEmail() (page ui.Node) {
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
	w.cur = &w.pageCtl[pageEmail]
	box := add("BUTTON", w.t(i18n.KeyEmGroup), 0, winapi.BS_GROUPBOX, 0, ui.Group, 0, 0)
	w.groups[i18n.KeyEmGroup] = box.H
	enabled := check(i18n.KeyEmEnabled, idEmEnabled)
	hostL := label(i18n.KeyEmHost, idEmHost)
	host := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idEmHost, ui.Edit, 280, 0)
	portL := label(i18n.KeyEmPort, idEmPort)
	port := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit|winapi.ES_NUMBER, idEmPort, ui.Edit, 60, 0)
	tlsL := label(i18n.KeyEmTLSMode, idEmTLSMode)
	tls := add("COMBOBOX", "", 0, winapi.WS_TABSTOP|winapi.WS_VSCROLL|winapi.CBS_DROPDOWNLIST, idEmTLSMode, ui.Combo, 160, 0)
	fromL := label(i18n.KeyEmFromAddr, idEmFrom)
	from := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idEmFrom, ui.Edit, 280, 0)
	pwL := label(i18n.KeyEmPassword, idEmPassword)
	pw := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit|winapi.ES_PASSWORD, idEmPassword, ui.Edit, 280, 0)
	pwState := add("STATIC", "", 0, winapi.SS_LEFT, idEmPasswordState, ui.Box, 220, 18)
	clear := check(i18n.KeyEmClearPassword, idEmClearPassword)
	recL := label(i18n.KeyEmRecipients, idEmRecipients)
	rec := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, winapi.WS_TABSTOP|winapi.ES_MULTILINE|winapi.ES_AUTOVSCROLL|winapi.WS_VSCROLL|winapi.ES_WANTRETURN,
		idEmRecipients, ui.Box, 525, 100)
	rec.Grow = true
	daysL := label(i18n.KeyEmDays, idEmDay1)
	var days []ui.Node
	for i := int32(0); i < 7; i++ {
		days = append(days, check(emDayKeys[i], idEmDay1+i))
	}
	fromTL := label(i18n.KeyEmFrom, idEmFromTime)
	fromT := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idEmFromTime, ui.Edit, 60, 0)
	toTL := label(i18n.KeyEmTo, idEmToTime)
	toT := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idEmToTime, ui.Edit, 60, 0)
	evL := label(i18n.KeyEmEvents, idEmEv1)
	var ev [4]ui.Node
	for i := int32(0); i < 4; i++ {
		ev[i] = check(emEventKeys[i], idEmEv1+i)
	}
	testL := label(i18n.KeyEmTestTo, idEmTestTo)
	testTo := add("COMBOBOX", "", 0, winapi.WS_TABSTOP|winapi.WS_VSCROLL|winapi.CBS_DROPDOWNLIST, idEmTestTo, ui.Combo, 270, 0)
	test := add("BUTTON", w.t(i18n.KeyEmTest), 0, winapi.WS_TABSTOP, idEmTest, ui.Button, 110, 0)
	result := add("STATIC", "", 0, winapi.SS_LEFT, idEmResult, ui.Box, 300, 64)
	status := add("STATIC", "", 0, winapi.SS_LEFT, idEmStatus, ui.Box, 300, 54)
	result.Grow, status.Grow = true, true
	w.wrap = append(w.wrap, result, status)
	w.cur = nil
	smtpG := &ui.Grid{Gap: ui.P4, Rows: []ui.GridRow{
		{Label: hostL, Field: &ui.Row{Items: []ui.Node{host, portL, port}}},
		{Label: tlsL, Field: tls},
		{Label: fromL, Field: from},
		{Label: pwL, Field: pw},
		{Field: pwState},
		{Field: clear},
	}}
	mainG := &ui.Grid{Gap: ui.P4, Rows: []ui.GridRow{
		{Label: daysL, Field: &ui.Col{Gap: ui.P4, Items: []ui.Node{&ui.Row{Gap: ui.P4, Items: days[:4]}, &ui.Row{Gap: ui.P4, Items: days[4:]}}}},
		{Label: fromTL, Field: &ui.Row{Items: []ui.Node{fromT, toTL, toT}}},
		{Label: evL, Field: &ui.Grid{Gap: ui.P4, Rows: []ui.GridRow{{Label: nil, Field: &ui.Row{Gap: ui.P8, Items: []ui.Node{ev[0], ev[1]}}},
			{Field: &ui.Row{Gap: ui.P8, Items: []ui.Node{ev[2], ev[3]}}}}}},
		{Label: testL, Field: &ui.Row{Items: []ui.Node{testTo, test}}},
	}}
	ui.Share(smtpG, mainG)
	page = &ui.GroupBox{Box: box, Child: &ui.Col{Items: []ui.Node{enabled, smtpG, recL, rec, mainG, result, status}}}
	for _, h := range w.pageCtl[pageEmail] {
		winapi.ShowWindow(h, winapi.SW_HIDE)
	}
	return page
}

func (w *settingsWindow) fillEmail() {
	f := w.ed.Email
	w.setCheck(idEmEnabled, f.Enabled)
	w.setCheck(idEmClearPassword, f.ClearPassword)
	if f.Password == "" && winapi.GetWindowTextLength(w.ctl[idEmPassword]) > 0 {
		winapi.SetWindowText(w.ctl[idEmPassword], "")
	}
	for id, s := range map[int32]string{
		idEmHost: f.Host, idEmPort: f.Port, idEmFrom: f.From, idEmRecipients: f.Recipients,
		idEmFromTime: f.FromTime, idEmToTime: f.ToTime,
	} {
		if w.text(id) != s {
			winapi.SetWindowText(w.ctl[id], s)
		}
	}
	winapi.SendMessage(w.ctl[idEmTLSMode], winapi.CB_SETCURSEL, uintptr(traysettings.EmailTLSModeIndex(f.TLSMode)), 0)
	for i := int32(0); i < 7; i++ {
		w.setCheck(idEmDay1+i, f.Days[i])
	}
	for i := int32(0); i < 4; i++ {
		w.setCheck(idEmEv1+i, f.Events[i])
	}
	w.fillEmailTestTo()
}

func (w *settingsWindow) fillEmailTestTo() {
	cb := w.ctl[idEmTestTo]
	sel := int(winapi.SendMessage(cb, winapi.CB_GETCURSEL, 0, 0))
	winapi.SendMessage(cb, winapi.CB_RESETCONTENT, 0, 0)
	winapi.SendMessageString(cb, winapi.CB_ADDSTRING, 0, w.t(i18n.KeyEmTestAll))
	w.emTestIDs = []string{""}
	if e := w.ed.Settings.Email; e != nil {
		for _, r := range e.Recipients {
			t := r.Addr
			if r.Name != "" {
				t = r.Name + " (" + r.ID + ")"
			} else if r.ID != "" {
				t = r.ID + " <" + r.Addr + ">"
			}
			winapi.SendMessageString(cb, winapi.CB_ADDSTRING, 0, t)
			w.emTestIDs = append(w.emTestIDs, r.ID)
		}
	}
	if sel < 0 || sel >= len(w.emTestIDs) {
		sel = 0
	}
	winapi.SendMessage(cb, winapi.CB_SETCURSEL, uintptr(sel), 0)
}

func (w *settingsWindow) readEmail() {
	if !w.ed.CanEditEmail() {
		return
	}
	f := &w.ed.Email
	f.Enabled, f.ClearPassword = w.check(idEmEnabled), w.check(idEmClearPassword)
	f.Password = w.text(idEmPassword)
	f.Host, f.Port, f.From = w.text(idEmHost), w.text(idEmPort), w.text(idEmFrom)
	f.Recipients, f.FromTime, f.ToTime = w.text(idEmRecipients), w.text(idEmFromTime), w.text(idEmToTime)
	if i := int(winapi.SendMessage(w.ctl[idEmTLSMode], winapi.CB_GETCURSEL, 0, 0)); i >= 0 && i < len(traysettings.EmailTLSModes()) {
		f.TLSMode = traysettings.EmailTLSModes()[i]
	}
	for i := int32(0); i < 7; i++ {
		f.Days[i] = w.check(idEmDay1 + i)
	}
	for i := int32(0); i < 4; i++ {
		f.Events[i] = w.check(idEmEv1 + i)
	}
}

func (w *settingsWindow) emailCommand(id int32, code uint32) bool {
	switch {
	case id == idSetEmail:
		if w.page != pageEmail {
			w.setPage(pageEmail)
		}
	case id == idEmTest:
		w.testEmail()
	case id == idEmHost || id == idEmPort || id == idEmFrom || id == idEmPassword || id == idEmRecipients || id == idEmFromTime || id == idEmToTime:
		if code == winapi.EN_CHANGE && !w.loading {
			w.readEmail()
			w.update()
		}
	case id == idEmEnabled || id == idEmClearPassword || id >= idEmDay1 && id < idEmEv1+4:
		if code == winapi.BN_CLICKED && !w.loading {
			w.readEmail()
			w.update()
		}
	case id == idEmTLSMode:
		if code == winapi.CBN_SELCHANGE && !w.loading {
			w.readEmail()
			w.update()
		}
	case id == idEmTestTo:
	default:
		return false
	}
	return true
}

func (w *settingsWindow) updateEmail() {
	avail := w.emailAvailable() && w.uiMode != traysettings.UISimple
	if !avail && w.page == pageEmail {
		w.setPage(0)
		return
	}
	before := len(w.tabPages)
	w.rebuildTabs()
	if len(w.tabPages) != before {
		w.relayoutAll()
	} else {
		w.markNav()
	}
	can := w.ed.CanEditEmail()
	needsAuth := model.NeedsSMTPAuth(w.ed.Email.TLSMode)
	for _, id := range []int32{idEmHost, idEmPort, idEmFrom, idEmRecipients, idEmFromTime, idEmToTime} {
		winapi.SendMessage(w.ctl[id], winapi.EM_SETREADONLY, boolPtr(!can), 0)
	}
	winapi.SendMessage(w.ctl[idEmPassword], winapi.EM_SETREADONLY, boolPtr(!can || !needsAuth), 0)
	winapi.EnableWindow(w.ctl[idEmEnabled], can)
	winapi.EnableWindow(w.ctl[idEmTLSMode], can)
	winapi.EnableWindow(w.ctl[idEmPassword], can && needsAuth)
	winapi.EnableWindow(w.ctl[idEmClearPassword], can && needsAuth && w.ed.Settings.Email != nil && w.ed.Settings.Email.HasPassword && !w.ed.Email.ClearPassword)
	if can && needsAuth && !(w.ed.Settings.Email != nil && w.ed.Settings.Email.HasPassword) {
		winapi.EnableWindow(w.ctl[idEmClearPassword], false)
	}
	if !needsAuth {
		winapi.EnableWindow(w.ctl[idEmClearPassword], false)
	}
	for i := int32(0); i < 11; i++ {
		winapi.EnableWindow(w.ctl[idEmDay1+i], can)
	}
	admin := w.cfg.mode == traysettings.Admin
	winapi.EnableWindow(w.ctl[idEmTestTo], admin && w.ed.HasEmail)
	winapi.EnableWindow(w.ctl[idEmTest], admin && w.ed.HasEmail && !w.busy && w.emTesting == nil)
	if t := w.ed.PasswordStateText(); w.text(idEmPasswordState) != t {
		winapi.SetWindowText(w.ctl[idEmPasswordState], t)
	}
	if t := w.emailStatusText(); w.text(idEmStatus) != t {
		winapi.SetWindowText(w.ctl[idEmStatus], t)
		w.relayoutAll()
	}
}

func (w *settingsWindow) emailStatusText() string {
	e := w.ed.Settings.Email
	if st := w.cfg.session.Status(); st.Snapshot != nil && st.Snapshot.Settings != nil && st.Snapshot.Settings.Email != nil &&
		st.Snapshot.Settings.Email.Status != nil {
		e = st.Snapshot.Settings.Email
	}
	return traysettings.EmailStatusText(e, w.cfg.lang)
}

func (w *settingsWindow) saveEmail() {
	if w.busy {
		return
	}
	w.readEmail()
	req, pr := w.ed.EmailRequest()
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
			w.status = w.ed.EmailDone(m, req)
			w.fill()
		}
	})
}

func (w *settingsWindow) testEmail() {
	if w.busy || w.emTesting != nil {
		return
	}
	w.readEmail()
	id := ""
	if i := int(winapi.SendMessage(w.ctl[idEmTestTo], winapi.CB_GETCURSEL, 0, 0)); i > 0 && i < len(w.emTestIDs) {
		id = w.emTestIDs[i]
	}
	req, pr := w.ed.EmailTestRequest(id, "")
	if pr != nil {
		w.problem(pr)
		return
	}
	w.emTesting = &req
	winapi.SetWindowText(w.ctl[idEmResult], w.t(i18n.KeyEmTesting))
	w.relayoutAll()
	w.call(req, func(m model.IPCMessage, err error) {
		w.emTesting = nil
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
			t = w.ed.EmailTestText(m.EmailTest)
		}
		winapi.SetWindowText(w.ctl[idEmResult], strings.TrimSpace(t))
		w.relayoutAll()
		w.update()
	})
}

// fillEmailTLSCombo fills the tlsMode dropdown once (RU/EN labels).
func (w *settingsWindow) fillEmailTLSCombo() {
	cb := w.ctl[idEmTLSMode]
	if cb == 0 || winapi.SendMessage(cb, winapi.CB_GETCOUNT, 0, 0) > 0 {
		return
	}
	for _, k := range emTLSKeys {
		winapi.SendMessageString(cb, winapi.CB_ADDSTRING, 0, w.t(k))
	}
}
