package sqlcheck

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// valueMatch is a row of a small table whose string column contains the
// searched point name.
type valueMatch struct {
	t       *tableInfo
	textCol string
	value   string
	exact   bool
	key     []byte       // _IDRRef of the row (nil if the table has none)
	dates   []Column     // datetime columns of t
	dateVal []*time.Time // their values in the matched row (naive)
}

// ErrEmptyPointName is returned by DiscoverByValue for an empty name.
var ErrEmptyPointName = errors.New("point name is empty")

// DiscoverByValue is the main Discovery tool for custom 1C databases
// (CHK-SQL-02): the user types the name of a known exchange point and,
// optionally, the approximate time of its last exchange.
//
//  1. The name is searched (LIKE, parameterized, literal substring) in the
//     bounded string columns of tables with at most MaxValueRows rows
//     (row counts from sys.partitions / sysindexes), smallest first, at most
//     MaxValueTables tables, TOP MatchRows rows each.
//  2. For every matching row: datetime columns of the same row give a
//     same-table candidate; the row's _IDRRef is searched in binary(16)
//     columns of tables with datetime columns (<= MaxLinkedRows rows, at
//     most MaxLinkTables) and gives linked candidates.
//  3. Score: exact name match, timestamps near ApproxTime (or close to now
//     when no time is given), two series, _Node bonus. SampleRows are read
//     through the resulting mapping.
//
// Same limits as Discover: read-only NOLOCK queries, per-query timeout,
// total time budget (partial result + model.HintTruncated).
func DiscoverByValue(ctx context.Context, st Store, q model.ValueQuery, opt DiscoverOptions, now time.Time) ([]model.DiscoveryCandidate, error) {
	name := strings.TrimSpace(q.PointName)
	if name == "" {
		return nil, &Error{Kind: KindConfig, Op: "discover by value", Err: ErrEmptyPointName}
	}
	d, cancel := newDiscoverer(ctx, st, opt, now)
	defer cancel()
	tables, err := d.catalog()
	if err != nil {
		return nil, err
	}
	matches, err := d.searchName(tables, name)
	var cands []model.DiscoveryCandidate
	if err == nil {
		err = d.followMatches(tables, matches, q, &cands)
	}
	if err != nil && !errors.Is(err, errBudget) {
		return nil, err
	}
	out := d.top(cands)
	for i := range out {
		c := &out[i]
		var raw []RawPoint
		ok, err := d.run(func(ctx context.Context) error {
			var e error
			raw, e = d.st.Points(ctx, c.Mapping(), d.opt.SampleRows)
			return e
		})
		if errors.Is(err, errBudget) {
			break
		}
		if err != nil {
			return nil, err
		}
		if !ok {
			c.Hints = append(c.Hints, model.HintSampleFailed)
			continue
		}
		c.SampleRows = toSampleRows(raw, c.YearOffset)
	}
	d.finishHints(out)
	return out, nil
}

// searchName finds rows containing name in small tables.
func (d *discoverer) searchName(tables []*tableInfo, name string) ([]valueMatch, error) {
	var search []*tableInfo
	for _, t := range tables {
		if n := d.rows(t.ref); n == 0 || n > int64(d.opt.MaxValueRows) {
			continue
		}
		if d.counts != nil && d.rows(t.ref) < 0 {
			continue // counts are available but not for this table: size unknown, skip
		}
		if len(t.textCols(d.opt.MaxTextLength, 1)) > 0 {
			search = append(search, t)
		}
	}
	d.sortBySize(search, func(t *tableInfo) bool { return re1CDirectory.MatchString(t.ref.Name) })
	if len(search) > d.opt.MaxValueTables {
		search = search[:d.opt.MaxValueTables]
	}
	pattern := LikeContains(name)
	best := map[string]int{} // table+column -> index in out
	var out []valueMatch
	for _, t := range search {
		texts := t.textCols(d.opt.MaxTextLength, d.opt.MaxTextColumns)
		dates := t.dateCols(d.opt.MaxDateColumns)
		key := t.key1C()
		sel := colNames(texts)
		if key != "" {
			sel = append(sel, key)
		}
		sel = append(sel, colNames(dates)...)
		var rows [][]interface{}
		ok, err := d.run(func(ctx context.Context) error {
			var e error
			rows, e = d.st.SearchText(ctx, t.ref, sel, colNames(texts), pattern, d.opt.MatchRows)
			return e
		})
		if err != nil {
			return out, err
		}
		if !ok {
			continue
		}
		for _, r := range rows {
			m, ok := matchRow(t, texts, key != "", dates, r, name)
			if !ok {
				continue
			}
			k := t.ref.String() + "\x00" + m.textCol
			if i, seen := best[k]; seen {
				if m.exact && !out[i].exact {
					out[i] = m
				}
				continue
			}
			if len(out) >= d.opt.MaxMatches {
				continue
			}
			best[k] = len(out)
			out = append(out, m)
		}
	}
	return out, nil
}

// matchRow decodes one SearchText row: texts..., [key], dates...
func matchRow(t *tableInfo, texts []Column, hasKey bool, dates []Column, r []interface{}, name string) (valueMatch, bool) {
	m := valueMatch{t: t, dates: dates}
	lname := strings.ToLower(name)
	for i, c := range texts {
		v := strings.TrimSpace(valueString(r[i]))
		if v == "" {
			continue
		}
		lv := strings.ToLower(v)
		exact := lv == lname
		if !exact && !strings.Contains(lv, lname) {
			continue
		}
		if m.textCol == "" || exact && !m.exact {
			m.textCol, m.value, m.exact = c.Name, v, exact
		}
	}
	if m.textCol == "" {
		// The server collation matched (e.g. accent-insensitive) but Go did
		// not: take the first non-empty string column.
		for i, c := range texts {
			if v := strings.TrimSpace(valueString(r[i])); v != "" {
				m.textCol, m.value = c.Name, v
				break
			}
		}
	}
	if m.textCol == "" {
		return m, false
	}
	pos := len(texts)
	if hasKey {
		if b, ok := r[pos].([]byte); ok && len(b) == 16 {
			m.key = append([]byte(nil), b...)
		}
		pos++
	}
	m.dateVal = make([]*time.Time, len(dates))
	for i := range dates {
		if tv, ok := r[pos+i].(time.Time); ok {
			m.dateVal[i] = &tv
		}
	}
	return m, true
}

// followMatches builds same-table and linked candidates for the matches.
func (d *discoverer) followMatches(tables []*tableInfo, matches []valueMatch, q model.ValueQuery, out *[]model.DiscoveryCandidate) error {
	var times []*tableInfo
	for _, t := range tables {
		if n := d.rows(t.ref); n == 0 || n > int64(d.opt.MaxLinkedRows) {
			continue
		}
		if len(t.refCols()) > 0 && len(t.dateCols(1)) > 0 {
			times = append(times, t)
		}
	}
	d.sortBySize(times, func(t *tableInfo) bool { return re1CInfoRg.MatchString(t.ref.Name) })
	if len(times) > d.opt.MaxLinkTables {
		times = times[:d.opt.MaxLinkTables]
	}
	for _, m := range matches {
		if len(m.dates) > 0 {
			ev := evaluateSeries(m.dates, m.dateVal, d.now)
			if ev.freshest != nil {
				c := model.DiscoveryCandidate{
					Mode: model.CandidateSingle, Schema: m.t.ref.Schema, Table: m.t.ref.Name,
					PointCol: m.textCol, SendCol: ev.send, RecvCol: ev.recv, YearOffset: ev.offset,
					FreshestAt: ev.freshest,
				}
				d.scoreByValue(&c, m, ev, q, model.HintSameTable)
				c.Reason = fmt.Sprintf("%q found in %s.%s; timestamps in the same row; %s", m.value, m.t.ref, m.textCol, ev.note)
				*out = append(*out, c)
			}
		}
		if m.key == nil {
			continue
		}
		for _, tt := range times {
			if tt == m.t {
				continue
			}
			if err := d.followRef(m, tt, q, out); err != nil {
				return err
			}
		}
	}
	return nil
}

// followRef searches the matched row's key in tt's reference columns.
func (d *discoverer) followRef(m valueMatch, tt *tableInfo, q model.ValueQuery, out *[]model.DiscoveryCandidate) error {
	refs := tt.refCols()
	dates := tt.dateCols(d.opt.MaxDateColumns)
	sel := append(colNames(refs), colNames(dates)...)
	var rows [][]interface{}
	ok, err := d.run(func(ctx context.Context) error {
		var e error
		rows, e = d.st.SearchRef(ctx, tt.ref, sel, colNames(refs), m.key, d.opt.MatchRows)
		return e
	})
	if err != nil || !ok || len(rows) == 0 {
		return err
	}
	// Per link column: MAX() of each datetime column over this point's rows.
	for j, rc := range refs {
		maxes := make([]*time.Time, len(dates))
		hit := false
		for _, r := range rows {
			b, ok := r[j].([]byte)
			if !ok || string(b) != string(m.key) {
				continue
			}
			hit = true
			for i := range dates {
				if tv, ok := r[len(refs)+i].(time.Time); ok && (maxes[i] == nil || tv.After(*maxes[i])) {
					v := tv
					maxes[i] = &v
				}
			}
		}
		if !hit {
			continue
		}
		ev := evaluateSeries(dates, maxes, d.now)
		pc, ic := m.textCol, ""
		if p1c, c1c := pointCols1C(m.t); p1c == m.textCol {
			ic = c1c
		}
		c := model.DiscoveryCandidate{
			Mode: model.CandidateLinked, Schema: m.t.ref.Schema, Table: m.t.ref.Name,
			PointCol: pc, PointIDCol: ic, PointKeyCol: m.t.key1C(),
			TimesSchema: tt.ref.Schema, TimesTable: tt.ref.Name, LinkCol: rc.Name,
			SendCol: ev.send, RecvCol: ev.recv, YearOffset: ev.offset, FreshestAt: ev.freshest,
		}
		d.scoreByValue(&c, m, ev, q, model.HintLinked)
		if re1CInfoRg.MatchString(tt.ref.Name) {
			c.Score += 2
		}
		c.Reason = fmt.Sprintf("%q found in %s.%s; linked rows in %s.%s; %s", m.value, m.t.ref, m.textCol, tt.ref, rc.Name, ev.note)
		*out = append(*out, c)
	}
	return nil
}

// scoreByValue: base 50, exact match +15 (substring +5), closeness to
// ApproxTime (<= 1h +30, <= tolerance +20, <= 7 days +8) or, without a
// time, freshness against now; series/name points; _Node bonus +10.
func (d *discoverer) scoreByValue(c *model.DiscoveryCandidate, m valueMatch, ev seriesEval, q model.ValueQuery, modeHint string) {
	c.Hints = append([]string{model.HintByValue, modeHint}, ev.hints...)
	c.Score = 50 + ev.score
	if m.exact {
		c.Score += 15
		c.Hints = append(c.Hints, model.HintExactMatch)
	} else {
		c.Score += 5
	}
	send, recv := ev.sendRecvAt()
	c.Matched = &model.SampleRow{Point: m.value, LastSend: send, LastRecv: recv}
	if q.ApproxTime != nil {
		best := time.Duration(-1)
		for _, t := range []*time.Time{send, recv} {
			if t == nil {
				continue
			}
			delta := t.Sub(*q.ApproxTime)
			if delta < 0 {
				delta = -delta
			}
			if best < 0 || delta < best {
				best = delta
			}
		}
		switch {
		case best < 0:
		case best <= time.Hour:
			c.Score += 30
		case best <= q.Tolerance():
			c.Score += 20
		case best <= 7*24*time.Hour:
			c.Score += 8
		}
		if best >= 0 && best <= q.Tolerance() {
			c.Hints = append(c.Hints, model.HintNearApproxTime)
		}
	} else {
		c.Score += ev.fresh
	}
	if re1CNode.MatchString(m.t.ref.Name) {
		c.Score += 10
		c.Hints = append(c.Hints, model.HintNodeTable)
	}
}
