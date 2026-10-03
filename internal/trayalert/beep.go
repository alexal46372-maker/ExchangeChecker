package trayalert

import (
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Tone of the endless alert beep.
type Tone struct {
	FreqHz, DurMs, GapMs int
}

// ToneFor returns the tone of level from the settings (StatusSnapshot
// .settings, PLAN §4.3); missing or out-of-range values use the defaults.
func ToneFor(level model.AlertLevel, s *model.Settings) Tone {
	d := model.DefaultSettings().Beep
	b := d
	if s != nil {
		b = s.Beep
	}
	freq, dur := b.WarningFreqHz, b.WarningDurMs
	dfreq, ddur := d.WarningFreqHz, d.WarningDurMs
	if level >= model.LevelError {
		freq, dur = b.ErrorFreqHz, b.ErrorDurMs
		dfreq, ddur = d.ErrorFreqHz, d.ErrorDurMs
	}
	if freq < model.MinBeepFreqHz || freq > model.MaxBeepFreqHz {
		freq = dfreq
	}
	if dur < 1 || dur > model.MaxBeepDurMs {
		dur = ddur
	}
	gap := b.GapMs
	if gap < 0 || gap > model.MaxBeepGapMs {
		gap = d.GapMs
	}
	return Tone{FreqHz: freq, DurMs: dur, GapMs: gap}
}

// Beeper plays one tone and returns when it ends (kernel32 Beep is
// synchronous). Tests use a fake.
type Beeper interface {
	Beep(freqHz, durMs int) error
}

// minWait between beeps when Beep fails at once (no speaker), so a zero
// gap never spins.
const minWait = 50 * time.Millisecond

// Player plays the endless beep on its own goroutine until stopped.
type Player struct {
	b Beeper

	mu     sync.Mutex
	on     bool
	tone   Tone
	stop   chan struct{}
	closed bool
	beepMu sync.Mutex // one Beep at a time across loop restarts
}

// NewPlayer returns a stopped player.
func NewPlayer(b Beeper) *Player { return &Player{b: b} }

// Set starts (on), stops or retunes the loop. Never blocks on a running
// Beep.
func (p *Player) Set(on bool, t Tone) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || (on == p.on && (!on || t == p.tone)) {
		return
	}
	if p.stop != nil {
		close(p.stop)
		p.stop = nil
	}
	p.on, p.tone = on, t
	if on {
		p.stop = make(chan struct{})
		go p.loop(t, p.stop)
	}
}

// Playing reports whether the loop is on.
func (p *Player) Playing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.on
}

// Close stops the loop for good.
func (p *Player) Close() {
	p.Set(false, Tone{})
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
}

func (p *Player) loop(t Tone, stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		p.beepMu.Lock()
		select {
		case <-stop:
			p.beepMu.Unlock()
			return
		default:
		}
		start := time.Now()
		err := p.b.Beep(t.FreqHz, t.DurMs)
		p.beepMu.Unlock()
		wait := time.Duration(t.GapMs) * time.Millisecond
		if err != nil {
			// no speaker / failure: keep the rhythm without spinning
			if rest := time.Duration(t.DurMs)*time.Millisecond - time.Since(start); rest > wait {
				wait = rest
			}
		}
		if wait < minWait && (err != nil || t.GapMs == 0) {
			wait = minWait
		}
		select {
		case <-stop:
			return
		case <-time.After(wait):
		}
	}
}
