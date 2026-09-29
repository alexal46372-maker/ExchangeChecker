package sqlcheck

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// DiscoverOptions bound the load Discovery puts on a production database.
// Zero fields = defaults.
type DiscoverOptions struct {
	TopN       int // candidates returned (10)
	SampleRows int // SampleRows per candidate (5)

	// Timeout is the total time budget of one Discover / DiscoverByValue
	// call (90s); when it runs out the partial result is returned with
	// model.HintTruncated. QueryTimeout bounds each single query (15s); a
	// table whose query fails or times out is skipped.
	Timeout      time.Duration
	QueryTimeout time.Duration

	MaxDirRows     int // a points directory has at most this many rows (5000)
	MaxDirTables   int // directory tables probed (300)
	RefSampleRows  int // rows sampled per times table to find links by value (500)
	StatsRowLimit  int // rows scanned by each aggregate query, TOP (200000)
	MaxTimesTables int // times tables probed (400)
	MaxDateColumns int // datetime columns considered per table (16)
	RankTop        int // best candidates re-scored per point (30)

	// DiscoverByValue limits.
	MaxValueRows   int // only tables with at most this many rows are searched (100000)
	MaxValueTables int // tables searched for the point name (300)
	MaxTextColumns int // string columns searched per table (24)
	MaxTextLength  int // longer string columns are not searched (1024)
	MaxLinkedRows  int // times tables searched by reference: at most this many rows (500000)
	MaxLinkTables  int // times tables searched by reference per matched point (200)
	MatchRows      int // rows returned per table by a search query (20)
	MaxMatches     int // matched (table, column) pairs followed (20)
}

func (o DiscoverOptions) withDefaults() DiscoverOptions {
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&o.TopN, 10)
	def(&o.SampleRows, 5)
	if o.Timeout <= 0 {
		o.Timeout = 90 * time.Second
	}
	if o.QueryTimeout <= 0 {
		o.QueryTimeout = 15 * time.Second
	}
	def(&o.MaxDirRows, 5000)
	def(&o.MaxDirTables, 300)
	def(&o.RefSampleRows, 500)
	def(&o.StatsRowLimit, 200000)
	def(&o.MaxTimesTables, 400)
	def(&o.MaxDateColumns, 16)
	def(&o.RankTop, 30)
	def(&o.MaxValueRows, 100000)
	def(&o.MaxValueTables, 300)
	def(&o.MaxTextColumns, 24)
	def(&o.MaxTextLength, 1024)
	def(&o.MaxLinkedRows, 500000)
	def(&o.MaxLinkTables, 200)
	def(&o.MatchRows, 20)
	def(&o.MaxMatches, 20)
	if o.RankTop < o.TopN {
		o.RankTop = o.TopN
	}
	return o
}

// errBudget stops probing when the Discovery time budget is exhausted.
var errBudget = errors.New("discovery time budget exhausted")

type discoverer struct {
	parent    context.Context
	ctx       context.Context // parent + Timeout
	st        Store
	opt       DiscoverOptions
	now       time.Time
	counts    map[TableRef]int64
	truncated bool
}

func newDiscoverer(parent context.Context, st Store, opt DiscoverOptions, now time.Time) (*discoverer, context.CancelFunc) {
	opt = opt.withDefaults()
	ctx, cancel := context.WithTimeout(parent, opt.Timeout)
	return &discoverer{parent: parent, ctx: ctx, st: st, opt: opt, now: now}, cancel
}

// run executes one bounded query. It returns ok=false with a nil error when
// only this query failed (skip the table), and a non-nil error when the
// whole discovery must stop: the caller's context (returned as is) or the
// time budget (errBudget).
func (d *discoverer) run(f func(ctx context.Context) error) (bool, error) {
	if err := d.stopErr(); err != nil {
		return false, err
	}
	qctx, cancel := context.WithTimeout(d.ctx, d.opt.QueryTimeout)
	defer cancel()
	if err := f(qctx); err != nil {
		if stop := d.stopErr(); stop != nil {
			return false, stop
		}
		return false, nil
	}
	return true, nil
}

func (d *discoverer) stopErr() error {
	if err := d.parent.Err(); err != nil {
		return err
	}
	if d.ctx.Err() != nil {
		d.truncated = true
		return errBudget
	}
	return nil
}

// catalog loads columns (fatal on error) and row counts (best effort).
func (d *discoverer) catalog() ([]*tableInfo, error) {
	var cols []Column
	var cerr error
	if _, err := d.run(func(ctx context.Context) error {
		cols, cerr = d.st.Columns(ctx)
		return cerr
	}); err != nil {
		return nil, err
	}
	if cerr != nil {
		return nil, cerr
	}
	if _, err := d.run(func(ctx context.Context) error {
		c, err := d.st.RowCounts(ctx)
		d.counts = c
		return err
	}); err != nil {
		return nil, err
	}
	return groupTables(cols), nil
}

// rows returns the approximate row count of t, or -1 when unknown.
func (d *discoverer) rows(t TableRef) int64 {
	if d.counts == nil {
		return -1
	}
	n, ok := d.counts[t]
	if !ok {
		return -1
	}
	return n
}

// sortBySize orders tables: preferred first, then known row count
// ascending, unknown counts last, then by name (stable input order).
func (d *discoverer) sortBySize(ts []*tableInfo, preferred func(*tableInfo) bool) {
	sort.SliceStable(ts, func(i, j int) bool {
		pi, pj := preferred(ts[i]), preferred(ts[j])
		if pi != pj {
			return pi
		}
		ri, rj := d.rows(ts[i].ref), d.rows(ts[j].ref)
		if (ri < 0) != (rj < 0) {
			return ri >= 0
		}
		return ri < rj
	})
}

func (d *discoverer) sampledHint(t TableRef) []string {
	if d.rows(t) > int64(d.opt.StatsRowLimit) {
		return []string{model.HintSampled}
	}
	return nil
}

func (d *discoverer) finishHints(cands []model.DiscoveryCandidate) {
	if !d.truncated {
		return
	}
	for i := range cands {
		if !cands[i].HasHint(model.HintTruncated) {
			cands[i].Hints = append(cands[i].Hints, model.HintTruncated)
		}
	}
}

// dirTable is a possible points directory.
type dirTable struct {
	t       *tableInfo
	key     string // _IDRRef or "" (not linkable)
	nameCol string
	codeCol string
	node    bool
}

// Discover searches the database for the tables/columns holding exchange
// timestamps and returns the top-N candidates, best first (CHK-SQL-02).
// The 1C exchange plans _NodeNNN are the PRIMARY source of points
// (customer clarification: standard platform plans; +nodeBonus); tables
// without plans are still covered by the lower-score universal path:
//
//  1. Catalog: INFORMATION_SCHEMA.COLUMNS of base tables, row counts from
//     sys.partitions (fallback sysindexes).
//  2. Points directories: _Node tables first, then _Reference / _Document /
//     _InfoRg with few rows (<= MaxDirRows) and _Description / _Code or a
//     string column.
//  3. Same-table candidates: a directory with its own datetime columns.
//  4. Linked candidates: any 1C table with datetime columns and a
//     binary(16) reference column whose sampled values are keys (_IDRRef)
//     of a directory: a simple _FldNNNRRef, or a 1C 8.1 composite
//     reference _FldNNN_TYPE (0x08) / _FldNNN_RTRef (= NNN of the
//     directory table, big-endian) / _FldNNN_RRRef, which is matched only
//     for rows referencing that table number; confirmed by a bounded JOIN
//     that also reads MAX() of the datetime columns of linked rows.
//  5. Generic (non-1C names): single tables with Node/Exchange/Msg markers,
//     datetime columns and a string point column (lower base score).
//  6. Counters fallback per _Node table (fixed score 5).
//  7. Scoring: two datetime series, MAX() close to now (after the 2000-year
//     offset), link quality, _Node bonus; the best RankTop candidates are
//     then read per point through their mapping: values updated across all
//     points and a small number of points raise the score.
//
// Candidates of _NodeNNN tables carry PlanTable / PlanNo. All queries are
// read-only (WITH (NOLOCK), no session-level SET), bounded by TOP, per-query
// timeouts, table caps and the total Timeout.
func Discover(ctx context.Context, st Store, opt DiscoverOptions, now time.Time) ([]model.DiscoveryCandidate, error) {
	d, cancel := newDiscoverer(ctx, st, opt, now)
	defer cancel()
	tables, err := d.catalog()
	if err != nil {
		return nil, err
	}
	var cands []model.DiscoveryCandidate
	err = d.discoverDirectories(tables, &cands)
	if err == nil {
		err = d.discoverGeneric(tables, &cands)
	}
	if err == nil {
		err = d.discoverCounters(tables, &cands)
	}
	if err != nil && !errors.Is(err, errBudget) {
		return nil, err
	}
	if err := d.rank(cands); err != nil {
		return nil, err
	}
	out := d.top(cands)
	d.finishHints(out)
	return out, nil
}

// top sorts by score, removes duplicates and keeps TopN. Candidates of
// exchange-plan tables are tagged with PlanTable / PlanNo.
func (d *discoverer) top(cands []model.DiscoveryCandidate) []model.DiscoveryCandidate {
	for i := range cands {
		cands[i].PlanTable, cands[i].PlanNo = model.PlanTableOf(cands[i].Table), model.PlanNoOf(cands[i].Table)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		return cands[i].Key() < cands[j].Key()
	})
	seen := map[string]bool{}
	out := make([]model.DiscoveryCandidate, 0, d.opt.TopN)
	for _, c := range cands {
		if seen[c.Key()] || len(out) >= d.opt.TopN {
			continue
		}
		seen[c.Key()] = true
		out = append(out, c)
	}
	return out
}

func (d *discoverer) directories(tables []*tableInfo) []*dirTable {
	var dirs []*tableInfo
	for _, t := range tables {
		if !re1CDirectory.MatchString(t.ref.Name) {
			continue
		}
		if n := d.rows(t.ref); n == 0 || n > int64(d.opt.MaxDirRows) {
			continue
		}
		if pc, _ := pointCols1C(t); pc == "" {
			continue
		}
		dirs = append(dirs, t)
	}
	d.sortBySize(dirs, func(t *tableInfo) bool { return re1CNode.MatchString(t.ref.Name) })
	if len(dirs) > d.opt.MaxDirTables {
		dirs = dirs[:d.opt.MaxDirTables]
	}
	out := make([]*dirTable, len(dirs))
	for i, t := range dirs {
		pc, ic := pointCols1C(t)
		out[i] = &dirTable{t: t, key: t.key1C(), nameCol: pc, codeCol: ic, node: re1CNode.MatchString(t.ref.Name)}
	}
	return out
}

func (d *discoverer) discoverDirectories(tables []*tableInfo, out *[]model.DiscoveryCandidate) error {
	dirs := d.directories(tables)
	// 3. same table
	for _, dt := range dirs {
		dates := dt.t.dateCols(d.opt.MaxDateColumns)
		if len(dates) == 0 {
			continue
		}
		c, ok, err := d.singleCandidate(dt.t, dt.nameCol, dt.codeCol, dates, 35, model.HintSameTable)
		if err != nil {
			return err
		}
		if ok {
			if dt.node {
				c.Score += nodeBonus
				c.Hints = append(c.Hints, model.HintNodeTable)
			}
			*out = append(*out, c)
		}
	}
	// 4. linked: keys of linkable directories
	keyOwner := map[string]int{}
	keyCount := map[int]int{}
	for i, dt := range dirs {
		if dt.key == "" {
			continue
		}
		var rows [][]interface{}
		ok, err := d.run(func(ctx context.Context) error {
			var e error
			rows, e = d.st.SampleColumns(ctx, dt.t.ref, []string{dt.key}, d.opt.MaxDirRows)
			return e
		})
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		for _, r := range rows {
			if b, ok := r[0].([]byte); ok && len(b) == 16 {
				keyOwner[string(b)] = i
				keyCount[i]++
			}
		}
	}
	if len(keyOwner) == 0 {
		return nil
	}
	var times []*tableInfo
	for _, t := range tables {
		if re1CStorage.MatchString(t.ref.Name) && len(t.refCols()) > 0 && len(t.dateCols(1)) > 0 {
			times = append(times, t)
		}
	}
	d.sortBySize(times, func(t *tableInfo) bool { return re1CInfoRg.MatchString(t.ref.Name) })
	if len(times) > d.opt.MaxTimesTables {
		times = times[:d.opt.MaxTimesTables]
	}
	for _, tt := range times {
		refs := tt.refCols()
		// composite references: also sample their _TYPE / _RTRef columns
		sel := colNames(refs)
		comps := make([]*compositeCols, len(refs))
		typePos := make([]int, len(refs))
		for j, rc := range refs {
			if cc, ok := tt.compositeOf(rc); ok {
				comps[j] = &cc
				typePos[j] = len(sel)
				sel = append(sel, cc.typeCol, cc.rtrefCol)
			}
		}
		var rows [][]interface{}
		ok, err := d.run(func(ctx context.Context) error {
			var e error
			rows, e = d.st.SampleColumns(ctx, tt.ref, sel, d.opt.RefSampleRows)
			return e
		})
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		for j, rc := range refs {
			hits := map[int]int{}
			for _, r := range rows {
				if b, ok := r[j].([]byte); ok && len(b) == 16 {
					owner, ok := keyOwner[string(b)]
					if !ok {
						continue
					}
					// composite: the row must reference the owner's table number
					if comps[j] != nil && !compositeMatch(r[typePos[j]], r[typePos[j]+1], tableNo(dirs[owner].t.ref.Name)) {
						continue
					}
					hits[owner]++
				}
			}
			for di, dt := range dirs {
				if hits[di] == 0 || dt.t == tt {
					continue
				}
				c, ok, err := d.linkedCandidate(dt, keyCount[di], tt, rc, comps[j])
				if err != nil {
					return err
				}
				if ok {
					*out = append(*out, c)
				}
			}
		}
	}
	return nil
}

func (d *discoverer) linkedCandidate(dt *dirTable, keys int, tt *tableInfo, link Column, comp *compositeCols) (model.DiscoveryCandidate, bool, error) {
	dates := tt.dateCols(d.opt.MaxDateColumns)
	lq := LinkQuery{
		Points: dt.t.ref, KeyCol: dt.key, Times: tt.ref, LinkCol: link.Name,
		DateCols: colNames(dates), Limit: d.opt.StatsRowLimit,
	}
	if comp != nil {
		lq.TypeCol, lq.RTRefCol, lq.RTRef = comp.typeCol, comp.rtrefCol, tableNo(dt.t.ref.Name)
	}
	var ls LinkStats
	ok, err := d.run(func(ctx context.Context) error {
		var e error
		ls, e = d.st.LinkStats(ctx, lq)
		return e
	})
	if err != nil || !ok || ls.Matched == 0 || ls.Sampled == 0 {
		return model.DiscoveryCandidate{}, false, err
	}
	ev := evaluateSeries(dates, ls.Max, d.now)
	c := model.DiscoveryCandidate{
		Mode: model.CandidateLinked, Schema: dt.t.ref.Schema, Table: dt.t.ref.Name,
		PointCol: dt.nameCol, PointIDCol: dt.codeCol, PointKeyCol: dt.key,
		TimesSchema: tt.ref.Schema, TimesTable: tt.ref.Name, LinkCol: link.Name,
		SendCol: ev.send, RecvCol: ev.recv, YearOffset: ev.offset, FreshestAt: ev.freshest,
		LinkTypeCol: lq.TypeCol, LinkRTRefCol: lq.RTRefCol, LinkRTRef: lq.RTRef,
	}
	ratio := float64(ls.Matched) / float64(ls.Sampled)
	dirRows := int64(keys)
	if n := d.rows(dt.t.ref); n > 0 {
		dirRows = n
	}
	coverage := 0.0
	if dirRows > 0 {
		coverage = float64(ls.Points) / float64(dirRows)
		if coverage > 1 {
			coverage = 1
		}
	}
	c.Score = 35 + ev.score + ev.fresh + int(10*ratio) + int(5*coverage)
	if re1CInfoRg.MatchString(tt.ref.Name) {
		c.Score += 2
	}
	c.Hints = append([]string{model.HintLinked}, ev.hints...)
	if dt.node {
		c.Score += nodeBonus
		c.Hints = append(c.Hints, model.HintNodeTable)
	}
	linkDesc := link.Name
	if comp != nil {
		c.Hints = append(c.Hints, model.HintCompositeRef)
		linkDesc = fmt.Sprintf("%s (composite: %s = 0x08, %s = %d)", link.Name, comp.typeCol, comp.rtrefCol, lq.RTRef)
	}
	c.Hints = append(c.Hints, d.sampledHint(tt.ref)...)
	c.Reason = fmt.Sprintf("points %s (%s) linked from %s.%s: %d of %d sampled rows match, %d of %d points; %s",
		dt.t.ref, dt.nameCol, tt.ref, linkDesc, ls.Matched, ls.Sampled, ls.Points, dirRows, ev.note)
	return c, true, nil
}

func (d *discoverer) singleCandidate(t *tableInfo, pointCol, idCol string, dates []Column, base int, hint string) (model.DiscoveryCandidate, bool, error) {
	var ss SingleStats
	ok, err := d.run(func(ctx context.Context) error {
		var e error
		ss, e = d.st.SingleStats(ctx, SingleQuery{Table: t.ref, PointCol: pointCol, DateCols: colNames(dates), Limit: d.opt.StatsRowLimit})
		return e
	})
	if err != nil || !ok || ss.Sampled == 0 {
		return model.DiscoveryCandidate{}, false, err
	}
	ev := evaluateSeries(dates, ss.Max, d.now)
	c := model.DiscoveryCandidate{
		Mode: model.CandidateSingle, Schema: t.ref.Schema, Table: t.ref.Name,
		PointCol: pointCol, PointIDCol: idCol, SendCol: ev.send, RecvCol: ev.recv,
		YearOffset: ev.offset, FreshestAt: ev.freshest,
	}
	c.Score = base + ev.score + ev.fresh
	c.Hints = append([]string{hint}, ev.hints...)
	c.Hints = append(c.Hints, d.sampledHint(t.ref)...)
	c.Reason = fmt.Sprintf("table %s: point column %s, %d rows, %d points; %s", t.ref, pointCol, ss.Sampled, ss.Points, ev.note)
	return c, true, nil
}

func (d *discoverer) discoverGeneric(tables []*tableInfo, out *[]model.DiscoveryCandidate) error {
	for _, t := range tables {
		if re1CStorage.MatchString(t.ref.Name) || !hasMarker(t) {
			continue
		}
		dates := t.dateCols(d.opt.MaxDateColumns)
		pc := pickPointCol(t)
		if len(dates) == 0 || pc == "" {
			continue
		}
		c, ok, err := d.singleCandidate(t, pc, "", dates, 25, model.HintGeneric)
		if err != nil {
			return err
		}
		if ok {
			*out = append(*out, c)
		}
	}
	return nil
}

// Per-point ranking thresholds.
const (
	freshWindow    = 7 * 24 * time.Hour // a point is "updated" within this of the freshest point
	smallPointsMax = 500                // 2..smallPointsMax points = a plausible exchange directory
)

// rank reads the best RankTop candidates per point through their mapping
// and adjusts the score: share of points updated close to the freshest
// one (up to +15), share of points with both series (up to +5), a small
// number of points (+5). SampleRows are filled from the same read.
func (d *discoverer) rank(cands []model.DiscoveryCandidate) error {
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })
	limit := d.opt.RankTop
	if limit > len(cands) {
		limit = len(cands)
	}
	for i := 0; i < limit; i++ {
		c := &cands[i]
		if c.Mode == model.CandidateCounters {
			continue // fixed low score, rows read in discoverCounters
		}
		var raw []RawPoint
		ok, err := d.run(func(ctx context.Context) error {
			var e error
			raw, e = d.st.Points(ctx, c.Mapping(), d.opt.MaxDirRows+1)
			return e
		})
		if errors.Is(err, errBudget) {
			return nil
		}
		if err != nil {
			return err
		}
		if !ok {
			c.Hints = append(c.Hints, model.HintSampleFailed)
			continue
		}
		applyPointStats(c, toSampleRows(raw, c.YearOffset), d.opt.SampleRows)
	}
	return nil
}

// applyPointStats scores the per-point distribution of a candidate.
func applyPointStats(c *model.DiscoveryCandidate, rows []model.SampleRow, sampleRows int) {
	n := len(rows)
	c.Points = n
	if n > sampleRows {
		c.SampleRows = rows[:sampleRows]
	} else {
		c.SampleRows = rows
	}
	if n == 0 {
		return
	}
	var freshest *time.Time
	fresh := make([]*time.Time, n)
	both := 0
	for i, r := range rows {
		p := model.ExchangePoint{LastSend: r.LastSend, LastRecv: r.LastRecv}
		if f, ok := p.FreshAt(); ok {
			fresh[i] = &f
			if freshest == nil || f.After(*freshest) {
				freshest = &f
			}
		}
		if r.LastSend != nil && r.LastRecv != nil {
			both++
		}
	}
	updated := 0
	if freshest != nil {
		for _, f := range fresh {
			if f != nil && freshest.Sub(*f) <= freshWindow {
				updated++
			}
		}
	}
	c.Score += 15*updated/n + 5*both/n
	switch {
	case n >= 2 && n <= smallPointsMax:
		c.Score += 5
	case n == 1:
		c.Score++
	}
	if n >= 2 && updated == n {
		c.Hints = append(c.Hints, model.HintAllPointsFresh)
	}
	c.Reason += fmt.Sprintf("; %d points, %d updated within 7 days of the freshest", n, updated)
}

// countersScore is the fixed low score of the counters fallback.
const countersScore = 5

// discoverCounters offers every 1C exchange-plan table with message
// counters as a low-score "counters" candidate: a fallback for
// configurations that store no exchange times at all.
func (d *discoverer) discoverCounters(tables []*tableInfo, out *[]model.DiscoveryCandidate) error {
	for _, t := range tables {
		if !re1CNode.MatchString(t.ref.Name) {
			continue
		}
		sent, ok1 := t.find(model.DefaultSentNoColumn)
		recv, ok2 := t.find(model.DefaultReceivedNoColumn)
		pc, ic := pointCols1C(t)
		if !ok1 || !ok2 || !sent.IsNumeric() || !recv.IsNumeric() || pc == "" {
			continue
		}
		c := model.DiscoveryCandidate{
			Mode: model.CandidateCounters, Schema: t.ref.Schema, Table: t.ref.Name,
			PointCol: pc, PointIDCol: ic, PointKeyCol: t.key1C(), SendCol: sent.Name, RecvCol: recv.Name,
			Score: countersScore, Hints: []string{model.HintCounters, model.HintNodeTable},
			Reason: fmt.Sprintf("fallback: message counters %s/%s of exchange-plan table %s; the service records when they "+
				"change (first poll after service start = baseline, the real time may be earlier)", sent.Name, recv.Name, t.ref),
		}
		var raw []RawCounter
		ok, err := d.run(func(ctx context.Context) error {
			var e error
			raw, e = d.st.Counters(ctx, c.Mapping(), d.opt.MaxDirRows+1)
			return e
		})
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		rows := counterSampleRows(raw)
		c.Points = len(rows)
		if len(rows) > d.opt.SampleRows {
			rows = rows[:d.opt.SampleRows]
		}
		c.SampleRows = rows
		*out = append(*out, c)
	}
	return nil
}
