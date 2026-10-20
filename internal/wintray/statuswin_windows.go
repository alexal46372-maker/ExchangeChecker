//go:build windows

package wintray

import (
	"fmt"
	"os"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// «Состояние» window control IDs (phase 9a, CHK-TRAY-19/20/21).
const (
	statusClass    = "ExchangeCheckerTrayStatus"
	idStList       = 400
	idStInfo       = 401
	idStCheckNow   = 410
	idStExportCSV  = 411
	idStExportHTML = 412
	idStHistory    = 413
	idStClose      = winapi.IDCANCEL
)

// exportKind of the save dialog.
type exportKind int

const (
	exportCSV exportKind = iota
	exportHTML
)

// statusWindow lists every database and point of the last snapshot
// (get_status / status pushes) with colour, time and «N мин назад».
type statusWindow struct {
	hwnd, list, info        uintptr
	check, csv, html, close uintptr
	history                 uintptr
	cards                   *cardPanel // phase 11 database cards (CHK-TRAY-48)
	content, bar            ui.Node    // phase 11: relayout when the info line shows / hides
	infoCtl                 *ui.Ctl
	canHistory              func() bool
	onHistory               func(trayui.StatusRow)
	tips                    *tooltips
	lang                    string
	rows                    []trayui.StatusRow
	now                     func() time.Time
	onCheck                 func()
	savePath                func(k exportKind) (string, bool) // test hook
	lastSaved               string
}

var statusColWidths = []int32{130, 140, 110, 95, 110, 80, 95, 95, 160}

var rowBk = map[trayui.Color]uint32{ // pale COLORREF (0x00BBGGRR)
	trayui.ColorGreen:  0xDFF3DF,
	trayui.ColorYellow: 0xC4F3FF,
	trayui.ColorRed:    0xD0D0F8,
	trayui.ColorGrey:   0xE6E6E6,
}

func newStatusWindow(lang string, show bool, onCheck func()) (*statusWindow, error) {
	const w = 1060
	// phase 11 (CHK-TRAY-48): one row of cards above the table (no empty
	// second row with few databases; the table has them all)
	cardRows := int32(1)
	h := 440 + cardPanelHeight(cardRows) + 8
	x := (winapi.GetSystemMetrics(winapi.SM_CXSCREEN) - w) / 2
	y := (winapi.GetSystemMetrics(winapi.SM_CYSCREEN) - h) / 3
	sw := &statusWindow{lang: lang, now: time.Now, onCheck: onCheck}
	sw.savePath = sw.dialogPath
	handler := func(hw uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
		switch msg {
		case winapi.WM_COMMAND:
			switch wp & 0xFFFF {
			case idStCheckNow:
				if sw.onCheck != nil {
					sw.onCheck()
				}
				return 0, true
			case idStHistory:
				sw.openHistory()
				return 0, true
			case idStExportCSV:
				sw.export(exportCSV)
				return 0, true
			case idStExportHTML:
				sw.export(exportHTML)
				return 0, true
			case idStClose:
				winapi.ShowWindow(hw, winapi.SW_HIDE)
				return 0, true
			}
		case winapi.WM_NOTIFY:
			if r, ok := sw.onNotify(lp); ok {
				return r, true
			}
		case winapi.WM_CLOSE:
			winapi.ShowWindow(hw, winapi.SW_HIDE)
			return 0, true
		}
		return 0, false
	}
	hwnd, err := createWindow(statusClass, i18n.T(i18n.KeyStTitle, lang)+" — "+i18n.T(i18n.KeyAppName, lang), 0,
		winapi.WS_OVERLAPPED|winapi.WS_CAPTION|winapi.WS_SYSMENU|winapi.WS_MINIMIZEBOX, x, y, w, h, 0, handler)
	if err != nil {
		return nil, err
	}
	sw.hwnd = hwnd
	sw.tips = newTooltips(hwnd)
	cp, err := newCardPanel(hwnd, sw.tips, lang, cardRows)
	if err != nil {
		destroyWindow(hwnd)
		return nil, err
	}
	sw.cards = cp
	sw.list = child(hwnd, winapi.WC_LISTVIEW, "", winapi.LVS_REPORT|winapi.LVS_SINGLESEL|winapi.LVS_SHOWSELALWAYS|
		winapi.WS_BORDER|winapi.WS_TABSTOP, idStList, 0, 0, 0, 0)
	winapi.SendMessage(sw.list, winapi.LVM_SETEXTENDEDLISTVIEWSTYLE, 0, winapi.LVS_EX_FULLROWSELECT|winapi.LVS_EX_GRIDLINES)
	for i, k := range trayui.StatusColumns {
		winapi.ListViewInsertColumn(sw.list, i, i18n.T(k, lang), px(statusColWidths[i]))
	}
	sw.info = child(hwnd, "STATIC", "", winapi.SS_LEFT|ssNotify, idStInfo, 0, 0, 0, 0)
	sw.history = child(hwnd, "BUTTON", i18n.T(i18n.KeyStHistory, lang), winapi.WS_TABSTOP, idStHistory, 0, 0, 0, 0)
	sw.check = child(hwnd, "BUTTON", i18n.T(i18n.KeyMenuCheckNow, lang), winapi.WS_TABSTOP, idStCheckNow, 0, 0, 0, 0)
	sw.csv = child(hwnd, "BUTTON", i18n.T(i18n.KeyStExportCSV, lang), winapi.WS_TABSTOP, idStExportCSV, 0, 0, 0, 0)
	sw.html = child(hwnd, "BUTTON", i18n.T(i18n.KeyStExportHTML, lang), winapi.WS_TABSTOP, idStExportHTML, 0, 0, 0, 0)
	sw.close = child(hwnd, "BUTTON", i18n.T(i18n.KeyStClose, lang), winapi.WS_TABSTOP, idStClose, 0, 0, 0, 0)
	// phase 11 (CHK-TRAY-45/47): the list, then the pinned bar (info line
	// above the buttons); never wider than the work area, the columns
	// share the width of the list
	// phase 11 (CHK-TRAY-49): the list fits 1024×768 at 144 dpi without
	// scrolling; the info line takes no room while empty
	sw.infoCtl = &ui.Ctl{H: sw.info, Kind: ui.Box, W: 320, Hgt: 18, Grow: true, Hidden: true}
	sw.content = &ui.Pad{Left: ui.P12, Top: ui.P12, Right: ui.P12, Child: &ui.Col{Gap: ui.P8, Items: []ui.Node{
		&ui.Ctl{H: cp.hwnd, Kind: ui.Box, W: w - 36, Hgt: cardPanelHeight(cardRows)},
		&ui.Ctl{H: sw.list, Kind: ui.Box, W: w - 36, Hgt: 230, Grow: true},
		sw.infoCtl}}}
	// phase 11 (CHK-TRAY-48): the info line under the table, the bar is
	// the buttons only (no empty line above them)
	sw.bar = &ui.Pad{All: ui.P12, Child: &ui.Row{Right: true, Items: []ui.Node{uiCtl(sw.history, ui.Button, ""), uiCtl(sw.check, ui.Button, ""),
		uiCtl(sw.csv, ui.Button, ""), uiCtl(sw.html, ui.Button, ""), uiCtl(sw.close, ui.Button, "")}}}
	fitForm(hwnd, sw.content, sw.bar, 0, false)
	fitColumns(sw.list, statusColWidths, trayui.StatusColumns, lang)
	sw.tips.set(sw.list, i18n.T(i18n.KeyTipStList, lang))
	sw.tips.set(cp.hwnd, i18n.T(i18n.KeyTipCards, lang))
	sw.tips.set(sw.check, i18n.T(i18n.KeyTipCheckNow, lang))
	sw.tips.set(sw.csv, i18n.T(i18n.KeyTipExportCSV, lang))
	sw.tips.set(sw.html, i18n.T(i18n.KeyTipExportHTML, lang))
	sw.tips.set(sw.history, i18n.T(i18n.KeyTipStHistory, lang))
	sw.tips.set(sw.info, i18n.T(i18n.KeyTipStInfo, lang))
	sw.tips.set(sw.close, i18n.T(i18n.KeyTipClose, lang))
	if show {
		sw.show()
	}
	return sw, nil
}

func (sw *statusWindow) show() {
	winapi.ShowWindow(sw.hwnd, winapi.SW_SHOW)
	winapi.SetForegroundWindow(sw.hwnd)
}

// update refills the list from the status (UI thread).
func (sw *statusWindow) update(st trayipc.Status, connected bool) {
	var selKey [2]string
	if i := winapi.ListViewSelected(sw.list); i >= 0 && i < len(sw.rows) {
		selKey = [2]string{sw.rows[i].ProfileID, sw.rows[i].PointKey}
	}
	sw.rows = trayui.StatusRows(st, sw.lang, sw.now())
	winapi.SendMessage(sw.list, winapi.LVM_DELETEALLITEMS, 0, 0)
	for i, r := range sw.rows {
		winapi.ListViewAddRow(sw.list, r.Cells())
		if selKey[0] != "" && r.ProfileID == selKey[0] && r.PointKey == selKey[1] {
			winapi.ListViewSelect(sw.list, i) // the selection survives a refresh
		}
	}
	sw.cards.set(trayui.Cards(sw.rows, sw.lang))
	var cells [][]string
	for _, r := range sw.rows {
		cells = append(cells, r.Cells())
	}
	fitColumns(sw.list, statusColWidths, trayui.StatusColumns, sw.lang, cells...)
	sw.updateHistoryButton()
	winapi.EnableWindow(sw.check, connected)
	if sw.rows == nil {
		winapi.SetWindowText(sw.info, i18n.T(i18n.KeyStNoData, sw.lang))
	} else if sw.info != 0 && winapi.GetWindowText(sw.info) == i18n.T(i18n.KeyStNoData, sw.lang) {
		winapi.SetWindowText(sw.info, "")
	}
	sw.fitInfo()
}

func (sw *statusWindow) setInfo(text string) {
	winapi.SetWindowText(sw.info, text)
	sw.fitInfo()
}

// fitInfo hides an empty info line (no gap above the buttons) and lays
// the window out again when that changed (phase 11, CHK-TRAY-49).
func (sw *statusWindow) fitInfo() {
	if sw.infoCtl == nil {
		return
	}
	hidden := winapi.GetWindowText(sw.info) == ""
	if hidden == sw.infoCtl.Hidden {
		return
	}
	sw.infoCtl.Hidden = hidden
	if hidden {
		winapi.ShowWindow(sw.info, winapi.SW_HIDE)
	}
	fitForm(sw.hwnd, sw.content, sw.bar, 0, true)
	sw.cards.place()
}

// onNotify colours rows: NM_CUSTOMDRAW of the list, pale background of
// the row level.
func (sw *statusWindow) onNotify(lp uintptr) (uintptr, bool) {
	h := winapi.ReadNMHDR(lp)
	if h.From == sw.list && h.Code == winapi.LVN_ITEMCHANGED {
		sw.updateHistoryButton()
		return 0, false
	}
	if h.From == sw.list && h.Code == winapi.NM_DBLCLK {
		sw.openHistory()
		return 0, true
	}
	if h.From != sw.list || h.Code != winapi.NM_CUSTOMDRAW {
		return 0, false
	}
	cd := winapi.ReadCustomDraw(lp)
	switch cd.DrawStage {
	case winapi.CDDS_PREPAINT:
		return winapi.CDRF_NOTIFYITEMDRAW, true
	case winapi.CDDS_ITEMPREPAINT:
		i := int(cd.ItemSpec)
		if i >= 0 && i < len(sw.rows) {
			if bk, ok := rowBk[sw.rows[i].Color]; ok {
				winapi.WriteCustomDrawColors(lp, 0x000000, bk)
				return winapi.CDRF_NEWFONT, true
			}
		}
		return winapi.CDRF_DODEFAULT, true
	}
	return winapi.CDRF_DODEFAULT, true
}

func (sw *statusWindow) dialogPath(k exportKind) (string, bool) {
	name := "ExchangeChecker-" + sw.now().Format("20060102-1504")
	if k == exportCSV {
		return winapi.GetSaveFileName(sw.hwnd, i18n.T(i18n.KeyStExportCSV, sw.lang),
			i18n.T(i18n.KeyStFilterCSV, sw.lang)+"\x00*.csv", "csv", name+".csv")
	}
	return winapi.GetSaveFileName(sw.hwnd, i18n.T(i18n.KeyStExportHTML, sw.lang),
		i18n.T(i18n.KeyStFilterHTML, sw.lang)+"\x00*.html;*.htm", "html", name+".html")
}

// export saves the shown rows (CHK-TRAY-21).
func (sw *statusWindow) export(k exportKind) {
	p, ok := sw.savePath(k)
	if !ok || p == "" {
		return
	}
	var data []byte
	if k == exportCSV {
		data = trayui.ExportCSV(sw.rows, sw.lang)
	} else {
		data = trayui.ExportHTML(sw.rows, sw.lang, sw.now())
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		sw.setInfo(fmt.Sprintf(i18n.T(i18n.KeyStSaveFailed, sw.lang), err))
		return
	}
	sw.lastSaved = p
	sw.setInfo(fmt.Sprintf(i18n.T(i18n.KeyStSaved, sw.lang), p))
}

// selected is the selected row (ok = false when none).
func (sw *statusWindow) selected() (trayui.StatusRow, bool) {
	i := winapi.ListViewSelected(sw.list)
	if i < 0 || i >= len(sw.rows) {
		return trayui.StatusRow{}, false
	}
	return sw.rows[i], true
}

// updateHistoryButton: «История…» needs a point row and the history
// capability of the service (CHK-TRAY-26 fallback).
func (sw *statusWindow) updateHistoryButton() {
	r, ok := sw.selected()
	winapi.EnableWindow(sw.history, ok && r.PointKey != "" && sw.canHistory != nil && sw.canHistory())
}

func (sw *statusWindow) openHistory() {
	r, ok := sw.selected()
	if !ok {
		sw.setInfo(i18n.T(i18n.KeyStHistoryNoPoint, sw.lang))
		return
	}
	if sw.onHistory != nil {
		sw.onHistory(r)
	}
}

// fitColumns spreads the column widths (96-dpi units) over the client
// width of the list view. Phase 11 (CHK-TRAY-49): every column first
// gets its header and its widest cell (cells: the rows), the rest of the
// width is shared in proportion to widths; when that is still wider than
// the list, the optional columns (send, receive, plan; dropped, in the
// export and the history) go first, then everything is scaled down (no
// horizontal scrolling).
func fitColumns(lv uintptr, widths []int32, keys []string, lang string, cells ...[]string) {
	r := winapi.GetClientRect(lv)
	avail := r.Right - r.Left - winapi.GetSystemMetrics(winapi.SM_CXVSCROLL)
	if avail <= 0 || len(widths) == 0 {
		return
	}
	m := uiMetrics()
	min := make([]int32, len(widths))
	want := make([]int32, len(widths))
	for i := range widths {
		if i < len(keys) {
			tw, _ := m.Text(i18n.T(keys[i], lang))
			min[i] = tw + px(ui.P12)
		}
		for _, row := range cells {
			if i < len(row) && row[i] != "" {
				tw, _ := m.Text(row[i])
				min[i] = max32(min[i], tw+px(ui.P12))
			}
		}
	}
	drop := map[int]bool{}
	sum := func() (s int32) {
		for i := range min {
			if !drop[i] {
				s += min[i]
			}
		}
		return s
	}
	for _, i := range statusOptional {
		if sum() <= avail || len(widths) != len(statusColWidths) {
			break
		}
		drop[i] = true
	}
	var sumMin, sumExtra int32
	for i, w := range widths {
		if drop[i] {
			continue
		}
		want[i] = max32(px(w), min[i])
		sumMin += min[i]
		sumExtra += want[i] - min[i]
	}
	free := avail - sumMin
	for i := range widths {
		var cw int32
		switch {
		case drop[i]:
			cw = 0
		case free <= 0:
			cw = min[i] * avail / max32(sumMin, 1)
		case sumExtra <= free:
			cw = want[i] + (free-sumExtra)*want[i]/max32(sumMin+sumExtra, 1)
		default:
			cw = min[i] + (want[i]-min[i])*free/max32(sumExtra, 1)
		}
		winapi.ListViewSetColumnWidth(lv, i, cw)
	}
}

// statusOptional: the columns dropped first on a narrow list (send,
// receive, plan).
var statusOptional = []int{6, 7, 2}
