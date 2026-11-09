//go:build windows

package wintray

import (
	"context"
	"io"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// flashService adds dismiss_flash (ipc.FlashDismisser, capability flash)
// to the alert fake: not_found (monitor.unknown_event) for an unknown id.
type flashService struct {
	*alertService
	fmu       sync.Mutex
	known     map[uint64]bool
	dismissed []uint64
}

func (f *flashService) DismissFlash(id uint64) error {
	f.fmu.Lock()
	defer f.fmu.Unlock()
	if !f.known[id] {
		return &model.IPCError{Code: model.ErrCodeNotFound, Key: model.MsgMonitorUnknownEvent, Message: "unknown event"}
	}
	f.dismissed = append(f.dismissed, id)
	return nil
}

func (f *flashService) nDismissed() int { f.fmu.Lock(); defer f.fmu.Unlock(); return len(f.dismissed) }

type memFlashStore struct {
	mu sync.Mutex
	on bool
}

func (s *memFlashStore) Load() (bool, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.on, nil }
func (s *memFlashStore) Save(on bool) error  { s.mu.Lock(); defer s.mu.Unlock(); s.on = on; return nil }

func startFlashApp(t *testing.T, svc *flashService, store ScreenFlashStore) (*App, *ipc.Server) {
	t.Helper()
	l := ipc.NewMemListener()
	srv := ipc.NewServer(svc, nil)
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	dial := func(context.Context) (io.ReadWriteCloser, error) { return l.Dial() }
	app, err := NewApp(Options{NoNotifyIcon: true, HiddenAlerts: true, HiddenPanel: true, NoBlink: true, HiddenFlash: true,
		Beeper: &testBeeper{}, ScreenFlashStore: store,
		Session: trayipc.Options{Dial: dial, BackoffMin: 10 * time.Millisecond, BackoffMax: 20 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Exit)
	pumpUntil(t, "connected", func() bool { return app.Status().State == trayipc.StateConnected })
	return app, srv
}

// TestCHK_TRAY_95_ScreenFlash — full-screen flashing (capability flash,
// real ipc.Server pushes): a flash push starts the black overlay over the
// virtual screen (all monitors), topmost, click-through, never activated
// (no focus steal), blinking on its own WM_TIMER; repeated pushes of the
// same alert keep one entry; ack_alert does not stop it; flash_stop of one
// point keeps the others; «Закрыть» of the alert window sends
// dismiss_flash {eventId} and stops it; «Мигание экрана» off = no overlay.
func TestCHK_TRAY_95_ScreenFlash(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &flashService{alertService: &alertService{}, known: map[uint64]bool{7: true, 8: true}}
	store := &memFlashStore{on: true}
	app, srv := startFlashApp(t, svc, store)
	if !app.Status().Has(model.CapFlash) {
		t.Fatalf("capability flash: %v", app.Status().Capabilities)
	}
	fg := winapi.GetForegroundWindow()
	srv.Flash(model.FlashEvent{ProfileID: "p1", EventID: 7, Level: model.LevelError})
	pumpUntil(t, "flash on", func() bool { return app.FlashActive() == 1 && app.Flash() != nil && app.Flash().timer })
	fw := app.Flash()
	ex := winapi.WindowExStyle(fw.hwnd)
	for _, bit := range []uint32{winapi.WS_EX_NOACTIVATE, winapi.WS_EX_TRANSPARENT, winapi.WS_EX_TOOLWINDOW, winapi.WS_EX_TOPMOST, winapi.WS_EX_LAYERED} {
		if ex&bit == 0 {
			t.Errorf("ex style %x lacks %x", ex, bit)
		}
	}
	if r, v := winapi.GetWindowRect(fw.hwnd), winapi.VirtualScreen(); r != v {
		t.Errorf("overlay %+v, virtual screen %+v", r, v)
	}
	if st := winapi.WindowStyle(fw.hwnd); st&(winapi.WS_VSCROLL|winapi.WS_HSCROLL) != 0 || st&winapi.WS_POPUP == 0 {
		t.Errorf("overlay style %x: a plain popup without scroll bars", st)
	}
	if c := winapi.GetClientRect(fw.hwnd); c.Right-c.Left != winapi.GetSystemMetrics(winapi.SM_CXVIRTUALSCREEN) || c.Bottom-c.Top != winapi.GetSystemMetrics(winapi.SM_CYVIRTUALSCREEN) {
		t.Errorf("overlay client %+v is not the whole virtual screen", c)
	}
	if winapi.GetForegroundWindow() == fw.hwnd || (fg != 0 && winapi.GetForegroundWindow() != fg) {
		t.Error("overlay took the focus")
	}
	n := fw.phases
	pumpFor(4 * FlashHalfMs * time.Millisecond)
	if fw.phases-n < 2 {
		t.Errorf("blink phases %d", fw.phases-n)
	}
	// every check repeats the push: still one alert; a point alert adds one
	srv.Flash(model.FlashEvent{ProfileID: "p1", EventID: 7, Level: model.LevelError})
	srv.Flash(model.FlashEvent{ProfileID: "p1", PointKey: "pt", EventID: 8, Level: model.LevelWarning})
	pumpUntil(t, "two flashes", func() bool { return app.FlashActive() == 2 })
	// «Подтвердить» = ack_alert: no stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, _ = app.session.Do(ctx, model.IPCRequest{Cmd: model.CmdAckAlert, ProfileID: "p1"})
	cancel()
	pumpFor(50 * time.Millisecond)
	if app.FlashActive() != 2 || !fw.timer {
		t.Fatal("ack_alert stopped the flash")
	}
	// recovery of the point
	srv.Flash(model.FlashEvent{ProfileID: "p1", PointKey: "pt", Stop: true})
	pumpUntil(t, "point stop", func() bool { return app.FlashActive() == 1 && fw.timer })
	// «Закрыть» → dismiss_flash {eventId}
	app.closeAlerts()
	if app.FlashActive() != 0 || fw.timer || fw.on {
		t.Fatal("«Закрыть» did not stop the flash")
	}
	pumpUntil(t, "dismiss_flash", func() bool { return svc.nDismissed() == 1 })
	svc.fmu.Lock()
	if svc.dismissed[0] != 7 {
		t.Errorf("dismissed %v", svc.dismissed)
	}
	svc.fmu.Unlock()
	// unknown event: not_found / monitor.unknown_event; no eventId: invalid_request
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	m, err := app.session.Do(ctx, model.IPCRequest{Cmd: model.CmdDismissFlash, EventID: 99})
	cancel()
	if err != nil || m.Type != model.MsgError || m.Code != model.ErrCodeNotFound || m.ErrorKey != model.MsgMonitorUnknownEvent {
		t.Errorf("unknown event %+v %v", m, err)
	}
	// «Мигание экрана» off: pushes are tracked, no overlay
	store.Save(false)
	srv.Flash(model.FlashEvent{ProfileID: "p2", EventID: 8, Level: model.LevelError})
	pumpUntil(t, "tracked", func() bool { return app.FlashActive() == 1 })
	if fw.timer || fw.on {
		t.Error("overlay with «Мигание экрана» off")
	}
}
