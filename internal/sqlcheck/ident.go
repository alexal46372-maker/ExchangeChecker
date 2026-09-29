package sqlcheck

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxIdentLen is the SQL Server sysname limit (QUOTENAME accepts at most
// 128 characters).
const MaxIdentLen = 128

// QuoteIdent returns s as a bracket-delimited T-SQL identifier with every
// ']' doubled — the same result as QUOTENAME(s, '[') for valid names.
func QuoteIdent(s string) string {
	return "[" + strings.ReplaceAll(s, "]", "]]") + "]"
}

// checkIdent rejects names that QUOTENAME would reject or that cannot be a
// catalog name: empty, longer than MaxIdentLen, invalid UTF-8, NUL.
func checkIdent(s string) error {
	switch {
	case s == "":
		return errors.New("empty identifier")
	case !utf8.ValidString(s):
		return errors.New("identifier is not valid UTF-8")
	case strings.ContainsRune(s, 0):
		return errors.New("identifier contains NUL")
	case utf8.RuneCountInString(s) > MaxIdentLen:
		return fmt.Errorf("identifier longer than %d characters", MaxIdentLen)
	}
	return nil
}

// quoteChecked validates and quotes an identifier.
func quoteChecked(s string) (string, error) {
	if err := checkIdent(s); err != nil {
		return "", fmt.Errorf("%w: %q", err, s)
	}
	return QuoteIdent(s), nil
}

// TableRef names a table; an empty Schema means the login's default schema.
type TableRef struct {
	Schema string `json:"schema,omitempty"`
	Name   string `json:"name"`
}

// String returns "schema.name" (or "name") for messages; not for SQL.
func (t TableRef) String() string {
	if t.Schema == "" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

// SQL returns the quoted, schema-qualified name: [schema].[name] or [name].
func (t TableRef) SQL() (string, error) {
	n, err := quoteChecked(t.Name)
	if err != nil {
		return "", err
	}
	if t.Schema == "" {
		return n, nil
	}
	s, err := quoteChecked(t.Schema)
	if err != nil {
		return "", err
	}
	return s + "." + n, nil
}
