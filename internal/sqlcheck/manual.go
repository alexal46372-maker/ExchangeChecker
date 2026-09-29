package sqlcheck

import (
	"errors"
	"fmt"
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
				return ErrManualNotSelect
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
				return fmt.Errorf("%w %s (only one read-only SELECT is allowed)", ErrManualForbidden, tk)
			}
		}
	}
	if selects != 1 {
		return ErrManualMulti
	}
	return nil
}

// ValidateManualSQL checks the manual query: not empty, a single statement
// (a trailing ';' is allowed), starting with SELECT or WITH, exactly one
// top-level SELECT, none of the forbidden keywords outside literals /
// comments / quoted identifiers (ErrManualForbidden, phase 2c), and no
// T-SQL newer than SQL Server 2008 (ErrManualCompat2008, CHK-SQL-05).
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
	if v := Compat2008Violations(body); len(v) > 0 {
		return fmt.Errorf("%w: %s (use TOP, ROW_NUMBER() OVER (PARTITION BY ... ORDER BY ...), CASE, CONVERT, + for strings)",
			ErrManualCompat2008, strings.Join(v, ", "))
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

// ManualSQLTemplate is an example manual query for the setup UI.
type ManualSQLTemplate struct {
	Name        string
	Description string
	SQL         string
}

// ManualSQLTemplates are examples of the manual SQL contract written for
// SQL Server 2008 (compatibility level 100): TOP / GROUP BY / MAX,
// ROW_NUMBER() OVER (PARTITION BY ... ORDER BY ...), CASE and binary
// literals only; no OFFSET/FETCH, TRY_CONVERT, IIF, CONCAT, FORMAT, LAG/LEAD
// or windowed aggregates with ORDER BY (CHK-SQL-05 checks them). Table
// and column names are the 1C storage names of the example databases;
// replace the numbers with those of the actual configuration.
var ManualSQLTemplates = []ManualSQLTemplate{
	{
		Name:        "1c_node_linked",
		Description: "exchange plan _NodeNNN + register with a simple reference _FldNNNRRef",
		SQL: `SELECT n.[_Description], MAX(r.[_Fld458]), MAX(r.[_Fld459])
FROM [dbo].[_Node123] AS n WITH (NOLOCK)
JOIN [dbo].[_InfoRg456] AS r WITH (NOLOCK) ON r.[_Fld457RRef] = n.[_IDRRef]
GROUP BY n.[_IDRRef], n.[_Description]`,
	},
	{
		Name:        "1c_node_composite",
		Description: "exchange plan _Node124 + register with a 1C 8.1 composite reference (_TYPE = 0x08, _RTRef = table number 124 = 0x0000007C)",
		SQL: `SELECT n.[_Description], MAX(r.[_Fld602]), MAX(r.[_Fld603])
FROM [dbo].[_Node124] AS n WITH (NOLOCK)
JOIN [dbo].[_InfoRg600] AS r WITH (NOLOCK)
  ON r.[_Fld601_TYPE] = 0x08 AND r.[_Fld601_RTRef] = 0x0000007C AND r.[_Fld601_RRRef] = n.[_IDRRef]
GROUP BY n.[_IDRRef], n.[_Description]`,
	},
	{
		Name:        "1c_latest_row",
		Description: "latest register row per point via ROW_NUMBER() (SQL 2005+; instead of LAG/OFFSET)",
		SQL: `WITH x AS (
  SELECT n.[_Description] AS point, r.[_Fld458] AS sent, r.[_Fld459] AS recv,
         ROW_NUMBER() OVER (PARTITION BY r.[_Fld457RRef] ORDER BY r.[_Fld459] DESC) AS rn
  FROM [dbo].[_InfoRg456] AS r WITH (NOLOCK)
  JOIN [dbo].[_Node123] AS n WITH (NOLOCK) ON n.[_IDRRef] = r.[_Fld457RRef]
)
SELECT point, sent, recv FROM x WHERE rn = 1`,
	},
	{
		Name:        "1c_directory_dates",
		Description: "exchange dates in the directory row itself; CASE instead of IIF (SQL 2012+); the service also turns 1C empty dates into NULL",
		SQL: `SELECT TOP (1000) d.[_Description],
  CASE WHEN d.[_Fld56] > '17530101' THEN d.[_Fld56] END,
  CASE WHEN d.[_Fld57] > '17530101' THEN d.[_Fld57] END
FROM [dbo].[_Reference55] AS d WITH (NOLOCK)
ORDER BY d.[_Description]`,
	},
}
