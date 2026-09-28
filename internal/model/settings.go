package model

import (
	"errors"
	"fmt"
	"time"
)

// UI language codes. LangAuto means "use the Windows UI language"
// (fallback RU), see config.EffectiveLanguage and PLAN.md §7.4.
const (
	LangAuto = ""
	LangRU   = "ru"
	LangEN   = "en"
)

// Defaults and limits.
const (
	DefaultPollIntervalSec = 60
	DefaultWarnMinutes     = 30
	DefaultErrorMinutes    = 60
	DefaultWarningFreqHz   = 800
	DefaultWarningDurMs    = 300
	DefaultErrorFreqHz     = 1500
	DefaultErrorDurMs      = 500
	DefaultBeepGapMs       = 700

	MinPollIntervalSec  = 5
	MaxPollIntervalSec  = 86400
	MaxThresholdMinutes = 30 * 24 * 60
	MinBeepFreqHz       = 37    // kernel32 Beep lower bound
	MaxBeepFreqHz       = 32767 // kernel32 Beep upper bound
	MaxBeepDurMs        = 10000
	MaxBeepGapMs        = 60000
)

// BeepSettings are PC-speaker parameters for alert sounds.
type BeepSettings struct {
	WarningFreqHz int `json:"warningFreqHz"`
	WarningDurMs  int `json:"warningDurMs"`
	ErrorFreqHz   int `json:"errorFreqHz"`
	ErrorDurMs    int `json:"errorDurMs"`
	GapMs         int `json:"gapMs"`
}

// Settings are global monitoring settings (settings.json). WarnMinutes and
// ErrorMinutes are single thresholds applied to the FreshAt lag (§12 p.2).
type Settings struct {
	PollIntervalSec int          `json:"pollIntervalSec"`
	WarnMinutes     int          `json:"warnMinutes"`
	ErrorMinutes    int          `json:"errorMinutes"`
	Beep            BeepSettings `json:"beep"`
	Language        string       `json:"language"`
	TrayAutostart   bool         `json:"trayAutostart"`
}

// DefaultSettings returns the settings used when settings.json is missing.
func DefaultSettings() Settings {
	return Settings{
		PollIntervalSec: DefaultPollIntervalSec,
		WarnMinutes:     DefaultWarnMinutes,
		ErrorMinutes:    DefaultErrorMinutes,
		Beep: BeepSettings{
			WarningFreqHz: DefaultWarningFreqHz,
			WarningDurMs:  DefaultWarningDurMs,
			ErrorFreqHz:   DefaultErrorFreqHz,
			ErrorDurMs:    DefaultErrorDurMs,
			GapMs:         DefaultBeepGapMs,
		},
		Language:      LangAuto,
		TrayAutostart: true,
	}
}

// PollInterval returns PollIntervalSec as a duration.
func (s Settings) PollInterval() time.Duration {
	return time.Duration(s.PollIntervalSec) * time.Second
}

// WarnAfter returns the warning threshold as a duration.
func (s Settings) WarnAfter() time.Duration { return time.Duration(s.WarnMinutes) * time.Minute }

// ErrorAfter returns the error threshold as a duration.
func (s Settings) ErrorAfter() time.Duration { return time.Duration(s.ErrorMinutes) * time.Minute }

// Validate checks ranges; WarnMinutes must be strictly less than ErrorMinutes.
func (s Settings) Validate() error {
	var errs []error
	if s.PollIntervalSec < MinPollIntervalSec || s.PollIntervalSec > MaxPollIntervalSec {
		errs = append(errs, fmt.Errorf("settings: pollIntervalSec must be in [%d..%d], got %d",
			MinPollIntervalSec, MaxPollIntervalSec, s.PollIntervalSec))
	}
	if s.WarnMinutes < 1 {
		errs = append(errs, fmt.Errorf("settings: warnMinutes must be >= 1, got %d", s.WarnMinutes))
	}
	if s.ErrorMinutes <= s.WarnMinutes {
		errs = append(errs, fmt.Errorf("settings: errorMinutes (%d) must be greater than warnMinutes (%d)",
			s.ErrorMinutes, s.WarnMinutes))
	}
	if s.ErrorMinutes > MaxThresholdMinutes {
		errs = append(errs, fmt.Errorf("settings: errorMinutes must be <= %d, got %d", MaxThresholdMinutes, s.ErrorMinutes))
	}
	errs = append(errs, s.Beep.validate()...)
	switch s.Language {
	case LangAuto, LangRU, LangEN:
	default:
		errs = append(errs, fmt.Errorf("settings: unsupported language %q (want \"\", %q or %q)", s.Language, LangRU, LangEN))
	}
	return errors.Join(errs...)
}

func (b BeepSettings) validate() []error {
	var errs []error
	freq := func(name string, v int) {
		if v < MinBeepFreqHz || v > MaxBeepFreqHz {
			errs = append(errs, fmt.Errorf("settings: beep.%s must be in [%d..%d] Hz, got %d", name, MinBeepFreqHz, MaxBeepFreqHz, v))
		}
	}
	dur := func(name string, v int) {
		if v < 1 || v > MaxBeepDurMs {
			errs = append(errs, fmt.Errorf("settings: beep.%s must be in [1..%d] ms, got %d", name, MaxBeepDurMs, v))
		}
	}
	freq("warningFreqHz", b.WarningFreqHz)
	dur("warningDurMs", b.WarningDurMs)
	freq("errorFreqHz", b.ErrorFreqHz)
	dur("errorDurMs", b.ErrorDurMs)
	if b.GapMs < 0 || b.GapMs > MaxBeepGapMs {
		errs = append(errs, fmt.Errorf("settings: beep.gapMs must be in [0..%d] ms, got %d", MaxBeepGapMs, b.GapMs))
	}
	return errs
}
