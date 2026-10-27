package traysettings

import (
	"strconv"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// General settings fields (focus targets; values are the JSON paths of
// settings.json, PLAN §4.3).
const (
	FieldPollInterval Field = "pollIntervalSec"
	FieldWarnMinutes  Field = "warnMinutes"
	FieldErrorMinutes Field = "errorMinutes"
	FieldBeepEnabled  Field = "beep.enabled"
	FieldWarnFreq     Field = "beep.warningFreqHz"
	FieldWarnDur      Field = "beep.warningDurMs"
	FieldErrorFreq    Field = "beep.errorFreqHz"
	FieldErrorDur     Field = "beep.errorDurMs"
	FieldGap             Field = "beep.gapMs"
	FieldStalledMinutes  Field = "stalledMinutes"
	FieldLanguage        Field = "language"
	FieldAutostart       Field = "trayAutostart"
)

// GeneralFields are the fields of the general settings page.
var GeneralFields = []Field{FieldPollInterval, FieldWarnMinutes, FieldErrorMinutes, FieldBeepEnabled, FieldWarnFreq,
	FieldWarnDur, FieldErrorFreq, FieldErrorDur, FieldGap, FieldStalledMinutes, FieldLanguage, FieldAutostart}

// IsGeneral tells whether f is on the general settings page.
func IsGeneral(f Field) bool {
	for _, g := range GeneralFields {
		if g == f {
			return true
		}
	}
	return false
}

// LanguageChoices are the items of the service language box (the
// language of the service's Event Log messages, settings.language).
var LanguageChoices = []string{model.LangAuto, model.LangRU, model.LangEN}

// LanguageIndex is the index of lang in LanguageChoices (0 = auto).
func LanguageIndex(lang string) int {
	for i, l := range LanguageChoices {
		if l == lang {
			return i
		}
	}
	return 0
}

// GeneralForm is the editable state of the general settings page: the
// numbers as typed, the sound switch, the service language and the tray
// autostart switch (machine-wide, PLAN §12 p.47).
type GeneralForm struct {
	PollSec, WarnMin, ErrorMin             string
	WarnFreq, WarnDur, ErrorFreq, ErrorDur string
	Gap, StalledMin                        string
	BeepEnabled                            bool
	Language                               string
	Autostart                              bool
}

// FromSettings fills the general page from settings.
func FromSettings(s model.Settings) GeneralForm {
	it := strconv.Itoa
	g := GeneralForm{
		PollSec: it(s.PollIntervalSec), WarnMin: it(s.WarnMinutes), ErrorMin: it(s.ErrorMinutes),
		WarnFreq: it(s.Beep.WarningFreqHz), WarnDur: it(s.Beep.WarningDurMs),
		ErrorFreq: it(s.Beep.ErrorFreqHz), ErrorDur: it(s.Beep.ErrorDurMs), Gap: it(s.Beep.GapMs),
		BeepEnabled: s.Beep.BeepEnabled(), Language: s.Language, Autostart: s.TrayAutostart,
	}
	if s.StalledMinutes != nil {
		g.StalledMin = it(*s.StalledMinutes)
	}
	return g
}

// Apply returns base with the page applied and validated (model rules);
// beep.enabled is always explicit. A number that does not parse is a
// range error of its field.
func (g GeneralForm) Apply(base model.Settings) (model.Settings, *model.FieldError) {
	s := base
	nums := []struct {
		f        Field
		text     string
		dst      *int
		min, max int
	}{
		{FieldPollInterval, g.PollSec, &s.PollIntervalSec, model.MinPollIntervalSec, model.MaxPollIntervalSec},
		{FieldWarnMinutes, g.WarnMin, &s.WarnMinutes, 1, model.MaxThresholdMinutes},
		{FieldErrorMinutes, g.ErrorMin, &s.ErrorMinutes, 2, model.MaxThresholdMinutes},
		{FieldWarnFreq, g.WarnFreq, &s.Beep.WarningFreqHz, model.MinBeepFreqHz, model.MaxBeepFreqHz},
		{FieldWarnDur, g.WarnDur, &s.Beep.WarningDurMs, 1, model.MaxBeepDurMs},
		{FieldErrorFreq, g.ErrorFreq, &s.Beep.ErrorFreqHz, model.MinBeepFreqHz, model.MaxBeepFreqHz},
		{FieldErrorDur, g.ErrorDur, &s.Beep.ErrorDurMs, 1, model.MaxBeepDurMs},
		{FieldGap, g.Gap, &s.Beep.GapMs, 0, model.MaxBeepGapMs},
	}
	for _, n := range nums {
		t := strings.TrimSpace(n.text)
		v, err := strconv.Atoi(t)
		if err != nil {
			return s, fieldErr(string(n.f), model.ValRange, strconv.Itoa(n.min), strconv.Itoa(n.max), t)
		}
		*n.dst = v
	}
	// stalledMinutes: empty = absent (default 120), 0 = off, else 10..1440
	if st := strings.TrimSpace(g.StalledMin); st == "" {
		s.StalledMinutes = nil
	} else {
		v, err := strconv.Atoi(st)
		if err != nil || !(v == 0 || (v >= model.MinStalledMinutes && v <= model.MaxStalledMinutes)) {
			return s, fieldErr(string(FieldStalledMinutes), model.ValRange,
				strconv.Itoa(model.MinStalledMinutes), strconv.Itoa(model.MaxStalledMinutes), st)
		}
		s.StalledMinutes = &v
	}
	s.Beep.Enabled = model.OptFalse
	if g.BeepEnabled {
		s.Beep.Enabled = model.OptTrue
	}
	s.Language, s.TrayAutostart = g.Language, g.Autostart
	if err := s.Validate(); err != nil {
		if fe := model.AsFieldError(err); fe != nil {
			return s, fe
		}
		return s, &model.FieldError{Field: "", Key: model.ValRequired, Msg: err.Error()}
	}
	return s, nil
}

// LoadSettings replaces the saved general settings (get_config /
// get_status); unsaved edits of the page are dropped.
func (e *Editor) LoadSettings(s model.Settings) {
	clean := e.HasProfile() && !e.formDirty() // before the inherited values change
	e.Settings = s.Normalized()
	e.HasSettings = true
	e.General = FromSettings(e.Settings)
	e.loadViber()
	e.loadEmail()
	if clean && !e.IsNew {
		e.Form = e.fromProfile(e.Base) // new inherited thresholds (phase 8)
	}
}

// GeneralUnsaved tells whether the general page has unsaved edits.
func (e *Editor) GeneralUnsaved() bool {
	return e.Mode == Admin && e.HasSettings && e.General != FromSettings(e.Settings)
}

// CanEditSettings tells whether the general page is editable.
func (e *Editor) CanEditSettings() bool { return e.Mode == Admin && e.HasSettings }

// SettingsRequest is save_settings of the general page: local validation,
// beep.enabled always explicit, expectedRevision (required), size limit.
func (e *Editor) SettingsRequest() (model.IPCRequest, *Problem) {
	if !e.CanEditSettings() {
		return model.IPCRequest{}, &Problem{Text: e.t(i18n.KeySetNoRevision), Reload: true}
	}
	if e.Revision == "" {
		return model.IPCRequest{}, &Problem{Text: e.t(i18n.KeySetNoRevision), Reload: true}
	}
	e.settingsCtx = true
	s, fe := e.General.Apply(e.Settings)
	if fe != nil {
		return model.IPCRequest{}, e.fieldProblem(fe)
	}
	req := model.IPCRequest{Cmd: model.CmdSaveSettings, Settings: &s, ExpectedRevision: e.Revision}
	if pr := e.checkSize(req); pr != nil {
		return model.IPCRequest{}, pr
	}
	return req, nil
}

// SettingsDone applies an ok reply of save_settings (the reply carries
// the new revision, not the settings: the sent ones become the saved).
func (e *Editor) SettingsDone(m model.IPCMessage, sent model.Settings) string {
	if m.Revision != "" {
		e.Revision = m.Revision
	}
	e.LoadSettings(sent)
	return e.t(i18n.KeyGenSaved)
}

// AutostartErrorText is the text of a trayAutostartError code of
// get_status (CHK-CFG-04): "" for no error, an unknown code is shown with
// a generic text.
func AutostartErrorText(code, lang string) string {
	key := map[string]string{
		model.AutostartErrAccessDenied: i18n.KeyAutostartErrAccess,
		model.AutostartErrRegistry:     i18n.KeyAutostartErrRegistry,
		model.AutostartErrTrayPath:     i18n.KeyAutostartErrPath,
		model.AutostartErrSettings:     i18n.KeyAutostartErrSettings,
	}[code]
	switch {
	case code == "":
		return ""
	case key == "":
		return i18n.T(i18n.KeyAutostartErrUnknown, lang) + " (" + code + ")"
	}
	return i18n.T(key, lang)
}
