//go:build windows

package wintray

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// toneRec records the outputs of «Тон» the winSounder tried.
type toneRec struct {
	calls            []string
	wav              []byte
	failWAV, failBep bool
	errMB            error
	mu               sync.Mutex
}

// n / count: the recorded calls (the player goroutine appends them).
func (r *toneRec) n() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *toneRec) count(call string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := 0
	for _, c := range r.calls {
		if c == call {
			k++
		}
	}
	return k
}

func (r *toneRec) outputs() toneOutputs {
	return toneOutputs{
		playWAV: func(alias string, wav []byte) error {
			r.mu.Lock()
			r.calls, r.wav = append(r.calls, "PlaySound:"+alias), wav
			r.mu.Unlock()
			if r.failWAV {
				return errors.New("no audio device")
			}
			return nil
		},
		stopWAV: func(string) { r.mu.Lock(); r.calls = append(r.calls, "stop"); r.mu.Unlock() },
		beep: func(f, d uint32) error {
			r.mu.Lock()
			r.calls = append(r.calls, "Beep")
			r.mu.Unlock()
			if f != 880 || d != 30 {
				return errors.New("bad args")
			}
			if r.failBep {
				return errors.New("no beep")
			}
			return nil
		},
		messageBeep: func() error { r.mu.Lock(); r.calls = append(r.calls, "MessageBeep"); r.mu.Unlock(); return r.errMB },
	}
}

// TestCHK_TRAY_97_ToneFallbackChain — without the PC speaker of the
// service (phase 14: no speaker here; with one see CHK-TRAY-98) «Тон»
// goes PlaySound(SND_MEMORY, sine WAV at full level — the tone volume is
// not adjustable, CHK-TRAY-99) → Beep → MessageBeep(MB_ICONHAND), each
// step only when the one before failed; each fallback is logged once;
// volume 0 plays nothing. Runs through winSounder.Play (the real dispatch).
func TestCHK_TRAY_97_ToneFallbackChain(t *testing.T) {
	tone := trayalert.Sound{Profile: trayalert.SoundProfile{Kind: trayalert.SoundBeep, FreqHz: 880, DurMs: 30}, Volume: 50}
	var logs []string
	newS := func(r *toneRec) *winSounder {
		return &winSounder{alias: aliasTest, log: func(s string) { logs = append(logs, s) }, tone: r.outputs()}
	}
	// 1: the sound card plays the sine WAV — no fallback
	r := &toneRec{}
	if err := newS(r).Play(tone, nil); err != nil || strings.Join(r.calls, ",") != "PlaySound:"+aliasTest || len(logs) != 0 {
		t.Fatalf("PlaySound ok: %v %v %v", err, r.calls, logs)
	}
	// phase 14: volume 50 % > 0 → the sine at full level (100), not 50
	if want := trayalert.SineWAV(880, 30, 100); !bytes.Equal(r.wav, want) || !bytes.HasPrefix(r.wav, []byte("RIFF")) {
		t.Error("PlaySound got another WAV than the full-level sine of the profile")
	}
	if bytes.Equal(r.wav, trayalert.SineWAV(880, 30, 50)) {
		t.Error("the tone still follows the volume")
	}
	// 2: PlaySound refused → Beep
	r = &toneRec{failWAV: true}
	if err := newS(r).Play(tone, nil); err != nil || strings.Join(r.calls, ",") != "PlaySound:"+aliasTest+",Beep" || len(logs) != 1 || !strings.Contains(logs[0], "PlaySound") {
		t.Fatalf("Beep fallback: %v %v %v", err, r.calls, logs)
	}
	// 3: Beep fails too → MessageBeep(MB_ICONHAND); logged once per sounder
	logs = nil
	r = &toneRec{failWAV: true, failBep: true}
	s := newS(r)
	for i := 0; i < 2; i++ {
		if err := s.Play(tone, nil); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(r.calls, ",") != strings.Repeat("PlaySound:"+aliasTest+",Beep,MessageBeep,", 2)[:len(strings.Repeat("PlaySound:"+aliasTest+",Beep,MessageBeep,", 2))-1] {
		t.Fatalf("MessageBeep fallback: %v", r.calls)
	}
	if len(logs) != 2 || !strings.Contains(logs[0], "PlaySound") || !strings.Contains(logs[1], "Beep") {
		t.Errorf("fallback logs %q", logs)
	}
	// 3b: MessageBeep returns at once, the tone still lasts its duration
	// (the cadence tone / gap stays); a stop ends the wait early
	r = &toneRec{failWAV: true, failBep: true}
	long := tone
	long.Profile.DurMs = 250
	start := time.Now()
	if err := newS(r).Play(long, nil); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 240*time.Millisecond {
		t.Errorf("MessageBeep fallback lasted %v, want the tone duration 250 ms", d)
	}
	stop := make(chan struct{})
	close(stop)
	start = time.Now()
	if err := newS(&toneRec{failWAV: true, failBep: true}).Play(long, stop); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("stop did not end the MessageBeep wait: %v", d)
	}
	// 4: the last fallback's error is returned
	r = &toneRec{failWAV: true, failBep: true, errMB: errors.New("mb")}
	if err := newS(r).Play(tone, nil); err == nil || err.Error() != "mb" {
		t.Errorf("MessageBeep error %v", err)
	}
	// 5: volume 0 plays nothing
	r = &toneRec{}
	mute := tone
	mute.Volume = 0
	if err := newS(r).Play(mute, nil); err != nil || len(r.calls) != 0 {
		t.Errorf("volume 0: %v", r.calls)
	}
	// the zero toneOutputs are the real WinAPI (MessageBeep exists in user32)
	if d := (toneOutputs{}).withDefaults(); d.playWAV == nil || d.stopWAV == nil || d.beep == nil || d.messageBeep == nil {
		t.Fatal("defaults")
	}
	if err := winapi.MessageBeep(winapi.MB_ICONHAND); err != nil {
		t.Logf("MessageBeep on this runner: %v", err)
	}
}

// TestCHK_TRAY_97_ToneLabel — phase 14 (Алекс, 10.11.2026): the label is
// «Тон (системный динамик)»; the tip says the PC speaker comes first, the
// speakers / sound card are the fallback and the volume is not adjustable.
func TestCHK_TRAY_97_ToneLabel(t *testing.T) {
	if l := i18n.T(i18n.KeySoundBeep); l != "Тон (системный динамик)" {
		t.Errorf("label %q", l)
	}
	tip := i18n.T(i18n.KeyTipSoundKind)
	for _, want := range []string{"системный динамик", "через колонки или звуковую карту", "не регулируется"} {
		if !strings.Contains(tip, want) {
			t.Errorf("tip %q lacks %q", tip, want)
		}
	}
}
