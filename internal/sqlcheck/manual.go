package sqlcheck

import (
	"errors"
	"strings"
)

// ManualSQL contract (CHK-SQL-04): one read-only SELECT (or WITH ... SELECT)
// statement returning exactly three columns in this order:
//
//	point name (any type, converted to text), last send (datetime or NULL),
//	last recv (datetime or NULL)
//
// The mapping's YearOffset applies to the timestamps, then they are read as
// UTC+3 like any other mapping. The query runs inside a transaction that is
// always rolled back, and at most Options.MaxPoints rows are read.

// Manual SQL errors (wrapped in *Error with KindMapping).
var (
	ErrManualEmpty     = errors.New("manual SQL is empty")
	ErrManualNotSelect = errors.New("manual SQL must be a single SELECT (or WITH ... SELECT) statement")
	ErrManualMulti     = errors.New("manual SQL must contain a single statement")
	ErrManualLiteral   = errors.New("manual SQL has an unterminated string, [identifier] or comment")
	ErrManualColumns   = errors.New("manual SQL must return exactly 3 columns: point, last send, last recv")
)

// ValidateManualSQL checks the manual query: not empty, a single statement
// (a trailing ';' is allowed), starting with SELECT or WITH.
func ValidateManualSQL(q string) error {
	body, err := stripManualSQL(q)
	if err != nil {
		return err
	}
	if body == "" {
		return ErrManualEmpty
	}
	first := strings.ToUpper(firstWord(body))
	if first != "SELECT" && first != "WITH" {
		return ErrManualNotSelect
	}
	return nil
}

// stripManualSQL removes comments and returns the statement text with
// string/identifier literals kept intact. It fails on a second statement
// (';' followed by anything but whitespace/comments) or unterminated
// literals/comments.
func stripManualSQL(q string) (string, error) {
	var b strings.Builder
	semi := false
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case c == '-' && i+1 < len(q) && q[i+1] == '-':
			for i < len(q) && q[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
			continue
		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			end := strings.Index(q[i+2:], "*/")
			if end < 0 {
				return "", ErrManualLiteral
			}
			i += end + 4
			b.WriteByte(' ')
			continue
		case c == ';':
			semi = true
			i++
			continue
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			b.WriteByte(c)
			i++
			continue
		}
		if semi {
			return "", ErrManualMulti
		}
		if c == '\'' || c == '"' || c == '[' {
			closeCh := c
			if c == '[' {
				closeCh = ']'
			}
			j := i + 1
			for {
				k := strings.IndexByte(q[j:], closeCh)
				if k < 0 {
					return "", ErrManualLiteral
				}
				j += k + 1
				if j < len(q) && q[j] == closeCh { // doubled quote = escaped
					j++
					continue
				}
				break
			}
			b.WriteString(q[i:j])
			i = j
			continue
		}
		b.WriteByte(c)
		i++
	}
	return strings.TrimSpace(b.String()), nil
}

func firstWord(s string) string {
	for i, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return s[:i]
		}
	}
	return s
}
