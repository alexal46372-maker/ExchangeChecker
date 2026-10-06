package model

// ManualSQLTemplate is an example manual query for the setup UI (phase 6,
// G4, CHK-SQL-04/05), JSON for IPC. SQL follows the manual SQL contract:
// one read-only SELECT returning point name, last send, last recv.
type ManualSQLTemplate struct {
	// ID is a stable identifier ("1c_node_linked", ...).
	ID          string        `json:"id"`
	Title       LocalizedText `json:"title"`
	Description LocalizedText `json:"description"`
	SQL         string        `json:"sql"`
}

// manualSQLTemplates are written for SQL Server 2008 (compatibility level
// 100): TOP / GROUP BY / MAX, ROW_NUMBER() OVER (PARTITION BY ... ORDER BY
// ...), CASE and binary literals only; no OFFSET/FETCH, TRY_CONVERT, IIF,
// CONCAT, FORMAT, LAG/LEAD or windowed aggregates with ORDER BY
// (sqlcheck.Compat2008Violations checks them). Table and column names are
// the 1C storage names of the example databases; replace the numbers with
// those of the actual configuration.
var manualSQLTemplates = []ManualSQLTemplate{
	{
		ID:    "1c_node_linked",
		Title: LocalizedText{"План обмена + регистр (простая ссылка)", "Exchange plan + register (simple reference)"},
		Description: LocalizedText{"План обмена _NodeNNN и регистр с простой ссылкой _FldNNNRRef",
			"exchange plan _NodeNNN + register with a simple reference _FldNNNRRef"},
		SQL: `SELECT n.[_Description], MAX(r.[_Fld458]), MAX(r.[_Fld459])
FROM [dbo].[_Node123] AS n WITH (NOLOCK)
JOIN [dbo].[_InfoRg456] AS r WITH (NOLOCK) ON r.[_Fld457RRef] = n.[_IDRRef]
GROUP BY n.[_IDRRef], n.[_Description]`,
	},
	{
		ID:    "1c_node_composite",
		Title: LocalizedText{"План обмена + регистр (составная ссылка 1С 8.1)", "Exchange plan + register (1C 8.1 composite reference)"},
		Description: LocalizedText{"План обмена _Node124 и регистр с составной ссылкой 1С 8.1 (_TYPE = 0x08, _RTRef = номер таблицы 124 = 0x0000007C)",
			"exchange plan _Node124 + register with a 1C 8.1 composite reference (_TYPE = 0x08, _RTRef = table number 124 = 0x0000007C)"},
		SQL: `SELECT n.[_Description], MAX(r.[_Fld602]), MAX(r.[_Fld603])
FROM [dbo].[_Node124] AS n WITH (NOLOCK)
JOIN [dbo].[_InfoRg600] AS r WITH (NOLOCK)
  ON r.[_Fld601_TYPE] = 0x08 AND r.[_Fld601_RTRef] = 0x0000007C AND r.[_Fld601_RRRef] = n.[_IDRRef]
GROUP BY n.[_IDRRef], n.[_Description]`,
	},
	{
		ID:    "1c_latest_row",
		Title: LocalizedText{"Последняя строка регистра по точке", "Latest register row per point"},
		Description: LocalizedText{"Последняя строка регистра по каждой точке через ROW_NUMBER() (SQL 2005+; вместо LAG/OFFSET)",
			"latest register row per point via ROW_NUMBER() (SQL 2005+; instead of LAG/OFFSET)"},
		SQL: `WITH x AS (
  SELECT n.[_Description] AS point, r.[_Fld458] AS sent, r.[_Fld459] AS recv,
         ROW_NUMBER() OVER (PARTITION BY r.[_Fld457RRef] ORDER BY r.[_Fld459] DESC) AS rn
  FROM [dbo].[_InfoRg456] AS r WITH (NOLOCK)
  JOIN [dbo].[_Node123] AS n WITH (NOLOCK) ON n.[_IDRRef] = r.[_Fld457RRef]
)
SELECT point, sent, recv FROM x WHERE rn = 1`,
	},
	{
		ID:    "1c_directory_dates",
		Title: LocalizedText{"Даты обмена в строке справочника", "Exchange dates in the directory row"},
		Description: LocalizedText{"Даты обмена в самой строке справочника; CASE вместо IIF (SQL 2012+); пустые даты 1С служба тоже превращает в NULL",
			"exchange dates in the directory row itself; CASE instead of IIF (SQL 2012+); the service also turns 1C empty dates into NULL"},
		SQL: `SELECT TOP (1000) d.[_Description],
  CASE WHEN d.[_Fld56] > '17530101' THEN d.[_Fld56] END,
  CASE WHEN d.[_Fld57] > '17530101' THEN d.[_Fld57] END
FROM [dbo].[_Reference55] AS d WITH (NOLOCK)
ORDER BY d.[_Description]`,
	},
}

// SQLManualTemplates returns a copy of the manual SQL templates.
func SQLManualTemplates() []ManualSQLTemplate {
	return append([]ManualSQLTemplate(nil), manualSQLTemplates...)
}
