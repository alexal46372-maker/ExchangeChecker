package sqlcheck

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// SQLStore implements Store on database/sql (go-mssqldb connector).
type SQLStore struct {
	db             *sql.DB
	connectTimeout time.Duration
	queryTimeout   time.Duration
}

// NewSQLStore wraps an open *sql.DB. Zero timeouts mean the model defaults.
func NewSQLStore(db *sql.DB, connectTimeout, queryTimeout time.Duration) *SQLStore {
	if connectTimeout <= 0 {
		connectTimeout = model.DefaultConnectTimeoutSec * time.Second
	}
	if queryTimeout <= 0 {
		queryTimeout = model.DefaultQueryTimeoutSec * time.Second
	}
	return &SQLStore{db: db, connectTimeout: connectTimeout, queryTimeout: queryTimeout}
}

// DB returns the underlying pool (tests and fixtures only).
func (s *SQLStore) DB() *sql.DB { return s.db }

// Close implements Store.
func (s *SQLStore) Close() error { return s.db.Close() }

func (s *SQLStore) qctx(ctx context.Context) (context.Context, context.CancelFunc) {
	// A new session may be opened by the pool inside any query.
	return context.WithTimeout(ctx, s.queryTimeout+s.connectTimeout)
}

// Ping implements Store: dial + TLS + login + "select 1" within
// 2 x connect timeout.
func (s *SQLStore) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*s.connectTimeout)
	defer cancel()
	return classify("connect", s.db.PingContext(ctx), true)
}

// Columns implements Store.
func (s *SQLStore) Columns(ctx context.Context) ([]Column, error) {
	ctx, cancel := s.qctx(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, catalogSQL, catalogTableType)
	if err != nil {
		return nil, classify("catalog", err, false)
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		var c Column
		var maxLen, ord int64
		if err := rows.Scan(&c.Table.Schema, &c.Table.Name, &c.Name, &c.DataType, &maxLen, &ord); err != nil {
			return nil, classify("catalog", err, false)
		}
		c.MaxLength, c.Ordinal = int(maxLen), int(ord)
		out = append(out, c)
	}
	return out, classify("catalog", rows.Err(), false)
}

// RowCounts implements Store: sys.partitions, falling back to the
// sysindexes compatibility view (both exist on SQL 2005/2008).
func (s *SQLStore) RowCounts(ctx context.Context) (map[TableRef]int64, error) {
	out, err := s.rowCounts(ctx, rowCountsSQL)
	if err != nil && !isCtxErr(err) {
		out, err = s.rowCounts(ctx, rowCountsLegacySQL)
	}
	return out, err
}

func (s *SQLStore) rowCounts(ctx context.Context, query string) (map[TableRef]int64, error) {
	ctx, cancel := s.qctx(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, query, rowCountsObjectType)
	if err != nil {
		return nil, classify("row counts", err, false)
	}
	defer rows.Close()
	out := map[TableRef]int64{}
	for rows.Next() {
		var t TableRef
		var n sql.NullInt64
		if err := rows.Scan(&t.Schema, &t.Name, &n); err != nil {
			return nil, classify("row counts", err, false)
		}
		out[t] = n.Int64
	}
	return out, classify("row counts", rows.Err(), false)
}

// SampleColumns implements Store.
func (s *SQLStore) SampleColumns(ctx context.Context, t TableRef, cols []string, limit int) ([][]interface{}, error) {
	q, err := buildSampleSQL(t, cols)
	if err != nil {
		return nil, &Error{Kind: KindMapping, Op: "sample", Err: err}
	}
	return s.valueRows(ctx, "sample "+t.String(), q, len(cols), limit)
}

// SearchText implements Store.
func (s *SQLStore) SearchText(ctx context.Context, t TableRef, selectCols, textCols []string, pattern string, limit int) ([][]interface{}, error) {
	q, err := buildSearchSQL(t, selectCols, textCols, "LIKE")
	if err != nil {
		return nil, &Error{Kind: KindMapping, Op: "search text", Err: err}
	}
	return s.valueRows(ctx, "search text "+t.String(), q, len(selectCols), limit, pattern)
}

// SearchRef implements Store.
func (s *SQLStore) SearchRef(ctx context.Context, t TableRef, selectCols, refCols []string, key []byte, limit int) ([][]interface{}, error) {
	q, err := buildSearchSQL(t, selectCols, refCols, "=")
	if err != nil {
		return nil, &Error{Kind: KindMapping, Op: "search ref", Err: err}
	}
	return s.valueRows(ctx, "search ref "+t.String(), q, len(selectCols), limit, key)
}

// SearchComposite implements Store.
func (s *SQLStore) SearchComposite(ctx context.Context, t TableRef, selectCols []string, ref CompositeRef, key []byte, limit int) ([][]interface{}, error) {
	q, err := buildCompositeSearchSQL(t, selectCols, ref)
	if err != nil {
		return nil, &Error{Kind: KindMapping, Op: "search composite ref", Err: err}
	}
	args := append(compositeArgs(ref.RTRef), key)
	return s.valueRows(ctx, "search composite ref "+t.String(), q, len(selectCols), limit, args...)
}

// valueRows runs q with @p1 = limit (+ extra parameters) and scans ncols
// driver values per row.
func (s *SQLStore) valueRows(ctx context.Context, op, q string, ncols, limit int, extra ...interface{}) ([][]interface{}, error) {
	ctx, cancel := s.qctx(ctx)
	defer cancel()
	args := append([]interface{}{limit}, extra...)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, classify(op, err, false)
	}
	defer rows.Close()
	var out [][]interface{}
	for rows.Next() {
		vals := make([]interface{}, ncols)
		ptrs := make([]interface{}, ncols)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, classify(op, err, false)
		}
		out = append(out, vals)
	}
	return out, classify(op, rows.Err(), false)
}

func nullTimes(n int) ([]sql.NullTime, []interface{}) {
	nt := make([]sql.NullTime, n)
	ptrs := make([]interface{}, n)
	for i := range nt {
		ptrs[i] = &nt[i]
	}
	return nt, ptrs
}

func timePtrs(nt []sql.NullTime) []*time.Time {
	out := make([]*time.Time, len(nt))
	for i, v := range nt {
		if v.Valid {
			t := v.Time
			out[i] = &t
		}
	}
	return out
}

// LinkStats implements Store.
func (s *SQLStore) LinkStats(ctx context.Context, q LinkQuery) (LinkStats, error) {
	var st LinkStats
	query, err := buildLinkStatsSQL(q)
	if err != nil {
		return st, &Error{Kind: KindMapping, Op: "link stats", Err: err}
	}
	ctx, cancel := s.qctx(ctx)
	defer cancel()
	nt, ptrs := nullTimes(len(q.DateCols))
	dest := append([]interface{}{&st.Sampled, &st.Matched, &st.Points}, ptrs...)
	if err := s.db.QueryRowContext(ctx, query, linkStatsArgs(q)...).Scan(dest...); err != nil {
		return st, classify("link stats "+q.Times.String(), err, false)
	}
	st.Max = timePtrs(nt)
	return st, nil
}

// SingleStats implements Store.
func (s *SQLStore) SingleStats(ctx context.Context, q SingleQuery) (SingleStats, error) {
	var st SingleStats
	query, err := buildSingleStatsSQL(q)
	if err != nil {
		return st, &Error{Kind: KindMapping, Op: "single stats", Err: err}
	}
	ctx, cancel := s.qctx(ctx)
	defer cancel()
	nt, ptrs := nullTimes(len(q.DateCols))
	dest := append([]interface{}{&st.Sampled, &st.Points}, ptrs...)
	if err := s.db.QueryRowContext(ctx, query, q.Limit).Scan(dest...); err != nil {
		return st, classify("single stats "+q.Table.String(), err, false)
	}
	st.Max = timePtrs(nt)
	return st, nil
}

// Points implements Store.
func (s *SQLStore) Points(ctx context.Context, m model.Mapping, limit int) ([]RawPoint, error) {
	query, err := buildPointsSQL(m)
	if err != nil {
		return nil, &Error{Kind: KindMapping, Op: "points", Err: err}
	}
	ctx, cancel := s.qctx(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, query, pointsArgs(m, limit)...)
	if err != nil {
		return nil, classify("points", err, false)
	}
	defer rows.Close()
	var out []RawPoint
	for rows.Next() {
		var name, id interface{}
		var send, recv sql.NullTime
		if err := rows.Scan(&name, &id, &send, &recv); err != nil {
			return nil, classify("points", err, false)
		}
		tp := timePtrs([]sql.NullTime{send, recv})
		out = append(out, RawPoint{Name: valueString(name), ID: valueString(id), Send: tp[0], Recv: tp[1]})
	}
	return out, classify("points", rows.Err(), false)
}

// Counters implements Store.
func (s *SQLStore) Counters(ctx context.Context, m model.Mapping, limit int) ([]RawCounter, error) {
	query, err := buildCountersSQL(m)
	if err != nil {
		return nil, &Error{Kind: KindMapping, Op: "counters", Err: err}
	}
	rows, err := s.valueRows(ctx, "counters", query, 5, limit)
	if err != nil {
		return nil, err
	}
	out := make([]RawCounter, 0, len(rows))
	for _, r := range rows {
		c := RawCounter{Name: valueString(r[1]), ID: valueString(r[2])}
		if b, ok := r[0].([]byte); ok {
			c.Key = b
		}
		if c.SentNo, err = valueInt(r[3]); err != nil {
			return nil, &Error{Kind: KindMapping, Op: "counters", Err: fmt.Errorf("%s: %w", m.SentNoCol(), err)}
		}
		if c.ReceivedNo, err = valueInt(r[4]); err != nil {
			return nil, &Error{Kind: KindMapping, Op: "counters", Err: fmt.Errorf("%s: %w", m.ReceivedNoCol(), err)}
		}
		out = append(out, c)
	}
	return out, nil
}

// ManualPoints implements Store. The query runs in a READ UNCOMMITTED
// transaction that is always rolled back.
func (s *SQLStore) ManualPoints(ctx context.Context, query string, limit int) (out []RawPoint, err error) {
	if err := ValidateManualSQL(query); err != nil {
		return nil, &Error{Kind: KindMapping, Op: "manual sql", Err: err}
	}
	ctx, cancel := s.qctx(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadUncommitted})
	if err != nil {
		return nil, classify("manual sql", err, false)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, classify("manual sql", err, false)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, classify("manual sql", err, false)
	}
	if len(cols) != 3 {
		return nil, &Error{Kind: KindMapping, Op: "manual sql",
			Err: fmt.Errorf("%w: got %d (%s)", ErrManualColumns, len(cols), strings.Join(cols, ", "))}
	}
	for rows.Next() {
		if limit > 0 && len(out) >= limit {
			break
		}
		var name, send, recv interface{}
		if err := rows.Scan(&name, &send, &recv); err != nil {
			return nil, classify("manual sql", err, false)
		}
		p := RawPoint{Name: valueString(name)}
		if p.Send, err = valueTime(send); err != nil {
			return nil, &Error{Kind: KindMapping, Op: "manual sql", Err: fmt.Errorf("column 2 (last send): %w", err)}
		}
		if p.Recv, err = valueTime(recv); err != nil {
			return nil, &Error{Kind: KindMapping, Op: "manual sql", Err: fmt.Errorf("column 3 (last recv): %w", err)}
		}
		out = append(out, p)
	}
	return out, classify("manual sql", rows.Err(), false)
}

// valueString converts a scanned driver value to text for point names/codes.
func valueString(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		if utf8.Valid(x) {
			return string(x)
		}
		return "0x" + hex.EncodeToString(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case time.Time:
		return x.Format("2006-01-02 15:04:05")
	}
	return fmt.Sprint(v)
}

var errNotTime = errors.New("value is not a date/time")

// valueInt converts a scanned counter value (numeric(10,0) arrives as text
// bytes) to int64; NULL = 0.
func valueInt(v interface{}) (int64, error) {
	switch x := v.(type) {
	case nil:
		return 0, nil
	case int64:
		return x, nil
	case float64:
		return int64(x), nil
	case []byte, string:
		str := strings.TrimSpace(valueString(x))
		if i := strings.IndexByte(str, '.'); i >= 0 {
			str = str[:i]
		}
		n, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("counter %q is not an integer", str)
		}
		return n, nil
	}
	return 0, fmt.Errorf("counter of type %T is not a number", v)
}

// valueTime converts a scanned driver value to a naive timestamp.
func valueTime(v interface{}) (*time.Time, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case time.Time:
		return &x, nil
	}
	return nil, fmt.Errorf("%w (%T)", errNotTime, v)
}
