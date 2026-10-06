package sqlcheck

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Manual mapping (CHK-SQL-04) is a first-class path: the tray lets the user
// type table/column names (or a manual SQL), the service validates them
// against the catalog (CheckMapping / ValidateMapping) and shows a preview
// (PreviewMapping) before the mapping is saved with Mode "manual". Polling
// then uses exactly the same code as a confirmed Discovery candidate.

// Mapping check errors (wrapped in *Error with KindMapping; the message
// names the table/column).
var (
	ErrTableNotFound     = errors.New("table not found")
	ErrTableAmbiguous    = errors.New("table name is ambiguous, set the schema")
	ErrColumnNotFound    = errors.New("column not found")
	ErrColumnType        = errors.New("column has an unsuitable type")
	ErrLinkTypeMismatch  = errors.New("link column and key column have different types")
	ErrNoTimestampColumn = errors.New("mapping has no timestamp column")
)

// findTable resolves a mapping table name in the catalog. Empty schema:
// the only table with that name, or the one in dbo. field / schemaField
// name the mapping fields for the typed error ("table" / "schema" or
// "timesTable" / "timesSchema").
func findTable(tables []*tableInfo, schema, name, field, schemaField string) (*tableInfo, error) {
	var hits []*tableInfo
	for _, t := range tables {
		if strings.EqualFold(t.ref.Name, name) && (schema == "" || strings.EqualFold(t.ref.Schema, schema)) {
			hits = append(hits, t)
		}
	}
	switch len(hits) {
	case 0:
		ref := TableRef{Schema: schema, Name: name}.String()
		return nil, newFieldError(ErrTableNotFound, field, model.ValSQLTableNotFound,
			fmt.Sprintf("%s: %s", ErrTableNotFound, ref), ref)
	case 1:
		return hits[0], nil
	}
	for _, t := range hits {
		if strings.EqualFold(t.ref.Schema, "dbo") {
			return t, nil
		}
	}
	return nil, newFieldError(ErrTableAmbiguous, schemaField, model.ValSQLTableAmbiguous,
		fmt.Sprintf("%s: %q", ErrTableAmbiguous, name), name)
}

// findCol resolves column name of the mapping field (role: English name
// for the message).
func findCol(t *tableInfo, name, field, role string) (Column, error) {
	if err := checkIdent(name); err != nil {
		return Column{}, identErr(field, role, name, err)
	}
	c, ok := t.find(name)
	if !ok {
		return Column{}, newFieldError(ErrColumnNotFound, field, model.ValSQLColumnNotFound,
			fmt.Sprintf("%s: %s column %s.%s", ErrColumnNotFound, role, t.ref, name), t.ref.String(), name)
	}
	return c, nil
}

// typeErr is ErrColumnType of column c of t for the mapping field.
func typeErr(field, role string, t *tableInfo, c Column, typ, want, wantText string) error {
	return newFieldError(ErrColumnType, field, model.ValSQLColumnType,
		fmt.Sprintf("%s: %s column %s.%s is %s (want %s)", ErrColumnType, role, t.ref, c.Name, typ, wantText),
		t.ref.String(), c.Name, typ, want)
}

// CheckMapping validates a mapping (typically edited by hand) against the
// catalog: tables and columns exist, point name/code columns are bounded
// strings or numbers, timestamp columns are date/time, key and link columns
// have the same type; a manual SQL passes ValidateManualSQL.
func CheckMapping(cols []Column, m model.Mapping) error {
	merr := func(err error) error { return &Error{Kind: KindMapping, Op: "check mapping", Err: err} }
	if err := m.Validate(); err != nil {
		return merr(err)
	}
	if strings.TrimSpace(m.ManualSQL) != "" {
		if err := ValidateManualSQL(m.ManualSQL); err != nil {
			return merr(err)
		}
		if strings.TrimSpace(m.Table) == "" {
			return nil
		}
	}
	if strings.TrimSpace(m.Table) == "" {
		return merr(newFieldError(ErrNotConfigured, "table", model.ValRequired, ErrNotConfigured.Error()))
	}
	tables := groupTables(cols)
	pt, err := findTable(tables, m.Schema, m.Table, "table", "schema")
	if err != nil {
		return merr(err)
	}
	for _, pc := range []struct{ name, field, role string }{
		{m.PointNameColumn, "pointNameColumn", "point name"}, {m.PointIDColumn, "pointIdColumn", "point code"}} {
		if pc.name == "" {
			continue
		}
		c, err := findCol(pt, pc.name, pc.field, pc.role)
		if err != nil {
			return merr(err)
		}
		if !c.IsText() && !c.IsNumeric() {
			return merr(typeErr(pc.field, pc.role, pt, c, c.DataType, model.SQLTypesPointColumn,
				"char/varchar/nchar/nvarchar up to 4000 or a number"))
		}
	}
	if m.Mode == model.MappingCounters {
		if m.PointKeyColumn != "" {
			if _, err := findCol(pt, m.PointKeyColumn, "pointKeyColumn", "point key"); err != nil {
				return merr(err)
			}
		}
		for _, cc := range []struct{ name, field, role string }{
			{m.SentNoCol(), "sentNoColumn", "sent counter"}, {m.ReceivedNoCol(), "receivedNoColumn", "received counter"}} {
			c, err := findCol(pt, cc.name, cc.field, cc.role)
			if err != nil {
				return merr(err)
			}
			if !c.IsNumeric() {
				return merr(typeErr(cc.field, cc.role, pt, c, c.DataType, model.SQLTypesNumber, "a number"))
			}
		}
		return nil
	}
	ts := pt
	if m.Linked() {
		key, err := findCol(pt, m.PointKeyColumn, "pointKeyColumn", "point key")
		if err != nil {
			return merr(err)
		}
		if ts, err = findTable(tables, m.TimesSchema, m.TimesTable, "timesTable", "timesSchema"); err != nil {
			return merr(err)
		}
		link, err := findCol(ts, m.TimesLinkColumn, "timesLinkColumn", "link")
		if err != nil {
			return merr(err)
		}
		if !strings.EqualFold(key.DataType, link.DataType) || key.MaxLength != link.MaxLength {
			lt := fmt.Sprintf("%s(%d)", link.DataType, link.MaxLength)
			kt := fmt.Sprintf("%s(%d)", key.DataType, key.MaxLength)
			return merr(newFieldError(ErrLinkTypeMismatch, "timesLinkColumn", model.ValSQLLinkTypeMismatch,
				fmt.Sprintf("%s: %s.%s is %s, %s.%s is %s", ErrLinkTypeMismatch, ts.ref, link.Name, lt, pt.ref, key.Name, kt),
				ts.ref.String()+"."+link.Name, lt, pt.ref.String()+"."+key.Name, kt))
		}
		if m.Composite() {
			// 1C composite reference: _TYPE binary(1), _RTRef binary(4)
			for _, cc := range []struct {
				name, field, role, want string
				size                    int
			}{{m.TimesLinkTypeColumn, "timesLinkTypeColumn", "link type (_TYPE)", model.SQLTypesBinary1, 1},
				{m.TimesLinkRTRefColumn, "timesLinkRTRefColumn", "link table number (_RTRef)", model.SQLTypesBinary4, 4}} {
				c, err := findCol(ts, cc.name, cc.field, cc.role)
				if err != nil {
					return merr(err)
				}
				if c.typ() != "binary" || c.MaxLength != cc.size {
					return merr(typeErr(cc.field, cc.role, ts, c, fmt.Sprintf("%s(%d)", c.DataType, c.MaxLength), cc.want, cc.want))
				}
			}
		}
	}
	n := 0
	for _, tc := range []struct{ name, field, role string }{
		{m.LastSendColumn, "lastSendColumn", "last send"}, {m.LastRecvColumn, "lastRecvColumn", "last recv"}} {
		if tc.name == "" {
			continue
		}
		c, err := findCol(ts, tc.name, tc.field, tc.role)
		if err != nil {
			return merr(err)
		}
		if !c.IsDateTime() {
			return merr(typeErr(tc.field, tc.role, ts, c, c.DataType, model.SQLTypesDateTime, model.SQLTypesDateTime))
		}
		n++
	}
	if n == 0 {
		return merr(newFieldError(ErrNoTimestampColumn, "lastSendColumn", model.ValSQLNoTimestamp, ErrNoTimestampColumn.Error()))
	}
	return nil
}

// ValidateMapping loads the catalog through st and runs CheckMapping.
func ValidateMapping(ctx context.Context, st Store, m model.Mapping) error {
	if strings.TrimSpace(m.Table) == "" && strings.TrimSpace(m.ManualSQL) != "" {
		return CheckMapping(nil, m)
	}
	cols, err := st.Columns(ctx)
	if err != nil {
		return ctxErr("check mapping", err)
	}
	return CheckMapping(cols, m)
}

// PreviewMapping validates m (table mapping, manual SQL or counters) and
// reads up to limit rows through it, converted like ListPoints (YearOffset,
// UTC+3): what the tray shows before the user saves a manual mapping.
// Counters mode previews point names only (times need two polls).
// At most model.MaxSampleRows rows are read and every point name is cut to
// model.MaxSampleCellRunes characters (SampleRow.PointTruncated; phase 6,
// G1/G4: the rows go to the tray over IPC).
func PreviewMapping(ctx context.Context, st Store, m model.Mapping, limit int) ([]model.SampleRow, error) {
	rows, err := previewMapping(ctx, st, m, limit)
	if err != nil {
		return nil, ctxErr("preview mapping", err)
	}
	model.TruncateSampleRows(rows)
	return rows, nil
}

func previewMapping(ctx context.Context, st Store, m model.Mapping, limit int) ([]model.SampleRow, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > model.MaxSampleRows {
		limit = model.MaxSampleRows
	}
	if err := ValidateMapping(ctx, st, m); err != nil {
		return nil, err
	}
	if m.Mode == model.MappingCounters {
		raw, err := st.Counters(ctx, m, limit)
		if err != nil {
			return nil, err
		}
		return counterSampleRows(raw), nil
	}
	var raw []RawPoint
	var err error
	if strings.TrimSpace(m.ManualSQL) != "" {
		raw, err = st.ManualPoints(ctx, m.ManualSQL, limit)
	} else {
		raw, err = st.Points(ctx, m, limit)
	}
	if err != nil {
		return nil, err
	}
	return toSampleRows(raw, m.YearOffset), nil
}

// usesManualSQL reports whether polling uses the manual SQL of m.
func usesManualSQL(m model.Mapping) bool { return strings.TrimSpace(m.ManualSQL) != "" }
