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
		ValServerFormat, ValMapping, ValDuplicate, ValConflict}
}

// FormatMessage returns MonitorMessage(key, lang) with {0}, {1}, ...
// replaced by args.
func FormatMessage(key string, args []string, lang string) string {
	s := MonitorMessage(key, lang)
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
