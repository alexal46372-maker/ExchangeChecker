//go:build windows

package wintray

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
	"github.com/alexal46372-maker/ExchangeChecker/internal/wintray/ui"
)

// Summary window (CHK-TRAY-53): 24h GDI chart of worst levels per bucket.
const (
	summaryClass  = "ExchangeCheckerTraySummary"
	idSumInfo     = 801
	idSumClose    = 802
	sumRetryTimer = 53
	msgSumApply   = winapi.WM_APP + 20
)

type summaryWindow struct {
	a           *App
	hwnd, chart uintptr
	info, close, cards uintptr
	lang        string
	session     *trayipc.Session

	mu      sync.Mutex
	pending *model.Summary
	pendErr string
	busy    bool

	chartModel trayui.SummaryChart
}

func (a *App) openSummary() {
	if a.session == nil || !(a.session.Supported(model.CmdSummary) || a.session.Status().Has(model.CapSummary)) {
		return
	}
	if a.summaryWin != nil && winapi.IsWindow(a.summaryWin.hwnd) {
		winapi.ShowWindow(a.summaryWin.hwnd, winapi.SW_SHOWNORMAL)
		winapi.SetForegroundWindow(a.summaryWin.hwnd)
		return
	}
	sw := &summaryWindow{a: a, lang: a.opt.Lang, session: a.session}
	hwnd, err := createWindow(summaryClass, i18n.T(i18n.KeySumTitle, a.opt.Lang), 0,
		winapi.WS_OVERLAPPED|winapi.WS_CAPTION|winapi.WS_SYSMENU|winapi.WS_MINIMIZEBOX, 0, 0, 720, 480, a.hwnd, sw.wndProc)
	if err != nil {
		return
	}
	sw.hwnd = hwnd
	sw.build()
	a.summaryWin = sw
	if !a.opt.HiddenStatus {
		winapi.ShowWindow(hwnd, winapi.SW_SHOWNORMAL)
	}
	sw.load()
}

func (sw *summaryWindow) t(k string) string { return i18n.T(k, sw.lang) }

func (sw *summaryWindow) build() {
	sw.info, _ = winapi.CreateWindowEx(0, "STATIC", "", winapi.WS_CHILD|winapi.WS_VISIBLE|winapi.SS_LEFT,
		12, 12, 680, 36, sw.hwnd, uintptr(idSumInfo), winapi.GetModuleHandle())
	winapi.SendMessage(sw.info, winapi.WM_SETFONT, uiFont(), 1)
	sw.close, _ = winapi.CreateWindowEx(0, "BUTTON", sw.t(i18n.KeySumClose), winapi.WS_CHILD|winapi.WS_VISIBLE|winapi.WS_TABSTOP,
		600, 420, 100, 28, sw.hwnd, uintptr(idSumClose), winapi.GetModuleHandle())
	winapi.SendMessage(sw.close, winapi.WM_SETFONT, uiFont(), 1)
	sw.cards, _ = winapi.CreateWindowEx(winapi.WS_EX_CLIENTEDGE, "EDIT", "",
		winapi.WS_CHILD|winapi.WS_VISIBLE|winapi.ES_MULTILINE|winapi.ES_READONLY|winapi.ES_AUTOVSCROLL|winapi.WS_VSCROLL,
		12, 400, 580, 72, sw.hwnd, 0, winapi.GetModuleHandle())
	winapi.SendMessage(sw.cards, winapi.WM_SETFONT, uiFont(), 1)
	var err error
	sw.chart, err = createWindow(summaryClass+"Chart", "", winapi.WS_EX_CLIENTEDGE, winapi.WS_CHILD|winapi.WS_VISIBLE,
		12, 56, 680, 300, sw.hwnd, sw.chartProc)
	if err != nil {
		sw.chart = 0
	}
	info := uiCtl(sw.info, ui.Box, "")
	info.Grow, info.Hgt = true, 36
	ch := uiCtl(sw.chart, ui.Box, "")
	ch.Grow, ch.Hgt = true, 240
	cd := uiCtl(sw.cards, ui.Box, "")
	cd.Grow, cd.Hgt = true, 72
	btn := uiCtl(sw.close, ui.Button, sw.t(i18n.KeySumClose))
	content := &ui.Pad{All: ui.P12, Child: &ui.Col{Gap: ui.P8, Items: []ui.Node{info, ch, cd}}}
	bar := &ui.Pad{All: ui.P12, Child: &ui.Row{Right: true, Items: []ui.Node{btn}}}
	fitForm(sw.hwnd, content, bar, 0, false)
}

func (sw *summaryWindow) wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case winapi.WM_COMMAND:
		if int32(wp&0xFFFF) == idSumClose {
			winapi.PostMessage(hwnd, winapi.WM_CLOSE, 0, 0)
			return 0, true
		}
	case msgSumApply:
		sw.onApply()
		return 0, true
	case winapi.WM_TIMER:
		if wp == sumRetryTimer {
			winapi.KillTimer(hwnd, sumRetryTimer)
			sw.load()
			return 0, true
		}
	case winapi.WM_CLOSE:
		destroyWindow(hwnd)
		return 0, true
	case winapi.WM_DESTROY:
		if sw.a != nil && sw.a.summaryWin == sw {
			sw.a.summaryWin = nil
		}
		sw.hwnd = 0
		return 0, false
	}
	return 0, false
}

func (sw *summaryWindow) chartProc(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	if msg == winapi.WM_PAINT {
		sw.paintChart(hwnd)
		return 0, true
	}
	return 0, false
}

func (sw *summaryWindow) paintChart(hwnd uintptr) {
	var ps winapi.PAINTSTRUCT
	dc := winapi.BeginPaint(hwnd, &ps)
	defer winapi.EndPaint(hwnd, &ps)
	rc := winapi.GetClientRect(hwnd)
	w, h := int(rc.Right-rc.Left), int(rc.Bottom-rc.Top)
	if w < 2 || h < 2 {
		return
	}
	br := winapi.CreateSolidBrush(trayui.White)
	winapi.FillRect(dc, rc, br)
	winapi.DeleteObject(br)
	n := len(sw.chartModel.Buckets)
	if n == 0 {
		return
	}
	bw := w / n
	if bw < 1 {
		bw = 1
	}
	for i, b := range sw.chartModel.Buckets {
		col := trayui.PaleColorRef(trayui.BucketColor(b.Level))
		brush := winapi.CreateSolidBrush(col)
		r := winapi.RECT{Left: int32(i * bw), Top: 0, Right: int32((i + 1) * bw), Bottom: int32(h)}
		if i == n-1 {
			r.Right = int32(w)
		}
		winapi.FillRect(dc, r, brush)
		winapi.DeleteObject(brush)
		if b.Level != nil {
			stripe := winapi.CreateSolidBrush(trayui.ColorRefOf(trayui.BucketColor(b.Level)))
			sr := r
			sr.Bottom = r.Top + 4
			winapi.FillRect(dc, sr, stripe)
			winapi.DeleteObject(stripe)
		}
	}
}

func (sw *summaryWindow) load() {
	if sw.session == nil {
		return
	}
	s := sw.session
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		m, err := s.Do(ctx, model.IPCRequest{Cmd: model.CmdSummary, Hours: 24, Buckets: 48})
		sw.mu.Lock()
		sw.pending, sw.pendErr, sw.busy = nil, "", false
		if err != nil {
			sw.pendErr = err.Error()
		} else if m.Type == model.MsgError {
			if m.Code == model.ErrCodeBusy || m.Code == model.ErrCodeRateLimited {
				sw.busy = true
				sw.pendErr = sw.t(i18n.KeySumBusy)
			} else {
				sw.pendErr = m.Error
			}
		} else if m.Type == model.MsgSummary && m.Summary != nil {
			cp := *m.Summary
			sw.pending = &cp
		} else {
			sw.pendErr = sw.t(i18n.KeySumEmpty)
		}
		sw.mu.Unlock()
		if winapi.IsWindow(sw.hwnd) {
			winapi.PostMessage(sw.hwnd, msgSumApply, 0, 0)
		}
	}()
}

func (sw *summaryWindow) onApply() {
	sw.mu.Lock()
	sum, errText, busy := sw.pending, sw.pendErr, sw.busy
	sw.pending = nil
	sw.mu.Unlock()
	if busy {
		winapi.SetWindowText(sw.info, errText)
		winapi.SetTimer(sw.hwnd, sumRetryTimer, 5000)
		return
	}
	if sum == nil {
		winapi.SetWindowText(sw.info, errText)
		return
	}
	sw.chartModel = trayui.BuildSummaryChart(*sum)
	cards := trayui.BuildSummaryCards(*sum)
	info := fmt.Sprintf(sw.t(i18n.KeySumHours), 24)
	if sum.Truncated {
		info += " — " + sw.t(i18n.KeySumTruncated)
	}
	if len(sum.Profiles) == 0 {
		info = sw.t(i18n.KeySumEmpty)
	} else {
		info += fmt.Sprintf(" · %d", len(sum.Profiles))
	}
	winapi.SetWindowText(sw.info, info)
	var lines []string
	for _, c := range cards {
		line := c.Name
		if c.FreshnessMin != nil {
			line += fmt.Sprintf(" · %d мин", *c.FreshnessMin)
		}
		lines = append(lines, line)
	}
	if sw.cards != 0 {
		winapi.SetWindowText(sw.cards, strings.Join(lines, "\r\n"))
	}
	if sw.chart != 0 {
		winapi.InvalidateRect(sw.chart, true)
	}
}
