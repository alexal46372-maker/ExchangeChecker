package model

import "fmt"

// Per-profile thresholds (phase 3, CHK-MON-01). A profile may override the
// global warning/error thresholds of settings.json; zero fields inherit
// the global value (defaults DefaultWarnMinutes / DefaultErrorMinutes).

// validateThresholds checks the optional per-profile overrides.
func (p Profile) validateThresholds() error {
	if p.WarnMinutes < 0 || p.WarnMinutes > MaxThresholdMinutes {
		return fieldErr("warnMinutes", ValRange, fmt.Sprintf("warnMinutes must be in [0..%d], got %d", MaxThresholdMinutes, p.WarnMinutes),
			"0", itoa(MaxThresholdMinutes), itoa(p.WarnMinutes))
	}
	if p.ErrorMinutes < 0 || p.ErrorMinutes > MaxThresholdMinutes {
		return fieldErr("errorMinutes", ValRange, fmt.Sprintf("errorMinutes must be in [0..%d], got %d", MaxThresholdMinutes, p.ErrorMinutes),
			"0", itoa(MaxThresholdMinutes), itoa(p.ErrorMinutes))
	}
	if !validStalled(p.StalledMinutes) {
		return stalledErr("stalledMinutes", p.StalledMinutes)
	}
	if p.WarnMinutes > 0 && p.ErrorMinutes > 0 && p.ErrorMinutes <= p.WarnMinutes {
		return fieldErr("errorMinutes", ValGreaterThan, fmt.Sprintf("errorMinutes (%d) must be greater than warnMinutes (%d)", p.ErrorMinutes, p.WarnMinutes),
			"warnMinutes", itoa(p.WarnMinutes))
	}
	return nil
}

// EffectiveSettings returns the global settings with the profile's
// threshold overrides applied. Zero global thresholds fall back to the
// defaults (30 / 60 min). If a single override would make the pair
// inconsistent (warning >= error), the global pair is used unchanged.
func (p Profile) EffectiveSettings(global Settings) Settings {
	s := global
	if s.WarnMinutes <= 0 {
		s.WarnMinutes = DefaultWarnMinutes
	}
	if s.ErrorMinutes <= 0 {
		s.ErrorMinutes = DefaultErrorMinutes
	}
	if s.ErrorMinutes <= s.WarnMinutes {
		s.WarnMinutes, s.ErrorMinutes = DefaultWarnMinutes, DefaultErrorMinutes
	}
	base := s
	if p.WarnMinutes > 0 {
		s.WarnMinutes = p.WarnMinutes
	}
	if p.ErrorMinutes > 0 {
		s.ErrorMinutes = p.ErrorMinutes
	}
	if s.ErrorMinutes <= s.WarnMinutes {
		return base
	}
	return s
}

// DefaultServerPreset is the connection preset of new profiles: the
// customer's target server is SQL Server 2008 R2 RTM (no service packs,
// no TLS 1.2), see PLAN.md §12 p.21/26 and CHK-MON-08.
const DefaultServerPreset = ServerPresetMSSQL2008

// NewProfile returns a new enabled profile (SQL authentication, default
// port) with the SQL defaults and the DefaultServerPreset applied: encrypt
// "disable" (phase 2d, PLAN.md §12 p.31) and TLSMinVersion "1.0" in case
// encryption is turned on, so SQL Server 2008 R2 RTM can be reached. The
// tray uses it for "add database"; the caller fills server, database and
// login.
func NewProfile(id string) Profile {
	p := Profile{ID: id, AuthMode: AuthSQL, Enabled: true}
	p.ApplySQLDefaults()
	_ = p.ApplyServerPreset(DefaultServerPreset) // known preset: cannot fail
	return p
}
