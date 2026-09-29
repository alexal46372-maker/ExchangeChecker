package sqlcheck

import (
	"context"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Column is one row of INFORMATION_SCHEMA.COLUMNS for a base table.
type Column struct {
	Table    TableRef `json:"table"`
	Name     string   `json:"name"`
	DataType string   `json:"dataType"`
	// MaxLength is CHARACTER_MAXIMUM_LENGTH (bytes for binary, characters
	// for strings); -1 = (max), 0 = not applicable.
	MaxLength int `json:"maxLength"`
	Ordinal   int `json:"ordinal"`
}

func (c Column) typ() string { return strings.ToLower(c.DataType) }

// IsDateTime reports a date/time column usable as an exchange timestamp.
func (c Column) IsDateTime() bool {
	switch c.typ() {
	case "datetime", "datetime2", "smalldatetime", "date":
		return true
	}
	return false
}

// IsRef16 reports a binary(16) column: a 1C reference (_IDRRef, _FldNNNRRef).
func (c Column) IsRef16() bool { return c.typ() == "binary" && c.MaxLength == 16 }

// IsText reports a bounded string column (groupable, usable as point name).
func (c Column) IsText() bool {
	switch c.typ() {
	case "char", "varchar", "nchar", "nvarchar":
		return c.MaxLength > 0
	}
	return false
}

// IsNumeric reports an exact numeric column (usable as a point code).
func (c Column) IsNumeric() bool {
	switch c.typ() {
	case "int", "bigint", "smallint", "tinyint", "numeric", "decimal":
		return true
	}
	return false
}

// LinkQuery describes the bounded JOIN used to confirm a points<->times link
// by values and to read MAX() of the times columns of linked rows.
type LinkQuery struct {
	Points   TableRef
	KeyCol   string
	Times    TableRef
	LinkCol  string
	DateCols []string
	Limit    int // rows of Times scanned (TOP)
	// Composite reference: when TypeCol is set, only Times rows with
	// TypeCol = 0x08 and RTRefCol = RTRef (4-byte big-endian) are scanned.
	TypeCol  string
	RTRefCol string
	RTRef    int
}

// CompositeRef is a 1C 8.1 composite reference column group:
// RRefCol (_FldNNN_RRRef) = key AND TypeCol (_FldNNN_TYPE) = 0x08 AND
// RTRefCol (_FldNNN_RTRef) = RTRef.
type CompositeRef struct {
	RRefCol  string
	TypeCol  string
	RTRefCol string
	RTRef    int
}

// LinkStats is the result of a LinkQuery. Max values are naive database
// wall clocks (no YearOffset applied); nil = no non-NULL value.
type LinkStats struct {
	Sampled int64 // times rows scanned
	Matched int64 // of them, rows whose link value is a points key
	Points  int64 // distinct points matched
	Max     []*time.Time
}

// SingleQuery describes the bounded aggregate over one table.
type SingleQuery struct {
	Table    TableRef
	PointCol string
	DateCols []string
	Limit    int
}

// SingleStats is the result of a SingleQuery (Max as in LinkStats).
type SingleStats struct {
	Sampled int64
	Points  int64
	Max     []*time.Time
}

// RawPoint is one aggregated row read by a mapping or manual SQL: naive
// database wall clocks, YearOffset not yet applied.
type RawPoint struct {
	Name string
	ID   string
	Send *time.Time
	Recv *time.Time
}

// RawCounter is one row of a counters-mode read (1C _NodeNNN).
type RawCounter struct {
	Key        []byte
	Name       string
	ID         string
	SentNo     int64
	ReceivedNo int64
}

// Store is the database access used by the readers. Every query it runs
// is a read-only SELECT with WITH (NOLOCK) on user tables and catalog views
// (no session-level SET), so Discovery and polling never block 1C writers. SQLStore implements it
// with database/sql + go-mssqldb; tests use in-memory schema fixtures.
type Store interface {
	// Ping opens (or reuses) a session and runs a trivial query.
	Ping(ctx context.Context) error
	// Columns lists the columns of all base tables (INFORMATION_SCHEMA).
	Columns(ctx context.Context) ([]Column, error)
	// RowCounts returns approximate row counts (sys.partitions); best effort.
	RowCounts(ctx context.Context) (map[TableRef]int64, error)
	// SampleColumns returns up to limit rows of the given columns.
	SampleColumns(ctx context.Context, t TableRef, cols []string, limit int) ([][]interface{}, error)
	// LinkStats runs a bounded JOIN between a points and a times table.
	LinkStats(ctx context.Context, q LinkQuery) (LinkStats, error)
	// SingleStats runs a bounded aggregate over one table.
	SingleStats(ctx context.Context, q SingleQuery) (SingleStats, error)
	// Points reads aggregated points by a table mapping (single or linked).
	Points(ctx context.Context, m model.Mapping, limit int) ([]RawPoint, error)
	// SearchText returns up to limit rows (selectCols) of t where any of
	// textCols LIKE pattern (DiscoverByValue).
	SearchText(ctx context.Context, t TableRef, selectCols, textCols []string, pattern string, limit int) ([][]interface{}, error)
	// SearchRef returns up to limit rows (selectCols) of t where any of
	// refCols equals key (DiscoverByValue, linked rows).
	SearchRef(ctx context.Context, t TableRef, selectCols, refCols []string, key []byte, limit int) ([][]interface{}, error)
	// SearchComposite returns up to limit rows (selectCols) of t whose
	// composite reference points to key in table number ref.RTRef.
	SearchComposite(ctx context.Context, t TableRef, selectCols []string, ref CompositeRef, key []byte, limit int) ([][]interface{}, error)
	// Counters reads the message counters of a counters-mode mapping.
	Counters(ctx context.Context, m model.Mapping, limit int) ([]RawCounter, error)
	// ManualPoints runs a validated manual SQL (point, last send, last recv).
	ManualPoints(ctx context.Context, query string, limit int) ([]RawPoint, error)
	// Close releases the connection pool.
	Close() error
}
