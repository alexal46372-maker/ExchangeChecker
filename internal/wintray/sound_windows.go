//go:build windows

package wintray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayipc"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayui"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Phase 10 A (CHK-TRAY-33..35): sound profiles in HKCU, the Windows
// sounder (sine WAV through PlaySound, files through MCI, fallback to the
// tone with a log line) and the master volume.

// SoundValue is the HKCU value of the sound profiles (REG_SZ JSON,
// trayalert.SoundPrefs) under TrayKey.
const SoundValue = "Sound"

// SoundStore keeps the per-user sound settings; raw "" = not stored.
type SoundStore interface {
	Load() (raw string, err error)
	Save(raw string) error
}

// RegistrySoundStore stores them in HKCU\Key\Sound.
type RegistrySoundStore struct {
	Key string // "" = TrayKey
}

func (s RegistrySoundStore) key() string {
	if s.Key == "" {
		return TrayKey
	}
	return s.Key
}

// Load implements SoundStore (missing = "", nil).
func (s RegistrySoundStore) Load() (string, error) {
	v, ok, err := winapi.RegGetStringView(winapi.HKEY_CURRENT_USER, s.key(), SoundValue, 0)
	if err != nil || !ok {
		return "", err
	}
	return v, nil
}

// Save implements SoundStore.
func (s RegistrySoundStore) Save(raw string) error {
	return winapi.RegSetStringView(winapi.HKEY_CURRENT_USER, s.key(), SoundValue, raw, 0)
}

// MCI aliases and PlaySound owners: the alarm and «Прослушать» never
// share a device or a buffer (the deferred close only closes its own
// alias).
const (
	aliasAlarm = "ecalarm"
	aliasTest  = "ectest"
)

// winSounder plays a Sound on Windows under its own alias.
type winSounder struct {
	alias string
	log   func(string)
	tone  toneOutputs // zero = the real WinAPI (tests: fakes)
	// speaker: the PC speaker of the service (phase 14, CHK-TRAY-98),
	// tried first for «Тон»; nil = only the toneOutputs.
	speaker speakerOut

	mu     sync.Mutex
	failed map[string]bool // files already logged as failing
}

// speakerOut is the PC speaker through the service (*trayipc.Speaker).
type speakerOut interface {
	Status(ctx context.Context) model.SpeakerStatus
	Tone(ctx context.Context, freqHz, durMs int) error
	Stop(ctx context.Context) error
}

// speakerOf: nil for a nil *trayipc.Speaker (no typed nil interface).
func speakerOf(sp *trayipc.Speaker) speakerOut {
	if sp == nil {
		return nil
	}
	return sp
}

// Play implements trayalert.Sounder.
func (w *winSounder) Play(s trayalert.Sound, stop <-chan struct{}) error {
	p := s.Profile
	if p.Kind == trayalert.SoundFile {
		err := w.playFile(p.File, s.Volume, stop)
		if err == nil {
			return nil
		}
		w.fallback(p.File, err)
	}
	return w.playTone(p.FreqHz, p.DurMs, s.Volume, stop)
}

func (w *winSounder) fallback(file string, err error) {
	w.mu.Lock()
	if w.failed == nil {
		w.failed = map[string]bool{}
	}
	first := !w.failed[file]
	w.failed[file] = true
	w.mu.Unlock()
	if first && w.log != nil {
		w.log("tray: sound: " + fmt.Sprintf(i18n.T(i18n.KeySoundFallback), file, err))
	}
}

// waitStep is the tick of waitOrStop.
const waitStep = 20 * time.Millisecond

// waitOrStop waits d on one ticker; false = stopped.
func waitOrStop(d time.Duration, stop <-chan struct{}) bool {
	end := time.Now().Add(d)
	t := time.NewTicker(waitStep)
	defer t.Stop()
	for time.Now().Before(end) {
		select {
		case <-stop:
			return false
		case <-t.C:
		}
	}
	return true
}

// toneOutputs are the fallback outputs of «Тон», tried in order after the
// PC speaker of the service (phase 13 item 1 CHK-TRAY-97, phase 14
// CHK-TRAY-98). All of them sound through the speakers / sound card.
type toneOutputs struct {
	playWAV     func(alias string, wav []byte) error // winmm PlaySound(SND_MEMORY) of the sine WAV
	stopWAV     func(alias string)
	beep        func(freq, dur uint32) error // kernel32 Beep (fixed level)
	messageBeep func() error                 // user32 MessageBeep(MB_ICONHAND)
}

func (o toneOutputs) withDefaults() toneOutputs {
	if o.playWAV == nil {
		o.playWAV = func(alias string, wav []byte) error { return winapi.PlayWAV(alias, wav, false) }
	}
	if o.stopWAV == nil {
		o.stopWAV = winapi.StopWAV
	}
	if o.beep == nil {
		o.beep = winapi.Beep
	}
	if o.messageBeep == nil {
		o.messageBeep = func() error { return winapi.MessageBeep(winapi.MB_ICONHAND) }
	}
	return o
}

// playTone (phase 14, CHK-TRAY-98/99): volume 0 % = silence, nothing is
// called (no IPC either); otherwise full level (trayalert.ToneLevel):
// first the PC speaker of the service (capabilities speaker +
// speaker_status, status.available) with speaker_tone {freqHz, durMs} and
// speaker_tone {durMs:0} at once on stop; not available / refused → one
// journal line per reason and the old chain: the sine WAV through
// PlaySound(SND_MEMORY); refused (no audio device) → kernel32 Beep;
// Beep failed too → MessageBeep(MB_ICONHAND), the system critical sound
// (CHK-TRAY-97). Each fallback is logged once.
func (w *winSounder) playTone(freq, dur, vol int, stop <-chan struct{}) error {
	d := time.Duration(dur) * time.Millisecond
	if trayalert.ToneLevel(vol) == 0 {
		waitOrStop(d, stop)
		return nil
	}
	if w.speaker != nil {
		reason, ok := w.playSpeaker(freq, dur, stop)
		if ok {
			return nil
		}
		w.speakerFallback(reason)
	}
	o := w.tone.withDefaults()
	if err := o.playWAV(w.alias, trayalert.SineWAV(freq, dur, trayalert.ToneLevel(vol))); err != nil {
		w.fallback("PlaySound", err)
		if err := o.beep(uint32(freq), uint32(dur)); err != nil {
			w.fallback("Beep", err)
			// MessageBeep returns at once: wait the tone duration, so the
			// cadence (tone, gap, tone) stays the same
			err := o.messageBeep()
			waitOrStop(d, stop)
			return err
		}
		return nil
	}
	if !waitOrStop(d+20*time.Millisecond, stop) {
		o.stopWAV(w.alias)
	}
	return nil
}

// playSpeaker plays one tone through the service; ok = it sounded (or
// was stopped), else the reason for the fallback.
func (w *winSounder) playSpeaker(freq, dur int, stop <-chan struct{}) (reason string, ok bool) {
	ctx := context.Background()
	st := w.speaker.Status(ctx)
	if !st.Available {
		return trayui.SpeakerReason(st.Reason), false
	}
	start := time.Now()
	if err := w.speaker.Tone(ctx, freq, dur); err != nil {
		return trayui.SpeakerReason(err.Error()), false
	}
	w.mu.Lock()
	for k := range w.failed { // available again: a new loss is logged again
		if strings.HasPrefix(k, speakerLogKey) {
			delete(w.failed, k)
		}
	}
	w.mu.Unlock()
	// the service may answer at once or after the tone: wait the rest
	rest := time.Duration(dur)*time.Millisecond - time.Since(start)
	if rest < 0 {
		rest = 0
	}
	if !waitOrStop(rest+20*time.Millisecond, stop) {
		_ = w.speaker.Stop(ctx) // «Подтвердить» / player stop / «Остановить»: speaker_tone {durMs:0} at once
	}
	return "", true
}

// speakerLogKey prefixes the failed-map keys of the speaker reasons.
const speakerLogKey = "\x00speaker:"

// speakerFallback logs «Системный динамик недоступен (…)» once per reason.
func (w *winSounder) speakerFallback(reason string) {
	w.mu.Lock()
	if w.failed == nil {
		w.failed = map[string]bool{}
	}
	k := speakerLogKey + reason
	first := !w.failed[k]
	w.failed[k] = true
	w.mu.Unlock()
	if first && w.log != nil {
		w.log("tray: sound: " + fmt.Sprintf(i18n.T(i18n.KeySpeakerFallback), reason))
	}
}

// errBadPath: control characters (CR, LF, NUL…) or quotes would end or
// extend the MCI command string.
var errBadPath = errors.New("the file name contains control characters or quotes")

// mciOpenCmd builds the open command; paths with control characters or
// quotes are refused before they reach mciSendString.
func mciOpenCmd(path, alias string) (string, error) {
	for _, r := range path {
		if r < 0x20 || r == 0x7f || r == '"' || unicode.IsControl(r) {
			return "", errBadPath
		}
	}
	return `open "` + path + `" type ` + trayalert.MCIType(path) + ` alias ` + alias, nil
}

// playFile plays a file once through MCI on one OS thread.
func (w *winSounder) playFile(path string, vol int, stop <-chan struct{}) error {
	cmd, err := mciOpenCmd(path, w.alias)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	winapi.MCI("close " + w.alias) // only our own alias
	if _, err := winapi.MCI(cmd); err != nil {
		return err
	}
	defer winapi.MCI("close " + w.alias)
	if trayalert.MCIType(path) == "mpegvideo" {
		winapi.MCI(fmt.Sprintf("setaudio %s volume to %d", w.alias, trayalert.MCIVolume(vol)))
	}
	if _, err := winapi.MCI("play " + w.alias); err != nil {
		return err
	}
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			winapi.MCI("stop " + w.alias)
			return nil
		case <-t.C:
		}
		mode, err := winapi.MCI("status " + w.alias + " mode")
		if err != nil || mode != "playing" {
			return nil
		}
	}
}

// loadSoundPrefs reads HKCU; not stored = defaults from the service beep
// settings (follow them until the user saves).
func (a *App) loadSoundPrefs() {
	d := trayalert.DefaultSoundPrefs(a.serviceSettings())
	a.soundPrefs = d
	a.soundStored = false
	if a.opt.SoundStore == nil {
		return
	}
	raw, err := a.opt.SoundStore.Load()
	if err != nil {
		a.logf("tray: sound: read HKCU %s\\%s: %v (using defaults)", TrayKey, SoundValue, err)
		return
	}
	if strings.TrimSpace(raw) == "" {
		return
	}
	p, err := trayalert.ParseSoundPrefs(raw, d)
	if err != nil {
		a.logf("tray: sound: HKCU %s\\%s: %v (using defaults)", TrayKey, SoundValue, err)
		return
	}
	a.soundPrefs, a.soundStored = p, true
}

func (a *App) serviceSettings() *model.Settings {
	if a.status.Snapshot != nil {
		return a.status.Snapshot.Settings
	}
	return nil
}

// SoundPrefs are the current sound settings.
func (a *App) SoundPrefs() trayalert.SoundPrefs { return a.soundPrefs }

// SaveSoundPrefs stores and applies the sound settings.
func (a *App) SaveSoundPrefs(p trayalert.SoundPrefs) error {
	p = p.Normalized(trayalert.DefaultSoundPrefs(a.serviceSettings()))
	a.soundPrefs, a.soundStored = p, true
	a.applySound()
	if a.opt.SoundStore != nil {
		return a.opt.SoundStore.Save(p.String())
	}
	return nil
}

// setMasterVolumeLive changes the master volume in memory (the alarm
// follows at once) without writing HKCU (the menu slider while dragging).
func (a *App) setMasterVolumeLive(v int) {
	p := a.soundPrefs
	p.Master = v
	a.soundPrefs = p.Normalized(trayalert.DefaultSoundPrefs(a.serviceSettings()))
	a.applySound()
}

// SetMasterVolume changes and stores the master volume (0..100).
func (a *App) SetMasterVolume(v int) error {
	p := a.soundPrefs
	p.Master = v
	return a.SaveSoundPrefs(p)
}

// alarmOn: the alarm sounds now (paused while «Прослушать» plays).
func (a *App) alarmOn(v trayalert.View) bool { return v.Sound && !a.previewing }

// alertSound is the sound of the alert view v.
func (a *App) alertSound(v trayalert.View) trayalert.Sound {
	if !a.soundStored { // follow the service beep settings
		m := a.soundPrefs.Master
		a.soundPrefs = trayalert.DefaultSoundPrefs(a.serviceSettings())
		a.soundPrefs.Master = m
	}
	pr := a.soundPrefs.For(v.Level)
	return trayalert.Sound{Profile: pr, Volume: trayalert.EffectiveVolume(pr.Volume, a.soundPrefs.Master)}
}

// applySound restarts the alarm sound with the current settings.
func (a *App) applySound() {
	a.player.Set(a.alarmOn(a.alertView), a.alertSound(a.alertView))
}

// testSound plays a profile once («Прослушать») on its own player and
// alias; there is one PlaySound per process, so the alarm pauses and
// resumes when the preview ends.
func (a *App) testSound(p trayalert.SoundProfile, master int) {
	if a.tester == nil {
		a.tester = trayalert.NewSoundPlayer(a.testSounder)
		a.tester.OnDone = func() { a.runOnUI(a.endPreview) }
	}
	p.Loop = false
	a.tester.Set(false, trayalert.Sound{})
	a.previewing = true
	a.applySound() // pause the alarm
	a.tester.Set(true, trayalert.Sound{Profile: p, Volume: trayalert.EffectiveVolume(p.Volume, master)})
}

// endPreview resumes the alarm after «Прослушать».
func (a *App) endPreview() {
	if !a.previewing {
		return
	}
	a.previewing = false
	a.applySound()
}

// stopTestSound stops «Прослушать» and resumes the alarm.
func (a *App) stopTestSound() {
	if a.tester != nil {
		a.tester.Set(false, trayalert.Sound{})
	}
	a.endPreview()
}

// Previewing reports a running «Прослушать» (test hook).
func (a *App) Previewing() bool { return a.previewing }
