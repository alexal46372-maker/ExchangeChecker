package sqlcheck

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Typed validation errors (phase 6, G1/G4, CHK-SQL-04): mapping and manual
// SQL problems carry a *model.FieldError (field, validation.sql_* key,
// args) the service passes into errorArgs without parsing text, and stay
// errors.Is-compatible with the sentinels of this package
// (ErrManualForbidden, ErrManualMulti, ErrManualCompat2008, ErrTableNotFound,
// ...). Error() is the same English text as before.
type fieldError struct {
	fe  *model.FieldError
	err error // sentinel / cause for errors.Is
}

func (e *fieldError) Error() string { return e.fe.Msg }

// Unwrap exposes both the FieldError (errors.As, model.AsFieldError) and
// the sentinel (errors.Is).
func (e *fieldError) Unwrap() []error { return []error{e.fe, e.err} }

func newFieldError(cause error, field, key, msg string, args ...string) error {
	return &fieldError{fe: &model.FieldError{Field: field, Key: key, Args: args, Msg: msg}, err: cause}
}

// FieldErrorOf returns the typed validation error of err (a mapping or
// manual SQL problem, also inside *Error), or nil: errorArgs =
// FieldErrorOf(err).IPCArgs(). Same as model.AsFieldError.
func FieldErrorOf(err error) *model.FieldError { return model.AsFieldError(err) }

// Fields of the manual SQL errors (JSON name of Mapping.ManualSQL).
const fieldManualSQL = "manualSql"

func manualErr(sentinel error, key, msg string, args ...string) error {
	return newFieldError(sentinel, fieldManualSQL, key, msg, args...)
}

// identErr turns a checkIdent failure of the mapping field into a typed
// error (empty -> validation.required).
func identErr(field, role, name string, err error) error {
	msg := role + " column: " + err.Error()
	reason := ""
	switch {
	case name == "":
		return newFieldError(err, field, model.ValRequired, msg)
	case !utf8.ValidString(name):
		reason = "invalid_utf8"
	case strings.ContainsRune(name, 0):
		reason = "nul"
	default:
		reason = "too_long"
	}
	return newFieldError(err, field, model.ValSQLIdentifier, msg, reason, strconv.Itoa(MaxIdentLen))
}
