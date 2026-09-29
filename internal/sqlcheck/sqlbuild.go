package sqlcheck

import (
	"errors"
	"fmt"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// All builders return read-only T-SQL that works on SQL Server 2005+
// (TOP (@p1), COUNT_BIG, INFORMATION_SCHEMA, sys.partitions / sysindexes).
// Identifiers are quoted with QuoteIdent; values (row limit, search
// pattern, reference key) are parameters @p1, @p2. User tables are read
// WITH (NOLOCK); catalog batches start with readUncommitted.

// readUncommitted prefixes catalog batches (metadata views take no table hint).
const readUncommitted = "SET TRANSACTION ISOLATION LEVEL READ UNCOMMITTED;\n"

// noLock is the table hint appended to every user table reference.
const noLock = " WITH (NOLOCK)"

// catalogSQL lists base-table columns; @p1 = 'BASE TABLE'.
const catalogSQL = readUncommitted + `SELECT c.TABLE_SCHEMA, c.TABLE_NAME, c.COLUMN_NAME, c.DATA_TYPE,
	ISNULL(c.CHARACTER_MAXIMUM_LENGTH, 0), c.ORDINAL_POSITION
FROM INFORMATION_SCHEMA.COLUMNS AS c
JOIN INFORMATION_SCHEMA.TABLES AS t
	ON t.TABLE_CATALOG = c.TABLE_CATALOG AND t.TABLE_SCHEMA = c.TABLE_SCHEMA AND t.TABLE_NAME = c.TABLE_NAME
WHERE t.TABLE_TYPE = @p1
ORDER BY c.TABLE_SCHEMA, c.TABLE_NAME, c.ORDINAL_POSITION`

const catalogTableType = "BASE TABLE"

// rowCountsSQL returns approximate row counts of user tables; @p1 = 'U'.
const rowCountsSQL = readUncommitted + `SELECT s.name, o.name, SUM(p.rows)
FROM sys.partitions AS p
JOIN sys.objects AS o ON o.object_id = p.object_id
JOIN sys.schemas AS s ON s.schema_id = o.schema_id
WHERE o.type = @p1 AND p.index_id IN (0, 1)
GROUP BY s.name, o.name`

const rowCountsObjectType = "U"

// rowCountsLegacySQL is the fallback through the sysindexes compatibility
// view (also present on SQL 2005/2008); @p1 = 'U'.
const rowCountsLegacySQL = readUncommitted + `SELECT SCHEMA_NAME(o.uid), o.name, MAX(i.rows)
FROM sysindexes AS i
JOIN sysobjects AS o ON o.id = i.id
WHERE o.xtype = @p1 AND i.indid IN (0, 1)
GROUP BY SCHEMA_NAME(o.uid), o.name`

func quoteCols(cols []string) ([]string, error) {
	out := make([]string, len(cols))
	for i, c := range cols {
		q, err := quoteChecked(c)
		if err != nil {
			return nil, err
		}
		out[i] = q
	}
	return out, nil
}

// buildSampleSQL: SELECT TOP (@p1) [c1], [c2] FROM [s].[t]
func buildSampleSQL(t TableRef, cols []string) (string, error) {
	if len(cols) == 0 {
		return "", errors.New("sample: no columns")
	}
	tbl, err := t.SQL()
	if err != nil {
		return "", err
	}
	qc, err := quoteCols(cols)
	if err != nil {
		return "", err
	}
	return "SELECT TOP (@p1) " + strings.Join(qc, ", ") + " FROM " + tbl + noLock, nil
}

// buildSearchSQL: SELECT TOP (@p1) sel FROM t WITH (NOLOCK)
// WHERE [c1] <op> @p2 OR [c2] <op> @p2; op is "LIKE" or "=".
func buildSearchSQL(t TableRef, selectCols, whereCols []string, op string) (string, error) {
	if len(selectCols) == 0 || len(whereCols) == 0 {
		return "", errors.New("search: no columns")
	}
	if op != "LIKE" && op != "=" {
		return "", fmt.Errorf("search: bad operator %q", op)
	}
	tbl, err := t.SQL()
	if err != nil {
		return "", err
	}
	sel, err := quoteCols(selectCols)
	if err != nil {
		return "", err
	}
	wc, err := quoteCols(whereCols)
	if err != nil {
		return "", err
	}
	conds := make([]string, len(wc))
	for i, c := range wc {
		conds[i] = c + " " + op + " @p2"
	}
	return "SELECT TOP (@p1) " + strings.Join(sel, ", ") + " FROM " + tbl + noLock +
		" WHERE " + strings.Join(conds, " OR "), nil
}

// LikeContains returns a LIKE pattern matching s as a literal substring:
// '[', '%' and '_' are escaped with brackets (no ESCAPE clause needed).
func LikeContains(s string) string {
	r := strings.NewReplacer("[", "[[]", "%", "[%]", "_", "[_]")
	return "%" + r.Replace(s) + "%"
}

// buildLinkStatsSQL confirms a link by values on a bounded sample of the
// times table and reads MAX() of its datetime columns for linked rows.
func buildLinkStatsSQL(q LinkQuery) (string, error) {
	if len(q.DateCols) == 0 {
		return "", errors.New("link stats: no datetime columns")
	}
	pt, err := q.Points.SQL()
	if err != nil {
		return "", err
	}
	tt, err := q.Times.SQL()
	if err != nil {
		return "", err
	}
	key, err := quoteChecked(q.KeyCol)
	if err != nil {
		return "", err
	}
	link, err := quoteChecked(q.LinkCol)
	if err != nil {
		return "", err
	}
	dc, err := quoteCols(q.DateCols)
	if err != nil {
		return "", err
	}
	inner := []string{link + " AS l"}
	aggs := []string{"COUNT_BIG(*)", "COUNT_BIG(n.k)", "COUNT_BIG(DISTINCT n.k)"}
	for i, c := range dc {
		inner = append(inner, fmt.Sprintf("%s AS c%d", c, i))
		aggs = append(aggs, fmt.Sprintf("MAX(CASE WHEN n.k IS NOT NULL THEN t.c%d END)", i))
	}
	return "SELECT " + strings.Join(aggs, ", ") +
		"\nFROM (SELECT TOP (@p1) " + strings.Join(inner, ", ") + " FROM " + tt + noLock + ") AS t" +
		"\nLEFT JOIN (SELECT " + key + " AS k FROM " + pt + noLock + ") AS n ON n.k = t.l", nil
}

// buildSingleStatsSQL reads row/point counts and MAX() of datetime columns
// on a bounded sample of one table.
func buildSingleStatsSQL(q SingleQuery) (string, error) {
	if len(q.DateCols) == 0 {
		return "", errors.New("single stats: no datetime columns")
	}
	tbl, err := q.Table.SQL()
	if err != nil {
		return "", err
	}
	pc, err := quoteChecked(q.PointCol)
	if err != nil {
		return "", err
	}
	dc, err := quoteCols(q.DateCols)
	if err != nil {
		return "", err
	}
	inner := []string{pc + " AS p"}
	aggs := []string{"COUNT_BIG(*)", "COUNT_BIG(DISTINCT t.p)"}
	for i, c := range dc {
		inner = append(inner, fmt.Sprintf("%s AS c%d", c, i))
		aggs = append(aggs, fmt.Sprintf("MAX(t.c%d)", i))
	}
	return "SELECT " + strings.Join(aggs, ", ") +
		"\nFROM (SELECT TOP (@p1) " + strings.Join(inner, ", ") + " FROM " + tbl + noLock + ") AS t", nil
}

// colOrNull quotes col as "[col] AS alias" or returns "CAST(NULL AS typ) AS alias".
func colOrNull(col, typ, alias string) (string, error) {
	if strings.TrimSpace(col) == "" {
		return "CAST(NULL AS " + typ + ") AS " + alias, nil
	}
	q, err := quoteChecked(col)
	if err != nil {
		return "", err
	}
	return q + " AS " + alias, nil
}

// buildPointsSQL builds the ListPoints query for a table mapping. Result
// columns: point name, point id/code, MAX(last send), MAX(last recv).
//
// single: SELECT TOP (@p1) t.n, t.i, MAX(t.s), MAX(t.r)
//
//	FROM (SELECT [name] AS n, [id] AS i, [send] AS s, [recv] AS r FROM [t]) AS t
//	GROUP BY t.n, t.i ORDER BY t.n, t.i
//
// linked: points p INNER (or LEFT, KeepUnlinkedPoints) JOIN times t ON t.l = p.k,
// grouped by the point key.
func buildPointsSQL(m model.Mapping) (string, error) {
	if strings.TrimSpace(m.Table) == "" {
		return "", errors.New("mapping: table is not set")
	}
	if err := m.Validate(); err != nil {
		return "", err
	}
	pt, err := TableRef{Schema: m.Schema, Name: m.Table}.SQL()
	if err != nil {
		return "", err
	}
	n, err := colOrNull(m.PointNameColumn, "nvarchar(1)", "n")
	if err != nil {
		return "", err
	}
	i, err := colOrNull(m.PointIDColumn, "nvarchar(1)", "i")
	if err != nil {
		return "", err
	}
	s, err := colOrNull(m.LastSendColumn, "datetime", "s")
	if err != nil {
		return "", err
	}
	r, err := colOrNull(m.LastRecvColumn, "datetime", "r")
	if err != nil {
		return "", err
	}
	if !m.Linked() {
		return "SELECT TOP (@p1) t.n, t.i, MAX(t.s), MAX(t.r)" +
			"\nFROM (SELECT " + n + ", " + i + ", " + s + ", " + r + " FROM " + pt + noLock + ") AS t" +
			"\nGROUP BY t.n, t.i\nORDER BY t.n, t.i", nil
	}
	tt, err := TableRef{Schema: m.TimesSchema, Name: m.TimesTable}.SQL()
	if err != nil {
		return "", err
	}
	k, err := quoteChecked(m.PointKeyColumn)
	if err != nil {
		return "", err
	}
	l, err := quoteChecked(m.TimesLinkColumn)
	if err != nil {
		return "", err
	}
	join := "INNER JOIN"
	if m.KeepUnlinkedPoints {
		join = "LEFT JOIN"
	}
	return "SELECT TOP (@p1) p.n, p.i, MAX(t.s), MAX(t.r)" +
		"\nFROM (SELECT " + k + " AS k, " + n + ", " + i + " FROM " + pt + noLock + ") AS p" +
		"\n" + join + " (SELECT " + l + " AS l, " + s + ", " + r + " FROM " + tt + noLock + ") AS t ON t.l = p.k" +
		"\nGROUP BY p.k, p.n, p.i\nORDER BY p.n, p.i", nil
}

// buildCountersSQL reads the exchange-plan counters (counters mode):
//
//	SELECT TOP (@p1) [key] AS k, [name] AS n, [code] AS i, [sent] AS s, [recv] AS r
//	FROM [t] WITH (NOLOCK) ORDER BY n, i
func buildCountersSQL(m model.Mapping) (string, error) {
	if m.Mode != model.MappingCounters {
		return "", errors.New("mapping: not in counters mode")
	}
	if err := m.Validate(); err != nil {
		return "", err
	}
	tbl, err := TableRef{Schema: m.Schema, Name: m.Table}.SQL()
	if err != nil {
		return "", err
	}
	k, err := colOrNull(m.PointKeyColumn, "binary(16)", "k")
	if err != nil {
		return "", err
	}
	n, err := colOrNull(m.PointNameColumn, "nvarchar(1)", "n")
	if err != nil {
		return "", err
	}
	i, err := colOrNull(m.PointIDColumn, "nvarchar(1)", "i")
	if err != nil {
		return "", err
	}
	sc, err := quoteChecked(m.SentNoCol())
	if err != nil {
		return "", err
	}
	rc, err := quoteChecked(m.ReceivedNoCol())
	if err != nil {
		return "", err
	}
	return "SELECT TOP (@p1) " + k + ", " + n + ", " + i + ", " + sc + " AS s, " + rc + " AS r" +
		" FROM " + tbl + noLock + " ORDER BY n, i", nil
}
