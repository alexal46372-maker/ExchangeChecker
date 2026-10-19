//go:build windows

package wintray

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/trayalert"
	"github.com/alexal46372-maker/ExchangeChecker/internal/winapi"
)

// Phase 10 A (CHK-TRAY-33..35): sound profiles in HKCU, the Windows
// sounder (sine WAV through PlaySound, files through MCI, fallback to the
// tone with a log line) and the master volume.

// SoundValue is the HKCU value of the sound profiles (REG_SZ JSON,
// trayalert.SoundPrefs) under LangKey.
const SoundValue = "Sound"

// SoundStore keeps the per-user sound settings; raw "" = not stored.
type SoundStore interface {
	Load() (raw string, err error)
	Save(raw string) error
}

// RegistrySoundStore stores them in HKCU\Key\Sound.
type RegistrySoundStore struct {
	Key string // "" = LangKey
}

func (s RegistrySoundStore) key() string {
	if s.Key == "" {
		return LangKey
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

// winSounder plays a Sound on Windows.
type winSounder struct {
	log  func(string)
	lang func() string

	mu     sync.Mutex
	failed map[string]bool // files already logged as failing
}

const mciAlias = "ecalarm"

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
		lang := i18n.RU
		if w.lang != nil {
			lang = w.lang()
		}
		w.log("tray: sound: " + fmt.Sprintf(i18n.T(i18n.KeySoundFallback, lang), file, err))
	}
}

func waitOrStop(d time.Duration, stop <-chan struct{}) bool {
	select {
	case <-stop:
		return false
	case <-time.After(d):
		return true
	}
}

// playTone: the sine WAV with the volume as amplitude (PLAN §12 p.115);
// PlaySound refused (no audio device) → kernel32 Beep without volume.
func (w *winSounder) playTone(freq, dur, vol int, stop <-chan struct{}) error {
	if vol <= 0 {
		waitOrStop(time.Duration(dur)*time.Millisecond, stop)
		return nil
	}
	if err := winapi.PlayWAV(trayalert.SineWAV(freq, dur, vol), false); err != nil {
		w.fallback("PlaySound", err)
		return winapi.Beep(uint32(freq), uint32(dur))
	}
	if !waitOrStop(time.Duration(dur)*time.Millisecond+20*time.Millisecond, stop) {
		winapi.StopWAV()
	}
	return nil
}

// playFile plays a file once through MCI on one OS thread.
func (w *winSounder) playFile(path string, vol int, stop <-chan struct{}) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if !trayalert.IsSoundFile(path) {
		// «Все файлы»: let MCI try (DirectShow knows more types)
		_ = path
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	winapi.MCI("close " + mciAlias)
	q := strings.ReplaceAll(path, `"`, "")
	typ := trayalert.MCIType(path)
	if _, err := winapi.MCI(`open "` + q + `" type ` + typ + ` alias ` + mciAlias); err != nil {
		return err
	}
	defer winapi.MCI("close " + mciAlias)
	if typ == "mpegvideo" {
		winapi.MCI(fmt.Sprintf("setaudio %s volume to %d", mciAlias, trayalert.MCIVolume(vol)))
	}
	if _, err := winapi.MCI("play " + mciAlias); err != nil {
		return err
	}
	for {
		if !waitOrStop(100*time.Millisecond, stop) {
			winapi.MCI("stop " + mciAlias)
			return nil
		}
		mode, err := winapi.MCI("status " + mciAlias + " mode")
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
		a.logf("tray: sound: read HKCU %s\\%s: %v (using defaults)", LangKey, SoundValue, err)
		return
	}
	if strings.TrimSpace(raw) == "" {
		return
	}
	p, err := trayalert.ParseSoundPrefs(raw, d)
	if err != nil {
		a.logf("tray: sound: HKCU %s\\%s: %v (using defaults)", LangKey, SoundValue, err)
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

// SetMasterVolume changes and stores the master volume (0..100).
func (a *App) SetMasterVolume(v int) error {
	p := a.soundPrefs
	p.Master = v
	return a.SaveSoundPrefs(p)
}

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
	a.player.Set(a.alertView.Sound, a.alertSound(a.alertView))
}

// testSound plays a profile once («Прослушать»), on its own player.
func (a *App) testSound(p trayalert.SoundProfile, master int) {
	if a.tester == nil {
		a.tester = trayalert.NewSoundPlayer(a.sounder)
	}
	p.Loop = false
	a.tester.Set(false, trayalert.Sound{})
	a.tester.Set(true, trayalert.Sound{Profile: p, Volume: trayalert.EffectiveVolume(p.Volume, master)})
}

// stopTestSound stops «Прослушать».
func (a *App) stopTestSound() {
	if a.tester != nil {
		a.tester.Set(false, trayalert.Sound{})
	}
}
