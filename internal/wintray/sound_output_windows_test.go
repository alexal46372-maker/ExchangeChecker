//go:build windows

package wintray

import (
	"strings"
	"sync"
	"testing"
	"time"

	"runtime"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/pcspeaker"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// TestCHK_TRAY_105_SoundOutputRouting — phase 16 E: «Вывод звука» of
// «Тон» through winSounder.Play (the real dispatch), the real trayipc
// Session and the real ipc.Server with its pcspeaker.Device on a fake I/O
// port (an active alarm: alarm mode). «Колонки»: the sine through
// PlaySound only — no speaker_status, no speaker_tone; «Динамик и
// колонки»: the PC speaker and PlaySound at once, a stop silences both at
// once (speaker_tone {durMs:0} + PlaySound stop); the speaker not available:
// one journal line, the speakers alone; «Прослушать» while the alarm is on
// the speaker (NoSpeaker): the speakers alone; «Системный динамик»: the
// speaker only, its fallback to the speakers as before (CHK-TRAY-98); master
// 0 %: silent for every output.
func TestCHK_TRAY_105_SoundOutputRouting(t *testing.T) {
	port := &ioPort{st: model.SpeakerStatus{Available: true, DriverVersion: "1.5.0.1"}}
	alarm := &alertService{}
	alarm.set(1, ptAlert("a", "Склад", "k1", model.LevelWarning))
	dial := speakerServer(t, alarm, newDevice(port, &durRec{}))
	sess := speakerSession(t, dial)
	sp := &trayipc.Speaker{C: sess}
	var mu sync.Mutex
	var logs []string
	logf := func(s string) { mu.Lock(); logs = append(logs, s); mu.Unlock() }
	logged := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), logs...) }
	newS := func(r *toneRec) *winSounder {
		return &winSounder{alias: aliasAlarm, log: logf, tone: r.outputs(), speaker: speakerOf(sp)}
	}
	tone := func(out trayalert.SoundOutput, dur int) trayalert.Sound {
		return trayalert.SoundFor(trayalert.SoundProfile{Kind: trayalert.SoundBeep, FreqHz: 880, DurMs: dur, Output: out}, 70)
	}

	// «Колонки»: PlaySound only, the service is never asked
	asked, starts := port.asked(), len(port.starts())
	r := &toneRec{}
	if err := newS(r).Play(tone(trayalert.OutputSpeakers, 30), nil); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.calls, ",") != "PlaySound:"+aliasAlarm || port.asked() != asked || len(port.starts()) != starts {
		t.Fatalf("speakers: local %v, speaker asked %d→%d starts %d→%d", r.calls, asked, port.asked(), starts, len(port.starts()))
	}

	// «Системный динамик»: the speaker only
	r = &toneRec{}
	if err := newS(r).Play(tone(trayalert.OutputSpeaker, 30), nil); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 0 || len(port.starts()) != starts+1 {
		t.Fatalf("speaker: local %v starts %v", r.calls, port.starts())
	}
	waitCond(t, "speaker auto-off", func() bool { return len(port.offs()) == 1 })

	// «Динамик и колонки»: both
	r = &toneRec{}
	s := newS(r)
	begin := time.Now()
	if err := s.Play(tone(trayalert.OutputBoth, 40), nil); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(begin); d < 40*time.Millisecond {
		t.Errorf("both: ended after %v", d)
	}
	if st := port.starts(); len(st) != starts+2 || st[len(st)-1] != pcspeaker.Divisor(880) || r.count("PlaySound:"+aliasAlarm) != 1 || !s.viaSpeaker.Load() {
		t.Fatalf("both: starts %v local %v", st, r.calls)
	}
	waitCond(t, "speaker auto-off (both)", func() bool { return len(port.offs()) == 2 })

	// a stop in the middle silences both at once
	r = &toneRec{}
	s = newS(r)
	stop := make(chan struct{})
	ended := make(chan error, 1)
	go func() { ended <- s.Play(tone(trayalert.OutputBoth, 5000), stop) }()
	waitCond(t, "both started", func() bool { return len(port.starts()) == starts+3 && r.count("PlaySound:"+aliasAlarm) == 1 })
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
		t.Errorf("speaker off %v after the stop", at.Sub(stopped))
	}
	if r.count("stop") != 1 {
		t.Errorf("PlaySound not stopped: %v", r.calls)
	}

	// master 0 %: silent for every output, no IPC
	asked = port.asked()
	for _, o := range trayalert.SoundOutputs {
		rz := &toneRec{}
		snd := trayalert.SoundFor(trayalert.SoundProfile{Kind: trayalert.SoundBeep, FreqHz: 880, DurMs: 20, Output: o}, 0)
		if err := newS(rz).Play(snd, nil); err != nil || len(rz.calls) != 0 || port.asked() != asked {
			t.Fatalf("%s at 0 %%: %v %v", o, err, rz.calls)
		}
	}

	// «Прослушать» while the alarm is on the speaker: the speakers alone
	r = &toneRec{}
	nb := tone(trayalert.OutputBoth, 20)
	nb.NoSpeaker = true
	starts = len(port.starts())
	if err := newS(r).Play(nb, nil); err != nil || len(port.starts()) != starts || strings.Join(r.calls, ",") != "PlaySound:"+aliasAlarm {
		t.Fatalf("both + NoSpeaker: %v %v", port.starts(), r.calls)
	}

	// the speaker not available: «Динамик и колонки» = the speakers alone,
	// one journal line
	port.set(model.SpeakerStatus{Reason: model.SpeakerNotInstalled}, nil)
	sp.Invalidate()
	r = &toneRec{}
	s = newS(r)
	for i := 0; i < 2; i++ {
		if err := s.Play(tone(trayalert.OutputBoth, 20), nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(port.starts()) != starts || r.count("PlaySound:"+aliasAlarm) != 2 || r.count("Beep") != 0 {
		t.Fatalf("both, unavailable: %v %v", port.starts(), r.calls)
	}
	n := 0
	for _, l := range logged() {
		if strings.Contains(l, "Системный динамик недоступен") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("journal lines %q", logged())
	}
	// neither the speaker nor PlaySound: the old chain (Beep)
	r = &toneRec{failWAV: true}
	if err := newS(r).Play(tone(trayalert.OutputBoth, 20), nil); err != nil || r.count("Beep") != 1 {
		t.Fatalf("both, nothing works: %v %v", err, r.calls)
	}
}

// TestCHK_TRAY_106_SoundSectionEnable — phase 16 E: the «Звук» group of the
// settings (real window, real clicks): no per-level volume field nor
// «громкость не регулируется»; «Тон» greys the file path and «Обзор…»,
// «Файл» greys the frequency, the duration and «Вывод звука» and shows the
// short note «файл — через колонки»; «Звуковой сигнал тревоги» off greys
// every control of the section in both levels (fields, labels, the speaker
// line, «Прослушать») and stops a running preview; the checkbox itself
// stays enabled; on again restores the per-kind state; «Вывод звука» is
// stored per level by the window Save path.
func TestCHK_TRAY_106_SoundSectionEnable(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	store := &memSoundStore{}
	svc := newViberCfg()
	app := startCfgApp(t, svc.cfgService, true, nil)
	app.opt.SoundStore = store
	app.loadSoundPrefs()
	var previews int
	w, err := newSettingsWindow(settingsConfig{mode: traysettings.Admin, session: app.session,
		hidden: true, uiMode: traysettings.UIExpert,
		stores:      trayStores{Sound: store},
		onSoundSave: app.SaveSoundPrefs,
		onSoundTest: func(p trayalert.SoundProfile, m int) { previews++; app.testSound(p, m) },
		onSoundStop: app.stopTestSound,
		soundPrefs:  func() trayalert.SoundPrefs { return app.SoundPrefs() },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { w.close(); app.Exit() }()
	w.Reload()
	pumpUntil(t, "loaded", func() bool { w.update(); return !w.busy && w.ed.HasProfile() })
	w.setPage(1)
	beep := w.ctl[idGenBeep]
	if beep == 0 || w.sndWarn.out == 0 {
		t.Fatal("sound section missing")
	}
	click := func(h uintptr) { winapi.SendMessage(h, winapi.BM_CLICK, 0, 0); pumpN(3) }
	if !isChecked(beep) {
		click(beep)
	}
	if !isChecked(beep) || !w.check(idGenBeep) {
		t.Fatal("«Звуковой сигнал тревоги» cannot be checked")
	}
	// no volume field, no «громкость не регулируется» anywhere in the window
	for id, h := range w.ctl {
		if strings.Contains(winapi.GetWindowText(h), "громкость не регулируется") || id == idSndWarn+idSndVol || id == idSndErr+idSndVol {
			t.Fatalf("volume leftovers: id %d %q", id, winapi.GetWindowText(h))
		}
	}
	type state struct{ freq, dur, out, path, browse bool }
	got := func(c profileCtl) state {
		return state{winapi.IsWindowEnabled(c.freq), winapi.IsWindowEnabled(c.dur), winapi.IsWindowEnabled(c.out),
			winapi.IsWindowEnabled(c.path), winapi.IsWindowEnabled(c.browse)}
	}
	toneSt, fileSt := state{true, true, true, false, false}, state{false, false, false, true, true}
	for _, c := range []profileCtl{w.sndWarn, w.sndErr} {
		click(c.beep)
		if g := got(c); g != toneSt || winapi.GetWindowText(c.outNote) != "" {
			t.Errorf("base %d «Тон»: %+v note %q", c.base, g, winapi.GetWindowText(c.outNote))
		}
		click(c.file)
		if g := got(c); g != fileSt || winapi.GetWindowText(c.outNote) != "файл — через колонки" {
			t.Errorf("base %d «Файл»: %+v note %q", c.base, g, winapi.GetWindowText(c.outNote))
		}
		click(c.beep)
		if g := got(c); g != toneSt {
			t.Errorf("base %d «Тон» again: %+v", c.base, g)
		}
	}
	// a running preview, then the section off
	click(w.sndWarn.test)
	if previews != 1 || w.sndPreviewing != w.sndWarn.base {
		t.Fatalf("preview not started: %d %d", previews, w.sndPreviewing)
	}
	click(beep)
	if isChecked(beep) || w.check(idGenBeep) {
		t.Fatal("checkbox did not uncheck")
	}
	if w.sndPreviewing != 0 || app.Previewing() {
		t.Error("the preview kept playing with the section off")
	}
	if !winapi.IsWindowEnabled(beep) {
		t.Error("the checkbox itself was disabled")
	}
	for _, c := range []profileCtl{w.sndWarn, w.sndErr} {
		all := append([]uintptr{c.head, c.beep, c.file, c.freq, c.dur, c.out, c.outNote, c.path, c.browse, c.loop, c.gap, c.test}, c.labels...)
		for i, h := range all {
			if h == 0 || winapi.IsWindowEnabled(h) {
				t.Errorf("base %d control #%d (%q) enabled with the section off", c.base, i, winapi.GetWindowText(h))
			}
		}
	}
	if winapi.IsWindowEnabled(w.sndSpeaker) || winapi.IsWindowEnabled(w.sndInfo) {
		t.Error("speaker / info line enabled with the section off")
	}
	click(w.sndErr.test) // grey: nothing happens even if a click got through
	if previews != 1 || w.sndPreviewing != 0 {
		t.Errorf("preview with the section off: %d", previews)
	}
	click(beep)
	if !w.check(idGenBeep) || got(w.sndWarn) != toneSt || !winapi.IsWindowEnabled(w.sndErr.test) || !winapi.IsWindowEnabled(w.sndSpeaker) {
		t.Errorf("section on again: %+v", got(w.sndWarn))
	}
	// «Вывод звука» per level through the window Save path
	winapi.SendMessage(w.sndWarn.out, winapi.CB_SETCURSEL, 1, 0) // «Колонки»
	winapi.SendMessage(w.sndErr.out, winapi.CB_SETCURSEL, 2, 0)  // «Динамик и колонки»
	if bad := w.persistSoundSection(); bad != "" || store.saves == 0 {
		t.Fatalf("save: %q", bad)
	}
	if p := app.SoundPrefs(); p.Warning.Output != trayalert.OutputSpeakers || p.Error.Output != trayalert.OutputBoth {
		t.Fatalf("stored outputs %q / %q", p.Warning.Output, p.Error.Output)
	}
	raw, _ := store.Load()
	if !strings.Contains(raw, `"output":"speakers"`) || !strings.Contains(raw, `"output":"both"`) {
		t.Errorf("HKCU JSON %s", raw)
	}
	w.loadSoundSection()
	if winapi.SendMessage(w.sndWarn.out, winapi.CB_GETCURSEL, 0, 0) != 1 || winapi.SendMessage(w.sndErr.out, winapi.CB_GETCURSEL, 0, 0) != 2 {
		t.Error("outputs not loaded back")
	}
}
