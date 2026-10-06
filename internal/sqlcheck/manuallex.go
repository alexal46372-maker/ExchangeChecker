package sqlcheck

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Manual SQL hardening (phase 6 review of the Discovery contour, PLAN §12
// p. 80, CHK-SQL-04). The manual query is lexed exactly once, the way SQL
// Server reads it, and every decision is made on those tokens:
//
//   - comments are not allowed at all (SQL Server nests /* */, so
//     "/* /* */ ' */ COMMIT DELETE FROM t --'" is a comment for a naive
//     validator and three statements for the server);
//   - string literals ('...', N'...', '' escapes), [identifiers] (]]
//     escapes) and "identifiers" ("" escapes) are opaque tokens;
//     unterminated ones are rejected;
//   - outside them only ASCII and Unicode letters / digits / marks are
//     accepted (no Unicode spaces, zero-width or control characters that
//     the server might read differently);
//   - a ';' may only end the text.
//
// The token check is a whitelist (manualStructure): SELECT or WITH first,
// exactly one top-level SELECT (UNION / EXCEPT / INTERSECT chains), every
// T-SQL reserved word must be one of the words a SELECT uses, statement
// verbs that are not reserved (RECEIVE, SEND, ENABLE, DISABLE, MOVE, GET,
// THROW, ...) are rejected at parenthesis depth 0 unless used as AS alias
// or part of a dotted name, END must close a CASE, WITH after the head
// only as a table hint list or TOP ... WITH TIES / GROUP BY ... WITH
// ROLLUP|CUBE; table hints: only NOLOCK, READUNCOMMITTED, NOWAIT; no
// OPTION (...). The keyword blacklist (manualForbidden) stays as a first
// layer.

// More manual SQL errors (wrapped in *Error with KindMapping).
var (
	// ErrManualComment: the manual SQL contains -- or /* */.
	ErrManualComment = errors.New("manual SQL must not contain comments (-- or /* */)")
	// ErrManualHint: a table / join hint other than NOLOCK,
	// READUNCOMMITTED, NOWAIT.
	ErrManualHint = errors.New("manual SQL may only use the table hints NOLOCK, READUNCOMMITTED, NOWAIT")
	// ErrManualOption: OPTION (...) query hints.
	ErrManualOption = errors.New("manual SQL must not use OPTION (...)")
)

type mtokKind int

const (
	mtWord   mtokKind = iota // keyword / identifier / number / @var (upper case)
	mtQuoted                 // [identifier] or "identifier"
	mtString                 // '...' or N'...'
	mtSymbol                 // one character
)

type mtok struct {
	kind mtokKind
	up   string // upper-case word or the symbol
}

func isManualWordRune(r rune) bool {
	return r == '_' || r == '@' || r == '#' || r == '$' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
		r >= 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r))
}

// lexManual splits the manual SQL into tokens. Errors: ErrManualComment,
// ErrManualLiteral (unterminated '...', [...], "..."), ErrManualMulti
// (text after ';'), ErrManualForbidden (a character outside literals that
// is not ASCII printable / whitespace nor a Unicode letter, digit or mark;
// the second result names it).
func lexManual(q string) ([]mtok, string, error) {
	var out []mtok
	semi := false
	for i := 0; i < len(q); {
		c := q[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			i++
			continue
		}
		if c == ';' {
			semi = true
			i++
			continue
		}
		if c == '-' && i+1 < len(q) && q[i+1] == '-' || c == '/' && i+1 < len(q) && q[i+1] == '*' {
			return nil, "", ErrManualComment
		}
		if semi {
			return nil, "", ErrManualMulti
		}
		switch {
		case c == '\'' || c == '"' || c == '[':
			j, ok := quotedEnd(q, i)
			if !ok {
				return nil, "", ErrManualLiteral
			}
			k := mtQuoted
			if c == '\'' {
				k = mtString
			}
			out = append(out, mtok{kind: k, up: q[i:j]})
			i = j
		default:
			r, n := utf8.DecodeRuneInString(q[i:])
			switch {
			case r == utf8.RuneError && n <= 1:
				return nil, "invalid UTF-8", ErrManualForbidden
			case isManualWordRune(r):
				j := i
				for j < len(q) {
					r2, n2 := utf8.DecodeRuneInString(q[j:])
					if !isManualWordRune(r2) || r2 == utf8.RuneError && n2 <= 1 {
						break
					}
					j += n2
				}
				w := strings.ToUpper(q[i:j])
				if w == "N" && j < len(q) && q[j] == '\'' { // N'...' unicode literal
					k, ok := quotedEnd(q, j)
					if !ok {
						return nil, "", ErrManualLiteral
					}
					out = append(out, mtok{kind: mtString, up: q[i:k]})
					i = k
					continue
				}
				out = append(out, mtok{kind: mtWord, up: w})
				i = j
			case r < 0x80 && r > ' ' && r < 0x7f:
				out = append(out, mtok{kind: mtSymbol, up: string(c)})
				i++
			default:
				return nil, fmt.Sprintf("U+%04X", r), ErrManualForbidden
			}
		}
	}
	return out, "", nil
}

// quotedEnd returns the index after the literal / quoted identifier that
// starts at q[i] ('...', "...", [...]; a doubled closing character is an
// escape), ok false when it is not terminated.
func quotedEnd(q string, i int) (int, bool) {
	closeCh := q[i]
	if closeCh == '[' {
		closeCh = ']'
	}
	for j := i + 1; j < len(q); j++ {
		if q[j] == closeCh {
			if j+1 < len(q) && q[j+1] == closeCh {
				j++
				continue
			}
			return j + 1, true
		}
	}
	return 0, false
}

// tsqlReserved: the SQL Server reserved keywords (Transact-SQL reference,
// SQL Server 2008 .. 2022). A reserved word can only be an identifier
// when quoted, so an unquoted one is always a keyword.
var tsqlReserved = wordSet(`ADD ALL ALTER AND ANY AS ASC AUTHORIZATION BACKUP BEGIN BETWEEN BREAK BROWSE BULK BY
	CASCADE CASE CHECK CHECKPOINT CLOSE CLUSTERED COALESCE COLLATE COLUMN COMMIT COMPUTE CONSTRAINT CONTAINS
	CONTAINSTABLE CONTINUE CONVERT CREATE CROSS CURRENT CURRENT_DATE CURRENT_TIME CURRENT_TIMESTAMP CURRENT_USER
	CURSOR DATABASE DBCC DEALLOCATE DECLARE DEFAULT DELETE DENY DESC DISK DISTINCT DISTRIBUTED DOUBLE DROP DUMP
	ELSE END ERRLVL ESCAPE EXCEPT EXEC EXECUTE EXISTS EXIT EXTERNAL FETCH FILE FILLFACTOR FOR FOREIGN FREETEXT
	FREETEXTTABLE FROM FULL FUNCTION GOTO GRANT GROUP HAVING HOLDLOCK IDENTITY IDENTITY_INSERT IDENTITYCOL IF IN
	INDEX INNER INSERT INTERSECT INTO IS JOIN KEY KILL LEFT LIKE LINENO LOAD MERGE NATIONAL NOCHECK NONCLUSTERED
	NOT NULL NULLIF OF OFF OFFSETS ON OPEN OPENDATASOURCE OPENQUERY OPENROWSET OPENXML OPTION OR ORDER OUTER OVER
	PERCENT PIVOT PLAN PRECISION PRIMARY PRINT PROC PROCEDURE PUBLIC RAISERROR READ READTEXT RECONFIGURE
	REFERENCES REPLICATION RESTORE RESTRICT RETURN REVERT REVOKE RIGHT ROLLBACK ROWCOUNT ROWGUIDCOL RULE SAVE
	SCHEMA SECURITYAUDIT SELECT SEMANTICKEYPHRASETABLE SEMANTICSIMILARITYDETAILSTABLE SEMANTICSIMILARITYTABLE
	SESSION_USER SET SETUSER SHUTDOWN SOME STATISTICS SYSTEM_USER TABLE TABLESAMPLE TEXTSIZE THEN TO TOP TRAN
	TRANSACTION TRIGGER TRUNCATE TRY_CONVERT TSEQUAL UNION UNIQUE UNPIVOT UPDATE UPDATETEXT USE USER VALUES
	VARYING VIEW WAITFOR WHEN WHERE WHILE WITH WITHIN WRITETEXT`)

// manualAllowedReserved: the reserved words a read-only SELECT uses
// (clauses, operators, CASE, joins, APPLY, PIVOT, built-in functions that
// are reserved words, CAST(... AS DOUBLE PRECISION / CHAR VARYING),
// full-text predicates). WITH is checked separately.
var manualAllowedReserved = wordSet(`ALL AND ANY AS ASC BETWEEN BY CASE COALESCE COLLATE CONTAINS CONTAINSTABLE
	CONVERT CROSS CURRENT_TIMESTAMP CURRENT_USER DESC DISTINCT DOUBLE ELSE END ESCAPE EXCEPT EXISTS FOR FREETEXT
	FREETEXTTABLE FROM FULL GROUP HAVING IN INNER INTERSECT IS JOIN LEFT LIKE NOT NULL NULLIF ON OR ORDER OUTER
	OVER PERCENT PIVOT PRECISION RIGHT SELECT SESSION_USER SOME SYSTEM_USER TABLESAMPLE THEN TOP UNION UNPIVOT
	USER VALUES VARYING WHEN WHERE WITH`)

// manualStmtVerbs: T-SQL statements that start with a word that is NOT
// reserved (so it could also be a column or alias name): rejected at
// parenthesis depth 0 unless it follows AS or is part of a dotted name
// (a new statement cannot start there). Quote such a column: [Send].
var manualStmtVerbs = wordSet(`RECEIVE SEND MOVE GET ENABLE DISABLE THROW CONVERSATION SIGNATURE DIALOG`)

// manualAllowedHints: the only table hints allowed in manual SQL.
var manualAllowedHints = wordSet(`NOLOCK READUNCOMMITTED NOWAIT`)

// manualLockHints: table hints that are not reserved words (HOLDLOCK and
// INDEX are); any of them outside a quoted name is ErrManualHint.
var manualLockHints = wordSet(`TABLOCK TABLOCKX UPDLOCK XLOCK PAGLOCK ROWLOCK SERIALIZABLE REPEATABLEREAD
	READCOMMITTED READCOMMITTEDLOCK READPAST NOEXPAND FORCESEEK FORCESCAN FASTFIRSTROW SNAPSHOT
	SPATIAL_WINDOW_MAX_CELLS KEEPIDENTITY KEEPDEFAULTS IGNORE_CONSTRAINTS IGNORE_TRIGGERS`)

// manualJoinHints: LOOP / HASH / REMOTE directly before JOIN (MERGE is a
// forbidden keyword anyway).
var manualJoinHints = wordSet(`LOOP HASH REMOTE`)

func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

func hintErr(h string) error {
	return manualErr(ErrManualHint, model.ValSQLManualHint, fmt.Sprintf("%s: %s", ErrManualHint, h), h)
}

func forbiddenErr(w string) error {
	return manualErr(ErrManualForbidden, model.ValSQLManualForbidden,
		fmt.Sprintf("%s %s (only one read-only SELECT is allowed)", ErrManualForbidden, w), w)
}

// manualStructure applies the whitelist to the tokens of a manual query
// (see the comment at the top of this file).
func manualStructure(toks []mtok) error {
	if len(toks) == 0 {
		return manualErr(ErrManualEmpty, model.ValSQLManualEmpty, ErrManualEmpty.Error())
	}
	notSelect := func() error {
		return manualErr(ErrManualNotSelect, model.ValSQLManualNotSelect, ErrManualNotSelect.Error())
	}
	multi := func() error { return manualErr(ErrManualMulti, model.ValSQLManualMulti, ErrManualMulti.Error()) }
	word := func(i int) string {
		if i >= 0 && i < len(toks) && toks[i].kind == mtWord {
			return toks[i].up
		}
		return ""
	}
	sym := func(i int) string {
		if i >= 0 && i < len(toks) && toks[i].kind == mtSymbol {
			return toks[i].up
		}
		return ""
	}
	switch word(0) {
	case "SELECT":
	case "WITH": // CTE: WITH name [(cols)] AS (...)
		if len(toks) < 2 || !(toks[1].kind == mtQuoted || word(1) != "" && !tsqlReserved[word(1)]) {
			return notSelect()
		}
	default:
		return notSelect()
	}
	// named: the word at i is an alias after AS or part of a dotted name
	named := func(i int) bool { return word(i-1) == "AS" || sym(i-1) == "." || sym(i+1) == "." }
	depth, selects, cases := 0, 0, 0
	hintStart, hintEnd := -1, -1 // indexes of the '(' and ')' of a WITH (...) hint list
	for i, tk := range toks {
		inHint := i > hintStart && i < hintEnd
		if tk.kind == mtSymbol {
			switch tk.up {
			case "(":
				depth++
			case ")":
				depth--
				if depth < 0 {
					return notSelect()
				}
			}
			if inHint && tk.up != "," {
				return hintErr(tk.up)
			}
			continue
		}
		if tk.kind != mtWord {
			if inHint {
				return hintErr(tk.up)
			}
			continue
		}
		w := tk.up
		if inHint { // inside WITH ( ... ): whitelisted hints only
			if !manualAllowedHints[w] {
				return hintErr(w)
			}
			continue
		}
		switch {
		case manualForbidden[w]:
			return forbiddenErr(w)
		case w == "OPTION":
			return manualErr(ErrManualOption, model.ValSQLManualOption, ErrManualOption.Error())
		case w == "HOLDLOCK" || w == "INDEX":
			return hintErr(w)
		case manualLockHints[w] && !named(i):
			return hintErr(w)
		case manualJoinHints[w] && word(i+1) == "JOIN":
			return hintErr(w)
		case w == "WITH" && i > 0:
			switch {
			case sym(i+1) == "(":
				hintStart, hintEnd = i+1, matchParen(toks, i+1)
				if hintEnd < 0 {
					return notSelect()
				}
			case word(i+1) == "TIES" || word(i+1) == "ROLLUP" || word(i+1) == "CUBE":
			default: // WITH name AS ( ... ): a second statement
				return multi()
			}
			continue
		case tsqlReserved[w] && !manualAllowedReserved[w]:
			return forbiddenErr(w)
		}
		switch w {
		case "CASE":
			cases++
		case "END":
			if cases == 0 { // END CONVERSATION, END (of a block)
				return forbiddenErr(w)
			}
			cases--
		case "SELECT":
			if depth == 0 {
				prev := word(i - 1)
				if prev == "ALL" {
					prev = word(i - 2)
				}
				if prev != "UNION" && prev != "EXCEPT" && prev != "INTERSECT" {
					selects++
				}
			}
		}
		if depth == 0 && manualStmtVerbs[w] && !named(i) {
			return forbiddenErr(w)
		}
	}
	if depth != 0 {
		return notSelect()
	}
	if selects != 1 {
		return multi()
	}
	return nil
}

// matchParen returns the index of the ')' matching the '(' at toks[open].
func matchParen(toks []mtok, open int) int {
	depth := 0
	for i := open; i < len(toks); i++ {
		if toks[i].kind != mtSymbol {
			continue
		}
		switch toks[i].up {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
