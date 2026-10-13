package model

import (
	"errors"
	"strconv"
	"strings"
)

// Typed validation errors (phase 6, CHK-CFG-03). Profile.Validate,
// Mapping.Validate and Settings.Validate return *FieldError (possibly
// wrapped with %w or joined with errors.Join) so the service can answer
// invalid_request with errorArgs = [Field, Key, Args...] and the tray can
// show a localized text next to the right input. Error() is the same
// English text as before phase 6 (logs and string checks unchanged).
type FieldError struct {
	// Field is the JSON path of the field: "server", "port",
	// "mapping.lastSendColumn", "mappings[1].table", "beep.warningFreqHz".
	Field string
	// Key is the reason, a validation.* message key (ValidationMessage).
	Key string
	// Args are the text parameters of Key ({0}, {1}, ... in the text).
	Args []string
	// Msg is the English technical text (Error()).
	Msg string
	// ProfileID is the id of the profile the error belongs to, set by
	// ValidateProfiles (phase 6: a write refused because of ANOTHER
	// invalid profile in profiles.json names it in the reply's
	// profileId). Not part of IPCArgs / Error().
	ProfileID string
}

func (e *FieldError) Error() string { return e.Msg }

// IPCArgs returns errorArgs of an invalid_request reply: field, reason key,
// parameters.
func (e *FieldError) IPCArgs() []string {
	return append([]string{e.Field, e.Key}, e.Args...)
}

// AsFieldError returns the first *FieldError in err's chain (also inside
// errors.Join), or nil.
func AsFieldError(err error) *FieldError {
	var fe *FieldError
	if errors.As(err, &fe) {
		return fe
	}
	return nil
}

func fieldErr(field, key, msg string, args ...string) *FieldError {
	return &FieldError{Field: field, Key: key, Args: args, Msg: msg}
}

// withFieldPrefix prefixes the Field of a direct *FieldError (mapping
// validators -> "mapping." / "mappings[i]."); other errors unchanged.
func withFieldPrefix(err error, prefix string) error {
	if fe, ok := err.(*FieldError); ok {
		c := *fe
		c.Field = prefix + fe.Field
		return &c
	}
	return err
}

func itoa(n int) string { return strconv.Itoa(n) }

// Validation reason keys (FieldError.Key). Texts: MonitorMessage /
// FormatMessage with the FieldError.Args.
const (
	// ValRequired: the field is empty.
	ValRequired = "validation.required"
	// ValRange: {0} = min, {1} = max, {2} = given value.
	ValRange = "validation.range"
	// ValEnum: unsupported value {0}.
	ValEnum = "validation.enum"
	// ValGreaterThan: must be greater than field {0} (value {1}).
	ValGreaterThan = "validation.greater_than"
	// ValTooLong: longer than {0} bytes.
	ValTooLong = "validation.too_long"
	// ValTooMany: more than {0} items.
	ValTooMany = "validation.too_many"
	// ValPresetConflict: the value conflicts with the server preset.
	ValPresetConflict = "validation.preset_conflict"
	// ValServerFormat: server is not "host" or "host\instance"; {0} =
	// empty_host | empty_instance | backslash.
	ValServerFormat = "validation.server_format"
	// ValMapping: inconsistent mapping fields; {0} = English detail.
	ValMapping = "validation.mapping"
	// ValDuplicate: duplicate profile id.
	ValDuplicate = "validation.duplicate"
	// ValConflict: mutually exclusive request fields (password and
	// clearPassword).
	ValConflict = "validation.conflict"
	// ValSQLServerForbidden: the SQL Server address names a named pipe /
	// LPC / DAC / UNC path (np:, lpc:, admin:, \\host\..., //host/...,
	// a leading backslash, ...\pipe\...): such a connection
	// authenticates the service account over SMB (phase 9, CHK-CFG-05);
	// {0} = np | lpc | admin | tcp | sm | via | unc | backslash | slash |
	// pipe (protocol prefixes are not allowed at all: the service picks TCP).
	ValSQLServerForbidden = "validation.sql_server_forbidden"
)

// Limits of a profile (phase 6): a save_profile request must fit into
// IPCMaxRequest (64 KiB).
const (
	MaxManualSQLBytes = 16 << 10
	MaxMappings       = 64
)

// ValidationKeys lists the validation.* keys.
func ValidationKeys() []string {
	return []string{ValRequired, ValRange, ValEnum, ValGreaterThan, ValTooLong, ValTooMany, ValPresetConflict,
		ValServerFormat, ValMapping, ValDuplicate, ValConflict, ValSQLServerForbidden}
}

// FormatMessage returns MonitorMessage(key, lang) (or SQLMessage for the
// sql.* / validation.sql_* keys) with {0}, {1}, ... replaced by args.
func FormatMessage(key string, args []string, lang string) string {
	s := MonitorMessage(key, lang)
	if s == "" { // validation.sql_* / sql.* (sqlmessages.go, phase 6 G1)
		s = SQLMessage(key, lang)
	}
	for i, a := range args {
		s = strings.ReplaceAll(s, "{"+itoa(i)+"}", a)
	}
	return s
}

// validateServer checks "host" or "host\instance": non-empty host, a
// non-empty instance after the backslash and at most one backslash.
func validateServer(server string) *FieldError {
	s := strings.TrimSpace(server)
	if s == "" {
		return fieldErr("server", ValRequired, "server is required")
	}
	if err := ValidateSQLServerAddr(s); err != nil {
		return err.(*FieldError)
	}
	i := strings.IndexByte(s, '\\')
	if i < 0 {
		return nil
	}
	host, inst := strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	switch {
	case host == "":
		return fieldErr("server", ValServerFormat, "server: host is empty (want host or host\\instance)", "empty_host")
	case strings.IndexByte(inst, '\\') >= 0:
		return fieldErr("server", ValServerFormat, "server: more than one backslash (want host or host\\instance)", "backslash")
	case inst == "":
		return fieldErr("server", ValServerFormat, "server: instance is empty after the backslash (want host or host\\instance)", "empty_instance")
	}
	return nil
}

// ValidateSQLServerAddr checks a SQL Server address (phase 9,
// CHK-CFG-05): nil when allowed, else a *FieldError (field "server", key
// ValSQLServerForbidden, Args [reason]). Forbidden: the np: / lpc: /
// admin: prefixes (ASCII case-insensitive) -> np / lpc / admin, a UNC
// path (\\host or //host) -> unc, any other name starting with a
// backslash -> backslash, a pipe path (\pipe\ or /pipe/ anywhere) ->
// pipe; the protocol prefixes tcp: / sm: / via: -> tcp / sm / via and a
// leading slash -> slash. Only a plain host / host\instance is allowed. Shared by the
// tray (early feedback), the service (save_profile, test_profile) and
// later EC SQL (sqlcheck).
func ValidateSQLServerAddr(addr string) error {
	why := sqlServerForbidden(addr)
	if why == "" {
		return nil
	}
	return fieldErr("server", ValSQLServerForbidden, "server: protocol prefixes, named pipes and UNC paths are not allowed ("+why+")", why)
}

func sqlServerForbidden(addr string) string {
	s := strings.ToLower(strings.TrimSpace(addr))
	switch {
	case strings.HasPrefix(s, "np:"):
		return "np"
	case strings.HasPrefix(s, "lpc:"):
		return "lpc"
	case strings.HasPrefix(s, "admin:"):
		return "admin"
	case strings.HasPrefix(s, "tcp:"):
		return "tcp"
	case strings.HasPrefix(s, "sm:"):
		return "sm"
	case strings.HasPrefix(s, "via:"):
		return "via"
	case strings.HasPrefix(s, `\\`), strings.HasPrefix(s, "//"), strings.HasPrefix(s, `\/`), strings.HasPrefix(s, `/\`):
		return "unc"
	case strings.HasPrefix(s, `\`):
		return "backslash"
	case strings.HasPrefix(s, "/"):
		return "slash"
	case strings.Contains(s, `\pipe\`), strings.Contains(s, "/pipe/"), strings.Contains(s, `\pipe/`), strings.Contains(s, `/pipe\`):
		return "pipe"
	}
	return ""
}

// TargetChanged reports whether p connects to another server than saved:
// host, instance or effective port differ (host / instance ASCII
// case-insensitive, port 0 = 1433 without an instance). A changed target
// requires the password to be entered again; the saved secret is never
// sent to a new target (phase 9, CHK-CFG-05).
func TargetChanged(p, saved Profile) bool {
	h1, i1 := splitHostInstance(p.Server)
	h2, i2 := splitHostInstance(saved.Server)
	return h1 != h2 || i1 != i2 || identityPort(p.Port, i1) != identityPort(saved.Port, i2)
}
