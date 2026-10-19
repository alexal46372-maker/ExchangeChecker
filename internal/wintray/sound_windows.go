//go:build windows

package wintray

import (
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
	lang  func() string

	mu     sync.Mutex
	failed map[string]bool // files already logged as failing
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
		lang := i18n.RU
		if w.lang != nil {
			lang = w.lang()
		}
		w.log("tray: sound: " + fmt.Sprintf(i18n.T(i18n.KeySoundFallback, lang), file, err))
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

// playTone: the sine WAV with the volume as amplitude (PLAN §12 p.115);
// PlaySound refused (no audio device) → kernel32 Beep without volume.
func (w *winSounder) playTone(freq, dur, vol int, stop <-chan struct{}) error {
	if vol <= 0 {
		waitOrStop(time.Duration(dur)*time.Millisecond, stop)
		return nil
	}
	if err := winapi.PlayWAV(w.alias, trayalert.SineWAV(freq, dur, vol), false); err != nil {
		w.fallback("PlaySound", err)
		return winapi.Beep(uint32(freq), uint32(dur))
	}
	if !waitOrStop(time.Duration(dur)*time.Millisecond+20*time.Millisecond, stop) {
		winapi.StopWAV(w.alias)
	}
	return nil
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
