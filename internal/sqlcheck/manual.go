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
	ErrManualLiteral   = errors.New("manual SQL has an unterminated string, [identifier] or \"identifier\"")
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

// ValidateManualSQL checks the manual query (phase 2c, hardened in the
// phase 6 review, PLAN §12 p. 80): no comments (ErrManualComment), no
// unterminated literals (ErrManualLiteral), a single statement (a
// trailing ';' is allowed), starting with SELECT or WITH, exactly one
// top-level SELECT, only the reserved words of a read-only SELECT
// (ErrManualForbidden; the keyword blacklist manualForbidden first), no
// non-reserved statement verb at depth 0, table hints only NOLOCK /
// READUNCOMMITTED / NOWAIT (ErrManualHint), no OPTION (...)
// (ErrManualOption), and no T-SQL newer than SQL Server 2008
// (ErrManualCompat2008, CHK-SQL-05). See manuallex.go.
//
// Every error carries a *model.FieldError (Field "manualSql", Key
// validation.sql_*, FieldErrorOf) and matches its sentinel with
// errors.Is (phase 6, G1/G4).
func ValidateManualSQL(q string) error {
	toks, what, err := lexManual(q)
	switch {
	case errors.Is(err, ErrManualComment):
		return manualErr(ErrManualComment, model.ValSQLComment, ErrManualComment.Error())
	case errors.Is(err, ErrManualLiteral):
		return manualErr(ErrManualLiteral, model.ValSQLManualLiteral, ErrManualLiteral.Error())
	case errors.Is(err, ErrManualMulti):
		return manualErr(ErrManualMulti, model.ValSQLManualMulti, ErrManualMulti.Error())
	case errors.Is(err, ErrManualForbidden):
		return forbiddenErr(what)
	case err != nil:
		return err
	}
	if len(toks) == 0 {
		return manualErr(ErrManualEmpty, model.ValSQLManualEmpty, ErrManualEmpty.Error())
	}
	if w := toks[0].up; toks[0].kind != mtWord || w != "SELECT" && w != "WITH" {
		return manualErr(ErrManualNotSelect, model.ValSQLManualNotSelect, ErrManualNotSelect.Error())
	}
	if v := Compat2008Violations(q); len(v) > 0 {
		list := strings.Join(v, ", ")
		return manualErr(ErrManualCompat2008, model.ValSQLManualCompat2008,
			fmt.Sprintf("%s: %s (use TOP, ROW_NUMBER() OVER (PARTITION BY ... ORDER BY ...), CASE, CONVERT, + for strings)",
				ErrManualCompat2008, list), list)
	}
	return manualStructure(toks)
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
