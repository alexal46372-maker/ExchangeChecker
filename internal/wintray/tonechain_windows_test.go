//go:build windows

package wintray

import (
	"bytes"
	"errors"
	"strings"
	"testing"

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
}

func (r *toneRec) outputs() toneOutputs {
	return toneOutputs{
		playWAV: func(alias string, wav []byte) error {
			r.calls, r.wav = append(r.calls, "PlaySound:"+alias), wav
			if r.failWAV {
				return errors.New("no audio device")
			}
			return nil
		},
		stopWAV: func(string) { r.calls = append(r.calls, "stop") },
		beep: func(f, d uint32) error {
			r.calls = append(r.calls, "Beep")
			if f != 880 || d != 30 {
				return errors.New("bad args")
			}
			if r.failBep {
				return errors.New("no beep")
			}
			return nil
		},
		messageBeep: func() error { r.calls = append(r.calls, "MessageBeep"); return r.errMB },
	}
}

// TestCHK_TRAY_97_ToneFallbackChain — «Тон» goes PlaySound(SND_MEMORY,
// sine WAV at the volume) → Beep → MessageBeep(MB_ICONHAND), each step
// only when the one before failed; each fallback is logged once; volume 0
// plays nothing. Runs through winSounder.Play (the real dispatch).
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
	if want := trayalert.SineWAV(880, 30, 50); !bytes.Equal(r.wav, want) || !bytes.HasPrefix(r.wav, []byte("RIFF")) {
		t.Error("PlaySound got another WAV than the sine of the profile")
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

// TestCHK_TRAY_97_ToneLabel — the UI says the tone sounds through the
// speakers / sound card, not the system speaker.
func TestCHK_TRAY_97_ToneLabel(t *testing.T) {
	if l := i18n.T(i18n.KeySoundBeep); !strings.Contains(l, "колонки") {
		t.Errorf("label %q", l)
	}
	if tip := i18n.T(i18n.KeyTipSoundKind); !strings.Contains(tip, "через колонки или звуковую карту, не через системный динамик") {
		t.Errorf("tip %q", tip)
	}
}
