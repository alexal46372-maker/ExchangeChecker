package sqlcheck

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// In-memory schema fixtures: a fakeStore implements Store with the same
// semantics as the SQL built by sqlbuild.go, so Discovery / ListPoints run
// without a server (CHK-SQL-02..04 unit tests).

type fxTable struct {
	ref  TableRef
	cols []Column
	rows [][]interface{}
}

// fxT builds a table; column spec "name:type" or "name:type(len)".
func fxT(schema, name string, specs ...string) *fxTable {
	t := &fxTable{ref: TableRef{Schema: schema, Name: name}}
	for i, s := range specs {
		p := strings.SplitN(s, ":", 2)
		typ, n := p[1], 0
		if j := strings.IndexByte(typ, '('); j >= 0 {
			if typ[j+1:len(typ)-1] == "max" {
				n = -1
			} else {
				n, _ = strconv.Atoi(typ[j+1 : len(typ)-1])
			}
			typ = typ[:j]
		}
		t.cols = append(t.cols, Column{Table: t.ref, Name: p[0], DataType: typ, MaxLength: n, Ordinal: i + 1})
	}
	return t
}

func (t *fxTable) add(vals ...interface{}) *fxTable {
	if len(vals) != len(t.cols) {
		panic(fmt.Sprintf("%s: %d values for %d columns", t.ref, len(vals), len(t.cols)))
	}
	t.rows = append(t.rows, vals)
	return t
}

func (t *fxTable) idx(col string) int {
	for i, c := range t.cols {
		if strings.EqualFold(c.Name, col) {
			return i
		}
	}
	return -1
}

type fakeManual struct {
	rows []RawPoint
	err  error
}

type fakeStore struct {
	mu       sync.Mutex
	tables   []*fxTable
	noCounts bool
	pingErr  error
	fail     map[string]error // table name -> error for every query touching it
	manual   map[string]fakeManual
	delay    time.Duration
	calls    []string
	closed   bool
}

func newFake(ts ...*fxTable) *fakeStore { return &fakeStore{tables: ts} }

func (f *fakeStore) enter(ctx context.Context, call string, tables ...TableRef) error {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, t := range tables {
		if err := f.fail[t.Name]; err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeStore) called(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeStore) find(ref TableRef) (*fxTable, error) {
	var hit *fxTable
	for _, t := range f.tables {
		if !strings.EqualFold(t.ref.Name, ref.Name) {
			continue
		}
		if ref.Schema != "" && !strings.EqualFold(t.ref.Schema, ref.Schema) {
			continue
		}
		if hit == nil || strings.EqualFold(t.ref.Schema, "dbo") {
			hit = t
		}
	}
	if hit == nil {
		return nil, fmt.Errorf("fake: invalid object name %s", ref)
	}
	return hit, nil
}

func (f *fakeStore) cols(t *fxTable, names []string) ([]int, error) {
	out := make([]int, len(names))
	for i, n := range names {
		if out[i] = t.idx(n); out[i] < 0 {
			return nil, fmt.Errorf("fake: invalid column name %s.%s", t.ref, n)
		}
	}
	return out, nil
}

func (f *fakeStore) Ping(ctx context.Context) error {
	if err := f.enter(ctx, "ping"); err != nil {
		return classify("connect", err, true)
	}
	return classify("connect", f.pingErr, true)
}

func (f *fakeStore) Columns(ctx context.Context) ([]Column, error) {
	if err := f.enter(ctx, "columns"); err != nil {
		return nil, err
	}
	var out []Column
	for _, t := range f.tables {
		out = append(out, t.cols...)
	}
	return out, nil
}

func (f *fakeStore) RowCounts(ctx context.Context) (map[TableRef]int64, error) {
	if err := f.enter(ctx, "rowcounts"); err != nil {
		return nil, err
	}
	if f.noCounts {
		return nil, errors.New("fake: permission denied on sys.partitions")
	}
	out := map[TableRef]int64{}
	for _, t := range f.tables {
		out[t.ref] = int64(len(t.rows))
	}
	return out, nil
}

func project(r []interface{}, idx []int) []interface{} {
	out := make([]interface{}, len(idx))
	for i, j := range idx {
		out[i] = r[j]
	}
	return out
}

func (f *fakeStore) SampleColumns(ctx context.Context, ref TableRef, cols []string, limit int) ([][]interface{}, error) {
	if err := f.enter(ctx, "sample:"+ref.Name, ref); err != nil {
		return nil, err
	}
	t, err := f.find(ref)
	if err != nil {
		return nil, err
	}
	idx, err := f.cols(t, cols)
	if err != nil {
		return nil, err
	}
	var out [][]interface{}
	for i, r := range t.rows {
		if i >= limit {
			break
		}
		out = append(out, project(r, idx))
	}
	return out, nil
}

func keyOf(v interface{}) (string, bool) {
	b, ok := v.([]byte)
	return string(b), ok && b != nil
}

func maxTime(cur *time.Time, v interface{}) *time.Time {
	t, ok := v.(time.Time)
	if !ok {
		return cur
	}
	if cur == nil || t.After(*cur) {
		return &t
	}
	return cur
}

func (f *fakeStore) LinkStats(ctx context.Context, q LinkQuery) (LinkStats, error) {
	var st LinkStats
	if _, err := buildLinkStatsSQL(q); err != nil {
		return st, err
	}
	if err := f.enter(ctx, "link:"+q.Points.Name+"<-"+q.Times.Name+"."+q.LinkCol, q.Points, q.Times); err != nil {
		return st, err
	}
	pt, err := f.find(q.Points)
	if err != nil {
		return st, err
	}
	tt, err := f.find(q.Times)
	if err != nil {
		return st, err
	}
	ki, li := pt.idx(q.KeyCol), tt.idx(q.LinkCol)
	di, err := f.cols(tt, q.DateCols)
	if err != nil || ki < 0 || li < 0 {
		return st, fmt.Errorf("fake: bad link query %+v", q)
	}
	keys := map[string]bool{}
	for _, r := range pt.rows {
		if k, ok := keyOf(r[ki]); ok {
			keys[k] = true
		}
	}
	st.Max = make([]*time.Time, len(di))
	distinct := map[string]bool{}
	scanned := 0
	for _, r := range tt.rows {
		if q.TypeCol != "" && !compositeMatch(cell(tt, r, q.TypeCol), cell(tt, r, q.RTRefCol), q.RTRef) {
			continue // WHERE [type] = 0x08 AND [rtref] = @p3 inside the TOP subquery
		}
		if scanned >= q.Limit {
			break
		}
		scanned++
		st.Sampled++
		k, ok := keyOf(r[li])
		if !ok || !keys[k] {
			continue
		}
		st.Matched++
		distinct[k] = true
		for j, d := range di {
			st.Max[j] = maxTime(st.Max[j], r[d])
		}
	}
	st.Points = int64(len(distinct))
	return st, nil
}

func (f *fakeStore) SingleStats(ctx context.Context, q SingleQuery) (SingleStats, error) {
	var st SingleStats
	if _, err := buildSingleStatsSQL(q); err != nil {
		return st, err
	}
	if err := f.enter(ctx, "single:"+q.Table.Name, q.Table); err != nil {
		return st, err
	}
	t, err := f.find(q.Table)
	if err != nil {
		return st, err
	}
	pi := t.idx(q.PointCol)
	di, err := f.cols(t, q.DateCols)
	if err != nil || pi < 0 {
		return st, fmt.Errorf("fake: bad single query %+v", q)
	}
	st.Max = make([]*time.Time, len(di))
	distinct := map[string]bool{}
	for i, r := range t.rows {
		if i >= q.Limit {
			break
		}
		st.Sampled++
		if r[pi] != nil {
			distinct[valueString(r[pi])] = true
		}
		for j, d := range di {
			st.Max[j] = maxTime(st.Max[j], r[d])
		}
	}
	st.Points = int64(len(distinct))
	return st, nil
}

func cell(t *fxTable, r []interface{}, col string) interface{} {
	if col == "" {
		return nil
	}
	return r[t.idx(col)]
}

// Points mirrors buildPointsSQL: GROUP BY point (key), MAX() of timestamps,
// INNER JOIN unless KeepUnlinkedPoints, ORDER BY name, id, TOP limit.
func (f *fakeStore) Points(ctx context.Context, m model.Mapping, limit int) ([]RawPoint, error) {
	if _, err := buildPointsSQL(m); err != nil {
		return nil, &Error{Kind: KindMapping, Op: "points", Err: err}
	}
	pref := TableRef{Schema: m.Schema, Name: m.Table}
	tref := TableRef{Schema: m.TimesSchema, Name: m.TimesTable}
	if err := f.enter(ctx, "points:"+m.Table+"|"+m.TimesTable, pref, tref); err != nil {
		return nil, classify("points", err, false)
	}
	pt, err := f.find(pref)
	if err != nil {
		return nil, classify("points", err, false)
	}
	for _, c := range []string{m.PointNameColumn, m.PointIDColumn, m.PointKeyColumn} {
		if c != "" && pt.idx(c) < 0 {
			return nil, classify("points", fmt.Errorf("fake: invalid column name %s", c), false)
		}
	}
	type agg struct {
		p    RawPoint
		seen bool
	}
	var order []string
	groups := map[string]*agg{}
	get := func(k string, name, id interface{}) *agg {
		g := groups[k]
		if g == nil {
			g = &agg{p: RawPoint{Name: valueString(name), ID: valueString(id)}}
			groups[k] = g
			order = append(order, k)
		}
		return g
	}
	if !m.Linked() {
		for _, r := range pt.rows {
			name, id := cell(pt, r, m.PointNameColumn), cell(pt, r, m.PointIDColumn)
			g := get(valueString(name)+"\x00"+valueString(id), name, id)
			g.seen = true
			g.p.Send = maxTime(g.p.Send, cell(pt, r, m.LastSendColumn))
			g.p.Recv = maxTime(g.p.Recv, cell(pt, r, m.LastRecvColumn))
		}
	} else {
		tt, err := f.find(tref)
		if err != nil {
			return nil, classify("points", err, false)
		}
		for _, r := range pt.rows {
			k, _ := keyOf(cell(pt, r, m.PointKeyColumn))
			g := get(k, cell(pt, r, m.PointNameColumn), cell(pt, r, m.PointIDColumn))
			g.seen = g.seen || m.KeepUnlinkedPoints
			for _, tr := range tt.rows {
				if m.Composite() && !compositeMatch(cell(tt, tr, m.TimesLinkTypeColumn), cell(tt, tr, m.TimesLinkRTRefColumn), m.TimesLinkRTRef) {
					continue
				}
				if lk, ok := keyOf(cell(tt, tr, m.TimesLinkColumn)); ok && lk == k {
					g.seen = true
					g.p.Send = maxTime(g.p.Send, cell(tt, tr, m.LastSendColumn))
					g.p.Recv = maxTime(g.p.Recv, cell(tt, tr, m.LastRecvColumn))
				}
			}
		}
	}
	var out []RawPoint
	for _, k := range order {
		if groups[k].seen {
			out = append(out, groups[k].p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func likeUnescape(p string) string {
	p = strings.TrimSuffix(strings.TrimPrefix(p, "%"), "%")
	return strings.NewReplacer("[[]", "[", "[%]", "%", "[_]", "_").Replace(p)
}

func (f *fakeStore) search(ctx context.Context, call string, ref TableRef, sel, where []string, limit int, match func(v interface{}) bool, rowMatch ...func(r []interface{}) bool) ([][]interface{}, error) {
	if err := f.enter(ctx, call+":"+ref.Name, ref); err != nil {
		return nil, err
	}
	t, err := f.find(ref)
	if err != nil {
		return nil, err
	}
	si, err := f.cols(t, sel)
	if err != nil {
		return nil, err
	}
	wi, err := f.cols(t, where)
	if err != nil {
		return nil, err
	}
	var out [][]interface{}
	for _, r := range t.rows {
		if len(out) >= limit {
			break
		}
		if len(rowMatch) > 0 {
			if rowMatch[0](r) {
				out = append(out, project(r, si))
			}
			continue
		}
		for _, w := range wi {
			if match(r[w]) {
				out = append(out, project(r, si))
				break
			}
		}
	}
	return out, nil
}

func (f *fakeStore) SearchText(ctx context.Context, ref TableRef, sel, texts []string, pattern string, limit int) ([][]interface{}, error) {
	needle := strings.ToLower(likeUnescape(pattern))
	return f.search(ctx, "text", ref, sel, texts, limit, func(v interface{}) bool {
		s, ok := v.(string)
		return ok && strings.Contains(strings.ToLower(s), needle)
	})
}

func (f *fakeStore) SearchRef(ctx context.Context, ref TableRef, sel, refs []string, key []byte, limit int) ([][]interface{}, error) {
	return f.search(ctx, "ref", ref, sel, refs, limit, func(v interface{}) bool {
		k, ok := keyOf(v)
		return ok && k == string(key)
	})
}

func (f *fakeStore) SearchComposite(ctx context.Context, ref TableRef, sel []string, cr CompositeRef, key []byte, limit int) ([][]interface{}, error) {
	if _, err := buildCompositeSearchSQL(ref, sel, cr); err != nil {
		return nil, err
	}
	t, err := f.find(ref)
	if err != nil {
		return nil, err
	}
	ti, ri, ki := t.idx(cr.TypeCol), t.idx(cr.RTRefCol), t.idx(cr.RRefCol)
	if ti < 0 || ri < 0 || ki < 0 {
		return nil, fmt.Errorf("fake: bad composite ref %+v", cr)
	}
	return f.search(ctx, "composite", ref, sel, []string{cr.RRefCol}, limit, nil, func(r []interface{}) bool {
		k, ok := keyOf(r[ki])
		return ok && k == string(key) && compositeMatch(r[ti], r[ri], cr.RTRef)
	})
}

func (f *fakeStore) ManualPoints(ctx context.Context, query string, limit int) ([]RawPoint, error) {
	if err := ValidateManualSQL(query); err != nil {
		return nil, &Error{Kind: KindMapping, Op: "manual sql", Err: err}
	}
	if err := f.enter(ctx, "manual"); err != nil {
		return nil, classify("manual sql", err, false)
	}
	r, ok := f.manual[query]
	if !ok {
		return nil, classify("manual sql", errors.New("fake: unknown manual query"), false)
	}
	if r.err != nil {
		return nil, r.err
	}
	if len(r.rows) > limit {
		return r.rows[:limit], nil
	}
	return r.rows, nil
}

func (f *fakeStore) Close() error { f.closed = true; return nil }

// ---- fixture helpers -------------------------------------------------

// fixNow is the fixed "now" of the unit tests: 2026-09-29 12:00 UTC+3.
var fixNow = time.Date(2026, 9, 29, 12, 0, 0, 0, model.DBZone)

// dbAt returns the naive database wall clock (UTC location) of the
// instant fixNow-ago, as 1C stores it with the given year offset.
func dbAt(ago time.Duration, off int) time.Time {
	t := fixNow.Add(-ago).In(model.DBZone)
	return time.Date(t.Year()+off, t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

// emptyDate is the 1C empty date for the offset (0001-01-01 + offset, or
// the datetime minimum without offset).
func emptyDate(off int) time.Time {
	if off == 0 {
		return time.Date(1753, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(1+off, 1, 1, 0, 0, 0, 0, time.UTC)
}

// ref16 returns a deterministic 16-byte 1C reference.
func ref16(table, n byte) []byte {
	b := make([]byte, 16)
	b[0], b[1], b[15] = 0x80, table, n
	return b
}

const (
	hour = time.Hour
	day  = 24 * time.Hour
)

// fxCurrency is a distractor present in real 1C bases: a small directory
// with a periodic register (one datetime series, updated for every row).
func fxCurrency(off int) []*fxTable {
	cur := fxT("dbo", "_Reference10", "_IDRRef:binary(16)", "_Marked:binary(1)", "_Code:nchar(3)", "_Description:nvarchar(25)")
	rates := fxT("dbo", "_InfoRg11", "_Period:datetime", "_Fld12RRef:binary(16)", "_Fld13:numeric")
	for i, n := range []string{"RUB", "USD", "EUR"} {
		cur.add(ref16(10, byte(i+1)), []byte{0}, n, n)
		rates.add(dbAt(10*hour, off), ref16(10, byte(i+1)), []byte("1.5"))
		rates.add(dbAt(34*hour, off), ref16(10, byte(i+1)), []byte("1.4"))
	}
	return []*fxTable{cur, rates}
}

// fx1CNode: exchange plan _Node123 + times register _InfoRg456 (the
// "this node" row ЦБ has no times row), an old linked register, a fresh
// but unlinked register, and the currency distractor.
func fx1CNode(off int) []*fxTable {
	node := fxT("dbo", "_Node123", "_IDRRef:binary(16)", "_Version:timestamp", "_Marked:binary(1)",
		"_Code:nvarchar(9)", "_Description:nvarchar(50)", "_SentNo:numeric", "_ReceivedNo:numeric", "_PredefinedID:binary(16)")
	for i, n := range []string{"ЦБ", "Магазин 1", "Магазин 2", "Магазин 3"} {
		node.add(ref16(123, byte(i+1)), []byte{0, 0, 0, 0, 0, 0, 0, byte(i)}, []byte{0}, fmt.Sprintf("%03d", i), n,
			[]byte("10"), []byte("9"), predefined(i == 0))
	}
	times := fxT("dbo", "_InfoRg456", "_Fld457RRef:binary(16)", "_Fld458:datetime", "_Fld459:datetime", "_Fld460:numeric").
		add(ref16(123, 2), dbAt(20*time.Minute, off), dbAt(10*time.Minute, off), []byte("1")).
		add(ref16(123, 2), dbAt(2*day, off), dbAt(2*day, off), []byte("0")).
		add(ref16(123, 3), dbAt(3*hour, off), dbAt(2*hour, off), []byte("1")).
		add(ref16(123, 4), emptyDate(off), emptyDate(off), []byte("0"))
	old := fxT("dbo", "_InfoRg500", "_Fld501RRef:binary(16)", "_Fld502:datetime").
		add(ref16(123, 2), dbAt(400*day, off)).add(ref16(123, 3), dbAt(410*day, off))
	unlinked := fxT("dbo", "_InfoRg900", "_Fld901RRef:binary(16)", "_Fld902:datetime").
		add(ref16(99, 1), dbAt(time.Minute, off))
	return append([]*fxTable{node, times, old, unlinked}, fxCurrency(off)...)
}

// predefined returns a _PredefinedID value: non-zero for the predefined
// "this node" row of an exchange plan.
func predefined(this bool) []byte {
	b := make([]byte, 16)
	if this {
		b[0], b[15] = 0x9a, 0x01
	}
	return b
}

// fx1CPlans: two standard exchange plans (customer clarification):
// _Node123 («Основной») with the simple-ref register _InfoRg456 (from
// fx1CNode) and _Node124 («Магазин») with the register _InfoRg600 whose
// point dimension is a 1C 8.1 COMPOSITE reference _Fld601_TYPE /
// _Fld601_RTRef / _Fld601_RRRef. _InfoRg600 also holds
//   - a row with RTRef = 123 whose _RRRef equals a _Node124 key (a
//     type-number collision that must be ignored: it is not a _Node124
//     reference; its dates are the freshest in the table),
//   - a row referencing the currency directory _Reference10 (RTRef 10),
//   - a row with _TYPE = 0x01 (empty value).
func fx1CPlans(off int) []*fxTable {
	base := fx1CNode(off)
	node := fxT("dbo", "_Node124", "_IDRRef:binary(16)", "_Version:timestamp", "_Marked:binary(1)",
		"_Code:nvarchar(9)", "_Description:nvarchar(50)", "_SentNo:numeric", "_ReceivedNo:numeric", "_PredefinedID:binary(16)")
	for i, n := range []string{"ЦБ", "Склад А", "Склад Б"} {
		node.add(ref16(124, byte(i+1)), []byte{0, 0, 0, 0, 0, 0, 1, byte(i)}, []byte{0}, fmt.Sprintf("S%02d", i), n,
			[]byte("5"), []byte("4"), predefined(i == 0))
	}
	rt := func(n int) []byte { return model.RTRefBytes(n) }
	reg := fxT("dbo", "_InfoRg600", "_Fld601_TYPE:binary(1)", "_Fld601_RTRef:binary(4)", "_Fld601_RRRef:binary(16)",
		"_Fld602:datetime", "_Fld603:datetime", "_Fld604:numeric").
		add([]byte{0x08}, rt(124), ref16(124, 2), dbAt(25*time.Minute, off), dbAt(12*time.Minute, off), []byte("1")).
		add([]byte{0x08}, rt(124), ref16(124, 3), dbAt(40*time.Minute, off), dbAt(35*time.Minute, off), []byte("1")).
		add([]byte{0x08}, rt(124), ref16(124, 3), dbAt(3*day, off), dbAt(3*day, off), []byte("0")).
		add([]byte{0x08}, rt(123), ref16(124, 3), dbAt(time.Minute, off), dbAt(time.Minute, off), []byte("9")).
		add([]byte{0x08}, rt(10), ref16(10, 1), dbAt(2*time.Minute, off), dbAt(2*time.Minute, off), []byte("0")).
		add([]byte{0x01}, rt(0), make([]byte, 16), dbAt(time.Minute, off), dbAt(time.Minute, off), []byte("0"))
	return append(base, node, reg)
}

// fx1CCustom: fully custom configuration without exchange plans:
// directory _Reference77 + register _InfoRg88 (send/receive dates linked
// by the binary(16) dimension _Fld89RRef).
func fx1CCustom(off int, dirName string) []*fxTable {
	dir := fxT("dbo", dirName, "_IDRRef:binary(16)", "_Marked:binary(1)", "_Code:nvarchar(9)",
		"_Description:nvarchar(100)", "_Fld78:nvarchar(150)")
	reg := fxT("dbo", "_InfoRg88", "_Fld89RRef:binary(16)", "_Fld90:datetime", "_Fld91:datetime", "_Fld92:nvarchar(200)")
	ago := []time.Duration{15 * time.Minute, 50 * time.Minute, day}
	for i, n := range []string{"Магазин 1", "Магазин 2", "Склад 50%"} {
		dir.add(ref16(77, byte(i+1)), []byte{0}, fmt.Sprintf("%09d", i+1), n, "ул. Ленина, 1")
		reg.add(ref16(77, byte(i+1)), dbAt(ago[i]+5*time.Minute, off), dbAt(ago[i], off), "ok")
	}
	return append([]*fxTable{dir, reg}, fxCurrency(off)...)
}

// fx1CDirDates: the exchange dates are columns of the directory itself.
func fx1CDirDates(off int) []*fxTable {
	dir := fxT("dbo", "_Reference55", "_IDRRef:binary(16)", "_Marked:binary(1)", "_Code:nvarchar(9)",
		"_Description:nvarchar(100)", "_Fld56:datetime", "_Fld57:datetime")
	for i, n := range []string{"Магазин 1", "Магазин 2", "Магазин 3"} {
		a := time.Duration(i+1) * 10 * time.Minute
		dir.add(ref16(55, byte(i+1)), []byte{0}, fmt.Sprintf("%09d", i+1), n, dbAt(a+time.Minute, off), dbAt(a, off))
	}
	return append([]*fxTable{dir}, fxCurrency(off)...)
}

// fxGeneric: non-1C schema with identifiers that need escaping.
func fxGeneric() []*fxTable {
	ex := fxT("exch", "Node]Exchange", "Id:int", "Point Name:nvarchar(100)", "Send]Date:datetime2", "RecvDate:datetime")
	for i, n := range []string{"North", "South", "West"} {
		a := time.Duration(i+1) * 7 * time.Minute
		ex.add(int64(i+1), n, dbAt(a+time.Minute, 0), dbAt(a, 0))
	}
	msg := fxT("dbo", "MsgQueue", "Topic:nvarchar(50)", "EnqueuedAt:datetime").add("t1", dbAt(30*day, 0))
	cust := fxT("dbo", "Customers", "Name:nvarchar(50)", "CreatedAt:datetime").add("ACME", dbAt(time.Minute, 0))
	return []*fxTable{ex, msg, cust}
}

func (f *fakeStore) Counters(ctx context.Context, m model.Mapping, limit int) ([]RawCounter, error) {
	if _, err := buildCountersSQL(m); err != nil {
		return nil, &Error{Kind: KindMapping, Op: "counters", Err: err}
	}
	ref := TableRef{Schema: m.Schema, Name: m.Table}
	if err := f.enter(ctx, "counters:"+m.Table, ref); err != nil {
		return nil, classify("counters", err, false)
	}
	t, err := f.find(ref)
	if err != nil {
		return nil, classify("counters", err, false)
	}
	var out []RawCounter
	for _, r := range t.rows {
		c := RawCounter{Name: valueString(cell(t, r, m.PointNameColumn)), ID: valueString(cell(t, r, m.PointIDColumn))}
		if b, ok := cell(t, r, m.PointKeyColumn).([]byte); ok {
			c.Key = b
		}
		if c.SentNo, err = valueInt(r[t.idx(m.SentNoCol())]); err != nil {
			return nil, err
		}
		if c.ReceivedNo, err = valueInt(r[t.idx(m.ReceivedNoCol())]); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
