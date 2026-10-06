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
)

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
}

// SplitServer splits "host\instance" at the first backslash.
func SplitServer(server string) (host, instance string) {
	s := strings.TrimSpace(server)
	if i := strings.IndexByte(s, '\\'); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

// FromProfile fills a form from a saved profile (password empty).
func FromProfile(p model.Profile) Form {
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
	return f
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

// Apply returns base with the form values: mapping, thresholds, timeouts
// and preset of base are kept. Local checks (host / instance / port) come
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
	p.TrustServerCert = f.TrustServerCert
	switch {
	case f.TLS10:
		p.TLSMinVersion = model.TLSMin10
	case base.EffectiveTLSMinVersion() == model.TLSMin10:
		p.TLSMinVersion = model.TLSMin12
	}
	p.Enabled = f.Enabled
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

// FocusField maps the JSON path of a service / model validation error to
// the control to focus (FieldNone when the form has no such control;
// mapping fields focus «Таблицы обмена…», FieldDiscoverButton).
func FocusField(path string, params []string) Field {
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
