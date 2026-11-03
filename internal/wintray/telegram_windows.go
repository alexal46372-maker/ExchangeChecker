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

// Phase 13 item 10 (CHK-TRAY-94): the «Telegram» tab (page 4) of the
// settings window, like «Viber»: shown only with capability telegram in
// «Экспертный»; the bot token box is write-only (only hasToken is read).

const (
	idTgBase = 900 + iota
	idTgEnabled
	idTgToken
	idTgClearToken
	idTgTokenState
	idTgChat
	idTgChatHint
	idTgFrom
	idTgTo
	idTgTest
	idTgResult
	idTgStatus
	idTgDay1                // .. idTgDay1+6
	idTgEv1  = idTgDay1 + 7 // .. idTgEv1+3, model.ViberEventKinds order
)

// pageTelegram is the page index of the Telegram tab.
const pageTelegram = 4

func telegramTips() map[int32]string {
	m := map[int32]string{idTgEnabled: i18n.KeyTipTgEnabled, idTgToken: i18n.KeyTipTgToken,
		idTgClearToken: i18n.KeyTipTgClearToken, idTgChat: i18n.KeyTipTgChat, idTgFrom: i18n.KeyTipVbFrom,
		idTgTo: i18n.KeyTipVbTo, idTgTest: i18n.KeyTipTgTest}
	for i := int32(0); i < 7; i++ {
		m[idTgDay1+i] = i18n.KeyTipVbDay
	}
	for i := int32(0); i < 4; i++ {
		m[idTgEv1+i] = i18n.KeyTipVbEvent
	}
	return m
}

var telegramOutputs = map[int32]bool{idTgTokenState: true, idTgChatHint: true, idTgResult: true, idTgStatus: true}

var telegramFieldControl = map[traysettings.Field]int32{
	traysettings.FieldTgToken: idTgToken, traysettings.FieldTgChat: idTgChat, traysettings.FieldTgDays: idTgDay1,
	traysettings.FieldTgFrom: idTgFrom, traysettings.FieldTgTo: idTgTo, traysettings.FieldTgEvents: idTgEv1,
	traysettings.FieldTgTest: idTgTest,
}

func init() {
	for f, id := range telegramFieldControl {
		fieldControl[f] = id
	}
	for id, k := range telegramTips() {
		settingsTips[id] = k
	}
	for id := range telegramOutputs {
		settingsOutputs[id] = true
	}
}

// telegramAvailable: the service offers capability telegram.
func (w *settingsWindow) telegramAvailable() bool {
	return w.cfg.session != nil && w.cfg.session.Status().Has(model.CapTelegram)
}

// buildTelegram creates page 4 (hidden).
func (w *settingsWindow) buildTelegram() (page ui.Node) {
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
	w.cur = &w.pageCtl[pageTelegram]
	box := add("BUTTON", w.t(i18n.KeyTgGroup), 0, winapi.BS_GROUPBOX, 0, ui.Group, 0, 0)
	w.groups[i18n.KeyTgGroup] = box.H
	enabled := check(i18n.KeyTgEnabled, idTgEnabled)
	tokenL := label(i18n.KeyTgToken, idTgToken)
	token := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit|winapi.ES_PASSWORD, idTgToken, ui.Edit, 390, 0)
	tokenState := add("STATIC", "", 0, winapi.SS_LEFT, idTgTokenState, ui.Box, 200, 18)
	clear := check(i18n.KeyTgClearToken, idTgClearToken)
	chatL := label(i18n.KeyTgChat, idTgChat)
	chat := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idTgChat, ui.Edit, 270, 0)
	chatHint := add("STATIC", w.t(i18n.KeyTgChatHint), 0, winapi.SS_LEFT, idTgChatHint, ui.Label, 0, 0)
	daysL := label(i18n.KeyVbDays, idTgDay1)
	var days []ui.Node
	for i := int32(0); i < 7; i++ {
		days = append(days, check(vbDayKeys[i], idTgDay1+i))
	}
	fromL := label(i18n.KeyVbFrom, idTgFrom)
	from := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idTgFrom, ui.Edit, 60, 0)
	toL := label(i18n.KeyVbTo, idTgTo)
	to := add("EDIT", "", winapi.WS_EX_CLIENTEDGE, edit, idTgTo, ui.Edit, 60, 0)
	evL := label(i18n.KeyVbEvents, idTgEv1)
	var ev [4]ui.Node
	for i := int32(0); i < 4; i++ {
		ev[i] = check(vbEventKeys[i], idTgEv1+i)
	}
	test := add("BUTTON", w.t(i18n.KeyTgTest), 0, winapi.WS_TABSTOP, idTgTest, ui.Button, 110, 0)
	result := add("STATIC", "", 0, winapi.SS_LEFT, idTgResult, ui.Box, 300, 40)
	status := add("STATIC", "", 0, winapi.SS_LEFT, idTgStatus, ui.Box, 300, 54)
	result.Grow, status.Grow = true, true
	w.wrap = append(w.wrap, result, status)
	w.cur = nil
	g := &ui.Grid{Gap: ui.P4, Rows: []ui.GridRow{
		{Label: tokenL, Field: token}, {Field: tokenState}, {Field: clear},
		{Label: chatL, Field: &ui.Col{Gap: 2, Items: []ui.Node{chat, chatHint}}},
		{Label: daysL, Field: &ui.Col{Gap: ui.P4, Items: []ui.Node{&ui.Row{Gap: ui.P4, Items: days[:4]}, &ui.Row{Gap: ui.P4, Items: days[4:]}}}},
		{Label: fromL, Field: &ui.Row{Items: []ui.Node{from, toL, to}}},
		{Label: evL, Field: &ui.Grid{Gap: ui.P4, Rows: []ui.GridRow{{Field: &ui.Row{Gap: ui.P8, Items: []ui.Node{ev[0], ev[1]}}},
			{Field: &ui.Row{Gap: ui.P8, Items: []ui.Node{ev[2], ev[3]}}}}}},
		{Field: test},
	}}
	page = &ui.GroupBox{Box: box, Child: &ui.Col{Items: []ui.Node{enabled, g, result, status}}}
	for _, h := range w.pageCtl[pageTelegram] {
		winapi.ShowWindow(h, winapi.SW_HIDE)
	}
	return page
}

// fillTelegram shows the editor's page state.
func (w *settingsWindow) fillTelegram() {
	f := w.ed.Telegram
	w.setCheck(idTgEnabled, f.Enabled)
	w.setCheck(idTgClearToken, f.ClearToken)
	if f.Token == "" && winapi.GetWindowTextLength(w.ctl[idTgToken]) > 0 {
		winapi.SetWindowText(w.ctl[idTgToken], "") // only ever cleared
	}
	for id, s := range map[int32]string{idTgChat: f.ChatID, idTgFrom: f.From, idTgTo: f.To} {
		if w.text(id) != s {
			winapi.SetWindowText(w.ctl[id], s)
		}
	}
	for i := int32(0); i < 7; i++ {
		w.setCheck(idTgDay1+i, f.Days[i])
	}
	for i := int32(0); i < 4; i++ {
		w.setCheck(idTgEv1+i, f.Events[i])
	}
}

// readTelegram takes the page into the editor.
func (w *settingsWindow) readTelegram() {
	if !w.ed.CanEditTelegram() {
		return
	}
	f := &w.ed.Telegram
	f.Enabled, f.ClearToken = w.check(idTgEnabled), w.check(idTgClearToken)
	f.Token = w.text(idTgToken)
	f.ChatID, f.From, f.To = w.text(idTgChat), w.text(idTgFrom), w.text(idTgTo)
	for i := int32(0); i < 7; i++ {
		f.Days[i] = w.check(idTgDay1 + i)
	}
	for i := int32(0); i < 4; i++ {
		f.Events[i] = w.check(idTgEv1 + i)
	}
}

// telegramCommand handles WM_COMMAND of the page; false = not a page id.
func (w *settingsWindow) telegramCommand(id int32, code uint32) bool {
	switch {
	case id == idTgTest:
		w.testTelegram()
	case id == idTgToken || id == idTgChat || id == idTgFrom || id == idTgTo:
		if code == winapi.EN_CHANGE && !w.loading {
			w.readTelegram()
			w.update()
		}
	case id == idTgEnabled || id == idTgClearToken || id >= idTgDay1 && id < idTgEv1+4:
		if code == winapi.BN_CLICKED && !w.loading {
			w.readTelegram()
			w.update()
		}
	default:
		return false
	}
	return true
}

// updateTelegram enables the page controls.
func (w *settingsWindow) updateTelegram() {
	if !(w.telegramAvailable() && w.uiMode != traysettings.UISimple) && w.page == pageTelegram {
		w.setPage(0)
		return
	}
	can := w.ed.CanEditTelegram()
	for _, id := range []int32{idTgToken, idTgChat, idTgFrom, idTgTo} {
		winapi.SendMessage(w.ctl[id], winapi.EM_SETREADONLY, boolPtr(!can), 0)
	}
	for _, id := range []int32{idTgEnabled, idTgClearToken} {
		winapi.EnableWindow(w.ctl[id], can)
	}
	if can && !(w.ed.Settings.Telegram != nil && w.ed.Settings.Telegram.HasToken) {
		winapi.EnableWindow(w.ctl[idTgClearToken], false)
	}
	for i := int32(0); i < 11; i++ {
		winapi.EnableWindow(w.ctl[idTgDay1+i], can)
	}
	admin := w.cfg.mode == traysettings.Admin
	winapi.EnableWindow(w.ctl[idTgTest], admin && w.ed.HasTelegram && !w.busy && w.tgTesting == nil)
	if t := w.ed.TelegramTokenStateText(); w.text(idTgTokenState) != t {
		winapi.SetWindowText(w.ctl[idTgTokenState], t)
	}
	if t := w.telegramStatusText(); w.text(idTgStatus) != t {
		winapi.SetWindowText(w.ctl[idTgStatus], t)
		w.relayoutAll()
	}
}

// telegramStatusText: delivery state of the running service (get_status,
// else get_config).
func (w *settingsWindow) telegramStatusText() string {
	t := w.ed.Settings.Telegram
	if st := w.cfg.session.Status(); st.Snapshot != nil && st.Snapshot.Settings != nil && st.Snapshot.Settings.Telegram != nil &&
		st.Snapshot.Settings.Telegram.Status != nil {
		t = st.Snapshot.Settings.Telegram
	}
	return traysettings.TelegramStatusText(t)
}

// saveTelegram sends save_settings of the page.
func (w *settingsWindow) saveTelegram() {
	if w.busy {
		return
	}
	w.readTelegram()
	req, pr := w.ed.TelegramRequest()
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
			w.status = w.ed.TelegramDone(m, req)
			w.fill()
		}
	})
}

// testTelegram sends telegram_test with the typed token / chat id (once,
// not saved); the result is the telegram_test_result push of the request.
func (w *settingsWindow) testTelegram() {
	if w.busy || w.tgTesting != nil {
		return
	}
	w.readTelegram()
	req, pr := w.ed.TelegramTestRequest()
	if pr != nil {
		w.problem(pr)
		return
	}
	w.tgTesting = &req
	winapi.SetWindowText(w.ctl[idTgResult], w.t(i18n.KeyTgTesting))
	w.relayoutAll()
	w.call(req, func(m model.IPCMessage, err error) {
		w.tgTesting = nil
		var t string
		if err != nil {
			t = w.ed.ErrorProblem(err, traysettings.CallTimeout).Text
		} else {
			t = w.ed.TelegramTestText(m)
			if m.Type == model.MsgError && m.Code != model.ErrCodeRateLimited {
				if pr := w.ed.Reply(m); pr.Field != traysettings.FieldNone {
					w.focus(pr.Field)
				}
			}
		}
		winapi.SetWindowText(w.ctl[idTgResult], strings.TrimSpace(t))
		w.relayoutAll()
		w.update()
	})
}
