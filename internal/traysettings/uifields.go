package traysettings

import (
	"fmt"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 10 D (CHK-TRAY-40, PLAN §12 p.121): «Простой» / «Экспертный»
// modes, the inventory of every settings field (uiFields) and the
// required-field rules of each mode.

// UIMode of the settings window (HKCU Tray\UIMode).
type UIMode string

const (
	UISimple UIMode = "simple" // default
	UIExpert UIMode = "expert"
)

// NormalizeUIMode: anything but "expert" is simple.
func NormalizeUIMode(s string) UIMode {
	if UIMode(strings.ToLower(strings.TrimSpace(s))) == UIExpert {
		return UIExpert
	}
	return UISimple
}

// Section of the expert window.
type Section string

const (
	SecDatabases     Section = "databases"     // Базы и подключение
	SecTables        Section = "tables"        // Таблицы обмена
	SecChecks        Section = "checks"        // Проверки и пороги
	SecAlarms        Section = "alarms"        // Тревоги и звук
	SecNotifications Section = "notifications" // Уведомления (баллоны, Viber)
	SecHistory       Section = "history"       // История и журнал
	SecService       Section = "service"       // Служба
	SecTransfer      Section = "transfer"      // Импорт/экспорт
)

// Sections in window order.
var Sections = []Section{SecDatabases, SecTables, SecChecks, SecAlarms, SecNotifications, SecHistory, SecService, SecTransfer}

// UIField is one settings field of 0.8/0.9.
type UIField struct {
	ID      Field
	Section Section
	// Simple: shown in «Простой» too (else expert only).
	Simple bool
	// HasDefault: an empty value means the default (never highlighted).
	HasDefault bool
	// Cue: i18n key of the EM_SETCUEBANNER of an edit box ("" = no cue:
	// not an edit, or the default is shown as the filled value).
	Cue string
	// CueDefault, when > 0, is sprintf'd into Cue («по умолчанию %d»).
	CueDefault int
	// Capability: shown only when the service has it ("" = always).
	Capability string
}

// Viber fields (phase 10, capability viber).
const (
	FieldViberToken      Field = "viber.token"
	FieldViberRecipients Field = "viber.recipients"
	FieldViberDays       Field = "viber.schedule.days"
	FieldViberFrom       Field = "viber.schedule.from"
	FieldViberTo         Field = "viber.schedule.to"
	FieldViberEvents     Field = "viber.events"
	FieldViberReminder   Field = "viber.reminderMinutes"
	FieldViberTest       Field = "viber.test"
	FieldBalloons        Field = "tray.balloons"
	FieldSoundProfiles   Field = "tray.sound"
	FieldMasterVolume    Field = "tray.masterVolume"
	FieldMappings        Field = "mappings"
	FieldHistoryWindow   Field = "tray.history"
	FieldServiceControl  Field = "service.control"
	FieldExport          Field = "transfer.export"
	FieldImport          Field = "transfer.import"
)

// CapViber gates the «Уведомления → Viber» section.
const CapViber = "viber"

// UIFields is the inventory of every field (a test checks each one is on
// a page of its mode).
var UIFields = []UIField{
	{ID: FieldName, Section: SecDatabases, Simple: true, HasDefault: true, Cue: i18n.KeyCueName},
	{ID: FieldHost, Section: SecDatabases, Simple: true, Cue: i18n.KeyCueHost},
	{ID: FieldInstance, Section: SecDatabases, Simple: true, HasDefault: true, Cue: i18n.KeyCueInstance},
	{ID: FieldPort, Section: SecDatabases, HasDefault: true, Cue: i18n.KeySetPortCue},
	{ID: FieldDatabase, Section: SecDatabases, Simple: true, Cue: i18n.KeyCueDatabase},
	{ID: FieldAuth, Section: SecDatabases, Simple: true},
	{ID: FieldLogin, Section: SecDatabases, Simple: true, Cue: i18n.KeyCueLogin},
	{ID: FieldPassword, Section: SecDatabases, Simple: true, Cue: i18n.KeyCuePassword},
	{ID: FieldClearPassword, Section: SecDatabases},
	{ID: FieldEncrypt, Section: SecDatabases, HasDefault: true},
	{ID: FieldTLS10, Section: SecDatabases, HasDefault: true},
	{ID: FieldTrust, Section: SecDatabases, HasDefault: true},
	{ID: FieldEnabled, Section: SecDatabases, Simple: true, HasDefault: true},
	{ID: FieldPreset, Section: SecDatabases, HasDefault: true},
	{ID: FieldConnTimeout, Section: SecDatabases, HasDefault: true, Cue: i18n.KeySetTimeoutCue, CueDefault: model.DefaultConnectTimeoutSec},
	{ID: FieldQueryTimeout, Section: SecDatabases, HasDefault: true, Cue: i18n.KeySetTimeoutCue, CueDefault: model.DefaultQueryTimeoutSec},
	{ID: FieldMappings, Section: SecTables},
	{ID: FieldDiscPoint, Section: SecTables},
	{ID: FieldDiscTime, Section: SecTables, HasDefault: true, Cue: i18n.KeyCueDiscTime},
	{ID: FieldDiscTolerance, Section: SecTables, HasDefault: true, Cue: i18n.KeySetTimeoutCue, CueDefault: 1440},
	{ID: FieldDiscLimit, Section: SecTables, HasDefault: true},
	{ID: FieldPollInterval, Section: SecChecks, HasDefault: true},
	{ID: FieldWarnMinutes, Section: SecChecks, Simple: true, HasDefault: true},
	{ID: FieldErrorMinutes, Section: SecChecks, Simple: true, HasDefault: true},
	{ID: FieldProfWarn, Section: SecChecks, HasDefault: true, Cue: i18n.KeySetThreshCue},
	{ID: FieldProfError, Section: SecChecks, HasDefault: true, Cue: i18n.KeySetThreshCue},
	{ID: FieldProfStalled, Section: SecChecks, HasDefault: true, Cue: i18n.KeyCueStalled},
	{ID: FieldBeepEnabled, Section: SecAlarms, Simple: true, HasDefault: true},
	{ID: FieldWarnFreq, Section: SecAlarms, HasDefault: true},
	{ID: FieldWarnDur, Section: SecAlarms, HasDefault: true},
	{ID: FieldErrorFreq, Section: SecAlarms, HasDefault: true},
	{ID: FieldErrorDur, Section: SecAlarms, HasDefault: true},
	{ID: FieldGap, Section: SecAlarms, HasDefault: true},
	{ID: FieldStalledMinutes, Section: SecChecks, HasDefault: true, Cue: i18n.KeyCueStalled},
	{ID: FieldSoundProfiles, Section: SecAlarms, HasDefault: true},
	{ID: FieldMasterVolume, Section: SecAlarms, Simple: true, HasDefault: true},
	{ID: FieldBalloons, Section: SecNotifications, Simple: true, HasDefault: true},
	{ID: FieldViberToken, Section: SecNotifications, Capability: CapViber, Cue: i18n.KeyCueVbToken},
	{ID: FieldViberRecipients, Section: SecNotifications, Capability: CapViber},
	{ID: FieldViberDays, Section: SecNotifications, Capability: CapViber, HasDefault: true},
	{ID: FieldViberFrom, Section: SecNotifications, Capability: CapViber, HasDefault: true},
	{ID: FieldViberTo, Section: SecNotifications, Capability: CapViber, HasDefault: true},
	{ID: FieldViberEvents, Section: SecNotifications, Capability: CapViber, HasDefault: true},
	{ID: FieldViberReminder, Section: SecNotifications, Capability: CapViber, HasDefault: true}, // hint: STATIC idVbReminderHint (CHK-TRAY-50 note)
	{ID: FieldViberTest, Section: SecNotifications, Capability: CapViber},
	{ID: FieldHistoryWindow, Section: SecHistory},
	{ID: FieldLanguage, Section: SecService, HasDefault: true},
	{ID: FieldAutostart, Section: SecService, HasDefault: true},
	{ID: FieldServiceControl, Section: SecService},
	{ID: FieldExport, Section: SecTransfer},
	{ID: FieldImport, Section: SecTransfer},
}

// FieldsOf are the fields shown in mode with the capabilities caps.
func FieldsOf(mode UIMode, caps []string) []UIField {
	has := map[string]bool{}
	for _, c := range caps {
		has[c] = true
	}
	var out []UIField
	for _, f := range UIFields {
		if (mode == UIExpert || f.Simple) && (f.Capability == "" || has[f.Capability]) {
			out = append(out, f)
		}
	}
	return out
}

// CueText is the cue banner of f in lang ("" if none).
func (f UIField) CueText(lang string) string {
	if f.Cue == "" {
		return ""
	}
	t := i18n.T(f.Cue, lang)
	if f.CueDefault > 0 {
		return fmt.Sprintf(t, f.CueDefault)
	}
	return t
}

// RequiredFields of the profile form in mode. Simple: server, database
// and the login method; login and password only for a SQL login (the
// password only while none is stored). Expert: only what
// model.Profile.Validate requires (server, database, login for SQL; the
// mapping is checked by its own dialog). A field with a default is never
// required.
func RequiredFields(mode UIMode, f Form, passwordStored bool) []Field {
	req := []Field{FieldHost, FieldDatabase}
	if mode == UISimple {
		req = append(req, FieldAuth)
	}
	if f.Auth == model.AuthSQL {
		req = append(req, FieldLogin)
		if mode == UISimple && !passwordStored && !f.PasswordTyped {
			req = append(req, FieldPassword)
		}
	}
	return req
}

// Missing are the required fields that are empty now (highlighted red
// with an asterisk).
func Missing(mode UIMode, f Form, passwordStored bool) []Field {
	var out []Field
	for _, id := range RequiredFields(mode, f, passwordStored) {
		empty := false
		switch id {
		case FieldHost:
			empty = strings.TrimSpace(f.Host) == ""
		case FieldDatabase:
			empty = strings.TrimSpace(f.Database) == ""
		case FieldAuth:
			empty = !f.Auth.Valid()
		case FieldLogin:
			empty = strings.TrimSpace(f.Login) == ""
		case FieldPassword:
			empty = !f.PasswordTyped
		}
		if empty {
			out = append(out, id)
		}
	}
	return out
}
