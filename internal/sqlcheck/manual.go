package sqlcheck

import (
	"errors"
	"fmt"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// ManualSQL contract (CHK-SQL-04): one read-only SELECT (or WITH ... SELECT)
// statement returning exactly three columns in this order:
//
//	point name (any type, converted to text), last send (datetime or NULL),
//	last recv (datetime or NULL)
//
// The mapping's YearOffset applies to the timestamps, then they are read as
// UTC+3 like any other mapping. The query runs inside a transaction that is
// always rolled back, and at most Options.MaxPoints rows are read. The
// rollback is not a security boundary (a COMMIT inside the text would end
// it), so ValidateManualSQL allows exactly one read-only SELECT (phase 2c);
// the real boundary is a login with db_datareader only (PLAN.md §12).

// Manual SQL errors (wrapped in *Error with KindMapping).
var (
	ErrManualEmpty     = errors.New("manual SQL is empty")
	ErrManualNotSelect = errors.New("manual SQL must be a single SELECT (or WITH ... SELECT) statement")
	ErrManualMulti     = errors.New("manual SQL must contain a single statement")
	ErrManualLiteral   = errors.New("manual SQL has an unterminated string, [identifier] or comment")
	ErrManualColumns   = errors.New("manual SQL must return exactly 3 columns: point, last send, last recv")
	// ErrManualForbidden: a data-changing / session / batch keyword outside
	// literals, comments and quoted identifiers (phase 2c, CHK-SQL-04).
	ErrManualForbidden = errors.New("manual SQL must be read-only: forbidden keyword")
)

// manualForbidden are T-SQL keywords that never occur in a single read-only
// SELECT. Statements need no ';' in T-SQL, so "SELECT 1 COMMIT DELETE FROM t"
// is three statements (and the COMMIT would end the rolled-back
// transaction); every keyword is therefore rejected wherever it appears
// outside string literals, comments, [bracketed] and "double-quoted"
// identifiers. All of them are T-SQL reserved words, so a column with such
// a name must be quoted anyway ([Update], "Set").
var manualForbidden = map[string]bool{}

func init() {
	for _, k := range strings.Fields(`INSERT UPDATE DELETE MERGE INTO EXEC EXECUTE COMMIT ROLLBACK SAVE BEGIN
		DROP ALTER CREATE TRUNCATE GRANT REVOKE DENY DBCC BACKUP RESTORE SHUTDOWN KILL
		OPENROWSET OPENQUERY OPENDATASOURCE BULK WAITFOR DECLARE SET USE
		TRAN TRANSACTION RECONFIGURE CHECKPOINT REVERT SETUSER DEALLOCATE OPEN CLOSE FETCH
		IF WHILE GOTO RETURN BREAK CONTINUE PRINT RAISERROR READTEXT WRITETEXT UPDATETEXT`) {
		manualForbidden[k] = true
	}
}

// sqlTokens splits blanked SQL (blankSQL: no comments, empty literals and
// quoted identifiers) into upper-case words and single-character symbols.
// Variables (@x, @@x) and temp names (#x) are kept whole, so "@set" is not
// the keyword SET.
func sqlTokens(blank string) []string {
	var out []string
	isWord := func(c byte) bool {
		return c == '_' || c == '@' || c == '#' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
	}
	for i := 0; i < len(blank); {
		c := blank[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case isWord(c):
			j := i
			for j < len(blank) && isWord(blank[j]) {
				j++
			}
			out = append(out, strings.ToUpper(blank[i:j]))
			i = j
		default:
			out = append(out, string(c))
			i++
		}
	}
	return out
}

// checkManualTokens rejects forbidden keywords and a second top-level
// statement: at parenthesis depth 0 exactly one SELECT is allowed, apart
// from the SELECTs joined by UNION [ALL] / EXCEPT / INTERSECT (CTE bodies
// and subqueries are inside parentheses).
func checkManualTokens(body string) error {
	toks := sqlTokens(blankSQL(body, false))
	depth, selects := 0, 0
	for i, tk := range toks {
		switch tk {
		case "(":
			depth++
		case ")":
			depth--
			if depth < 0 {
				return manualErr(ErrManualNotSelect, model.ValSQLManualNotSelect, ErrManualNotSelect.Error())
			}
		case "SELECT":
			if depth == 0 {
				prev := ""
				if i > 0 {
					prev = toks[i-1]
				}
				if prev == "ALL" && i > 1 {
					prev = toks[i-2]
				}
				if prev != "UNION" && prev != "EXCEPT" && prev != "INTERSECT" {
					selects++
				}
			}
		default:
			if manualForbidden[tk] {
				return manualErr(ErrManualForbidden, model.ValSQLManualForbidden,
					fmt.Sprintf("%s %s (only one read-only SELECT is allowed)", ErrManualForbidden, tk), tk)
			}
		}
	}
	if selects != 1 {
		return manualErr(ErrManualMulti, model.ValSQLManualMulti, ErrManualMulti.Error())
	}
	return nil
}

// ValidateManualSQL checks the manual query: not empty, a single statement
// (a trailing ';' is allowed), starting with SELECT or WITH, exactly one
// top-level SELECT, none of the forbidden keywords outside literals /
// comments / quoted identifiers (ErrManualForbidden, phase 2c), and no
// T-SQL newer than SQL Server 2008 (ErrManualCompat2008, CHK-SQL-05).
//
// Every error carries a *model.FieldError (Field "manualSql", Key
// validation.sql_manual_*, FieldErrorOf) and matches its sentinel with
// errors.Is (phase 6, G1/G4).
func ValidateManualSQL(q string) error {
	body, err := stripManualSQL(q)
	switch {
	case errors.Is(err, ErrManualLiteral):
		return manualErr(ErrManualLiteral, model.ValSQLManualLiteral, ErrManualLiteral.Error())
	case errors.Is(err, ErrManualMulti):
		return manualErr(ErrManualMulti, model.ValSQLManualMulti, ErrManualMulti.Error())
	case err != nil:
		return err
	}
	if body == "" {
		return manualErr(ErrManualEmpty, model.ValSQLManualEmpty, ErrManualEmpty.Error())
	}
	first := strings.ToUpper(firstWord(body))
	if first != "SELECT" && first != "WITH" {
		return manualErr(ErrManualNotSelect, model.ValSQLManualNotSelect, ErrManualNotSelect.Error())
	}
	if v := Compat2008Violations(body); len(v) > 0 {
		list := strings.Join(v, ", ")
		return manualErr(ErrManualCompat2008, model.ValSQLManualCompat2008,
			fmt.Sprintf("%s: %s (use TOP, ROW_NUMBER() OVER (PARTITION BY ... ORDER BY ...), CASE, CONVERT, + for strings)",
				ErrManualCompat2008, list), list)
	}
	return checkManualTokens(body)
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

// ManualSQLTemplate is an example manual query for the setup UI (the
// pre-phase-6 shape; model.ManualSQLTemplate carries RU/EN titles).
type ManualSQLTemplate struct {
	Name        string
	Description string
	SQL         string
}

// ManualSQLTemplates are the examples of model.SQLManualTemplates (phase
// 6, G4) in the old shape: Name = ID, Description = English description.
// They are SQL Server 2008 T-SQL (CHK-SQL-05).
var ManualSQLTemplates = legacyTemplates()

func legacyTemplates() []ManualSQLTemplate {
	var out []ManualSQLTemplate
	for _, t := range model.SQLManualTemplates() {
		out = append(out, ManualSQLTemplate{Name: t.ID, Description: t.Description.EN, SQL: t.SQL})
	}
	return out
}
