//go:build windows

package wintray

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/ipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// fakePort is the PC speaker of the service (ipc.SpeakerPort) in tests.
type fakePort struct {
	mu    sync.Mutex
	st    model.SpeakerStatus
	err   error // Tone fails
	tones []portTone
	asked int // speaker_status requests
}

func (p *fakePort) askedN() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.asked
}

type portTone struct {
	freq, dur int
	at        time.Time
}

func (p *fakePort) Tone(freq, dur int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil && dur > 0 {
		return p.err
	}
	p.tones = append(p.tones, portTone{freq, dur, time.Now()})
	return nil
}

func (p *fakePort) Status() model.SpeakerStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asked++
	return p.st
}

func (p *fakePort) set(st model.SpeakerStatus, err error) {
	p.mu.Lock()
	p.st, p.err = st, err
	p.mu.Unlock()
}

func (p *fakePort) got() []portTone {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]portTone(nil), p.tones...)
}

// stopAfter: the first durMs 0 request after i tones, if any.
func stopAt(ts []portTone) (time.Time, bool) {
	for _, x := range ts {
		if x.dur == 0 {
			return x.at, true
		}
	}
	return time.Time{}, false
}

// speakerServer is a real ipc.Server (MemListener) with the fake port
// (nil = a service without the speaker capabilities).
func speakerServer(t *testing.T, svc ipc.Handler, port *fakePort) (*ipc.Server, func(context.Context) (io.ReadWriteCloser, error)) {
	t.Helper()
	l := ipc.NewMemListener()
	srv := ipc.NewServer(svc, nil)
	if port != nil {
		srv.Speaker = port
	}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return srv, func(context.Context) (io.ReadWriteCloser, error) { return l.Dial() }
}

// speakerSession is a real trayipc.Session connected to the server.
func speakerSession(t *testing.T, dial func(context.Context) (io.ReadWriteCloser, error)) *trayipc.Session {
	t.Helper()
	s := trayipc.New(trayipc.Options{Dial: dial, BackoffMin: 10 * time.Millisecond, BackoffMax: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	end := time.Now().Add(10 * time.Second)
	for s.Status().State != trayipc.StateConnected {
		if time.Now().After(end) {
			t.Fatal("session not connected")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return s
}

// TestCHK_TRAY_98_SpeakerChain — phase 14 (Алекс, 10.11.2026): «Тон»
// through winSounder.Play (the real dispatch), the real trayipc Session
// and a real ipc.Server with a fake speaker port: with capabilities
// speaker + speaker_status and status.available the tone goes as
// speaker_tone {freqHz, durMs} and nothing sounds locally; a stop sends
// speaker_tone {durMs:0} at once; not available / refused / no capability
// / no connection → the old chain PlaySound → Beep → MessageBeep with ONE
// journal line per reason (no spam); «Подтвердить» (app.ackAlerts), the
// player stop and «Прослушать» / «Остановить» of the App go through the
// speaker too, the player still owns repeat and pause.
func TestCHK_TRAY_98_SpeakerChain(t *testing.T) {
	tone := trayalert.Sound{Profile: trayalert.SoundProfile{Kind: trayalert.SoundBeep, FreqHz: 880, DurMs: 30}, Volume: 50}
	port := &fakePort{st: model.SpeakerStatus{Available: true, DriverVersion: "1.0"}}
	_, dial := speakerServer(t, &alertService{}, port)
	sess := speakerSession(t, dial)
	if st := sess.Status(); !st.Has(model.CapSpeaker) || !st.Has(model.CapSpeakerStatus) {
		t.Fatalf("caps %v", st.Capabilities)
	}
	var mu sync.Mutex
	var logs []string
	logged := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), logs...) }
	sp := &trayipc.Speaker{C: sess}
	newS := func(r *toneRec) *winSounder {
		return &winSounder{alias: aliasAlarm, log: func(s string) { mu.Lock(); logs = append(logs, s); mu.Unlock() },
			tone: r.outputs(), speaker: speakerOf(sp)}
	}

	// 1: available → speaker_tone {880, 30}; no local output, no log
	r := &toneRec{}
	s := newS(r)
	start := time.Now()
	if err := s.Play(tone, nil); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 30*time.Millisecond {
		t.Errorf("the speaker tone did not last its duration: %v", d)
	}
	if g := port.got(); len(g) != 1 || g[0].freq != 880 || g[0].dur != 30 || len(r.calls) != 0 || len(logged()) != 0 {
		t.Fatalf("speaker: port %+v local %v logs %v", g, r.calls, logged())
	}
	// file kind with a broken file falls back to the tone — via the speaker
	file := tone
	file.Profile.Kind, file.Profile.File = trayalert.SoundFile, `C:\нет\такого.wav`
	if err := s.Play(file, nil); err != nil || len(port.got()) != 2 || len(r.calls) != 0 {
		t.Fatalf("file fallback: %v %+v %v", err, port.got(), r.calls)
	}
	logs = nil

	// 2: stop in the middle → speaker_tone {durMs:0} at once
	long := tone
	long.Profile.DurMs = 5000
	stop := make(chan struct{})
	ended := make(chan error, 1)
	go func() { ended <- s.Play(long, stop) }()
	end := time.Now().Add(5 * time.Second)
	for len(port.got()) < 3 && time.Now().Before(end) {
		time.Sleep(5 * time.Millisecond)
	}
	stopped := time.Now()
	close(stop)
	select {
	case err := <-ended:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Play did not end on stop")
	}
	g := port.got()
	at, ok := stopAt(g)
	if len(g) != 4 || g[2].dur != 5000 || !ok || at.Sub(stopped) > 300*time.Millisecond {
		t.Fatalf("stop: %+v (stop %v after)", g, at.Sub(stopped))
	}

	// 3: volume 0 % → silent: no IPC request, no local output
	asked := port.askedN()
	mute := tone
	mute.Volume = 0
	r0 := &toneRec{}
	if err := newS(r0).Play(mute, nil); err != nil || len(port.got()) != 4 || port.askedN() != asked || len(r0.calls) != 0 {
		t.Fatalf("volume 0: %v %+v asked %d→%d %v", err, port.got(), asked, port.askedN(), r0.calls)
	}
	// any volume above 0 → the speaker (no volume there)
	low := tone
	low.Volume = 1
	if err := newS(&toneRec{}).Play(low, nil); err != nil || len(port.got()) != 5 {
		t.Fatalf("volume 1: %+v", port.got())
	}

	// 4: speaker_status not available → the old chain, one log line
	port.set(model.SpeakerStatus{Reason: "драйвер не установлен"}, nil)
	sp.Invalidate()
	r = &toneRec{failWAV: true}
	s = newS(r)
	for i := 0; i < 3; i++ {
		if err := s.Play(tone, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(port.got()) != 5 || strings.Count(strings.Join(r.calls, ","), "PlaySound:"+aliasAlarm+",Beep") != 3 {
		t.Fatalf("unavailable: port %+v local %v", port.got(), r.calls)
	}
	var spk []string
	for _, l := range logged() {
		if strings.Contains(l, "Системный динамик недоступен") {
			spk = append(spk, l)
		}
	}
	if len(spk) != 1 || !strings.Contains(spk[0], "драйвер не установлен") || !strings.Contains(spk[0], "через колонки") {
		t.Fatalf("one journal line for the speaker: %q", logged())
	}

	// 5: available, but speaker_tone refused → fallback with the error,
	// logged once, and the following tones skip the speaker
	port.set(model.SpeakerStatus{Available: true}, errors.New("порт занят"))
	sp.Invalidate()
	logs = nil
	r = &toneRec{}
	s = newS(r)
	for i := 0; i < 3; i++ {
		if err := s.Play(tone, nil); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Count(strings.Join(r.calls, ","), "PlaySound:"+aliasAlarm) != 3 || len(logged()) != 1 || !strings.Contains(logged()[0], model.MonitorMessage(model.MsgMonitorInternal)) {
		t.Fatalf("refused: local %v logs %q", r.calls, logged())
	}
	if st, _ := sp.Cached(); st.Available {
		t.Error("a refused speaker_tone must mark the speaker not available")
	}

	// 6: back available → the speaker again; a later loss is logged again
	port.set(model.SpeakerStatus{Available: true}, nil)
	sp.Invalidate()
	n := len(port.got())
	r = &toneRec{}
	if err := s.Play(tone, nil); err != nil || len(port.got()) != n+1 || len(r.calls) != 0 {
		t.Fatalf("available again: %+v %v", port.got(), r.calls)
	}

	// 7: a service without the speaker capabilities → the chain, no
	// speaker request, one log line «служба не поддерживает…»
	_, dial2 := speakerServer(t, &alertService{}, nil)
	sess2 := speakerSession(t, dial2)
	logs = nil
	r = &toneRec{}
	s2 := &winSounder{alias: aliasTest, log: func(s string) { mu.Lock(); logs = append(logs, s); mu.Unlock() },
		tone: r.outputs(), speaker: speakerOf(&trayipc.Speaker{C: sess2})}
	for i := 0; i < 2; i++ {
		if err := s2.Play(tone, nil); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(r.calls, ",") != "PlaySound:"+aliasTest+",PlaySound:"+aliasTest || len(logged()) != 1 || !strings.Contains(logged()[0], "служба не поддерживает") {
		t.Fatalf("no capability: %v %q", r.calls, logged())
	}
	if m, err := sess2.Do(context.Background(), model.IPCRequest{Cmd: model.CmdSpeakerTone, FreqHz: 880}); !errors.Is(err, trayipc.ErrUnsupported) {
		t.Errorf("speaker_tone to a service without the capability: %+v %v", m, err)
	}
	// 8: no connection → the chain (no request can go)
	logs = nil
	r = &toneRec{}
	s3 := &winSounder{alias: aliasTest, log: func(s string) { mu.Lock(); logs = append(logs, s); mu.Unlock() },
		tone: r.outputs(), speaker: speakerOf(&trayipc.Speaker{C: trayipc.New(trayipc.Options{})})}
	if err := s3.Play(tone, nil); err != nil || len(r.calls) != 1 || len(logged()) != 1 || !strings.Contains(logged()[0], "нет связи со службой") {
		t.Fatalf("not connected: %v %q", r.calls, logged())
	}
	// the server checks speaker_tone: durMs is required
	if m, err := sess.Do(context.Background(), model.IPCRequest{Cmd: model.CmdSpeakerTone, FreqHz: 880}); err != nil || m.Code != model.ErrCodeInvalidRequest {
		t.Errorf("speaker_tone without durMs: %+v %v", m, err)
	}
}

// TestCHK_TRAY_98_SpeakerApp — the App: the alarm loop (player: repeat,
// gap) sounds through the speaker; «Подтвердить» (app.ackAlerts, the
// button handler) stops it with speaker_tone {durMs:0} at once;
// «Прослушать» goes through the speaker and «Остановить» stops it the
// same way.
func TestCHK_TRAY_98_SpeakerApp(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	port := &fakePort{st: model.SpeakerStatus{Available: true}}
	svc := &alertService{}
	svc.set(1, ptAlert("a", "Склад", "k1", model.LevelError))
	_, dial := speakerServer(t, svc, port)
	prefs := trayalert.DefaultSoundPrefs(nil)
	prefs.Master = 100
	prefs.Error = trayalert.SoundProfile{Kind: trayalert.SoundBeep, FreqHz: 1234, DurMs: 4000, Volume: 70, Loop: true, GapMs: 100}
	prefs.Warning.Volume = 70
	store := &memSoundStore{raw: prefs.String()}
	r := &toneRec{}
	var logs []string
	var lmu sync.Mutex
	ws := &winSounder{alias: aliasAlarm, tone: r.outputs(), log: func(s string) { lmu.Lock(); logs = append(logs, s); lmu.Unlock() }}
	app, err := NewApp(Options{NoNotifyIcon: true, HiddenAlerts: true, HiddenPanel: true, NoBlink: true, HiddenFlash: true,
		Sounder: ws, SoundStore: store,
		Session: trayipc.Options{Dial: dial, BackoffMin: 10 * time.Millisecond, BackoffMax: 20 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Exit)
	if ws.speaker == nil {
		t.Fatal("the App did not give its winSounder the speaker")
	}
	pumpUntil(t, "the alarm tone through the speaker", func() bool {
		g := port.got()
		return app.player.Playing() && len(g) == 1 && g[0].freq == 1234 && g[0].dur == 4000
	})
	pumpUntil(t, "alerts shown", func() bool { v := app.AlertView(); return v.Visible && v.CanAck })
	acked := time.Now()
	app.ackAlerts()
	pumpUntil(t, "«Подтвердить» stops the speaker", func() bool { _, ok := stopAt(port.got()); return ok })
	if at, _ := stopAt(port.got()); at.Sub(acked) > time.Second {
		t.Errorf("speaker_tone {durMs:0} %v after «Подтвердить»", at.Sub(acked))
	}
	pumpUntil(t, "player stopped", func() bool { return !app.player.Playing() })
	if len(r.calls) != 0 {
		t.Errorf("local outputs with an available speaker: %v", r.calls)
	}
	// «Прослушать» → speaker; «Остановить» → durMs 0 at once
	n := len(port.got())
	app.testSound(trayalert.SoundProfile{Kind: trayalert.SoundBeep, FreqHz: 2222, DurMs: 4000, Volume: 50}, 100)
	pumpUntil(t, "preview through the speaker", func() bool {
		g := port.got()
		return len(g) == n+1 && g[n].freq == 2222 && g[n].dur == 4000
	})
	stopped := time.Now()
	app.stopTestSound()
	pumpUntil(t, "«Остановить» stops the speaker", func() bool {
		g := port.got()
		return len(g) == n+2 && g[n+1].dur == 0
	})
	if g := port.got(); g[n+1].at.Sub(stopped) > time.Second {
		t.Errorf("preview stop after %v", g[n+1].at.Sub(stopped))
	}
	// «Прослушать» at volume 0 → no speaker request at all
	n = len(port.got())
	app.testSound(trayalert.SoundProfile{Kind: trayalert.SoundBeep, FreqHz: 2222, DurMs: 100, Volume: 0}, 100)
	pumpUntil(t, "silent preview ended", func() bool { return !app.Previewing() })
	if len(port.got()) != n || len(r.calls) != 0 {
		t.Errorf("volume 0 preview: %+v %v", port.got()[n:], r.calls)
	}
	lmu.Lock()
	defer lmu.Unlock()
	if len(logs) != 0 {
		t.Errorf("journal with an available speaker: %q", logs)
	}
}

// speakerCfg: the settings service with the fake speaker port.
type speakerCfg struct {
	*cfgService
	port *fakePort
}

func (s speakerCfg) Tone(f, d int) error         { return s.port.Tone(f, d) }
func (s speakerCfg) Status() model.SpeakerStatus { return s.port.Status() }

// TestCHK_TRAY_99_SpeakerLabels — the «Звук» group of the settings: the
// profile label «Тон (системный динамик)»; the status line «Системный
// динамик: доступен» / «Системный динамик: недоступен (причина) — тон
// звучит через колонки» from speaker_status of the real server (also for
// a service without the capability); «громкость не регулируется» next to
// the volume while «Тон» is selected (not for «Файл»).
func TestCHK_TRAY_99_SpeakerLabels(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	port := &fakePort{st: model.SpeakerStatus{Available: true, DriverVersion: "2.1"}}
	svc := newCfgService()
	svc.handler = speakerCfg{cfgService: svc, port: port}
	app := startCfgApp(t, svc, true, nil)
	for _, shared := range []bool{true, false} { // the App's speaker / the window's own
		cfg := settingsConfig{mode: traysettings.Admin, session: app.session, hidden: true, uiMode: traysettings.UIExpert}
		if shared {
			cfg.speaker = app.speaker
		}
		port.set(model.SpeakerStatus{Available: true, DriverVersion: "2.1"}, nil)
		app.speaker.Invalidate()
		w, err := newSettingsWindow(cfg)
		if err != nil {
			t.Fatal(err)
		}
		w.Reload()
		pumpUntil(t, "loaded", func() bool { w.update(); return !w.busy && w.ed.HasProfile() })
		if w.sndSpeaker == 0 || w.sndWarn.volNote == 0 || w.sndErr.volNote == 0 {
			t.Fatal("no speaker line / volume note")
		}
		for _, c := range []profileCtl{w.sndWarn, w.sndErr} {
			if l := winapi.GetWindowText(c.beep); l != "Тон (системный динамик)" {
				t.Errorf("profile label %q", l)
			}
		}
		w.refreshSpeakerLine()
		pumpUntil(t, "available", func() bool {
			return winapi.GetWindowText(w.sndSpeaker) == "Системный динамик: доступен"
		})

		port.set(model.SpeakerStatus{Reason: "драйвер не установлен"}, nil)
		w.speaker().Invalidate()
		w.refreshSpeakerLine()
		want := "Системный динамик: недоступен (драйвер не установлен) — тон звучит через колонки"
		pumpUntil(t, "unavailable", func() bool { return winapi.GetWindowText(w.sndSpeaker) == want })

		// «Тон» → the volume note; «Файл» → none; back → again
		c := w.sndWarn
		setCheck(c.file, false)
		setCheck(c.beep, true)
		w.soundCommand(c.base + idSndBeep)
		if n := winapi.GetWindowText(c.volNote); n != "громкость не регулируется" {
			t.Errorf("tone volume note %q", n)
		}
		setCheck(c.beep, false)
		setCheck(c.file, true)
		w.soundCommand(c.base + idSndFile)
		if n := winapi.GetWindowText(c.volNote); n != "" {
			t.Errorf("file volume note %q", n)
		}
		setCheck(c.file, false)
		setCheck(c.beep, true)
		w.soundCommand(c.base + idSndBeep)
		if n := winapi.GetWindowText(c.volNote); n != "громкость не регулируется" {
			t.Errorf("tone volume note again %q", n)
		}
		w.close()
	}
	// a service without the speaker capabilities
	svc2 := newCfgService()
	app2 := startCfgApp(t, svc2, true, nil)
	w, err := newSettingsWindow(settingsConfig{mode: traysettings.Admin, session: app2.session, speaker: app2.speaker,
		hidden: true, uiMode: traysettings.UIExpert})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	w.Reload()
	pumpUntil(t, "loaded", func() bool { w.update(); return !w.busy && w.ed.HasProfile() })
	w.refreshSpeakerLine()
	if l := winapi.GetWindowText(w.sndSpeaker); l != "Системный динамик: недоступен (служба не поддерживает системный динамик) — тон звучит через колонки" {
		t.Errorf("no capability: %q", l)
	}
}
