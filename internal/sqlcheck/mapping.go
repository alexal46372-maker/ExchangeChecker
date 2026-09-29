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
// the only table with that name, or the one in dbo.
func findTable(tables []*tableInfo, schema, name string) (*tableInfo, error) {
	var hits []*tableInfo
	for _, t := range tables {
		if strings.EqualFold(t.ref.Name, name) && (schema == "" || strings.EqualFold(t.ref.Schema, schema)) {
			hits = append(hits, t)
		}
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("%w: %s", ErrTableNotFound, TableRef{Schema: schema, Name: name})
	case 1:
		return hits[0], nil
	}
	for _, t := range hits {
		if strings.EqualFold(t.ref.Schema, "dbo") {
			return t, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrTableAmbiguous, name)
}

func findCol(t *tableInfo, name, role string) (Column, error) {
	if err := checkIdent(name); err != nil {
		return Column{}, fmt.Errorf("%s column: %w", role, err)
	}
	c, ok := t.find(name)
	if !ok {
		return Column{}, fmt.Errorf("%w: %s column %s.%s", ErrColumnNotFound, role, t.ref, name)
	}
	return c, nil
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
		return merr(ErrNotConfigured)
	}
	tables := groupTables(cols)
	pt, err := findTable(tables, m.Schema, m.Table)
	if err != nil {
		return merr(err)
	}
	for _, pc := range []struct{ name, role string }{{m.PointNameColumn, "point name"}, {m.PointIDColumn, "point code"}} {
		if pc.name == "" {
			continue
		}
		c, err := findCol(pt, pc.name, pc.role)
		if err != nil {
			return merr(err)
		}
		if !c.IsText() && !c.IsNumeric() {
			return merr(fmt.Errorf("%w: %s column %s.%s is %s (want char/varchar/nchar/nvarchar up to 4000 or a number)",
				ErrColumnType, pc.role, pt.ref, c.Name, c.DataType))
		}
	}
	if m.Mode == model.MappingCounters {
		if m.PointKeyColumn != "" {
			if _, err := findCol(pt, m.PointKeyColumn, "point key"); err != nil {
				return merr(err)
			}
		}
		for _, cc := range []struct{ name, role string }{{m.SentNoCol(), "sent counter"}, {m.ReceivedNoCol(), "received counter"}} {
			c, err := findCol(pt, cc.name, cc.role)
			if err != nil {
				return merr(err)
			}
			if !c.IsNumeric() {
				return merr(fmt.Errorf("%w: %s column %s.%s is %s (want a number)", ErrColumnType, cc.role, pt.ref, c.Name, c.DataType))
			}
		}
		return nil
	}
	ts := pt
	if m.Linked() {
		key, err := findCol(pt, m.PointKeyColumn, "point key")
		if err != nil {
			return merr(err)
		}
		if ts, err = findTable(tables, m.TimesSchema, m.TimesTable); err != nil {
			return merr(err)
		}
		link, err := findCol(ts, m.TimesLinkColumn, "link")
		if err != nil {
			return merr(err)
		}
		if !strings.EqualFold(key.DataType, link.DataType) || key.MaxLength != link.MaxLength {
			return merr(fmt.Errorf("%w: %s.%s is %s(%d), %s.%s is %s(%d)", ErrLinkTypeMismatch,
				ts.ref, link.Name, link.DataType, link.MaxLength, pt.ref, key.Name, key.DataType, key.MaxLength))
		}
	}
	n := 0
	for _, tc := range []struct{ name, role string }{{m.LastSendColumn, "last send"}, {m.LastRecvColumn, "last recv"}} {
		if tc.name == "" {
			continue
		}
		c, err := findCol(ts, tc.name, tc.role)
		if err != nil {
			return merr(err)
		}
		if !c.IsDateTime() {
			return merr(fmt.Errorf("%w: %s column %s.%s is %s (want date/datetime/datetime2/smalldatetime)",
				ErrColumnType, tc.role, ts.ref, c.Name, c.DataType))
		}
		n++
	}
	if n == 0 {
		return merr(ErrNoTimestampColumn)
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
		return err
	}
	return CheckMapping(cols, m)
}

// PreviewMapping validates m (table mapping, manual SQL or counters) and
// reads up to limit rows through it, converted like ListPoints (YearOffset,
// UTC+3): what the tray shows before the user saves a manual mapping.
// Counters mode previews point names only (times need two polls).
func PreviewMapping(ctx context.Context, st Store, m model.Mapping, limit int) ([]model.SampleRow, error) {
	if limit <= 0 {
		limit = 20
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
