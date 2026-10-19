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
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

type memSoundStore struct {
	mu    sync.Mutex
	raw   string
	saves int
}

func (s *memSoundStore) Load() (string, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.raw, nil }
func (s *memSoundStore) Save(r string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.raw = r
	s.saves++
	return nil
}

func startP10App(t *testing.T, svc *alertService, b *testBeeper, store SoundStore) (*App, *ipc.Server, *[]string) {
	t.Helper()
	l := ipc.NewMemListener()
	srv := ipc.NewServer(svc, nil)
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	var notes []string
	dial := func(context.Context) (io.ReadWriteCloser, error) { return l.Dial() }
	app, err := NewApp(Options{Lang: i18n.RU, NoNotifyIcon: true, HiddenAlerts: true, HiddenPanel: true, NoBlink: true,
		Beeper: b, SoundStore: store, Notify: func(s string, _ bool) { notes = append(notes, s) },
		Session: trayipc.Options{Dial: dial, BackoffMin: 10 * time.Millisecond, BackoffMax: 20 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Exit)
	return app, srv, &notes
}

// TestCHK_TRAY_33_SoundWindow — «Звук тревог»: two profiles, defaults
// 80 / 100 %, file via «Обзор…», validation, «Прослушать», saved to HKCU
// (JSON) and applied to the alarm at once.
func TestCHK_TRAY_33_SoundWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	store := &memSoundStore{}
	b := &testBeeper{}
	app, _, _ := startP10App(t, &alertService{}, b, store)
	found := 0
	for _, it := range app.RightMenu() {
		if it.ID == trayui.CmdSound || it.ID == trayui.CmdVolume || it.ID == trayui.CmdPinHelp {
			found++
		}
	}
	if found != 3 || app.RightMenu()[len(app.RightMenu())-1].ID != trayui.CmdExit {
		t.Fatalf("menu %+v", app.RightMenu())
	}
	if p := app.SoundPrefs(); p.Warning.Volume != 80 || p.Error.Volume != 100 {
		t.Fatalf("defaults %+v", p)
	}
	app.Command(trayui.CmdSound)
	w := app.soundWin
	if w == nil || winapi.GetWindowText(w.hwnd) != "Звук тревог" || winapi.GetWindowText(w.warn.vol) != "80" ||
		winapi.GetWindowText(w.err.vol) != "100" || !isChecked(w.warn.beep) {
		t.Fatal("window / defaults")
	}
	for _, c := range []uintptr{w.warn.beep, w.warn.freq, w.warn.path, w.warn.browse, w.warn.vol, w.warn.loop, w.warn.gap, w.warn.test, w.save} {
		if w.tips.text(c) == "" {
			t.Errorf("tooltip missing on %x", c)
		}
	}
	// «Обзор…» → file
	w.pickFile = func(string) (string, bool) { return `C:\Windows\Media\chord.wav`, true }
	winapi.SendMessage(w.warn.browse, winapi.BM_CLICK, 0, 0)
	if winapi.GetWindowText(w.warn.path) != `C:\Windows\Media\chord.wav` || !isChecked(w.warn.file) || isChecked(w.warn.beep) {
		t.Fatal("browse")
	}
	// validation
	winapi.SetWindowText(w.err.freq, "5")
	winapi.SendMessage(w.save, winapi.BM_CLICK, 0, 0)
	if store.saves != 0 || !strings.Contains(winapi.GetWindowText(w.info), "Ошибка") {
		t.Fatalf("invalid saved: %q", winapi.GetWindowText(w.info))
	}
	winapi.SetWindowText(w.err.freq, "1500")
	winapi.SetWindowText(w.err.dur, "20")
	winapi.SetWindowText(w.err.vol, "60")
	// «Прослушать» plays once on its own player
	winapi.SendMessage(w.err.test, winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "listen", func() bool { return b.has(1500) })
	winapi.SendMessage(w.save, winapi.BM_CLICK, 0, 0)
	if store.saves != 1 || app.soundWin != nil || winapi.IsWindow(w.hwnd) {
		t.Fatalf("not saved / not closed: %d", store.saves)
	}
	p, err := trayalert.ParseSoundPrefs(store.raw, trayalert.DefaultSoundPrefs(nil))
	if err != nil || p.Warning.Kind != trayalert.SoundFile || p.Error.FreqHz != 1500 || p.Error.Volume != 60 || p != app.SoundPrefs() {
		t.Fatalf("stored %q %+v", store.raw, p)
	}
	// a new app reads it back
	app2, _, _ := startP10App(t, &alertService{}, &testBeeper{}, store)
	if app2.SoundPrefs() != p {
		t.Error("reload from HKCU")
	}
}

// TestCHK_TRAY_35_MasterVolume — the trackbar by the clock stores the
// master volume at once; the alarm plays profile × master.
func TestCHK_TRAY_35_MasterVolume(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	store := &memSoundStore{}
	app, _, _ := startP10App(t, &alertService{}, &testBeeper{}, store)
	app.Command(trayui.CmdVolume)
	v := app.volWin
	if v == nil || int(winapi.SendMessage(v.bar, winapi.TBM_GETPOS, 0, 0)) != 100 || v.tips.text(v.bar) == "" {
		t.Fatal("volume window")
	}
	winapi.SendMessage(v.bar, winapi.TBM_SETPOS, 1, 30)
	winapi.SendMessage(v.hwnd, winapi.WM_HSCROLL, 0, v.bar)
	if app.SoundPrefs().Master != 30 || store.saves != 1 || winapi.GetWindowText(v.label) != "30 %" {
		t.Fatalf("master %d saves %d", app.SoundPrefs().Master, store.saves)
	}
	s := app.alertSound(trayalert.View{Level: model.LevelWarning})
	if s.Volume != 24 { // 80 % × 30 %
		t.Errorf("effective %d", s.Volume)
	}
	winapi.SendMessage(v.hwnd, winapi.WM_CLOSE, 0, 0)
	if app.volWin != nil {
		t.Error("closed")
	}
}

// TestCHK_TRAY_36_ErrorWindow — the reworked CHK-TRAY-03 window: only
// Minimize and Close boxes, «Звук вкл/выкл» mutes the current alarm,
// «Подтвердить» hides it until the next check that still sees the error.
func TestCHK_TRAY_36_ErrorWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t0 := alertFresh
	svc := &alertService{snap: model.StatusSnapshot{Profiles: []model.ProfileSnapshot{{ProfileID: "a", Name: "DB-a", Enabled: true, CheckedAt: t0}}}}
	svc.set(1, ptAlert("a", "Склад", "k1", model.LevelError))
	b := &testBeeper{}
	app, srv, _ := startP10App(t, svc, b, nil)
	pumpUntil(t, "alarm", func() bool { v := app.AlertView(); return v.Visible && v.Sound && app.awin != nil })
	aw := app.awin
	if aw.style&winapi.WS_MINIMIZEBOX == 0 || aw.style&0x00010000 != 0 /* WS_MAXIMIZEBOX */ {
		t.Errorf("style %x", aw.style)
	}
	mute := winapi.GetDlgItem(aw.hwnd, idAlertMute)
	if winapi.GetWindowText(mute) != "Звук выкл" || aw.tips.text(mute) == "" {
		t.Fatalf("mute button %q", winapi.GetWindowText(mute))
	}
	winapi.SendMessage(mute, winapi.BM_CLICK, 0, 0)
	if v := app.AlertView(); v.Sound || !v.Muted || !v.Visible || app.player.Playing() || winapi.GetWindowText(mute) != "Звук вкл" {
		t.Fatalf("muted %+v", v)
	}
	winapi.SendMessage(mute, winapi.BM_CLICK, 0, 0)
	if v := app.AlertView(); !v.Sound || !app.player.Playing() {
		t.Fatal("unmuted")
	}
	winapi.SendMessage(winapi.GetDlgItem(aw.hwnd, idAlertAck), winapi.BM_CLICK, 0, 0)
	pumpUntil(t, "acked", func() bool { return !app.AlertView().Visible && len(svc.ackCalls()) == 1 })
	// the same check: stays hidden
	srv.PublishStatus(svc.set(5, svc.Snapshot().Alerts...))
	pumpFor(150 * time.Millisecond)
	if app.AlertView().Visible {
		t.Fatal("same check reopened")
	}
	// the next check still sees it: shown again with sound
	svc.mu.Lock()
	svc.snap.Profiles[0].CheckedAt = t0.Add(time.Minute)
	svc.mu.Unlock()
	srv.PublishStatus(svc.set(6, svc.Snapshot().Alerts...))
	pumpUntil(t, "next check reopens", func() bool { v := app.AlertView(); return v.Visible && v.Sound })
	// the icon keeps blinking for an acknowledged problem; the panel lists it
	if app.Blink() != trayalert.BlinkRed || len(app.Problems()) != 1 {
		t.Errorf("blink %v problems %+v", app.Blink(), app.Problems())
	}
	// optional alert_repeat
	app.OnAlertRepeat(trayalert.AlertRepeat{ProfileID: "a", Level: model.LevelError})
	if !app.AlertView().Visible {
		t.Error("repeat")
	}
}

// TestCHK_TRAY_37_IconAndHelp — the icon flags (NIF_SHOWTIP, version 4),
// re-added on TaskbarCreated; the «Как закрепить значок» note.
func TestCHK_TRAY_37_IconAndHelp(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	app, _, notes := startP10App(t, &alertService{}, &testBeeper{}, nil)
	if n := app.nid(); n.Flags&winapi.NIF_SHOWTIP == 0 || n.Flags&winapi.NIF_TIP == 0 {
		t.Errorf("flags %x", n.Flags)
	}
	if app.taskbarCreated == 0 {
		t.Fatal("TaskbarCreated not registered")
	}
	winapi.SendMessage(app.hwnd, app.taskbarCreated, 0, 0)
	if app.taskbarRecreated != 1 {
		t.Error("TaskbarCreated not handled")
	}
	app.Command(trayui.CmdPinHelp)
	if len(*notes) != 1 || !strings.Contains((*notes)[0], "Настроить") {
		t.Fatalf("notes %q", *notes)
	}
	if !strings.Contains(PinHelpText(i18n.EN), "Customize") {
		t.Error("EN note")
	}
}

// TestCHK_TRAY_38_BlinkIcon — about 1 Hz: red / blank for an error,
// yellow / blank for a warning, steady when fine.
func TestCHK_TRAY_38_BlinkIcon(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &alertService{}
	svc.set(1, ptAlert("a", "Склад", "k1", model.LevelWarning))
	app, srv, _ := startP10App(t, svc, &testBeeper{}, nil)
	pumpUntil(t, "warning", func() bool { return app.Blink() == trayalert.BlinkYellow })
	if app.ShownIcon() != trayui.IconWarning {
		t.Error("on phase")
	}
	app.blinkTick()
	if app.ShownIcon() != trayui.IconBlank || app.Blinks() != 1 {
		t.Error("off phase")
	}
	app.blinkTick()
	if app.ShownIcon() != trayui.IconWarning {
		t.Error("on again")
	}
	srv.PublishStatus(svc.set(2, ptAlert("a", "Склад", "k1", model.LevelError)))
	pumpUntil(t, "error", func() bool { return app.Blink() == trayalert.BlinkRed })
	if app.ShownIcon() != trayui.IconError {
		t.Error("red")
	}
	srv.PublishStatus(svc.set(3))
	pumpUntil(t, "fine", func() bool { return app.Blink() == trayalert.BlinkNone })
	app.blinkTick()
	if app.ShownIcon() != app.Indicator().Icon || app.blinkOff {
		t.Error("steady")
	}
	if blinkHalfMs != 500 {
		t.Error("~1 Hz")
	}
	px := trayui.IconPixels(trayui.IconBlank, 16)
	if px[8*16+8] != 0 {
		t.Error("blank center")
	}
}

// TestCHK_TRAY_39_Panel — the topmost panel by the clock: no caption or
// close button, never activated, lists the problems and disappears at
// once on recovery.
func TestCHK_TRAY_39_Panel(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	svc := &alertService{}
	svc.set(1, ptAlert("a", "Склад", "k1", model.LevelError), ptAlert("a", "Магазин", "k2", model.LevelWarning))
	app, srv, _ := startP10App(t, svc, &testBeeper{}, nil)
	pumpUntil(t, "panel", func() bool { return app.Panel() != nil && app.Panel().shown })
	p := app.Panel()
	if len(p.lines) != 2 || !strings.Contains(p.lines[0], "Склад") || !strings.Contains(winapi.GetWindowText(p.text), "Магазин") {
		t.Fatalf("lines %q", p.lines)
	}
	winapi.SendMessage(p.hwnd, winapi.WM_CLOSE, 0, 0)
	if !winapi.IsWindow(p.hwnd) {
		t.Fatal("the panel has no close")
	}
	srv.PublishStatus(svc.set(2))
	pumpUntil(t, "gone on recovery", func() bool { return !app.Panel().shown })
	srv.PublishStatus(svc.set(3, ptAlert("a", "Склад", "k1", model.LevelError)))
	pumpUntil(t, "back", func() bool { return app.Panel().shown && len(app.Panel().lines) == 1 })
}
