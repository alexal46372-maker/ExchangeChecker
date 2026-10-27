//go:build windows

package wintray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// History window of one point (phase 9b, CHK-TRAY-26 / CHK-TRAY-27).
const (
	histClass   = "ExchangeCheckerTrayHistory"
	chartClass  = "ExchangeCheckerTrayChart"
	idHistDay   = 500
	idHistWeek  = 501
	idHistChart = 502
	idHistInfo  = 503
	idHistCSV   = 510
	idHistHTML  = 511
	idHistClose = winapi.IDCANCEL
)

// History read limits (vars: tests lower them); hitting one marks the
// window / export «показаны не все записи».
var (
	histMaxPages   = 400    // bucket / event / export pages per request
	histMaxRecords = 250000 // export cap (31 days of 10 s polls fit)
)

// errHistory is a history error reply (text already localized).
type errHistory struct{ text string }

func (e errHistory) Error() string { return e.text }

type histWindow struct {
	app                    *App
	hwnd, chart, day, week uintptr
	info, csv, html, close uintptr
	tips                   *tooltips
	lang                   string
	row                    trayui.StatusRow
	span                   trayui.HistorySpan
	model                  trayui.Chart
	gap                    time.Duration
	loaded                 bool
	gen                    int
	cancel                 context.CancelFunc
	expCancel              context.CancelFunc
	savePath               func(k exportKind) (string, bool) // test hook
	lastSaved              string
	pages, events          int // loaded (tests)
	partial, lastPartial   bool
}

// fetchHistory reads every page of req (cursor), calling onPage on the
// loader goroutine (false = record limit reached: stop); a history error
// reply returns errHistory. partial: histMaxPages or the record limit
// stopped the reading before the last page.
func fetchHistory(ctx context.Context, a *App, req model.IPCRequest, lang string, onPage func(*model.HistoryPage) bool) (partial bool, err error) {
	for i := 0; ; i++ {
		if i == histMaxPages {
			return true, nil
		}
		m, err := a.session.DoCancel(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, errHistory{trayui.HistoryErrorText(m, err, lang)}
		}
		if m.Type == model.MsgError || m.History == nil {
			return false, errHistory{trayui.HistoryErrorText(m, nil, lang)}
		}
		more := m.History.HasMore && m.History.NextCursor != ""
		if !onPage(m.History) {
			return more, nil
		}
		if !more {
			return false, nil
		}
		req.Cursor = m.History.NextCursor
	}
}

func newHistWindow(a *App, row trayui.StatusRow, show bool) (*histWindow, error) {
	const w, h = 940, 540
	x := (winapi.GetSystemMetrics(winapi.SM_CXSCREEN) - w) / 2
	y := (winapi.GetSystemMetrics(winapi.SM_CYSCREEN) - h) / 3
	hw := &histWindow{app: a, lang: a.opt.Lang, row: row}
	hw.savePath = hw.dialogPath
	handler := func(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
		switch msg {
		case winapi.WM_COMMAND:
			switch wp & 0xFFFF {
			case idHistDay:
				hw.setSpan(trayui.SpanDay)
				return 0, true
			case idHistWeek:
				hw.setSpan(trayui.SpanWeek)
				return 0, true
			case idHistCSV:
				hw.export(exportCSV)
				return 0, true
			case idHistHTML:
				hw.export(exportHTML)
				return 0, true
			case idHistClose:
				destroyWindow(hwnd)
				return 0, true
			}
		case winapi.WM_CLOSE:
			destroyWindow(hwnd)
			return 0, true
		case winapi.WM_DESTROY:
			hw.stop() // cancel {ref} of a running page
			if a.histWin == hw {
				a.histWin = nil
			}
		}
		return 0, false
	}
	title := fmt.Sprintf(i18n.T(i18n.KeyHistTitle, hw.lang), row.Database+" — "+row.Point)
	hwnd, err := createWindow(histClass, title, 0, winapi.WS_OVERLAPPED|winapi.WS_CAPTION|winapi.WS_SYSMENU|winapi.WS_MINIMIZEBOX,
		x, y, w, h, 0, handler)
	if err != nil {
		return nil, err
	}
	hw.hwnd = hwnd
	hw.tips = newTooltips(hwnd)
	hw.day = child(hwnd, "BUTTON", i18n.T(i18n.KeyHistDay, hw.lang), winapi.BS_AUTORADIO|winapi.WS_TABSTOP, idHistDay, 0, 0, 0, 0)
	hw.week = child(hwnd, "BUTTON", i18n.T(i18n.KeyHistWeek, hw.lang), winapi.BS_AUTORADIO, idHistWeek, 0, 0, 0, 0)
	hw.chart, err = createWindow(chartClass, "", winapi.WS_EX_CLIENTEDGE, winapi.WS_CHILD|winapi.WS_VISIBLE, 0, 0, 0, 0, hwnd,
		func(c uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
			switch msg {
			case winapi.WM_PAINT:
				hw.paint(c)
				return 0, true
			case winapi.WM_ERASEBKGND:
				return 1, true
			}
			return 0, false
		})
	if err != nil {
		destroyWindow(hwnd)
		return nil, err
	}
	hw.info = child(hwnd, "STATIC", "", winapi.SS_LEFT|ssNotify, idHistInfo, 0, 0, 0, 0)
	hw.csv = child(hwnd, "BUTTON", i18n.T(i18n.KeyStExportCSV, hw.lang), winapi.WS_TABSTOP, idHistCSV, 0, 0, 0, 0)
	hw.html = child(hwnd, "BUTTON", i18n.T(i18n.KeyStExportHTML, hw.lang), winapi.WS_TABSTOP, idHistHTML, 0, 0, 0, 0)
	hw.close = child(hwnd, "BUTTON", i18n.T(i18n.KeyStClose, hw.lang), winapi.WS_TABSTOP, idHistClose, 0, 0, 0, 0)
	// phase 11 (CHK-TRAY-45): span radios, the chart, info | exports, close
	// phase 11 (CHK-TRAY-47): bar pinned, never wider than the work area
	fitForm(hwnd, &ui.Pad{Left: ui.P12, Top: ui.P12, Right: ui.P12, Child: &ui.Col{Gap: ui.P8, Items: []ui.Node{
		&ui.Row{Gap: ui.P16, Items: []ui.Node{uiCtl(hw.day, ui.Check, ""), uiCtl(hw.week, ui.Check, "")}},
		&ui.Ctl{H: hw.chart, Kind: ui.Box, W: w - 36, Hgt: h - 130, Grow: true},
	}}}, &ui.Pad{All: ui.P12, Child: &ui.Row{Grow: true, Items: []ui.Node{
		&ui.Ctl{H: hw.info, Kind: ui.Box, W: 470, Hgt: 22},
		&ui.Row{Right: true, Items: []ui.Node{uiCtl(hw.csv, ui.Button, ""), uiCtl(hw.html, ui.Button, ""), uiCtl(hw.close, ui.Button, "")}},
	}}}, 0, false)
	for c, k := range map[uintptr]string{hw.day: i18n.KeyTipHistDay, hw.week: i18n.KeyTipHistWeek, hw.chart: i18n.KeyTipHistChart,
		hw.info: i18n.KeyTipHistInfo, hw.csv: i18n.KeyTipHistExportCSV, hw.html: i18n.KeyTipHistExportHTML, hw.close: i18n.KeyTipHistClose} {
		hw.tips.set(c, i18n.T(k, hw.lang))
	}
	winapi.SendMessage(hw.day, winapi.BM_SETCHECK, winapi.BST_CHECKED, 0)
	if show {
		winapi.ShowWindow(hwnd, winapi.SW_SHOW)
		winapi.SetForegroundWindow(hwnd)
	}
	hw.load()
	return hw, nil
}

func (hw *histWindow) alive() bool { return hw.hwnd != 0 && winapi.IsWindow(hw.hwnd) }

// stop cancels the chart load and a running export (window closed).
func (hw *histWindow) stop() {
	hw.stopLoad()
	if hw.expCancel != nil {
		hw.expCancel()
		hw.expCancel = nil
	}
}

func (hw *histWindow) stopLoad() {
	if hw.cancel != nil {
		hw.cancel()
		hw.cancel = nil
	}
}

func (hw *histWindow) setInfo(s string) { winapi.SetWindowText(hw.info, s) }

func (hw *histWindow) setSpan(s trayui.HistorySpan) {
	if hw.span == s && (hw.loaded || hw.cancel != nil) {
		return
	}
	hw.span = s
	day, week := uintptr(winapi.BST_CHECKED), uintptr(0)
	if s == trayui.SpanWeek {
		day, week = 0, winapi.BST_CHECKED
	}
	winapi.SendMessage(hw.day, winapi.BM_SETCHECK, day, 0)
	winapi.SendMessage(hw.week, winapi.BM_SETCHECK, week, 0)
	hw.load()
}

// load reads the buckets (kinds point) and the events of the span; a
// new load or closing the window cancels the previous one.
func (hw *histWindow) load() {
	hw.stopLoad()
	hw.gen++
	gen := hw.gen
	hw.loaded = false
	ctx, cancel := context.WithCancel(context.Background())
	hw.cancel = cancel
	a, lang, row := hw.app, hw.lang, hw.row
	from, to, bucket := trayui.SpanRange(hw.span, a.serverNow())
	gap := 5 * time.Minute
	if bucket == model.HistoryBucket1h {
		gap = time.Hour
	}
	hw.setInfo(fmt.Sprintf(i18n.T(i18n.KeyHistLoading, lang), 0))
	go func() {
		var bm trayui.BucketMerger
		var evs []model.HistoryRecord
		pages := 0
		progress := func() {
			n := pages
			a.runOnUI(func() {
				if hw.alive() && hw.gen == gen {
					hw.setInfo(fmt.Sprintf(i18n.T(i18n.KeyHistLoading, lang), n))
				}
			})
		}
		partial, err := fetchHistory(ctx, a, trayui.HistoryRequest(row.ProfileID, row.PointKey, from, to, bucket,
			[]string{model.HistoryKindPoint}, ""), lang, func(p *model.HistoryPage) bool {
			bm.Add(p.Buckets)
			pages++
			progress()
			return true
		})
		if err == nil {
			var p2 bool
			p2, err = fetchHistory(ctx, a, trayui.HistoryRequest(row.ProfileID, row.PointKey, from, to, "",
				[]string{model.HistoryKindEvent, model.HistoryKindRecovered}, ""), lang, func(p *model.HistoryPage) bool {
				evs = append(evs, p.Records...)
				pages++
				progress()
				return len(evs) < histMaxRecords
			})
			partial = partial || p2
		}
		buckets := bm.Buckets()
		a.runOnUI(func() {
			if !hw.alive() || hw.gen != gen {
				return
			}
			hw.cancel = nil
			cancel()
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					hw.setInfo(err.Error())
				}
				return
			}
			hw.model = trayui.BuildChart(from, to, buckets, evs, row.PointKey, row.WarnMin, row.ErrMin)
			hw.gap, hw.loaded, hw.pages, hw.events, hw.partial = gap, true, pages, len(hw.model.Marks), partial
			text := fmt.Sprintf(i18n.T(i18n.KeyHistLoaded, lang), len(hw.model.Points), len(hw.model.Marks))
			if len(hw.model.Points) == 0 && len(hw.model.Marks) == 0 {
				text = i18n.T(i18n.KeyHistEmpty, lang)
			}
			if partial {
				text += " " + i18n.T(i18n.KeyHistPartial, lang)
			}
			hw.setInfo(text)
			winapi.InvalidateRect(hw.chart, true)
		})
	}()
}

// colours from the tray theme (CHK-TRAY-51 / 55). State/chart lines are
// fixed; page / text / axis / grid follow the active chrome.
const (
	clrAvg    = trayui.ChartAvg
	clrMinMax = trayui.ChartBand
	clrWarn   = trayui.StateWarning
	clrErr    = trayui.StateError
	clrOK     = trayui.StateOK
)

func toGDI(ps []trayui.Pt) []winapi.GDIPoint {
	out := make([]winapi.GDIPoint, len(ps))
	for i, p := range ps {
		out[i] = winapi.GDIPoint{X: p.X, Y: p.Y}
	}
	return out
}

func (hw *histWindow) paint(c uintptr) {
	var ps winapi.PAINTSTRUCT
	dc := winapi.BeginPaint(c, &ps)
	defer winapi.EndPaint(c, &ps)
	rc := winapi.GetClientRect(c)
	bg := winapi.CreateSolidBrush(trayui.ChromePage())
	winapi.FillRect(dc, rc, bg)
	winapi.DeleteObject(bg)
	if !hw.loaded {
		return
	}
	winapi.SetBkMode(dc, winapi.TRANSPARENT)
	winapi.SetTextColor(dc, trayui.ChromeText())
	font := winapi.SelectObject(dc, uiFont())
	defer winapi.SelectObject(dc, font)
	L := hw.model.Layout(rc.Left+48, rc.Top+14, rc.Right-14, rc.Bottom-34, hw.gap)
	pen := func(style, width int32, clr uint32, draw func()) {
		p := winapi.CreatePen(style, width, clr)
		old := winapi.SelectObject(dc, p)
		draw()
		winapi.SelectObject(dc, old)
		winapi.DeleteObject(p)
	}
	pen(winapi.PS_SOLID, 1, trayui.ChromeGrid(), func() {
		for _, t := range L.YTicks {
			winapi.Line(dc, L.Left, t.Y, L.Right, t.Y)
		}
	})
	pen(winapi.PS_SOLID, 1, trayui.ChromeAxis(), func() {
		winapi.Line(dc, L.Left, L.Top, L.Left, L.Bottom)
		winapi.Line(dc, L.Left, L.Bottom, L.Right, L.Bottom)
		for _, t := range L.XTicks {
			winapi.Line(dc, t.X, L.Bottom, t.X, L.Bottom+4)
		}
	})
	for _, t := range L.YTicks {
		winapi.TextOut(dc, rc.Left+4, t.Y-7, fmt.Sprintf("%d", t.Min))
	}
	winapi.TextOut(dc, rc.Left+4, rc.Top+1, i18n.T(i18n.KeyHistAxisMin, hw.lang))
	for i, t := range L.XTicks {
		x := t.X - 14
		if i == len(L.XTicks)-1 {
			x = t.X - 32
		}
		winapi.TextOut(dc, x, L.Bottom+7, t.Text)
	}
	if L.WarnY >= 0 {
		pen(winapi.PS_DOT, 1, clrWarn, func() { winapi.Line(dc, L.Left, L.WarnY, L.Right, L.WarnY) })
	}
	if L.ErrY >= 0 {
		pen(winapi.PS_DOT, 1, clrErr, func() { winapi.Line(dc, L.Left, L.ErrY, L.Right, L.ErrY) })
	}
	pen(winapi.PS_SOLID, 1, clrMinMax, func() {
		for i := range L.MinL {
			winapi.Polyline(dc, toGDI(L.MinL[i]))
			winapi.Polyline(dc, toGDI(L.MaxL[i]))
		}
	})
	pen(winapi.PS_SOLID, 2, clrAvg, func() {
		for _, line := range L.Avg {
			if len(line) == 1 {
				winapi.Ellipse(dc, line[0].X-2, line[0].Y-2, line[0].X+2, line[0].Y+2)
				continue
			}
			winapi.Polyline(dc, toGDI(line))
		}
	})
	for _, m := range L.Marks {
		clr := uint32(clrErr)
		if m.Mark.Recov {
			clr = clrOK
		} else if m.Mark.Level == model.LevelWarning {
			clr = clrWarn
		}
		br := winapi.CreateSolidBrush(clr)
		oldB := winapi.SelectObject(dc, br)
		pen(winapi.PS_SOLID, 1, clr, func() {
			y := L.Bottom - 6
			if m.Mark.Recov {
				winapi.Ellipse(dc, m.X-4, y-4, m.X+4, y+4)
			} else {
				winapi.Polygon(dc, []winapi.GDIPoint{{X: m.X, Y: y - 5}, {X: m.X - 5, Y: y + 4}, {X: m.X + 5, Y: y + 4}})
			}
		})
		winapi.SelectObject(dc, oldB)
		winapi.DeleteObject(br)
	}
}

func (hw *histWindow) dialogPath(k exportKind) (string, bool) {
	name := "ExchangeChecker-history-" + hw.app.serverNow().Format("20060102-1504")
	if k == exportCSV {
		return winapi.GetSaveFileName(hw.hwnd, i18n.T(i18n.KeyStExportCSV, hw.lang), i18n.T(i18n.KeyStFilterCSV, hw.lang)+"\x00*.csv", "csv", name+".csv")
	}
	return winapi.GetSaveFileName(hw.hwnd, i18n.T(i18n.KeyStExportHTML, hw.lang), i18n.T(i18n.KeyStFilterHTML, hw.lang)+"\x00*.html;*.htm", "html", name+".html")
}

// export reads every record page of the span (no buckets) and saves it
// (CHK-TRAY-27); it runs beside the chart load and stops with the window.
func (hw *histWindow) export(k exportKind) {
	if hw.expCancel != nil {
		return // one export at a time; no save dialog while one runs
	}
	p, ok := hw.savePath(k)
	if !ok || p == "" {
		return
	}
	a, lang, row := hw.app, hw.lang, hw.row
	from, to, _ := trayui.SpanRange(hw.span, a.serverNow())
	if hw.expCancel != nil || !hw.alive() {
		return // an export started while the dialog was open / window closed
	}
	ctx, cancel := context.WithCancel(context.Background())
	hw.expCancel = cancel
	csv, html := hw.csv, hw.html
	winapi.EnableWindow(csv, false)
	winapi.EnableWindow(html, false)
	go func() {
		var recs []model.HistoryRecord
		partial, err := fetchHistory(ctx, a, trayui.HistoryRequest(row.ProfileID, row.PointKey, from, to, "", nil, ""), lang,
			func(pg *model.HistoryPage) bool {
				recs = append(recs, pg.Records...)
				n := len(recs)
				a.runOnUI(func() {
					if hw.alive() {
						hw.setInfo(fmt.Sprintf(i18n.T(i18n.KeyHistExporting, lang), n))
					}
				})
				return len(recs) < histMaxRecords
			})
		if err == nil {
			var data []byte
			if k == exportCSV {
				data = trayui.ExportHistoryCSV(recs, lang, partial)
			} else {
				data = trayui.ExportHistoryHTML(recs, fmt.Sprintf(i18n.T(i18n.KeyHistTitle, lang), row.Database+" — "+row.Point), from, to, lang, partial)
			}
			if werr := os.WriteFile(p, data, 0o644); werr != nil {
				err = errHistory{fmt.Sprintf(i18n.T(i18n.KeyStSaveFailed, lang), werr)}
			}
		}
		cancel()
		a.runOnUI(func() {
			if !hw.alive() {
				return
			}
			hw.expCancel = nil
			winapi.EnableWindow(csv, true)
			winapi.EnableWindow(html, true)
			switch {
			case errors.Is(err, context.Canceled):
			case err != nil:
				hw.setInfo(err.Error())
			default:
				hw.lastSaved, hw.lastPartial = p, partial
				text := fmt.Sprintf(i18n.T(i18n.KeyStSaved, lang), p)
				if partial {
					text += " " + i18n.T(i18n.KeyHistPartial, lang)
				}
				hw.setInfo(text)
			}
		})
	}()
}
