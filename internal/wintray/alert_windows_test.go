//go:build windows

package wintray

import (
	"context"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

type ackCall struct{ profile, key string }

// alertService is a fake version-2 service with active alerts and
// ack_alert.
type alertService struct {
	mu    sync.Mutex
	snap  model.StatusSnapshot
	calls []ackCall
	// fail: reply this error code to every ack_alert
	fail string
}

func (f *alertService) Snapshot() model.StatusSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.snap
	s.Alerts = append([]model.AlertEvent(nil), f.snap.Alerts...)
	return s
}
func (f *alertService) PollNow(string) (bool, error) { return false, nil }
func (f *alertService) ReloadConfig() bool           { return false }
func (f *alertService) Heartbeat() model.Heartbeat {
	return model.Heartbeat{Protocol: 2, ServiceStartedAt: time.Unix(100, 0)}
}
func (f *alertService) AckAlert(profile, key string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ackCall{profile, key})
	switch f.fail {
	case model.ErrCodeNoActiveAlert:
		return 0, &model.IPCError{Code: model.ErrCodeNoActiveAlert, Key: model.MsgMonitorNoActiveAlert, Message: "no active alert"}
	case model.ErrCodeNotFound:
		return 0, &model.IPCError{Code: model.ErrCodeNotFound, Key: model.MsgMonitorUnknownProfile, Message: "unknown profile"}
	}
	n := 0
	for i, a := range f.snap.Alerts {
		if a.ProfileID == profile && (key == "" || a.Key == key) && !a.Acknowledged {
			f.snap.Alerts[i].Acknowledged = true
			n++
		}
	}
	if n == 0 {
		return 0, &model.IPCError{Code: model.ErrCodeNoActiveAlert, Key: model.MsgMonitorNoActiveAlert, Message: "no active alert"}
	}
	f.snap.Seq++
	return n, nil
}

func (f *alertService) ackCalls() []ackCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ackCall(nil), f.calls...)
}

func (f *alertService) set(seq uint64, alerts ...model.AlertEvent) model.StatusSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snap.Seq = seq
	f.snap.Alerts = alerts
	s := f.snap
	s.Alerts = append([]model.AlertEvent(nil), alerts...)
	return s
}

type testBeeper struct {
	mu    sync.Mutex
	freqs []int
}

func (b *testBeeper) Beep(freq, dur int) error {
	b.mu.Lock()
	b.freqs = append(b.freqs, freq)
	b.mu.Unlock()
	time.Sleep(time.Duration(dur) * time.Millisecond)
	return nil
}

func (b *testBeeper) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.freqs)
}

func (b *testBeeper) has(freq int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, f := range b.freqs {
		if f == freq {
			return true
		}
	}
	return false
}

var alertFresh = time.Date(2026, 10, 3, 12, 0, 0, 0, time.FixedZone("UTC+3", 3*3600))

func ptAlert(profile, name, key string, lvl model.AlertLevel) model.AlertEvent {
	return model.AlertEvent{ProfileID: profile, ProfileName: "DB-" + profile, PointName: name, PlanTable: "_Node1",
		Key: key, To: lvl, FreshAt: &alertFresh, LagMinutes: 70}
}

func startAlertApp(t *testing.T, svc *alertService, b *testBeeper) (*App, *ipc.Server) {
	t.Helper()
	l := ipc.NewMemListener()
	srv := ipc.NewServer(svc, nil)
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	dial := func(context.Context) (io.ReadWriteCloser, error) { return l.Dial() }
	app, err := NewApp(Options{Lang: i18n.RU, NoNotifyIcon: true, HiddenAlerts: true, Beeper: b,
		Session: trayipc.Options{Dial: dial, BackoffMin: 10 * time.Millisecond, BackoffMax: 20 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Exit)
	return app, srv
}

// pumpFor dispatches messages for d (lets the beep loop / pushes run).
func pumpFor(d time.Duration) {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		PumpPending()
		time.Sleep(2 * time.Millisecond)
	}
}

// TestCHK_TRAY_03_AlertWindowAck — an active alert opens the (hidden in
// tests) topmost alert window and beeps with the error tone from the
// settings; BM_CLICK on «Подтвердить» sends ack_alert (profile-level alert
// as a whole, points by pointKey), the window closes and the beep stops; a
// new alert reopens it; WM_CLOSE only mutes until the next change.
func TestCHK_TRAY_03_AlertWindowAck(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	set := model.DefaultSettings()
	set.Beep.ErrorFreqHz, set.Beep.ErrorDurMs, set.Beep.WarningFreqHz, set.Beep.WarningDurMs, set.Beep.GapMs = 1234, 5, 777, 5, 5
	svc := &alertService{snap: model.StatusSnapshot{Settings: &set}}
	svc.set(1,
		ptAlert("a", "Склад", "_Node1/Склад", model.LevelError),
		model.AlertEvent{ProfileID: "b", ProfileName: "DB-b", To: model.LevelWarning, ErrorKey: model.MsgMonitorConnError},
		ptAlert("b", "Филиал", "_Node1/Филиал", model.LevelWarning))
	b := &testBeeper{}
	app, srv := startAlertApp(t, svc, b)

	pumpUntil(t, "alert window + error beep", func() bool {
		v := app.AlertView()
		return v.Visible && v.Sound && app.AlertWindow() != 0 && b.has(1234)
	})
	h := app.AlertWindow()
	if title := winapi.GetWindowText(h); title != "Exchange Checker — ошибка обмена" {
		t.Errorf("title %q", title)
	}
	text := winapi.GetWindowText(winapi.GetDlgItem(h, idAlertText))
	for _, want := range []string{"DB-a — Склад: ошибка", "DB-b: предупреждение", "DB-b — Филиал"} {
		if !strings.Contains(text, want) {
			t.Errorf("alert text %q lacks %q", text, want)
		}
	}
	ack := winapi.GetDlgItem(h, idAlertAck)
	if ack == 0 || !winapi.IsWindowEnabled(ack) || winapi.GetWindowText(ack) != "Подтвердить" {
		t.Fatalf("ack button %x %q", ack, winapi.GetWindowText(ack))
	}
	if winapi.GetWindowText(winapi.GetDlgItem(h, idAlertClose)) != "Закрыть" {
		t.Error("close button")
	}

	winapi.SendMessage(ack, winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "acknowledged and closed", func() bool {
		return len(svc.ackCalls()) == 2 && !app.AlertView().Visible
	})
	calls := svc.ackCalls()
	got := map[ackCall]bool{}
	for _, c := range calls {
		got[c] = true
	}
	if !got[ackCall{"b", ""}] || !got[ackCall{"a", "_Node1/Склад"}] {
		t.Errorf("ack_alert calls %+v", calls)
	}
	if app.AlertView().Sound {
		t.Error("sound must stop after ack")
	}
	pumpFor(100 * time.Millisecond)
	n := b.count()
	pumpFor(200 * time.Millisecond)
	if b.count() > n+1 {
		t.Errorf("beeping after ack: %d -> %d", n, b.count())
	}

	// a new alert -> window and warning beep again
	acked := svc.Snapshot().Alerts
	newAlert := ptAlert("c", "Магазин", "_Node1/Магазин", model.LevelWarning)
	srv.PublishStatus(svc.set(10, append(acked, newAlert)...))
	pumpUntil(t, "new alert reopens", func() bool {
		v := app.AlertView()
		return v.Visible && v.Sound && len(v.Alerts) == 1 && b.has(777)
	})

	// WM_CLOSE: hidden + muted, window kept, alert not acknowledged
	winapi.SendMessage(h, winapi.WM_CLOSE, 0, 0)
	if v := app.AlertView(); v.Visible || v.Sound || len(v.Alerts) != 1 || !winapi.IsWindow(h) {
		t.Fatalf("after close: %+v", v)
	}
	if len(svc.ackCalls()) != 2 {
		t.Error("closing must not acknowledge")
	}
	srv.PublishStatus(svc.set(11, append(acked, newAlert)...))
	pumpFor(150 * time.Millisecond)
	if v := app.AlertView(); v.Visible || v.Sound {
		t.Fatal("same status reopened the muted window")
	}
	worse := newAlert
	worse.To = model.LevelError
	srv.PublishStatus(svc.set(12, append(acked, worse)...))
	pumpUntil(t, "escalation reopens", func() bool { v := app.AlertView(); return v.Visible && v.Sound })

	// already visible: a new alert and a warning -> error escalation bring
	// the window to the front again; the same status does not
	raised := app.awin.raised
	another := ptAlert("d", "Офис", "_Node1/Офис", model.LevelWarning)
	srv.PublishStatus(svc.set(13, append(acked, worse, another)...))
	pumpUntil(t, "new alert while visible raises", func() bool { return app.awin.raised == raised+1 })
	srv.PublishStatus(svc.set(14, append(acked, worse, another)...))
	pumpFor(150 * time.Millisecond)
	if app.awin.raised != raised+1 {
		t.Error("unchanged status raised the window")
	}
	another.To = model.LevelError
	srv.PublishStatus(svc.set(15, append(acked, worse, another)...))
	pumpUntil(t, "escalation while visible raises", func() bool { return app.awin.raised == raised+2 })
	if !app.AlertView().Visible {
		t.Error("window hidden")
	}
}

// TestCHK_TRAY_03_AckGone — no_active_alert / not_found replies (alarm
// already cleared or acknowledged elsewhere, profile deleted): the window
// closes, the sound stops, nothing crashes, the status is re-read.
func TestCHK_TRAY_03_AckGone(t *testing.T) {
	for _, code := range []string{model.ErrCodeNoActiveAlert, model.ErrCodeNotFound} {
		t.Run(code, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			svc := &alertService{fail: code}
			svc.set(1, ptAlert("a", "Склад", "k1", model.LevelWarning))
			b := &testBeeper{}
			app, _ := startAlertApp(t, svc, b)
			pumpUntil(t, "alert", func() bool { return app.AlertView().Visible && app.AlertWindow() != 0 })
			winapi.SendMessage(winapi.GetDlgItem(app.AlertWindow(), idAlertAck), winapi.BM_CLICK, 0, 0)
			pumpUntil(t, "closed", func() bool { return len(svc.ackCalls()) == 1 && !app.AlertView().Visible })
			if app.AlertView().Sound {
				t.Error("sound after gone")
			}
			// the refreshed status still lists it unacknowledged at the same
			// level: it stays hidden (no reopen loop)
			pumpFor(150 * time.Millisecond)
			if app.AlertView().Visible {
				t.Error("reopened after no_active_alert")
			}
		})
	}
}
