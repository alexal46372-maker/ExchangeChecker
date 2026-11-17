//go:build windows

package wintray

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// resumeProxy sits between the tray session and the real ipc.Server of
// the flash fake and plays the service side of resume_flash as agreed with
// EC Service (phase 17; the service implements it in feat/svc-phase17):
// the hello gains the capability resume_flash; resume_flash {eventId} of
// an event that is known, dismissed and not confirmed (no ack_alert for its
// profile / point) clears the dismiss mark, replies ok {resumed:true} and
// pushes flash with the same eventId through the real server; otherwise
// ok {resumed:false} and nothing happens; no eventId → invalid_request.
// Every other frame passes through unchanged.
type resumeProxy struct {
	l      *ipc.MemListener
	srv    *ipc.Server
	svc    *flashService
	events map[uint64]model.FlashEvent
	mu     sync.Mutex
	asked  []uint64           // resume_flash eventIds received
	pushed []uint64           // resumed (flash pushed)
	acks   []model.IPCRequest // ack_alert requests seen (forwarded)
}

func (p *resumeProxy) nAcks() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.acks) }

func (p *resumeProxy) dial(context.Context) (io.ReadWriteCloser, error) {
	up, err := p.l.Dial()
	if err != nil {
		return nil, err
	}
	cli, mid := net.Pipe()
	var wmu sync.Mutex
	write := func(v interface{}) error {
		wmu.Lock()
		defer wmu.Unlock()
		return ipc.WriteFrame(mid, v, model.IPCMaxMessage)
	}
	go func() { // service → tray
		defer mid.Close()
		for {
			var raw map[string]json.RawMessage
			if err := ipc.ReadFrame(up, &raw, model.IPCMaxMessage); err != nil {
				return
			}
			if string(raw["type"]) == `"`+model.MsgHello+`"` {
				var caps []string
				_ = json.Unmarshal(raw["capabilities"], &caps)
				caps = append(caps, trayipc.CapResumeFlash)
				raw["capabilities"], _ = json.Marshal(caps)
			}
			if write(raw) != nil {
				return
			}
		}
	}()
	go func() { // tray → service
		defer up.Close()
		for {
			var raw map[string]json.RawMessage
			if err := ipc.ReadFrame(mid, &raw, model.IPCMaxImportRequest); err != nil {
				return
			}
			var req model.IPCRequest
			b, _ := json.Marshal(raw)
			_ = json.Unmarshal(b, &req)
			if req.Cmd == model.CmdAckAlert {
				p.mu.Lock()
				p.acks = append(p.acks, req)
				p.mu.Unlock()
			}
			if req.Cmd != trayipc.CmdResumeFlash {
				if ipc.WriteFrame(up, raw, model.IPCMaxImportRequest) != nil {
					return
				}
				continue
			}
			if req.EventID == 0 {
				write(map[string]interface{}{"id": req.ID, "type": model.MsgError, "code": model.ErrCodeInvalidRequest, "error": "resume_flash needs eventId"})
				continue
			}
			ev, resumed := p.resume(req.EventID)
			write(map[string]interface{}{"id": req.ID, "type": model.MsgOK, "resumed": resumed})
			if resumed {
				p.srv.Flash(ev)
			}
		}
	}()
	return cli, nil
}

func (p *resumeProxy) resume(id uint64) (model.FlashEvent, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asked = append(p.asked, id)
	ev, ok := p.events[id]
	if !ok {
		return ev, false
	}
	for _, a := range p.acks {
		if (a.EventID != 0 && a.EventID == id) || (a.EventID == 0 && a.ProfileID == ev.ProfileID && (a.PointKey == "" || a.PointKey == ev.PointKey)) {
			return ev, false // confirmed
		}
	}
	p.svc.fmu.Lock()
	dismissed := false
	for i, d := range p.svc.dismissed {
		if d == id {
			dismissed = true
			p.svc.dismissed = append(p.svc.dismissed[:i:i], p.svc.dismissed[i+1:]...)
			break
		}
	}
	p.svc.fmu.Unlock()
	if !dismissed {
		return ev, false
	}
	p.pushed = append(p.pushed, id)
	return ev, true
}

func (p *resumeProxy) counts() (asked, pushed []uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uint64(nil), p.asked...), append([]uint64(nil), p.pushed...)
}

func (f *flashService) dismissedIDs() []uint64 {
	f.fmu.Lock()
	defer f.fmu.Unlock()
	out := append([]uint64(nil), f.dismissed...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func startResumeApp(t *testing.T, svc *flashService, withResume bool, events map[uint64]model.FlashEvent) (*App, *ipc.Server, *resumeProxy) {
	t.Helper()
	l := ipc.NewMemListener()
	srv := ipc.NewServer(svc, nil)
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	p := &resumeProxy{l: l, srv: srv, svc: svc, events: events}
	dial := p.dial
	if !withResume {
		dial = func(context.Context) (io.ReadWriteCloser, error) { return l.Dial() }
	}
	app, err := NewApp(Options{NoNotifyIcon: true, HiddenPanel: true, NoBlink: true, HiddenFlash: true,
		Beeper: &testBeeper{}, ScreenFlashStore: &memFlashStore{on: true},
		Session: trayipc.Options{Dial: dial, BackoffMin: 10 * time.Millisecond, BackoffMax: 20 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Exit)
	pumpUntil(t, "connected", func() bool { return app.Status().State == trayipc.StateConnected })
	return app, srv, p
}

func idsOf(reqs []string, tag string) string {
	var out []string
	for _, r := range reqs {
		if strings.HasPrefix(r, tag) {
			out = append(out, strings.TrimPrefix(r, tag))
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// TestCHK_TRAY_109_FlashFollowsAlertWindow — phase 17 (Alex 17.11.2026):
// the screen flash follows the alert window state, through the real alert
// window (WM_SYSCOMMAND / ShowWindow), the real trayipc Session and the real
// ipc.Server (resume_flash played by resumeProxy until feat/svc-phase17):
// SC_MINIMIZE stops the overlay at once and sends dismiss_flash for every
// active flash (the alert stays: the window, the alert view and the sound
// are untouched); a push of a paused event that crossed the dismiss does
// not restart it; a restore (SW_RESTORE, SC_RESTORE = the taskbar button)
// sends resume_flash and the service's flash push restarts the overlay;
// confirmed while restored or while minimized → never resumed (no
// resume_flash, no overlay); a new eventId while minimized restores the
// window and the flash (the paused ones resume too).
func TestCHK_TRAY_109_FlashFollowsAlertWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &flashService{alertService: &alertService{}, known: map[uint64]bool{7: true, 8: true, 12: true, 13: true, 14: true}}
	events := map[uint64]model.FlashEvent{
		7:  {ProfileID: "p1", EventID: 7, Level: model.LevelError},
		8:  {ProfileID: "p1", PointKey: "pt", EventID: 8, Level: model.LevelError},
		12: {ProfileID: "p2", EventID: 12, Level: model.LevelError},
		13: {ProfileID: "p3", EventID: 13, Level: model.LevelError},
		14: {ProfileID: "p4", EventID: 14, Level: model.LevelError},
	}
	alertOf := func(id uint64) model.AlertEvent {
		e := events[id]
		a := ptAlert(e.ProfileID, "Склад", e.PointKey, model.LevelError)
		if e.PointKey == "" {
			a = model.AlertEvent{ProfileID: e.ProfileID, ProfileName: "DB-" + e.ProfileID, To: model.LevelError, ErrorKey: model.MsgMonitorConnError}
		}
		a.ID = id
		return a
	}
	svc.set(1, alertOf(7), alertOf(8))
	app, srv, p := startResumeApp(t, svc, true, events)
	if !app.Status().Has(trayipc.CapResumeFlash) || !app.Session().Supported(trayipc.CmdResumeFlash) {
		t.Fatalf("capability resume_flash: %v", app.Status().Capabilities)
	}
	srv.Flash(events[7])
	srv.Flash(events[8])
	pumpUntil(t, "flash on + alert window", func() bool {
		return app.FlashActive() == 2 && app.Flash() != nil && app.Flash().timer &&
			app.AlertWindow() != 0 && winapi.IsWindowVisible(app.AlertWindow()) && app.AlertView().Visible
	})
	fw, hw := app.Flash(), app.AlertWindow()
	view := app.AlertView()

	minimize := func(tag string) {
		t.Helper()
		winapi.SendMessage(hw, winapi.WM_SYSCOMMAND, winapi.SC_MINIMIZE, 0)
		if !winapi.IsIconic(hw) {
			t.Fatalf("%s: the alert window is not minimized", tag)
		}
		// at once, before any message of the service
		if app.FlashActive() != 0 || fw.timer || fw.on || winapi.IsWindowVisible(fw.hwnd) {
			t.Fatalf("%s: minimize did not stop the flash: active %d timer %v on %v", tag, app.FlashActive(), fw.timer, fw.on)
		}
	}

	// 1. SC_MINIMIZE: stop + dismiss_flash for 7 and 8; the alert stays
	minimize("minimize 1")
	if on, n := app.FlashPaused(); !on || n != 2 {
		t.Fatalf("paused %v %d", on, n)
	}
	waitCond(t, "dismiss_flash 7, 8", func() bool { d := svc.dismissedIDs(); return len(d) == 2 && d[0] == 7 && d[1] == 8 })
	if v := app.AlertView(); !v.Visible || len(v.Alerts) != len(view.Alerts) || !winapi.IsWindow(hw) {
		t.Fatal("minimize dismissed the alert")
	}
	// a push of a paused event (sent before the dismiss arrived) is ignored
	srv.Flash(events[7])
	pumpFor(3 * FlashHalfMs * time.Millisecond)
	if app.FlashActive() != 0 || fw.timer || !winapi.IsIconic(hw) {
		t.Fatal("a crossing push of a paused event restarted the flash")
	}
	// 2. SW_RESTORE: resume_flash 7, 8 → the service pushes → flash
	winapi.ShowWindow(hw, winapi.SW_RESTORE)
	pumpUntil(t, "resumed after SW_RESTORE", func() bool { return app.FlashActive() == 2 && fw.timer })
	if got := idsOf(app.FlashRequests(), "resume:"); got != "7,8" {
		t.Fatalf("resume requests %q", got)
	}
	if on, _ := app.FlashPaused(); on {
		t.Fatal("still paused after restore")
	}
	// 3. minimize again, SC_RESTORE (taskbar button / system menu)
	minimize("minimize 2")
	waitCond(t, "dismiss again", func() bool { return len(svc.dismissedIDs()) == 2 })
	winapi.SendMessage(hw, winapi.WM_SYSCOMMAND, winapi.SC_RESTORE, 0)
	if winapi.IsIconic(hw) {
		t.Fatal("SC_RESTORE did not restore")
	}
	pumpUntil(t, "resumed after SC_RESTORE", func() bool { return app.FlashActive() == 2 && fw.timer })

	// 4. confirmed while restored: «Подтвердить» stops it; a later
	// minimize / restore never resumes it
	app.ackAlerts()
	if app.FlashActive() != 0 || fw.timer {
		t.Fatal("«Подтвердить» did not stop the flash")
	}
	pumpUntil(t, "ack sent", func() bool { return p.nAcks() >= 1 })
	reqs := len(app.FlashRequests())
	if winapi.IsWindowVisible(hw) {
		minimize("minimize after ack")
		winapi.ShowWindow(hw, winapi.SW_RESTORE)
	} else {
		app.alertMinimized()
		app.alertRestored()
	}
	pumpFor(3 * FlashHalfMs * time.Millisecond)
	if app.FlashActive() != 0 || fw.timer || len(app.FlashRequests()) != reqs {
		t.Fatalf("confirmed alert resumed: active %d, requests %v", app.FlashActive(), app.FlashRequests()[reqs:])
	}

	// 5. confirmed while minimized: a new alert 12 flashes, the window is
	// minimized, «Подтвердить» comes (keyboard / another path) → restore
	// sends no resume_flash 12, no flash
	srv.PublishStatus(svc.set(20, alertOf(12)))
	srv.Alert(alertOf(12))
	srv.Flash(events[12])
	pumpUntil(t, "alert 12 shown + flash", func() bool {
		v := app.AlertView()
		return v.Visible && v.CanAck && app.FlashActive() == 1 && fw.timer && winapi.IsWindowVisible(hw) && !winapi.IsIconic(hw)
	})
	minimize("minimize 12")
	waitCond(t, "dismiss 12", func() bool { d := svc.dismissedIDs(); return len(d) == 1 && d[0] == 12 })
	acks := p.nAcks()
	app.ackAlerts()
	pumpUntil(t, "ack 12 sent", func() bool { return p.nAcks() > acks })
	if _, n := app.FlashPaused(); n != 0 {
		t.Errorf("confirmed 12 still paused (%d)", n)
	}
	reqs = len(app.FlashRequests())
	winapi.ShowWindow(hw, winapi.SW_RESTORE)
	pumpFor(3 * FlashHalfMs * time.Millisecond)
	if app.FlashActive() != 0 || fw.timer || len(app.FlashRequests()) != reqs {
		t.Fatalf("confirmed-while-minimized resumed: active %d requests %v", app.FlashActive(), app.FlashRequests()[reqs:])
	}
	if a, _ := p.counts(); len(a) != 4 {
		t.Errorf("resume_flash asked %v (want only 7, 8 twice)", a)
	}

	// 6. a new eventId while minimized restores the window and the flash:
	// 13 flashes and is paused, then alert 14 arrives
	srv.PublishStatus(svc.set(30, alertOf(13)))
	srv.Alert(alertOf(13))
	srv.Flash(events[13])
	pumpUntil(t, "alert 13 shown + flash", func() bool {
		return app.AlertView().Visible && app.FlashActive() == 1 && fw.timer && winapi.IsWindowVisible(hw)
	})
	if winapi.IsIconic(hw) {
		winapi.ShowWindow(hw, winapi.SW_RESTORE)
		pumpUntil(t, "13 flashing", func() bool { return app.FlashActive() == 1 && fw.timer })
	}
	minimize("minimize 13")
	waitCond(t, "dismiss 13", func() bool {
		for _, d := range svc.dismissedIDs() {
			if d == 13 {
				return true
			}
		}
		return false
	})
	srv.Flash(events[14]) // the first push of the new alert
	pumpUntil(t, "window restored + 13, 14 flash", func() bool {
		return !winapi.IsIconic(hw) && app.FlashActive() == 2 && fw.timer
	})
	if !strings.Contains(","+idsOf(app.FlashRequests(), "resume:")+",", ",13,") {
		t.Errorf("13 not resumed: %v", app.FlashRequests())
	}
	if on, _ := app.FlashPaused(); on {
		t.Error("still paused after the new alert")
	}
}

// TestCHK_TRAY_109_OldServiceKeepsFlashing — without the capability
// resume_flash (a service before feat/svc-phase17) minimizing the alert
// window changes nothing: the flash goes on, no dismiss_flash is sent.
func TestCHK_TRAY_109_OldServiceKeepsFlashing(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &flashService{alertService: &alertService{}, known: map[uint64]bool{7: true}}
	a := model.AlertEvent{ID: 7, ProfileID: "p1", ProfileName: "DB-p1", To: model.LevelError, ErrorKey: model.MsgMonitorConnError}
	svc.set(1, a)
	app, srv, _ := startResumeApp(t, svc, false, nil)
	if app.Status().Has(trayipc.CapResumeFlash) || app.Session().Supported(trayipc.CmdResumeFlash) {
		t.Fatal("resume_flash without the capability")
	}
	srv.Flash(model.FlashEvent{ProfileID: "p1", EventID: 7, Level: model.LevelError})
	pumpUntil(t, "flash + window", func() bool {
		return app.FlashActive() == 1 && app.Flash() != nil && app.Flash().timer && app.AlertWindow() != 0 && winapi.IsWindowVisible(app.AlertWindow())
	})
	hw := app.AlertWindow()
	winapi.SendMessage(hw, winapi.WM_SYSCOMMAND, winapi.SC_MINIMIZE, 0)
	pumpFor(3 * FlashHalfMs * time.Millisecond)
	if !winapi.IsIconic(hw) || app.FlashActive() != 1 || !app.Flash().timer || svc.nDismissed() != 0 || len(app.FlashRequests()) != 0 {
		t.Fatalf("old service: active %d timer %v dismissed %d requests %v", app.FlashActive(), app.Flash().timer, svc.nDismissed(), app.FlashRequests())
	}
	if on, _ := app.FlashPaused(); on {
		t.Fatal("paused without resume_flash")
	}
	winapi.ShowWindow(hw, winapi.SW_RESTORE)
	pumpFor(2 * FlashHalfMs * time.Millisecond)
	if app.FlashActive() != 1 || len(app.FlashRequests()) != 0 {
		t.Fatal("restore changed the flash of an old service")
	}
}
