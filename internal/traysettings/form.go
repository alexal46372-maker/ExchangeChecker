package traysettings

import (
	"strconv"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Field names a control of the form (focus target of a validation error).
type Field string

// Form fields.
const (
	FieldNone          Field = ""
	FieldName          Field = "name"
	FieldHost          Field = "host"
	FieldInstance      Field = "instance"
	FieldPort          Field = "port"
	FieldDatabase      Field = "database"
	FieldAuth          Field = "authMode"
	FieldLogin         Field = "login"
	FieldPassword      Field = "password"
	FieldClearPassword Field = "clearPassword"
	FieldEncrypt       Field = "encrypt"
	FieldTLS10         Field = "tls10"
	FieldTrust         Field = "trustServerCert"
	FieldEnabled       Field = "enabled"
	// phase 8 (CHK-TRAY-12): per-profile thresholds, timeouts and the
	// server preset. The thresholds have their own focus targets: the
	// model path warnMinutes / errorMinutes is also a general setting.
	FieldPreset       Field = "serverPreset"
	FieldProfWarn     Field = "profile.warnMinutes"
	FieldProfError    Field = "profile.errorMinutes"
	FieldProfStalled  Field = "profile.stalledMinutes"
	FieldConnTimeout  Field = "connectTimeoutSec"
	FieldQueryTimeout Field = "queryTimeoutSec"
)

// PresetChoices are the server presets of the form, in combo box order
// (model.ServerPresetMSSQL2008, none = SQL Server 2012+).
var PresetChoices = []string{model.ServerPresetMSSQL2008, ""}

// PresetIndex is the combo box index of preset (unknown = none).
func PresetIndex(preset string) int {
	if preset == model.ServerPresetMSSQL2008 {
		return 0
	}
	return 1
}

// Inherited are the thresholds a profile without its own values uses:
// the general settings (model.Profile.EffectiveSettings of an empty
// profile: defaults 30 / 60 when unset or inconsistent).
func Inherited(global model.Settings) (warn, errMin int) {
	s := model.Profile{}.EffectiveSettings(global)
	return s.WarnMinutes, s.ErrorMinutes
}

// EncryptChoices are the encryption modes of the form, in combo box order.
var EncryptChoices = []model.EncryptMode{model.EncryptDisable, model.EncryptOptional, model.EncryptOn, model.EncryptStrict}

// EncryptIndex is the combo box index of e (effective mode).
func EncryptIndex(e model.EncryptMode) int {
	e = e.Effective()
	for i, c := range EncryptChoices {
		if c == e {
			return i
		}
	}
	return 0
}

// Form is the edited state of one profile. The server is split into host
// and instance (stored as "host\instance"); Port is the text of the edit
// box. The password itself is not part of the form (it stays in the edit
// control until a save / test request is built, see SaveRequest);
// PasswordTyped tells that the box is not empty. Empty means "keep the saved
// one" (when the service allows it).
type Form struct {
	Name            string
	Host            string
	Instance        string
	Port            string
	Database        string
	Auth            model.AuthMode
	Login           string
	PasswordTyped   bool
	ClearPassword   bool
	Encrypt         model.EncryptMode
	TLS10           bool
	TrustServerCert bool
	Enabled         bool
	// Phase 8 (CHK-TRAY-12): server preset, thresholds and timeouts as
	// typed. The boxes show the effective value: the profile's own or the
	// inherited one (InhWarn / InhError, the general settings) and the
	// timeout defaults of model; a value equal to the inherited / default
	// one is stored as 0 (no override), empty also means 0.
	Preset                    string
	WarnMin, ErrorMin         string
	ConnTimeout, QueryTimeout string
	StalledMin                string // 0 = inherit settings.stalledMinutes
	InhWarn, InhError         int
}

// SplitServer splits "host\instance" at the first backslash.
func SplitServer(server string) (host, instance string) {
	s := strings.TrimSpace(server)
	if i := strings.IndexByte(s, '\\'); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

// FromProfile fills a form from a saved profile (password empty) with the
// default general thresholds (30 / 60).
func FromProfile(p model.Profile) Form { return FromProfileIn(p, model.Settings{}) }

// FromProfileIn fills a form from a saved profile; global gives the
// inherited thresholds shown when the profile has none.
func FromProfileIn(p model.Profile, global model.Settings) Form {
	host, inst := SplitServer(p.Server)
	f := Form{
		Name: p.Name, Host: host, Instance: inst, Database: p.Database,
		Auth: p.AuthMode, Login: p.Login, Encrypt: p.EffectiveEncrypt(),
		TLS10: p.EffectiveTLSMinVersion() == model.TLSMin10, TrustServerCert: p.TrustServerCert, Enabled: p.Enabled,
	}
	if f.Auth == "" {
		f.Auth = model.AuthSQL
	}
	if p.Port != 0 {
		f.Port = strconv.Itoa(p.Port)
	}
	f.Preset = p.ServerPreset
	f.InhWarn, f.InhError = Inherited(global)
	pick := func(own, def int) string {
		if own > 0 {
			return strconv.Itoa(own)
		}
		return strconv.Itoa(def)
	}
	f.WarnMin, f.ErrorMin = pick(p.WarnMinutes, f.InhWarn), pick(p.ErrorMinutes, f.InhError)
	f.ConnTimeout = pick(p.ConnectTimeoutSec, model.DefaultConnectTimeoutSec)
	f.QueryTimeout = pick(p.QueryTimeoutSec, model.DefaultQueryTimeoutSec)
	f.StalledMin = strconv.Itoa(p.StalledMinutes) // 0 = inherit
	return f
}

// number parses an optional number box: empty = 0, otherwise min..max;
// a value equal to def is stored as 0 (no override).
func number(path, text string, min, max, def int) (int, *model.FieldError) {
	t := strings.TrimSpace(text)
	if t == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(t)
	if err != nil || n < min || n > max {
		return 0, fieldErr(path, model.ValRange, strconv.Itoa(min), strconv.Itoa(max), t)
	}
	if n == def {
		return 0, nil
	}
	return n, nil
}

// applyLimits applies the thresholds, timeouts and preset of the form
// (phase 8, CHK-TRAY-12) with local checks: the effective error
// threshold must stay greater than the effective warning (an override
// that breaks the pair would otherwise be ignored by the service).
func (f Form) applyLimits(p *model.Profile) *model.FieldError {
	iw, ie := f.InhWarn, f.InhError
	if iw <= 0 || ie <= iw {
		iw, ie = Inherited(model.Settings{})
	}
	var fe *model.FieldError
	if p.WarnMinutes, fe = number("warnMinutes", f.WarnMin, 1, model.MaxThresholdMinutes, iw); fe != nil {
		return fe
	}
	if p.ErrorMinutes, fe = number("errorMinutes", f.ErrorMin, 1, model.MaxThresholdMinutes, ie); fe != nil {
		return fe
	}
	ew, ee := iw, ie
	if p.WarnMinutes > 0 {
		ew = p.WarnMinutes
	}
	if p.ErrorMinutes > 0 {
		ee = p.ErrorMinutes
	}
	if ee <= ew {
		// both explicit then (equal to the inherited pair they are 0)
		p.WarnMinutes, p.ErrorMinutes = ew, ee
		return fieldErr("errorMinutes", model.ValGreaterThan, "warnMinutes", strconv.Itoa(ew))
	}
	if p.ConnectTimeoutSec, fe = number("connectTimeoutSec", f.ConnTimeout, 1, model.MaxTimeoutSec, model.DefaultConnectTimeoutSec); fe != nil {
		return fe
	}
	if p.QueryTimeoutSec, fe = number("queryTimeoutSec", f.QueryTimeout, 1, model.MaxTimeoutSec, model.DefaultQueryTimeoutSec); fe != nil {
		return fe
	}
	// stalledMinutes: 0 = inherit; else 10..1440 (empty → 0)
	st := strings.TrimSpace(f.StalledMin)
	if st == "" {
		p.StalledMinutes = 0
	} else {
		v, err := strconv.Atoi(st)
		if err != nil || !(v == 0 || (v >= model.MinStalledMinutes && v <= model.MaxStalledMinutes)) {
			return fieldErr("stalledMinutes", model.ValRange,
				strconv.Itoa(model.MinStalledMinutes), strconv.Itoa(model.MaxStalledMinutes), st)
		}
		p.StalledMinutes = v
	}
	p.ServerPreset = f.Preset
	return nil
}

// ProfileFromSnapshot builds the read-only view of a profile from
// get_status (no mapping details, no password: HasPassword only).
func ProfileFromSnapshot(ps model.ProfileSnapshot) model.Profile {
	p := model.Profile{ID: ps.ProfileID, Name: ps.Name, Enabled: ps.Enabled}
	if c := ps.Connection; c != nil {
		p.Server, p.Port, p.Database, p.AuthMode, p.Login, p.HasPassword = c.Server, c.Port, c.Database, c.AuthMode, c.Login, c.HasPassword
		p.Encrypt, p.TrustServerCert, p.TLSMinVersion, p.ServerPreset = c.Encrypt, c.TrustServerCert, c.TLSMinVersion, c.ServerPreset
		p.ConnectTimeoutSec, p.QueryTimeoutSec = c.ConnectTimeoutSec, c.QueryTimeoutSec
	}
	if t := ps.ThresholdOverride; t != nil {
		p.WarnMinutes, p.ErrorMinutes = t.WarnMinutes, t.ErrorMinutes
	}
	return p
}

func fieldErr(field, key string, args ...string) *model.FieldError {
	return &model.FieldError{Field: field, Key: key, Args: args, Msg: field + ": " + key}
}

// Apply returns base with the form values; the mapping of base is kept. Local checks (host / instance / port) come
// first, then model.Profile.Validate (a new profile is validated with a
// temporary id; the service assigns the real one). The login is cleared
// for Windows authentication; unchecking TLS 1.0 on a profile whose
// effective minimum is 1.0 sets "1.2".
func (f Form) Apply(base model.Profile) (model.Profile, *model.FieldError) {
	p := base
	host, inst := strings.TrimSpace(f.Host), strings.TrimSpace(f.Instance)
	switch {
	case host == "" && inst != "":
		return p, fieldErr("server", model.ValServerFormat, "empty_host")
	case host == "":
		return p, fieldErr("server", model.ValRequired)
	case strings.IndexByte(host, '\\') >= 0 || strings.IndexByte(inst, '\\') >= 0:
		return p, fieldErr("server", model.ValServerFormat, "backslash")
	}
	p.Server = host
	if inst != "" {
		p.Server = host + "\\" + inst
	}
	p.Port = 0
	if s := strings.TrimSpace(f.Port); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 || n > 65535 {
			return p, fieldErr("port", model.ValRange, "0", "65535", s)
		}
		p.Port = n
	}
	p.Name = strings.TrimSpace(f.Name)
	p.Database = strings.TrimSpace(f.Database)
	p.AuthMode = f.Auth
	p.Login = strings.TrimSpace(f.Login)
	if p.AuthMode == model.AuthWindows {
		p.Login = ""
	}
	p.Encrypt = f.Encrypt
	// «Доверять сертификату» acts only with «Весь сеанс (TLS)» (phase 8,
	// CHK-TRAY-13): optional never validates the certificate and strict
	// always does (sqlcheck.driverConfig), so it is not stored there.
	p.TrustServerCert = f.TrustServerCert && TrustApplies(f.Encrypt)
	switch {
	case f.TLS10:
		p.TLSMinVersion = model.TLSMin10
	case base.EffectiveTLSMinVersion() == model.TLSMin10:
		p.TLSMinVersion = model.TLSMin12
	}
	p.Enabled = f.Enabled
	if fe := f.applyLimits(&p); fe != nil {
		return p, fe
	}
	check := p
	if strings.TrimSpace(check.ID) == "" {
		check.ID = "new"
	}
	if err := check.Validate(); err != nil {
		if fe := model.AsFieldError(err); fe != nil {
			return p, fe
		}
		return p, &model.FieldError{Field: "", Key: model.ValRequired, Msg: err.Error()}
	}
	return p, nil
}

// TrustApplies tells whether «Доверять сертификату сервера» has an effect
// in encryption mode e: only EncryptOn (CHK-TRAY-13).
func TrustApplies(e model.EncryptMode) bool { return e.Effective() == model.EncryptOn }

// FocusProfileField is FocusField for a reply / error of a profile
// request: warnMinutes / errorMinutes are the profile's own thresholds.
func FocusProfileField(path string, params []string) Field {
	switch path {
	case "warnMinutes":
		return FieldProfWarn
	case "errorMinutes":
		return FieldProfError
	case "stalledMinutes":
		return FieldProfStalled
	case "connectTimeoutSec", "queryTimeoutSec", "serverPreset":
		return Field(path)
	}
	return FocusField(path, params)
}

// FocusField maps the JSON path of a service / model validation error to
// the control to focus (FieldNone when the form has no such control;
// mapping fields focus «Таблицы обмена…», FieldDiscoverButton).
func FocusField(path string, params []string) Field {
	if f, ok := EmailFocus(path); ok {
		return f
	}
	if f, ok := ViberFocus(path); ok {
		return f
	}
	switch path {
	case "server":
		if len(params) > 0 && params[0] == "empty_instance" {
			return FieldInstance
		}
		return FieldHost
	case "tlsMinVersion":
		return FieldTLS10
	case "name", "port", "database", "authMode", "login", "password", "clearPassword", "encrypt", "trustServerCert", "enabled":
		return Field(path)
	}
	if IsGeneral(Field(path)) {
		return Field(path)
	}
	if f, ok := discoveryFocus(path); ok {
		return f // mapping.* / mappings[i].*: «Таблицы обмена…»; query.*, limit: the dialog
	}
	return FieldNone
}
