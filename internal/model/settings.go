package model

import (
	"errors"
	"fmt"
	"reflect"
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

// OptBool is a JSON bool that also remembers "not given" (phase 6,
// beep.enabled): comparable (Settings stays usable with ==, unlike a
// *bool), zero = unset and omitted with omitempty, otherwise marshalled
// as false / true; JSON null reads as unset.
type OptBool uint8

const (
	OptUnset OptBool = iota
	OptFalse
	OptTrue
)

// OptBoolOf returns OptTrue / OptFalse.
func OptBoolOf(b bool) OptBool {
	if b {
		return OptTrue
	}
	return OptFalse
}

// Or returns the value, def when unset.
func (o OptBool) Or(def bool) bool {
	if o == OptUnset {
		return def
	}
	return o == OptTrue
}

// MarshalJSON writes false / true (null when unset and not omitted).
func (o OptBool) MarshalJSON() ([]byte, error) {
	switch o {
	case OptTrue:
		return []byte("true"), nil
	case OptFalse:
		return []byte("false"), nil
	}
	return []byte("null"), nil
}

// UnmarshalJSON reads true / false; null = unset.
func (o *OptBool) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case "true":
		*o = OptTrue
	case "false":
		*o = OptFalse
	case "null":
		*o = OptUnset
	default:
		return fmt.Errorf("want true or false, got %s", b)
	}
	return nil
}

// BeepSettings are PC-speaker parameters for alert sounds.
type BeepSettings struct {
	// Enabled switches the alert sound (phase 6, PLAN §12 p.65, Вика's
	// decision of 03.10): machine-wide, default true; a settings.json
	// without the field reads as true, an explicit false stays false; the
	// alert window is shown either way. Read it with BeepEnabled().
	Enabled       OptBool `json:"enabled,omitempty"`
	WarningFreqHz int     `json:"warningFreqHz"`
	WarningDurMs  int     `json:"warningDurMs"`
	ErrorFreqHz   int     `json:"errorFreqHz"`
	ErrorDurMs    int     `json:"errorDurMs"`
	GapMs         int     `json:"gapMs"`
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
	// Viber (phase 10, CHK-SVC-06): nil in save_settings = keep the
	// stored value; always set in views; never in export_config.
	Viber *ViberSettings `json:"viber,omitempty"`
	// Email (phase 13b, CHK-CFG-09): nil in save_settings = keep the
	// stored value; always set in views; export only with
	// includeEmailRecipients (never the SMTP password).
	Email *EmailSettings `json:"email,omitempty"`
	// StalledMinutes (phase 11, CHK-MON-18): stalled-exchange threshold;
	// absent = DefaultStalledMinutes (120), 0 = off, else 10..1440. A
	// save_settings without it keeps the stored value.
	StalledMinutes *int `json:"stalledMinutes,omitempty"`
}

// DefaultSettings returns the settings used when settings.json is missing.
func DefaultSettings() Settings {
	stalled := DefaultStalledMinutes
	return Settings{
		StalledMinutes:  &stalled,
		PollIntervalSec: DefaultPollIntervalSec,
		WarnMinutes:     DefaultWarnMinutes,
		ErrorMinutes:    DefaultErrorMinutes,
		Beep: BeepSettings{
			WarningFreqHz: DefaultWarningFreqHz,
			WarningDurMs:  DefaultWarningDurMs,
			ErrorFreqHz:   DefaultErrorFreqHz,
			ErrorDurMs:    DefaultErrorDurMs,
			GapMs:         DefaultBeepGapMs,
			Enabled:       OptTrue,
		},
		Language:      LangAuto,
		TrayAutostart: true,
		Viber:         defaultViberPtr(),
		Email:         defaultEmailPtr(),
	}
}

// BeepEnabled reports beep.enabled (unset = true).
func (b BeepSettings) BeepEnabled() bool { return b.Enabled.Or(true) }

// Normalized returns s with an unset beep.enabled made explicit (true),
// so get_status / get_config always carry the field.
func (s Settings) Normalized() Settings {
	if s.Beep.Enabled == OptUnset {
		s.Beep.Enabled = OptTrue
	}
	if s.StalledMinutes == nil { // phase 11 (CHK-MON-18)
		v := DefaultStalledMinutes
		s.StalledMinutes = &v
	} else {
		v := *s.StalledMinutes
		s.StalledMinutes = &v
	}
	if s.Viber == nil {
		s.Viber = defaultViberPtr()
	} else {
		v := *s.Viber
		v.Recipients = append([]ViberRecipient{}, v.Recipients...)
		v.Events = append([]string{}, v.Events...)
		v.Schedule.Days = append([]int{}, v.Schedule.Days...)
		s.Viber = &v
	}
	if s.Email == nil {
		s.Email = defaultEmailPtr()
	} else {
		e := *s.Email
		e.Recipients = append([]EmailRecipient{}, e.Recipients...)
		e.Events = append([]string{}, e.Events...)
		e.Schedule.Days = append([]int{}, e.Schedule.Days...)
		s.Email = &e
	}
	return s
}

func defaultViberPtr() *ViberSettings {
	v := DefaultViberSettings()
	return &v
}

func defaultEmailPtr() *EmailSettings {
	e := DefaultEmailSettings()
	return &e
}

// Equal compares settings field by field (Settings holds slices since
// phase 10): Normalized forms, the read-only viber fields ignored.
func (s Settings) Equal(o Settings) bool {
	a, b := s.Normalized(), o.Normalized()
	va, vb := a.Viber.Stored(), b.Viber.Stored()
	ea, eb := a.Email.Stored(), b.Email.Stored()
	a.Viber, b.Viber = nil, nil
	a.Email, b.Email = nil, nil
	return reflect.DeepEqual(a, b) && reflect.DeepEqual(va, vb) && reflect.DeepEqual(ea, eb)
}

// Stored returns the form written to settings.json (viber without the
// read-only fields).
func (s Settings) Stored() Settings {
	s = s.Normalized()
	v := s.Viber.Stored()
	s.Viber = &v
	e := s.Email.Stored()
	s.Email = &e
	return s
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
		errs = append(errs, fieldErr("pollIntervalSec", ValRange, fmt.Sprintf("settings: pollIntervalSec must be in [%d..%d], got %d",
			MinPollIntervalSec, MaxPollIntervalSec, s.PollIntervalSec), itoa(MinPollIntervalSec), itoa(MaxPollIntervalSec), itoa(s.PollIntervalSec)))
	}
	if s.WarnMinutes < 1 {
		errs = append(errs, fieldErr("warnMinutes", ValRange, fmt.Sprintf("settings: warnMinutes must be >= 1, got %d", s.WarnMinutes),
			"1", itoa(MaxThresholdMinutes), itoa(s.WarnMinutes)))
	}
	if s.ErrorMinutes <= s.WarnMinutes {
		errs = append(errs, fieldErr("errorMinutes", ValGreaterThan, fmt.Sprintf("settings: errorMinutes (%d) must be greater than warnMinutes (%d)",
			s.ErrorMinutes, s.WarnMinutes), "warnMinutes", itoa(s.WarnMinutes)))
	}
	if s.ErrorMinutes > MaxThresholdMinutes {
		errs = append(errs, fieldErr("errorMinutes", ValRange, fmt.Sprintf("settings: errorMinutes must be <= %d, got %d", MaxThresholdMinutes, s.ErrorMinutes),
			"2", itoa(MaxThresholdMinutes), itoa(s.ErrorMinutes)))
	}
	errs = append(errs, s.Beep.validate()...)
	if s.StalledMinutes != nil && !validStalled(*s.StalledMinutes) {
		errs = append(errs, stalledErr("stalledMinutes", *s.StalledMinutes))
	}
	if s.Viber != nil {
		if err := s.Viber.Validate(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.Email != nil {
		if err := s.Email.Validate(); err != nil {
			errs = append(errs, err)
		}
	}
	switch s.Language {
	case LangAuto, LangRU, LangEN:
	default:
		errs = append(errs, fieldErr("language", ValEnum, fmt.Sprintf("settings: unsupported language %q (want \"\", %q or %q)", s.Language, LangRU, LangEN), s.Language))
	}
	return errors.Join(errs...)
}

func (b BeepSettings) validate() []error {
	var errs []error
	freq := func(name string, v int) {
		if v < MinBeepFreqHz || v > MaxBeepFreqHz {
			errs = append(errs, fieldErr("beep."+name, ValRange, fmt.Sprintf("settings: beep.%s must be in [%d..%d] Hz, got %d", name, MinBeepFreqHz, MaxBeepFreqHz, v),
				itoa(MinBeepFreqHz), itoa(MaxBeepFreqHz), itoa(v)))
		}
	}
	dur := func(name string, v int) {
		if v < 1 || v > MaxBeepDurMs {
			errs = append(errs, fieldErr("beep."+name, ValRange, fmt.Sprintf("settings: beep.%s must be in [1..%d] ms, got %d", name, MaxBeepDurMs, v),
				"1", itoa(MaxBeepDurMs), itoa(v)))
		}
	}
	freq("warningFreqHz", b.WarningFreqHz)
	dur("warningDurMs", b.WarningDurMs)
	freq("errorFreqHz", b.ErrorFreqHz)
	dur("errorDurMs", b.ErrorDurMs)
	if b.GapMs < 0 || b.GapMs > MaxBeepGapMs {
		errs = append(errs, fieldErr("beep.gapMs", ValRange, fmt.Sprintf("settings: beep.gapMs must be in [0..%d] ms, got %d", MaxBeepGapMs, b.GapMs),
			"0", itoa(MaxBeepGapMs), itoa(b.GapMs)))
	}
	return errs
}
