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
	"github.com/alexal46372-maker/ExchangeChecker/internal/pcspeaker"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// ioPort is the I/O port under the service's pcspeaker.Device (the
// "driver" of the service in tests): it records the tones the device
// programs (PIT divisor, speaker bits of port 0x61) and reports
// speaker_status itself, like the InpOutx64 port.
type ioPort struct {
	mu   sync.Mutex
	st   model.SpeakerStatus
	fail error
	r61  byte
	on   bool
	evs  []portEv
	asks int
}

// portEv: a tone started (div != 0) or the speaker turned off (div 0).
type portEv struct {
	div uint16
	at  time.Time
}

func (p *ioPort) Read(uint16) (byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.r61, nil
}

func (p *ioPort) Write(b []pcspeaker.PortWrite) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail != nil {
		return p.fail
	}
	var div uint16
	n := 0
	for _, w := range b {
		switch w.Port {
		case pcspeaker.PortTimerData:
			div |= uint16(w.Val) << (8 * n)
			n++
		case pcspeaker.PortPPI:
			p.r61 = w.Val
			on := w.Val&pcspeaker.SpeakerBits == pcspeaker.SpeakerBits
			if on {
				p.evs = append(p.evs, portEv{div, time.Now()})
			} else if p.on {
				p.evs = append(p.evs, portEv{0, time.Now()})
			}
			p.on = on
		}
	}
	return nil
}

func (p *ioPort) Status() model.SpeakerStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asks++
	return p.st
}

func (p *ioPort) set(st model.SpeakerStatus, fail error) {
	p.mu.Lock()
	p.st, p.fail = st, fail
	p.mu.Unlock()
}

func (p *ioPort) asked() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.asks
}

// starts / offs: the divisors of the started tones, the times of the offs.
func (p *ioPort) starts() []uint16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []uint16
	for _, e := range p.evs {
		if e.div != 0 {
			out = append(out, e.div)
		}
	}
	return out
}

func (p *ioPort) offs() []time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []time.Time
	for _, e := range p.evs {
		if e.div == 0 {
			out = append(out, e.at)
		}
	}
	return out
}

// durRec records the durations the device was asked for (its auto-off).
type durRec struct {
	mu sync.Mutex
	d  []time.Duration
}

func (r *durRec) got() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.d...)
}

// newDevice is the service's pcspeaker.Device on the fake port.
func newDevice(port *ioPort, durs *durRec) *pcspeaker.Device {
	return pcspeaker.New(pcspeaker.Options{Port: port, After: func(d time.Duration) <-chan time.Time {
		if durs != nil {
			durs.mu.Lock()
			durs.d = append(durs.d, d)
			durs.mu.Unlock()
		}
		return time.After(d)
	}})
}

// speakerServer is a real ipc.Server (MemListener) with the service's
// speaker device (nil = a service without the speaker capabilities).
func speakerServer(t *testing.T, svc ipc.Handler, dev *pcspeaker.Device) func(context.Context) (io.ReadWriteCloser, error) {
	t.Helper()
	l := ipc.NewMemListener()
	srv := ipc.NewServer(svc, nil)
	if dev != nil {
		srv.Speaker = dev
	}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return func(context.Context) (io.ReadWriteCloser, error) { return l.Dial() }
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

func waitCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(end) {
			t.Fatal(what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestCHK_TRAY_98_SpeakerChain — phase 14 (Алекс, 10.11.2026): «Тон»
// through winSounder.Play (the real dispatch), the real trayipc Session
// and the real ipc.Server of the service with its pcspeaker.Device on a
// fake I/O port: with capabilities speaker + speaker_status and
// status.available the tone is programmed on the speaker (PIT divisor,
// the device's auto-off after durMs) and nothing sounds locally; a stop
// sends speaker_tone {durMs:0} and the speaker is off at once; not
// available / refused (notify.speaker_failed) / busy (another
// connection owns the speaker) / no capability / no connection → the old
// chain PlaySound → Beep → MessageBeep with ONE journal line per reason
// in Russian (no spam); volume 0 % = no IPC at all.
func TestCHK_TRAY_98_SpeakerChain(t *testing.T) {
	tone := trayalert.Sound{Profile: trayalert.SoundProfile{Kind: trayalert.SoundBeep, FreqHz: 880, DurMs: 30}, Volume: 50}
	port := &ioPort{st: model.SpeakerStatus{Available: true, DriverVersion: "1.5.0.1"}}
	durs := &durRec{}
	dial := speakerServer(t, &alertService{}, newDevice(port, durs))
	sess := speakerSession(t, dial)
	if st := sess.Status(); !st.Has(model.CapSpeaker) || !st.Has(model.CapSpeakerStatus) {
		t.Fatalf("caps %v", st.Capabilities)
	}
	var mu sync.Mutex
	var logs []string
	logf := func(s string) { mu.Lock(); logs = append(logs, s); mu.Unlock() }
	logged := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), logs...) }
	clear := func() { mu.Lock(); logs = nil; mu.Unlock() }
	sp := &trayipc.Speaker{C: sess}
	newS := func(r *toneRec) *winSounder {
		return &winSounder{alias: aliasAlarm, log: logf, tone: r.outputs(), speaker: speakerOf(sp)}
	}

	// 1: available → the speaker plays 880 Hz for 30 ms; no local output
	r := &toneRec{}
	s := newS(r)
	start := time.Now()
	if err := s.Play(tone, nil); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 30*time.Millisecond {
		t.Errorf("the speaker tone did not last its duration: %v", d)
	}
	if st := port.starts(); len(st) != 1 || st[0] != pcspeaker.Divisor(880) || len(r.calls) != 0 || len(logged()) != 0 {
		t.Fatalf("speaker: starts %v local %v logs %v", st, r.calls, logged())
	}
	if d := durs.got(); len(d) != 1 || d[0] != 30*time.Millisecond {
		t.Fatalf("durations %v", d)
	}
	waitCond(t, "auto-off after 30 ms", func() bool { return len(port.offs()) == 1 })
	// file kind with a broken file falls back to the tone — via the speaker
	file := tone
	file.Profile.Kind, file.Profile.File = trayalert.SoundFile, `C:\нет\такого.wav`
	if err := s.Play(file, nil); err != nil || len(port.starts()) != 2 || len(r.calls) != 0 {
		t.Fatalf("file fallback: %v %v %v", err, port.starts(), r.calls)
	}
	waitCond(t, "auto-off of the file fallback", func() bool { return len(port.offs()) == 2 })
	clear()

	// 2: stop in the middle → speaker_tone {durMs:0}, the speaker is off at once
	long := tone
	long.Profile.DurMs = 5000
	stop := make(chan struct{})
	ended := make(chan error, 1)
	go func() { ended <- s.Play(long, stop) }()
	waitCond(t, "long tone started", func() bool { return len(port.starts()) == 3 })
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
	waitCond(t, "speaker off after stop", func() bool { return len(port.offs()) == 3 })
	if at := port.offs()[2]; at.Sub(stopped) > 300*time.Millisecond {
		t.Fatalf("speaker off %v after the stop", at.Sub(stopped))
	}

	// 3: volume 0 % → silent: no IPC request, no local output
	asked := port.asked()
	mute := tone
	mute.Volume = 0
	r0 := &toneRec{}
	if err := newS(r0).Play(mute, nil); err != nil || len(port.starts()) != 3 || port.asked() != asked || len(r0.calls) != 0 {
		t.Fatalf("volume 0: %v %v asked %d→%d %v", err, port.starts(), asked, port.asked(), r0.calls)
	}
	// any volume above 0 → the speaker (no volume there)
	low := tone
	low.Volume = 1
	if err := newS(&toneRec{}).Play(low, nil); err != nil || len(port.starts()) != 4 {
		t.Fatalf("volume 1: %v", port.starts())
	}

	// 4: speaker_status not available (not_installed) → the old chain,
	// one journal line with the Russian reason
	port.set(model.SpeakerStatus{Reason: model.SpeakerNotInstalled}, nil)
	sp.Invalidate()
	r = &toneRec{failWAV: true}
	s = newS(r)
	for i := 0; i < 3; i++ {
		if err := s.Play(tone, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(port.starts()) != 4 || strings.Count(strings.Join(r.calls, ","), "PlaySound:"+aliasAlarm+",Beep") != 3 {
		t.Fatalf("unavailable: starts %v local %v", port.starts(), r.calls)
	}
	var spk []string
	for _, l := range logged() {
		if strings.Contains(l, "Системный динамик недоступен") {
			spk = append(spk, l)
		}
	}
	if len(spk) != 1 || !strings.Contains(spk[0], "(драйвер не установлен)") || !strings.Contains(spk[0], "через колонки") {
		t.Fatalf("one journal line for the speaker: %q", logged())
	}

	// 5: available, but the port fails → notify.speaker_failed: fallback,
	// logged once in Russian, the following tones skip the speaker
	port.set(model.SpeakerStatus{Available: true}, errors.New("ioctl"))
	sp.Invalidate()
	clear()
	r = &toneRec{}
	s = newS(r)
	for i := 0; i < 3; i++ {
		if err := s.Play(tone, nil); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Count(strings.Join(r.calls, ","), "PlaySound:"+aliasAlarm) != 3 || len(logged()) != 1 || !strings.Contains(logged()[0], "(ошибка обращения к динамику)") {
		t.Fatalf("refused: local %v logs %q", r.calls, logged())
	}
	if st, _ := sp.Cached(); st.Available || st.Reason != model.MsgSpeakerFailed {
		t.Errorf("a refused speaker_tone must mark the speaker not available: %+v", st)
	}

	// 6: back available → the speaker again
	port.set(model.SpeakerStatus{Available: true}, nil)
	sp.Invalidate()
	n := len(port.starts())
	r = &toneRec{}
	s = newS(r)
	if err := s.Play(tone, nil); err != nil || len(port.starts()) != n+1 || len(r.calls) != 0 {
		t.Fatalf("available again: %v %v", port.starts(), r.calls)
	}

	// 7: another connection owns the speaker → busy: this tone falls back
	// (one line «динамик занят другим подключением»), the next one after
	// the owner stopped goes to the speaker again (busy is not cached)
	other := &trayipc.Speaker{C: speakerSession(t, dial)}
	if err := other.Tone(context.Background(), 500, 5000); err != nil {
		t.Fatal(err)
	}
	clear()
	r = &toneRec{}
	s = newS(r)
	if err := s.Play(tone, nil); err != nil || len(r.calls) != 1 || len(logged()) != 1 || !strings.Contains(logged()[0], "(динамик занят другим подключением)") {
		t.Fatalf("busy: %v %q", r.calls, logged())
	}
	if err := other.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	n = len(port.starts())
	if err := s.Play(tone, nil); err != nil || len(port.starts()) != n+1 || len(r.calls) != 1 {
		t.Fatalf("after busy: %v %v", port.starts(), r.calls)
	}

	// 8: a service without the speaker capabilities → the chain, no
	// speaker request, one log line «служба не поддерживает…»
	sess2 := speakerSession(t, speakerServer(t, &alertService{}, nil))
	clear()
	r = &toneRec{}
	s2 := &winSounder{alias: aliasTest, log: logf, tone: r.outputs(), speaker: speakerOf(&trayipc.Speaker{C: sess2})}
	for i := 0; i < 2; i++ {
		if err := s2.Play(tone, nil); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(r.calls, ",") != "PlaySound:"+aliasTest+",PlaySound:"+aliasTest || len(logged()) != 1 || !strings.Contains(logged()[0], "служба не поддерживает") {
		t.Fatalf("no capability: %v %q", r.calls, logged())
	}
	if m, err := sess2.Do(context.Background(), model.IPCRequest{Cmd: model.CmdSpeakerTone, FreqHz: 880, DurMs: new(int)}); !errors.Is(err, trayipc.ErrUnsupported) {
		t.Errorf("speaker_tone to a service without the capability: %+v %v", m, err)
	}
	// 9: no connection → the chain (no request can go)
	clear()
	r = &toneRec{}
	s3 := &winSounder{alias: aliasTest, log: logf, tone: r.outputs(), speaker: speakerOf(&trayipc.Speaker{C: trayipc.New(trayipc.Options{})})}
	if err := s3.Play(tone, nil); err != nil || len(r.calls) != 1 || len(logged()) != 1 || !strings.Contains(logged()[0], "нет связи со службой") {
		t.Fatalf("not connected: %v %q", r.calls, logged())
	}
}

// TestCHK_TRAY_98_SpeakerApp — the App against the service's server and
// device: the alarm loop (player: repeat, gap) sounds on the speaker;
// «Подтвердить» (app.ackAlerts, the button handler) turns it off at once
// (speaker_tone {durMs:0}); «Прослушать» goes to the speaker and
// «Остановить» turns it off the same way; «Прослушать» at volume 0 sends
// nothing.
func TestCHK_TRAY_98_SpeakerApp(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	port := &ioPort{st: model.SpeakerStatus{Available: true}}
	durs := &durRec{}
	svc := &alertService{}
	svc.set(1, ptAlert("a", "Склад", "k1", model.LevelError))
	dial := speakerServer(t, svc, newDevice(port, durs))
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
	pumpUntil(t, "the alarm tone on the speaker", func() bool {
		st := port.starts()
		return app.player.Playing() && len(st) == 1 && st[0] == pcspeaker.Divisor(1234)
	})
	if d := durs.got(); len(d) != 1 || d[0] != 4*time.Second {
		t.Fatalf("durations %v", d)
	}
	pumpUntil(t, "alerts shown", func() bool { v := app.AlertView(); return v.Visible && v.CanAck })
	acked := time.Now()
	app.ackAlerts()
	pumpUntil(t, "«Подтвердить» turns the speaker off", func() bool { return len(port.offs()) == 1 })
	if at := port.offs()[0]; at.Sub(acked) > time.Second {
		t.Errorf("speaker off %v after «Подтвердить»", at.Sub(acked))
	}
	pumpUntil(t, "player stopped", func() bool { return !app.player.Playing() })
	if len(r.calls) != 0 {
		t.Errorf("local outputs with an available speaker: %v", r.calls)
	}
	// «Прослушать» → speaker; «Остановить» → off at once
	app.testSound(trayalert.SoundProfile{Kind: trayalert.SoundBeep, FreqHz: 2222, DurMs: 4000, Volume: 50}, 100)
	pumpUntil(t, "preview on the speaker", func() bool {
		st := port.starts()
		return len(st) == 2 && st[1] == pcspeaker.Divisor(2222)
	})
	stopped := time.Now()
	app.stopTestSound()
	pumpUntil(t, "«Остановить» turns the speaker off", func() bool { return len(port.offs()) == 2 })
	if at := port.offs()[1]; at.Sub(stopped) > time.Second {
		t.Errorf("preview stop after %v", at.Sub(stopped))
	}
	// «Прослушать» at volume 0 → no speaker request at all
	asked := port.asked()
	app.testSound(trayalert.SoundProfile{Kind: trayalert.SoundBeep, FreqHz: 2222, DurMs: 100, Volume: 0}, 100)
	pumpUntil(t, "silent preview ended", func() bool { return !app.Previewing() })
	if len(port.starts()) != 2 || port.asked() != asked || len(r.calls) != 0 {
		t.Errorf("volume 0 preview: %v asked %d→%d %v", port.starts(), asked, port.asked(), r.calls)
	}
	lmu.Lock()
	defer lmu.Unlock()
	if len(logs) != 0 {
		t.Errorf("journal with an available speaker: %q", logs)
	}
}

// speakerCfg: the settings service with the service's speaker device.
type speakerCfg struct {
	*cfgService
	dev *pcspeaker.Device
}

func (s speakerCfg) Tone(owner any, f, d int) error { return s.dev.Tone(owner, f, d) }
func (s speakerCfg) Status() model.SpeakerStatus    { return s.dev.Status() }
func (s speakerCfg) Release(owner any)              { s.dev.Release(owner) }
func (s speakerCfg) Silence()                       { s.dev.Silence() }

// TestCHK_TRAY_99_SpeakerLabels — the «Звук» group of the settings: the
// profile label «Тон (системный динамик)»; the status line «Системный
// динамик: доступен» / «Системный динамик: недоступен (причина) — тон
// звучит через колонки» from speaker_status of the real server (also for
// a service without the capability); «громкость не регулируется» next to
// the volume while «Тон» is selected (not for «Файл»).
func TestCHK_TRAY_99_SpeakerLabels(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	port := &ioPort{st: model.SpeakerStatus{Available: true, DriverVersion: "2.1"}}
	svc := newCfgService()
	svc.handler = speakerCfg{cfgService: svc, dev: newDevice(port, nil)}
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

		port.set(model.SpeakerStatus{Reason: model.SpeakerNotInstalled}, nil)
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
