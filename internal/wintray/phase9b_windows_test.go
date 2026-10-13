//go:build windows

package wintray

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// p9Service is a fake phase 9 service: check_now, events_since, history.
type p9Service struct {
	alertService
	m        sync.Mutex
	checks   int
	rate     int64 // retryAfterMs of the next check_now (0 = accept)
	evReqs   []model.IPCRequest
	evReply  func(run string, since uint64) model.EventsPage
	histReqs []model.IPCRequest
	hist     func(ctx context.Context, req model.IPCRequest) (model.HistoryPage, error)
	canceled int
}

func (f *p9Service) CheckNow(string) (model.CheckResult, error) {
	f.m.Lock()
	defer f.m.Unlock()
	f.checks++
	if f.rate > 0 {
		return model.CheckResult{}, &model.IPCError{Code: model.ErrCodeRateLimited, Key: model.MsgCheckRateLimited, Message: "rate", RetryAfterMs: f.rate}
	}
	return model.CheckResult{CheckSeq: f.Snapshot().Seq}, nil
}

func (f *p9Service) EventsSince(run string, since uint64, limit int) model.EventsPage {
	f.m.Lock()
	f.evReqs = append(f.evReqs, model.IPCRequest{RunID: run, SinceID: since})
	r := f.evReply
	f.m.Unlock()
	if r == nil {
		return model.EventsPage{RunID: "r1", LastID: 5, ResetRequired: run != "r1"}
	}
	return r(run, since)
}

func (f *p9Service) History(ctx context.Context, req model.IPCRequest) (model.HistoryPage, error) {
	f.m.Lock()
	f.histReqs = append(f.histReqs, req)
	h := f.hist
	f.m.Unlock()
	p, err := h(ctx, req)
	if ctx.Err() != nil {
		f.m.Lock()
		f.canceled++
		f.m.Unlock()
	}
	return p, err
}

func (f *p9Service) snapEvReqs() []model.IPCRequest {
	f.m.Lock()
	defer f.m.Unlock()
	return append([]model.IPCRequest(nil), f.evReqs...)
}

func (f *p9Service) snapHist() ([]model.IPCRequest, int) {
	f.m.Lock()
	defer f.m.Unlock()
	return append([]model.IPCRequest(nil), f.histReqs...), f.canceled
}

func startP9App(t *testing.T, svc *p9Service) (*App, *ipc.Server) {
	t.Helper()
	l := ipc.NewMemListener()
	srv := ipc.NewServer(svc, nil)
	srv.History = svc // history capability (EC Service sets it in cmd/exchsvc)
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	dial := func(context.Context) (io.ReadWriteCloser, error) { return l.Dial() }
	app, err := NewApp(Options{Lang: i18n.RU, NoNotifyIcon: true, HiddenAlerts: true, HiddenStatus: true, Beeper: &testBeeper{},
		Session: trayipc.Options{Dial: dial, BackoffMin: 10 * time.Millisecond, BackoffMax: 20 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Exit)
	pumpUntil(t, "connected", func() bool { return app.connected() && app.Status().Snapshot != nil })
	return app, srv
}

func rightItem(app *App, id int) trayui.Item {
	for _, it := range app.RightMenu() {
		if it.ID == id {
			return it
		}
	}
	return trayui.Item{}
}

// TestCHK_TRAY_22_CheckNowCapability — with check_now announced the
// button and the menu item wait for the status with lastCheckSeq >=
// checkSeq; rate_limited shows the seconds.
func TestCHK_TRAY_22_CheckNowCapability(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &p9Service{alertService: alertService{snap: phase9Snapshot()}}
	app, srv := startP9App(t, svc)
	defer app.Exit()
	app.opt.Notify = func(string, bool) {} // no MessageBox in tests
	if !app.session.Supported(model.CmdCheckNow) {
		t.Fatal("check_now capability")
	}
	sw := app.OpenStatus()
	app.Command(trayui.CmdCheckNow)
	pumpUntil(t, "check_now reply", func() bool { return len(app.checkResults) == 1 })
	if app.checkResults[0] != "Проверка выполняется…" || !app.CheckBusy() || winapi.IsWindowEnabled(sw.check) || !rightItem(app, trayui.CmdCheckNow).Disabled {
		t.Fatalf("waiting: %q busy=%v", app.checkResults, app.CheckBusy())
	}
	snap := phase9Snapshot()
	snap.Seq, snap.LastCheckSeq = 2, 0
	srv.PublishStatus(snap) // another poll, not ours
	pumpFor(100 * time.Millisecond)
	if !app.CheckBusy() {
		t.Fatal("must wait for lastCheckSeq")
	}
	snap.Seq, snap.LastCheckSeq = 3, 1
	srv.PublishStatus(snap)
	pumpUntil(t, "result status", func() bool { return !app.CheckBusy() })
	if app.checkResults[len(app.checkResults)-1] != "Проверка выполнена" || !winapi.IsWindowEnabled(sw.check) {
		t.Errorf("done %q", app.checkResults)
	}
	svc.m.Lock()
	svc.rate = 25500
	svc.m.Unlock()
	time.Sleep(model.CheckConnInterval) // the per-connection limit of the service
	winapi.SendMessage(sw.check, winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "rate limited", func() bool { return len(app.checkResults) == 3 })
	if app.checkResults[2] != "Проверка уже запрашивалась недавно, повторите через 26 с" || app.CheckBusy() {
		t.Errorf("rate %q", app.checkResults[2])
	}
	if winapi.GetWindowText(sw.info) != app.checkResults[2] {
		t.Errorf("info %q", winapi.GetWindowText(sw.info))
	}
}

// TestCHK_TRAY_23_ServerClock — «N мин назад» counts from serverNow.
func TestCHK_TRAY_23_ServerClock(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	snap := phase9Snapshot()
	snap.ServerNow = time.Now().Add(10 * time.Minute) // the service clock is 10 min ahead
	svc := &p9Service{alertService: alertService{snap: snap}}
	app, _ := startP9App(t, svc)
	defer app.Exit()
	sw := app.OpenStatus()
	got := ""
	for i := range sw.rows {
		if sw.rows[i].Point == "Склад" {
			got = sw.rows[i].Ago
		}
	}
	if got != "47 мин назад" && got != "48 мин назад" {
		t.Fatalf("ago %q (offset %v)", got, app.clock.Offset)
	}
}

// TestCHK_TRAY_24_BalloonDebounce — overdue → recovery → overdue within
// the window gives one balloon; a confirmed recovery comes after it.
func TestCHK_TRAY_24_BalloonDebounce(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &p9Service{alertService: alertService{snap: phase9Snapshot()}}
	app, srv := startP9App(t, svc)
	defer app.Exit()
	app.opt.BalloonDebounce = 400 * time.Millisecond
	ev := func(id uint64, from, to model.AlertLevel) model.AlertEvent {
		return model.AlertEvent{ProfileID: "a", ProfileName: "ERP", PointName: "Склад", Key: "k", From: from, To: to, LagMinutes: 40, RunID: "r1", EventID: id, ID: id}
	}
	srv.Alert(ev(6, model.LevelOK, model.LevelWarning))
	pumpUntil(t, "overdue", func() bool { return len(app.Balloons()) == 1 })
	srv.Alert(ev(7, model.LevelWarning, model.LevelOK))
	pumpFor(50 * time.Millisecond)
	srv.Alert(ev(8, model.LevelOK, model.LevelWarning))
	srv.Alert(ev(8, model.LevelOK, model.LevelWarning)) // duplicate push
	pumpFor(600 * time.Millisecond)
	if n := len(app.Balloons()); n != 1 {
		t.Fatalf("flap gave %d balloons: %+v", n, app.Balloons())
	}
	srv.Alert(ev(9, model.LevelWarning, model.LevelOK))
	pumpFor(150 * time.Millisecond)
	if len(app.Balloons()) != 1 {
		t.Fatal("recovery must be held")
	}
	pumpUntil(t, "confirmed recovery", func() bool { return len(app.Balloons()) == 2 })
	if !app.Balloons()[1].Recovered {
		t.Errorf("recovery %+v", app.Balloons()[1])
	}
}

// TestCHK_TRAY_25_EventsSince — the first connection only learns the
// position; a reconnection replays the missed transitions once (dedup by
// runId / eventId), resetRequired replays nothing.
func TestCHK_TRAY_25_EventsSince(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &p9Service{alertService: alertService{snap: phase9Snapshot()}}
	app, srv := startP9App(t, svc)
	defer app.Exit()
	app.opt.BalloonDebounce = -1
	pumpUntil(t, "positioning", func() bool { return len(svc.snapEvReqs()) == 1 && app.tracker.RunID == "r1" })
	if r := svc.snapEvReqs()[0]; r.RunID != "" || app.tracker.LastID != 5 || len(app.caughtUp) != 0 {
		t.Fatalf("first %+v last %d", r, app.tracker.LastID)
	}
	ev := func(id uint64, name string, from, to model.AlertLevel) model.AlertEvent {
		return model.AlertEvent{ProfileID: "a", ProfileName: "ERP", PointName: name, Key: name, From: from, To: to, RunID: "r1", EventID: id, ID: id}
	}
	srv.Alert(ev(6, "Склад", model.LevelOK, model.LevelWarning))
	pumpUntil(t, "push", func() bool { return app.tracker.LastID == 6 && len(app.Balloons()) == 1 })
	svc.m.Lock()
	svc.evReply = func(run string, since uint64) model.EventsPage {
		last := uint64(8)
		return model.EventsPage{RunID: "r1", LastID: last, Events: []model.AlertEvent{
			ev(6, "Склад", model.LevelOK, model.LevelWarning), // already seen
			ev(7, "Склад", model.LevelWarning, model.LevelOK),
			ev(8, "Касса", model.LevelOK, model.LevelError)}}
	}
	svc.m.Unlock()
	app.onConnected() // as after a reconnection
	pumpUntil(t, "catch-up", func() bool { return len(app.caughtUp) == 2 })
	if r := svc.snapEvReqs()[1]; r.RunID != "r1" || r.SinceID != 6 {
		t.Errorf("request %+v", r)
	}
	if app.caughtUp[0].EventID != 7 || app.caughtUp[1].EventID != 8 || app.tracker.LastID != 8 || len(app.Balloons()) != 2 {
		t.Errorf("caught %+v balloons %d", app.caughtUp, len(app.Balloons()))
	}
	svc.m.Lock()
	svc.evReply = func(string, uint64) model.EventsPage {
		return model.EventsPage{RunID: "r2", LastID: 3, ResetRequired: true}
	}
	svc.m.Unlock()
	app.onConnected()
	pumpUntil(t, "reset", func() bool { return app.tracker.RunID == "r2" })
	if len(app.caughtUp) != 2 || app.tracker.LastID != 3 || len(app.Balloons()) != 2 {
		t.Errorf("reset replayed: %d %d", len(app.caughtUp), len(app.Balloons()))
	}
}

func p9Bucket(start time.Time, key string, n int, mn, mx int64, avg float64) model.HistoryBucket {
	return model.HistoryBucket{Start: start, Kind: model.HistoryKindPoint, ProfileID: "a", PointKey: key, Records: n,
		LagMin: &model.HistoryAgg{Min: mn, Max: mx, Avg: avg, Count: n}}
}

func pointRow(t *testing.T, sw *statusWindow, name string) int {
	t.Helper()
	for i, r := range sw.rows {
		if r.Point == name {
			return i
		}
	}
	t.Fatalf("no row %s", name)
	return -1
}

// TestCHK_TRAY_26_HistoryWindow — «История…» of a point: buckets 5m for a
// day / 1h for a week, pages by cursor (a bucket cut in two is merged),
// event marks, history.* texts, cancel on close, no history capability =
// unavailable.
func TestCHK_TRAY_26_HistoryWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &p9Service{alertService: alertService{snap: phase9Snapshot()}}
	now := time.Now()
	start := now.Add(-2 * time.Hour).Truncate(5 * time.Minute)
	to := model.LevelOK
	svc.hist = func(ctx context.Context, req model.IPCRequest) (model.HistoryPage, error) {
		if req.Bucket == "" {
			return model.HistoryPage{Records: []model.HistoryRecord{{Kind: model.HistoryKindRecovered, CheckedAt: start, PointKey: req.PointKey, To: &to}}}, nil
		}
		if req.Cursor == "" {
			return model.HistoryPage{Bucket: req.Bucket, Buckets: []model.HistoryBucket{p9Bucket(start, req.PointKey, 2, 10, 20, 15)}, HasMore: true, NextCursor: "c2", Truncated: true}, nil
		}
		return model.HistoryPage{Bucket: req.Bucket, Buckets: []model.HistoryBucket{p9Bucket(start, req.PointKey, 2, 30, 40, 35),
			p9Bucket(start.Add(5*time.Minute), req.PointKey, 1, 5, 5, 5)}}, nil
	}
	app, _ := startP9App(t, svc)
	defer app.Exit()
	sw := app.OpenStatus()
	if winapi.IsWindowEnabled(sw.history) {
		t.Error("history needs a selected point row")
	}
	i := pointRow(t, sw, "Склад")
	winapi.ListViewSelect(sw.list, i)
	PumpPending()
	sw.updateHistoryButton()
	if !winapi.IsWindowEnabled(sw.history) || sw.tips.text(sw.history) != i18n.T(i18n.KeyTipStHistory, i18n.RU) {
		t.Fatal("history button")
	}
	winapi.SendMessage(sw.history, winapi.BM_CLICK, 0, 0)
	hw := app.histWin
	if hw == nil {
		t.Fatal("no history window")
	}
	pumpUntil(t, "chart", func() bool { return hw.loaded })
	reqs, _ := svc.snapHist()
	if len(reqs) != 3 || reqs[0].Bucket != model.HistoryBucket5m || reqs[1].Cursor != "c2" || reqs[0].ProfileID != "a" || reqs[0].PointKey == "" ||
		reqs[2].Bucket != "" || len(reqs[2].Kinds) != 2 || reqs[0].To.Sub(*reqs[0].From) != 24*time.Hour {
		t.Fatalf("requests %+v", reqs)
	}
	if len(hw.model.Points) != 2 || hw.model.Points[0].Avg != 25 || hw.model.Points[0].Max != 40 || len(hw.model.Marks) != 1 ||
		hw.model.Warn != 0 && hw.model.Warn != sw.rows[i].WarnMin {
		t.Fatalf("model %+v", hw.model)
	}
	if !strings.HasPrefix(winapi.GetWindowText(hw.info), "Интервалов: 2, событий: 1") {
		t.Errorf("info %q", winapi.GetWindowText(hw.info))
	}
	winapi.SendMessage(hw.chart, winapi.WM_PAINT, 0, 0) // GDI smoke
	for c, k := range map[uintptr]string{hw.day: i18n.KeyTipHistDay, hw.week: i18n.KeyTipHistWeek, hw.chart: i18n.KeyTipHistChart,
		hw.info: i18n.KeyTipHistInfo, hw.csv: i18n.KeyTipHistExportCSV, hw.html: i18n.KeyTipHistExportHTML, hw.close: i18n.KeyTipHistClose} {
		if hw.tips.text(c) != i18n.T(k, i18n.RU) {
			t.Errorf("tooltip %s", k)
		}
	}
	winapi.SendMessage(hw.week, winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "week", func() bool { r, _ := svc.snapHist(); return len(r) == 6 && hw.loaded })
	if reqs, _ = svc.snapHist(); reqs[3].Bucket != model.HistoryBucket1h || reqs[3].To.Sub(*reqs[3].From) != 7*24*time.Hour {
		t.Errorf("week request %+v", reqs[3])
	}

	// history.* error text
	svc.m.Lock()
	svc.hist = func(context.Context, model.IPCRequest) (model.HistoryPage, error) {
		return model.HistoryPage{}, &model.IPCError{Code: model.ErrCodeBusy, Key: model.MsgHistoryBusy, Message: "busy"}
	}
	svc.m.Unlock()
	winapi.SendMessage(hw.day, winapi.BM_CLICK, 0, 0)
	want := model.MonitorMessage(model.MsgHistoryBusy, i18n.RU)
	pumpUntil(t, "busy text", func() bool { return winapi.GetWindowText(hw.info) == want })

	// closing the window cancels the running page (cancel {ref})
	block := make(chan struct{})
	svc.m.Lock()
	svc.hist = func(ctx context.Context, req model.IPCRequest) (model.HistoryPage, error) {
		close(block)
		<-ctx.Done()
		return model.HistoryPage{}, ctx.Err()
	}
	svc.m.Unlock()
	winapi.SendMessage(hw.week, winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "blocked", func() bool {
		select {
		case <-block:
			return true
		default:
			return false
		}
	})
	winapi.SendMessage(hw.close, winapi.BM_CLICK, 0, 0)
	if hw.alive() || app.histWin != nil {
		t.Fatal("window must close")
	}
	pumpUntil(t, "canceled", func() bool { _, c := svc.snapHist(); return c == 1 })
}

// TestCHK_TRAY_26_NoHistoryCapability — a service without history: the
// button stays unavailable and the window does not open.
func TestCHK_TRAY_26_NoHistoryCapability(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &alertService{snap: phase9Snapshot()}
	app, _ := startAlertApp(t, svc, &testBeeper{})
	defer app.Exit()
	app.opt.HiddenStatus = true
	pumpUntil(t, "status", func() bool { return app.connected() && app.Status().Snapshot != nil })
	sw := app.OpenStatus()
	winapi.ListViewSelect(sw.list, pointRow(t, sw, "Склад"))
	PumpPending()
	sw.updateHistoryButton()
	if winapi.IsWindowEnabled(sw.history) {
		t.Fatal("history must be unavailable")
	}
	sw.openHistory()
	if app.histWin != nil || winapi.GetWindowText(sw.info) != i18n.T(i18n.KeyStHistoryOff, i18n.RU) {
		t.Fatalf("opened / info %q", winapi.GetWindowText(sw.info))
	}
}

// TestCHK_TRAY_27_HistoryExport — every record page of the span goes to
// the CSV / HTML file.
func TestCHK_TRAY_27_HistoryExport(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &p9Service{alertService: alertService{snap: phase9Snapshot()}}
	at := time.Now().Add(-time.Hour)
	lag := 12
	svc.hist = func(ctx context.Context, req model.IPCRequest) (model.HistoryPage, error) {
		if req.Bucket != "" {
			return model.HistoryPage{}, nil
		}
		if len(req.Kinds) > 0 { // chart events
			return model.HistoryPage{}, nil
		}
		rec := func(n string) model.HistoryRecord {
			return model.HistoryRecord{Kind: model.HistoryKindPoint, CheckedAt: at, ProfileName: "ERP", PointName: n, LagMin: &lag}
		}
		if req.Cursor == "" {
			return model.HistoryPage{Records: []model.HistoryRecord{rec("p1"), rec("p2")}, HasMore: true, NextCursor: "n1", Truncated: true}, nil
		}
		return model.HistoryPage{Records: []model.HistoryRecord{rec("p3")}}, nil
	}
	app, _ := startP9App(t, svc)
	defer app.Exit()
	sw := app.OpenStatus()
	hw := app.OpenHistory(sw.rows[pointRow(t, sw, "Склад")])
	pumpUntil(t, "loaded", func() bool { return hw.loaded })
	dir := t.TempDir()
	hw.savePath = func(k exportKind) (string, bool) {
		if k == exportCSV {
			return filepath.Join(dir, "h.csv"), true
		}
		return filepath.Join(dir, "h.html"), true
	}
	winapi.SendMessage(hw.csv, winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "csv", func() bool { return hw.lastSaved != "" })
	c, _ := os.ReadFile(filepath.Join(dir, "h.csv"))
	if !strings.HasPrefix(string(c), "\uFEFFВремя;") || strings.Count(string(c), "\r\n") != 4 || !strings.Contains(string(c), ";p3;") {
		t.Fatalf("csv %q", c)
	}
	hw.lastSaved = ""
	winapi.SendMessage(hw.html, winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "html", func() bool { return hw.lastSaved != "" })
	h, _ := os.ReadFile(filepath.Join(dir, "h.html"))
	if strings.Count(string(h), "<td>ERP</td>") != 3 {
		t.Fatalf("html %s", h)
	}
	if !strings.HasPrefix(winapi.GetWindowText(hw.info), "Сохранено: ") {
		t.Errorf("info %q", winapi.GetWindowText(hw.info))
	}
}

// TestCHK_TRAY_28_AppLog — a balloon-switch read error goes to
// Options.Log (wired to tray.log in cmd/exchtray).
func TestCHK_TRAY_28_AppLog(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "cmd", "exchtray", "main_windows.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "traylog.Logger{Path: traylog.DefaultPath()}") || !strings.Contains(string(src), "Log:") {
		t.Error("cmd/exchtray must pass the tray log as Options.Log")
	}
}
