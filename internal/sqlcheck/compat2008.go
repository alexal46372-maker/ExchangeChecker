package sqlcheck

import (
	"errors"
	"regexp"
	"strings"
)

// Target server (customer requirement, PLAN.md §12): SQL Server 2008,
// possibly R2, treated as compatibility level 100 with no 2008 R2-only
// features. Every query of the service (builders in sqlbuild.go, the
// driver's "select 1;" ping, sp_executesql parameters, TDS transactions)
// and the manual SQL templates use T-SQL of SQL Server 2005/2008 only:
// TOP (@p), COUNT_BIG, CASE, ROW_NUMBER() OVER (PARTITION BY ... ORDER BY ...),
// INFORMATION_SCHEMA, sys.partitions / sysindexes, WITH (NOLOCK).
// Compat2008Violations is the forbidden-construct check of CHK-SQL-05; it
// also rejects manual SQL (ErrManualCompat2008).

// ErrManualCompat2008: the manual SQL uses a construct that SQL Server
// 2008 (compatibility level 100) does not have.
var ErrManualCompat2008 = errors.New("manual SQL uses T-SQL not available in SQL Server 2008 (compatibility level 100)")

type compatRule struct {
	name string
	re   *regexp.Regexp
	// idents: match against the text with [quoted] / "quoted" identifier
	// contents kept (catalog names such as [sys].[dm_exec_sessions]).
	idents bool
}

func fn(name string) compatRule {
	return compatRule{name: name, re: regexp.MustCompile(`(?i)\b` + name + `\s*\(`)}
}

func kw(name, re string) compatRule {
	return compatRule{name: name, re: regexp.MustCompile(`(?i)` + re)}
}

// compat2008Rules: SQL Server 2012+ constructs (word boundary, case
// insensitive, applied after comments, string literals and quoted
// identifiers are blanked). sys.dm_* is rejected entirely: the service uses
// no DMV (they need VIEW SERVER STATE and several are 2008 R2 SP1+).
var compat2008Rules = []compatRule{
	kw("OFFSET ... ROWS", `\bOFFSET\s+\S+\s+ROWS?\b`),
	kw("FETCH NEXT/FIRST", `\bFETCH\s+(NEXT|FIRST)\b`),
	fn("TRY_CONVERT"), fn("TRY_CAST"), fn("TRY_PARSE"), fn("PARSE"),
	fn("IIF"), fn("CHOOSE"), fn("CONCAT"), fn("CONCAT_WS"), fn("FORMAT"),
	fn("STRING_AGG"), fn("STRING_SPLIT"), fn("STRING_ESCAPE"), fn("TRIM"), fn("TRANSLATE"),
	fn("EOMONTH"),
	kw("*FROMPARTS", `\b(DATE|DATETIME|DATETIME2|DATETIMEOFFSET|SMALLDATETIME|TIME)FROMPARTS\s*\(`),
	kw("THROW", `\bTHROW\b`),
	fn("LAG"), fn("LEAD"), fn("FIRST_VALUE"), fn("LAST_VALUE"),
	fn("PERCENT_RANK"), fn("CUME_DIST"), fn("PERCENTILE_CONT"), fn("PERCENTILE_DISC"),
	kw("window frame ROWS/RANGE", `\b(ROWS|RANGE)\s+(BETWEEN|UNBOUNDED|CURRENT)\b`),
	kw("WITHIN GROUP", `\bWITHIN\s+GROUP\b`),
	{name: "sys.dm_*", re: regexp.MustCompile(`(?i)\bsys\s*\.\s*dm_\w+`), idents: true},
	fn("JSON_VALUE"), fn("JSON_QUERY"), fn("JSON_MODIFY"), fn("JSON_OBJECT"), fn("JSON_ARRAY"),
	fn("ISJSON"), fn("OPENJSON"),
	kw("FOR JSON", `\bFOR\s+JSON\b`),
	fn("GREATEST"), fn("LEAST"), fn("DATETRUNC"), fn("DATE_BUCKET"), fn("GENERATE_SERIES"),
	fn("DATEDIFF_BIG"), fn("APPROX_COUNT_DISTINCT"),
	kw("NEXT VALUE FOR", `\bNEXT\s+VALUE\s+FOR\b`),
	kw("AT TIME ZONE", `\bAT\s+TIME\s+ZONE\b`),
	kw("CREATE OR ALTER", `\bCREATE\s+OR\s+ALTER\b`),
	kw("DROP ... IF EXISTS", `\bDROP\s+\w+\s+IF\s+EXISTS\b`),
}

// Compat2008Rules returns the names of the checked constructs (tests,
// documentation), including the windowed-aggregate rule.
func Compat2008Rules() []string {
	out := make([]string, 0, len(compat2008Rules)+1)
	for _, r := range compat2008Rules {
		out = append(out, r.name)
	}
	return append(out, ruleWindowedAggregate)
}

const ruleWindowedAggregate = "aggregate OVER (ORDER BY)"

var reAggregateCall = regexp.MustCompile(`(?i)\b(SUM|AVG|COUNT|COUNT_BIG|MIN|MAX|STDEV|STDEVP|VAR|VARP|CHECKSUM_AGG)\s*\(`)
var reOver = regexp.MustCompile(`(?i)^\s*OVER\s*\(`)
var reOrderBy = regexp.MustCompile(`(?i)\bORDER\s+BY\b`)

// Compat2008Violations returns the names of SQL Server 2012+ constructs
// found in q (nil = compatible with level 100).
func Compat2008Violations(q string) []string {
	s, si := blankSQL(q, false), blankSQL(q, true)
	var out []string
	for _, r := range compat2008Rules {
		t := s
		if r.idents {
			t = si
		}
		if r.re.MatchString(t) {
			out = append(out, r.name)
		}
	}
	if windowedAggregateOrderBy(s) {
		out = append(out, ruleWindowedAggregate)
	}
	return out
}

// windowedAggregateOrderBy finds AGG(...) OVER (... ORDER BY ...), a
// running aggregate (SQL 2012+). AGG(...) OVER (PARTITION BY ...) and
// ROW_NUMBER() OVER (... ORDER BY ...) are SQL 2005 and allowed.
func windowedAggregateOrderBy(s string) bool {
	for _, loc := range reAggregateCall.FindAllStringIndex(s, -1) {
		end := closeParen(s, loc[1]-1)
		if end < 0 {
			continue
		}
		rest := s[end+1:]
		m := reOver.FindStringIndex(rest)
		if m == nil {
			continue
		}
		oe := closeParen(rest, m[1]-1)
		if oe < 0 {
			oe = len(rest) - 1
		}
		if reOrderBy.MatchString(rest[m[1]:oe]) {
			return true
		}
	}
	return false
}

// closeParen returns the index of the ')' matching s[open] == '(' or -1.
func closeParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// blankSQL removes comments and replaces string literals ('...', N'...')
// and quoted identifiers ([...], "...") by empty ones, so names such as
// [Format] or 'IIF(' in data never match a rule. keepIdents keeps the
// identifier contents without the quotes (string literals stay blank).
func blankSQL(q string, keepIdents bool) string {
	var b strings.Builder
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case c == '-' && i+1 < len(q) && q[i+1] == '-':
			for i < len(q) && q[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			end := strings.Index(q[i+2:], "*/")
			if end < 0 {
				i = len(q)
			} else {
				i += end + 4
			}
			b.WriteByte(' ')
		case c == '\'' || c == '"' || c == '[':
			closeCh := c
			if c == '[' {
				closeCh = ']'
			}
			j := i + 1
			for j < len(q) {
				if q[j] == closeCh {
					if j+1 < len(q) && q[j+1] == closeCh {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if keepIdents && c != '\'' && j <= len(q) {
				b.WriteString(strings.ReplaceAll(q[i+1:minInt(j, len(q))], string([]byte{closeCh, closeCh}), string(closeCh)))
			} else {
				b.WriteByte(c)
				b.WriteByte(closeCh)
			}
			i = j + 1
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
