package trayalert

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 10 A (CHK-TRAY-33..35): alert sound profiles of the tray user
// (HKCU, Кончита's decision 1), a beep synthesized as a sine WAV in
// memory (volume = sample amplitude, CHK-TRAY-34) and a master volume.

// SoundKind is the source of a profile.
type SoundKind string

const (
	SoundBeep SoundKind = "beep" // sine tone (FreqHz, DurMs)
	SoundFile SoundKind = "file" // WAV / MP3 / WMA / MID through MCI
)

// SoundProfile is the sound of one level.
type SoundProfile struct {
	Kind   SoundKind `json:"kind"`
	FreqHz int       `json:"freqHz"`
	DurMs  int       `json:"durMs"`
	File   string    `json:"file,omitempty"`
	Volume int       `json:"volume"` // 0..100 %
	Loop   bool      `json:"loop"`
	GapMs  int       `json:"gapMs"`
}

// SoundPrefs are the per-user sound settings (one REG_SZ JSON value).
type SoundPrefs struct {
	Warning SoundProfile `json:"warning"`
	Error   SoundProfile `json:"error"`
	Master  int          `json:"master"` // 0..100 %
}

// Default volumes (Кончита's decision 2).
const (
	DefaultWarningVolume = 80
	DefaultErrorVolume   = 100
	DefaultMasterVolume  = 100
)

// SoundExts are the file types of SoundFile (decision 3).
var SoundExts = []string{".wav", ".mp3", ".wma", ".mid", ".midi", ".rmi"}

// DefaultSoundPrefs: beep tones of the service settings (s may be nil),
// volume 80 / 100 %, looped.
func DefaultSoundPrefs(s *model.Settings) SoundPrefs {
	w, e := ToneFor(model.LevelWarning, s), ToneFor(model.LevelError, s)
	return SoundPrefs{
		Warning: SoundProfile{Kind: SoundBeep, FreqHz: w.FreqHz, DurMs: w.DurMs, Volume: DefaultWarningVolume, Loop: true, GapMs: w.GapMs},
		Error:   SoundProfile{Kind: SoundBeep, FreqHz: e.FreqHz, DurMs: e.DurMs, Volume: DefaultErrorVolume, Loop: true, GapMs: e.GapMs},
		Master:  DefaultMasterVolume,
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Normalized clamps the values (d: the defaults of the same level).
func (p SoundProfile) Normalized(d SoundProfile) SoundProfile {
	if p.Kind != SoundFile {
		p.Kind = SoundBeep
	}
	if p.FreqHz < model.MinBeepFreqHz || p.FreqHz > model.MaxBeepFreqHz {
		p.FreqHz = d.FreqHz
	}
	if p.DurMs < 1 || p.DurMs > model.MaxBeepDurMs {
		p.DurMs = d.DurMs
	}
	if p.GapMs < 0 || p.GapMs > model.MaxBeepGapMs {
		p.GapMs = d.GapMs
	}
	p.Volume = clamp(p.Volume, 0, 100)
	p.File = strings.TrimSpace(p.File)
	if p.Kind == SoundFile && p.File == "" {
		p.Kind = SoundBeep
	}
	return p
}

// Normalized clamps all values.
func (p SoundPrefs) Normalized(d SoundPrefs) SoundPrefs {
	p.Warning = p.Warning.Normalized(d.Warning)
	p.Error = p.Error.Normalized(d.Error)
	p.Master = clamp(p.Master, 0, 100)
	return p
}

// ParseSoundPrefs reads the stored JSON; empty or invalid = defaults.
func ParseSoundPrefs(raw string, d SoundPrefs) (SoundPrefs, error) {
	if strings.TrimSpace(raw) == "" {
		return d, nil
	}
	p := d
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return d, err
	}
	return p.Normalized(d), nil
}

// String is the stored JSON.
func (p SoundPrefs) String() string {
	b, _ := json.Marshal(p)
	return string(b)
}

// For returns the profile of level.
func (p SoundPrefs) For(level model.AlertLevel) SoundProfile {
	if level >= model.LevelError {
		return p.Error
	}
	return p.Warning
}

// EffectiveVolume is the profile volume scaled by the master volume.
func EffectiveVolume(profile, master int) int {
	return clamp(profile, 0, 100) * clamp(master, 0, 100) / 100
}

// IsSoundFile reports a supported extension (case-insensitive).
func IsSoundFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range SoundExts {
		if ext == e {
			return true
		}
	}
	return false
}

// MCIType is the MCI device type of a file: sequencer for MIDI,
// mpegvideo (DirectShow) for WAV / MP3 / WMA — it supports
// "setaudio … volume".
func MCIType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mid", ".midi", ".rmi":
		return "sequencer"
	}
	return "mpegvideo"
}

// MCIVolume: "setaudio … volume to N" takes 0..1000.
func MCIVolume(pct int) int { return clamp(pct, 0, 100) * 10 }

// SineWAV parameters: 16-bit PCM mono at WAVRate, 5 ms fade in / out.
const (
	WAVRate   = 22050
	wavRampMs = 5
)

// SineWAV builds a RIFF/WAVE file in memory: a sine of freqHz for durMs
// with the amplitude volumePct of full scale (PlaySound SND_MEMORY plays
// it; the volume never touches the system mixer).
func SineWAV(freqHz, durMs, volumePct int) []byte {
	n := WAVRate * clamp(durMs, 1, model.MaxBeepDurMs) / 1000
	if n < 1 {
		n = 1
	}
	amp := float64(clamp(volumePct, 0, 100)) / 100 * 32767
	ramp := WAVRate * wavRampMs / 1000
	data := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		g := 1.0
		if i < ramp {
			g = float64(i) / float64(ramp)
		}
		if r := n - 1 - i; r < ramp {
			g = math.Min(g, float64(r)/float64(ramp))
		}
		v := amp * g * math.Sin(2*math.Pi*float64(freqHz)*float64(i)/WAVRate)
		binary.LittleEndian.PutUint16(data[2*i:], uint16(int16(math.Round(v))))
	}
	b := make([]byte, 44, 44+len(data))
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+len(data)))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1) // PCM
	binary.LittleEndian.PutUint16(b[22:], 1) // mono
	binary.LittleEndian.PutUint32(b[24:], WAVRate)
	binary.LittleEndian.PutUint32(b[28:], WAVRate*2)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(data)))
	return append(b, data...)
}

// Sound is one thing to play: the profile and the effective volume.
type Sound struct {
	Profile SoundProfile
	Volume  int
}

// Sounder plays one sound once and returns when it ended or stop was
// closed (the Windows one: PlaySound / MCI with fallback to Beep).
type Sounder interface {
	Play(s Sound, stop <-chan struct{}) error
}

// BeeperSounder adapts a Beeper (kernel32 Beep; tests): the tone only,
// no volume.
type BeeperSounder struct{ B Beeper }

// Play implements Sounder.
func (b BeeperSounder) Play(s Sound, stop <-chan struct{}) error {
	return b.B.Beep(s.Profile.FreqHz, s.Profile.DurMs)
}

// SoundPlayer repeats a sound until stopped (Loop) or plays it once.
type SoundPlayer struct {
	out Sounder

	mu     sync.Mutex
	on     bool
	cur    Sound
	stop   chan struct{}
	closed bool
	plays  int // sounds started (test hook)
	// OnDone runs (on the player goroutine) when a sound played once
	// ended by itself.
	OnDone func()
}

// NewSoundPlayer returns a stopped player.
func NewSoundPlayer(out Sounder) *SoundPlayer { return &SoundPlayer{out: out} }

// Set starts, stops or changes the sound; never blocks on a playing one.
func (p *SoundPlayer) Set(on bool, s Sound) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || (on == p.on && (!on || s == p.cur)) {
		return
	}
	if p.stop != nil {
		close(p.stop)
		p.stop = nil
	}
	p.on, p.cur = on, s
	if on {
		p.stop = make(chan struct{})
		go p.loop(s, p.stop)
	}
}

// Playing reports whether the loop is on.
func (p *SoundPlayer) Playing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.on
}

// Plays is the number of sounds started.
func (p *SoundPlayer) Plays() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.plays
}

// Close stops for good.
func (p *SoundPlayer) Close() {
	p.Set(false, Sound{})
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
}

func (p *SoundPlayer) loop(s Sound, stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		p.mu.Lock()
		p.plays++
		p.mu.Unlock()
		start := time.Now()
		err := p.out.Play(s, stop)
		if !s.Profile.Loop {
			p.mu.Lock()
			natural := p.stop == stop
			if natural {
				p.on, p.stop = false, nil
			}
			done := p.OnDone
			p.mu.Unlock()
			if natural && done != nil {
				done()
			}
			return
		}
		wait := time.Duration(s.Profile.GapMs) * time.Millisecond
		if err != nil {
			if rest := time.Duration(s.Profile.DurMs)*time.Millisecond - time.Since(start); rest > wait {
				wait = rest
			}
		}
		if wait < minWait {
			wait = minWait
		}
		select {
		case <-stop:
			return
		case <-time.After(wait):
		}
	}
}
