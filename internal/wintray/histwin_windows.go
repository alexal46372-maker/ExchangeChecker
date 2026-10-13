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
)

// History window of one point (phase 9b, CHK-TRAY-26 / CHK-TRAY-27).
const (
	histClass      = "ExchangeCheckerTrayHistory"
	chartClass     = "ExchangeCheckerTrayChart"
	idHistDay      = 500
	idHistWeek     = 501
	idHistChart    = 502
	idHistInfo     = 503
	idHistCSV      = 510
	idHistHTML     = 511
	idHistClose    = winapi.IDCANCEL
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
}

// fetchHistory reads every page of req (cursor), calling onPage on the
// loader goroutine; a history error reply returns errHistory.
func fetchHistory(ctx context.Context, a *App, req model.IPCRequest, lang string, onPage func(*model.HistoryPage) bool) error {
	for i := 0; i < histMaxPages; i++ {
		m, err := a.session.DoCancel(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errHistory{trayui.HistoryErrorText(m, err, lang)}
		}
		if m.Type == model.MsgError || m.History == nil {
			return errHistory{trayui.HistoryErrorText(m, nil, lang)}
		}
		if !onPage(m.History) {
			return nil
		}
		if !m.History.HasMore || m.History.NextCursor == "" {
			return nil
		}
		req.Cursor = m.History.NextCursor
	}
	return nil
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
				winapi.DestroyWindow(hwnd)
				return 0, true
			}
		case winapi.WM_CLOSE:
			winapi.DestroyWindow(hwnd)
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
	hw.day = child(hwnd, "BUTTON", i18n.T(i18n.KeyHistDay, hw.lang), winapi.BS_AUTORADIO|winapi.WS_TABSTOP, idHistDay, 12, 8, 110, 24)
	hw.week = child(hwnd, "BUTTON", i18n.T(i18n.KeyHistWeek, hw.lang), winapi.BS_AUTORADIO, idHistWeek, 130, 8, 110, 24)
	hw.chart, err = createWindow(chartClass, "", winapi.WS_EX_CLIENTEDGE, winapi.WS_CHILD|winapi.WS_VISIBLE, 10, 38, w-36, h-130, hwnd,
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
		winapi.DestroyWindow(hwnd)
		return nil, err
	}
	by := int32(h - 84)
	hw.info = child(hwnd, "STATIC", "", winapi.SS_LEFT|ssNotify, idHistInfo, 12, by+5, 470, 22)
	hw.csv = child(hwnd, "BUTTON", i18n.T(i18n.KeyStExportCSV, hw.lang), winapi.WS_TABSTOP, idHistCSV, w-446, by, 130, 28)
	hw.html = child(hwnd, "BUTTON", i18n.T(i18n.KeyStExportHTML, hw.lang), winapi.WS_TABSTOP, idHistHTML, w-306, by, 130, 28)
	hw.close = child(hwnd, "BUTTON", i18n.T(i18n.KeyStClose, hw.lang), winapi.WS_TABSTOP, idHistClose, w-166, by, 130, 28)
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
		err := fetchHistory(ctx, a, trayui.HistoryRequest(row.ProfileID, row.PointKey, from, to, bucket,
			[]string{model.HistoryKindPoint}, ""), lang, func(p *model.HistoryPage) bool {
			bm.Add(p.Buckets)
			pages++
			progress()
			return true
		})
		if err == nil {
			err = fetchHistory(ctx, a, trayui.HistoryRequest(row.ProfileID, row.PointKey, from, to, "",
				[]string{model.HistoryKindEvent, model.HistoryKindRecovered}, ""), lang, func(p *model.HistoryPage) bool {
				evs = append(evs, p.Records...)
				pages++
				progress()
				return len(evs) < histMaxRecords
			})
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
			hw.gap, hw.loaded, hw.pages, hw.events = gap, true, pages, len(hw.model.Marks)
			if len(hw.model.Points) == 0 && len(hw.model.Marks) == 0 {
				hw.setInfo(i18n.T(i18n.KeyHistEmpty, lang))
			} else {
				hw.setInfo(fmt.Sprintf(i18n.T(i18n.KeyHistLoaded, lang), len(hw.model.Points), len(hw.model.Marks)))
			}
			winapi.InvalidateRect(hw.chart, true)
		})
	}()
}

// colours (COLORREF 0x00BBGGRR)
const (
	clrWhite  = 0xFFFFFF
	clrAxis   = 0x808080
	clrGrid   = 0xE0E0E0
	clrAvg    = 0xB05A1E // blue
	clrMinMax = 0xE0B48C // light blue
	clrWarn   = 0x00C2F2 // yellow (#F2C200)
	clrErr    = 0x2530D9 // red (#D93025)
	clrOK     = 0x43A02E // green (#2EA043)
	clrText   = 0x404040
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
	bg := winapi.CreateSolidBrush(clrWhite)
	winapi.FillRect(dc, rc, bg)
	winapi.DeleteObject(bg)
	if !hw.loaded {
		return
	}
	winapi.SetBkMode(dc, winapi.TRANSPARENT)
	winapi.SetTextColor(dc, clrText)
	font := winapi.SelectObject(dc, winapi.GetStockObject(winapi.DEFAULT_GUI_FONT))
	defer winapi.SelectObject(dc, font)
	L := hw.model.Layout(rc.Left+48, rc.Top+14, rc.Right-14, rc.Bottom-34, hw.gap)
	pen := func(style, width int32, clr uint32, draw func()) {
		p := winapi.CreatePen(style, width, clr)
		old := winapi.SelectObject(dc, p)
		draw()
		winapi.SelectObject(dc, old)
		winapi.DeleteObject(p)
	}
	pen(winapi.PS_SOLID, 1, clrGrid, func() {
		for _, t := range L.YTicks {
			winapi.Line(dc, L.Left, t.Y, L.Right, t.Y)
		}
	})
	pen(winapi.PS_SOLID, 1, clrAxis, func() {
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
	p, ok := hw.savePath(k)
	if !ok || p == "" {
		return
	}
	a, lang, row := hw.app, hw.lang, hw.row
	from, to, _ := trayui.SpanRange(hw.span, a.serverNow())
	if hw.expCancel != nil {
		return // one export at a time (the buttons are disabled meanwhile)
	}
	ctx, cancel := context.WithCancel(context.Background())
	hw.expCancel = cancel
	csv, html := hw.csv, hw.html
	winapi.EnableWindow(csv, false)
	winapi.EnableWindow(html, false)
	go func() {
		var recs []model.HistoryRecord
		err := fetchHistory(ctx, a, trayui.HistoryRequest(row.ProfileID, row.PointKey, from, to, "", nil, ""), lang,
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
				data = trayui.ExportHistoryCSV(recs, lang)
			} else {
				data = trayui.ExportHistoryHTML(recs, fmt.Sprintf(i18n.T(i18n.KeyHistTitle, lang), row.Database+" — "+row.Point), from, to, lang)
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
				hw.lastSaved = p
				hw.setInfo(fmt.Sprintf(i18n.T(i18n.KeyStSaved, lang), p))
			}
		})
	}()
}
