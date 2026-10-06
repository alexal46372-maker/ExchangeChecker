package sqlcheck

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Time-based DiscoverByValue (customer clarification): the user picks a
// known point of an exchange plan (_NodeNNN: by _IDRRef, name or code) and
// the approximate time of its last exchange; the service looks for
// datetime columns near that time in rows linked to the point.

// nodePoint is a resolved exchange-plan row.
type nodePoint struct {
	t       *tableInfo
	key     []byte
	name    string
	code    string
	nameCol string
	codeCol string
	dates   []Column
	dateVal []*time.Time
}

// ErrBadPointRef is returned for a ValueQuery.PointRef that is not 16 bytes of hex.
var ErrBadPointRef = errors.New("point ref must be 32 hex digits (_IDRRef)")

func parsePointRef(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "0x"), "0X")
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return nil, ErrBadPointRef
	}
	return b, nil
}

// nodeTables returns the exchange-plan tables with an _IDRRef key.
func nodeTables(tables []*tableInfo, only string) []*tableInfo {
	var out []*tableInfo
	for _, t := range tables {
		if !re1CNode.MatchString(t.ref.Name) || t.key1C() == "" {
			continue
		}
		if only != "" && !strings.EqualFold(t.ref.Name, only) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// resolveNodePoints finds the chosen point in the exchange-plan tables:
// by _IDRRef (q.PointRef), else by exact name or code (case-insensitive),
// else by substring.
func (d *discoverer) resolveNodePoints(tables []*tableInfo, q model.ValueQuery, ref []byte) ([]nodePoint, error) {
	name := strings.TrimSpace(q.PointName)
	var exact, partial []nodePoint
	for _, t := range nodeTables(tables, strings.TrimSpace(q.PlanTable)) {
		pc, ic := pointCols1C(t)
		key := t.key1C()
		dates := t.dateCols(d.opt.MaxDateColumns)
		sel := []string{key}
		for _, c := range []string{pc, ic} {
			if c != "" {
				sel = append(sel, c)
			}
		}
		nsel := len(sel)
		sel = append(sel, colNames(dates)...)
		var rows [][]interface{}
		ok, err := d.run(func(ctx context.Context) error {
			var e error
			if ref != nil {
				rows, e = d.st.SearchRef(ctx, t.ref, sel, []string{key}, ref, d.opt.MatchRows)
				return e
			}
			var texts []string
			for _, c := range []string{pc, ic} {
				if col, ok := t.find(c); c != "" && ok && col.IsText() {
					texts = append(texts, c)
				}
			}
			if len(texts) == 0 {
				return nil
			}
			rows, e = d.st.SearchText(ctx, t.ref, sel, texts, LikeContains(name), d.opt.MatchRows)
			return e
		})
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, r := range rows {
			np := nodePoint{t: t, nameCol: pc, codeCol: ic, dates: dates}
			if b, ok := r[0].([]byte); ok && len(b) == 16 {
				np.key = append([]byte(nil), b...)
			} else {
				continue
			}
			pos := 1
			if pc != "" {
				np.name = strings.TrimSpace(valueString(r[pos]))
				pos++
			}
			if ic != "" {
				np.code = strings.TrimSpace(valueString(r[pos]))
			}
			np.dateVal = make([]*time.Time, len(dates))
			for i := range dates {
				if tv, ok := r[nsel+i].(time.Time); ok {
					np.dateVal[i] = &tv
				}
			}
			switch {
			case ref != nil, strings.EqualFold(np.name, name), strings.EqualFold(np.code, name):
				exact = append(exact, np)
			default:
				partial = append(partial, np)
			}
		}
	}
	if len(exact) > 0 {
		return exact, nil
	}
	if len(partial) > d.opt.MaxMatches {
		partial = partial[:d.opt.MaxMatches]
	}
	return partial, nil
}

// timeMatch is the evaluation of one table / link column for a point.
type timeMatch struct {
	ev    seriesEval
	best  time.Duration
	near  int
	dates []Column
}

// nearColumns keeps the datetime columns whose (point) maximum, after the
// detected year offset and read as UTC+3, is within tolerance of approx.
func nearColumns(dates []Column, maxes []*time.Time, approx time.Time, tol time.Duration, now time.Time) (timeMatch, bool) {
	off := DetectYearOffset(maxes, now)
	var nd []Column
	var nm []*time.Time
	best := time.Duration(-1)
	for i, c := range dates {
		at := ConvertDBTime(maxes[i], off)
		if at == nil {
			continue
		}
		delta := at.Sub(approx)
		if delta < 0 {
			delta = -delta
		}
		if delta > tol {
			continue
		}
		nd = append(nd, c)
		nm = append(nm, maxes[i])
		if best < 0 || delta < best {
			best = delta
		}
	}
	if len(nd) == 0 {
		return timeMatch{}, false
	}
	ev := evaluateSeries(nd, nm, now)
	return timeMatch{ev: ev, best: best, near: len(nd), dates: nd}, true
}

// closenessPoints scores the distance between the best column and approx.
func closenessPoints(best time.Duration) int {
	switch {
	case best <= 15*time.Minute:
		return 35
	case best <= time.Hour:
		return 30
	case best <= 6*time.Hour:
		return 25
	}
	return 20
}

// discoverByTime builds candidates for the chosen exchange-plan point:
// same-row datetime columns of the plan table and rows of other tables
// linked to the point by a simple (_FldNNNRRef) or composite
// (_TYPE/_RTRef/_RRRef, RTRef = plan table number) reference whose
// datetime columns are within the tolerance of q.ApproxTime. resolved
// reports whether the point was found in a plan table.
func (d *discoverer) discoverByTime(tables []*tableInfo, q model.ValueQuery, ref []byte) (cands []model.DiscoveryCandidate, resolved bool, err error) {
	points, err := d.resolveNodePoints(tables, q, ref)
	if err != nil || len(points) == 0 {
		return nil, false, err
	}
	approx, tol := q.ApproxTime.In(model.DBZone), q.Tolerance()
	var times []*tableInfo
	for _, t := range tables {
		if n := d.rows(t.ref); n == 0 || n > int64(d.opt.MaxLinkedRows) {
			continue
		}
		if len(t.refCols()) > 0 && len(t.dateCols(1)) > 0 && !re1CNode.MatchString(t.ref.Name) {
			times = append(times, t)
		}
	}
	d.sortBySize(times, func(t *tableInfo) bool { return re1CInfoRg.MatchString(t.ref.Name) })
	if len(times) > d.opt.MaxLinkTables {
		times = times[:d.opt.MaxLinkTables]
	}
	for _, np := range points {
		label := np.name
		if label == "" {
			label = np.code
		}
		base := model.DiscoveryCandidate{
			Schema: np.t.ref.Schema, Table: np.t.ref.Name, PointCol: np.nameCol, PointIDCol: np.codeCol,
		}
		if len(np.dates) > 0 {
			if tm, ok := nearColumns(np.dates, np.dateVal, approx, tol, d.now); ok {
				c := base
				c.Mode = model.CandidateSingle
				d.scoreByTime(&c, tm, label, model.HintSameTable)
				c.Reason = fmt.Sprintf("point %q of %s: %d datetime column(s) of the same row within %s of %s; %s",
					label, np.t.ref, tm.near, tol, approx.Format("2006-01-02 15:04"), tm.ev.note)
				cands = append(cands, c)
			}
		}
		planNo := tableNo(np.t.ref.Name)
		for _, tt := range times {
			dates := tt.dateCols(d.opt.MaxDateColumns)
			for _, rc := range tt.refCols() {
				cc, composite := tt.compositeOf(rc)
				var rows [][]interface{}
				ok, err := d.run(func(ctx context.Context) error {
					var e error
					if composite {
						rows, e = d.st.SearchComposite(ctx, tt.ref, colNames(dates),
							CompositeRef{RRefCol: rc.Name, TypeCol: cc.typeCol, RTRefCol: cc.rtrefCol, RTRef: planNo}, np.key, d.opt.MatchRows)
					} else {
						rows, e = d.st.SearchRef(ctx, tt.ref, colNames(dates), []string{rc.Name}, np.key, d.opt.MatchRows)
					}
					return e
				})
				if err != nil {
					return cands, true, err
				}
				if !ok || len(rows) == 0 {
					continue
				}
				maxes := make([]*time.Time, len(dates))
				for _, r := range rows {
					for i := range dates {
						maxes[i] = maxTimeValue(maxes[i], r[i])
					}
				}
				tm, ok := nearColumns(dates, maxes, approx, tol, d.now)
				if !ok {
					continue
				}
				c := base
				c.Mode = model.CandidateLinked
				c.PointKeyCol = np.t.key1C()
				c.TimesSchema, c.TimesTable, c.LinkCol = tt.ref.Schema, tt.ref.Name, rc.Name
				d.scoreByTime(&c, tm, label, model.HintLinked)
				linkDesc := rc.Name
				if composite {
					c.LinkTypeCol, c.LinkRTRefCol, c.LinkRTRef = cc.typeCol, cc.rtrefCol, planNo
					c.Hints = append(c.Hints, model.HintCompositeRef)
					linkDesc = fmt.Sprintf("%s (composite, %s = %d)", rc.Name, cc.rtrefCol, planNo)
				}
				if re1CInfoRg.MatchString(tt.ref.Name) {
					c.Score += 2
				}
				c.Reason = fmt.Sprintf("point %q of %s: %d datetime column(s) of %s linked by %s within %s of %s; %s",
					label, np.t.ref, tm.near, tt.ref, linkDesc, tol, approx.Format("2006-01-02 15:04"), tm.ev.note)
				cands = append(cands, c)
			}
		}
	}
	return cands, true, nil
}

// scoreByTime: base 60, closeness of the best column (+20..+35), series
// points (two near columns +10, name hints), _Node bonus.
func (d *discoverer) scoreByTime(c *model.DiscoveryCandidate, tm timeMatch, label, modeHint string) {
	ev := tm.ev
	c.SendCol, c.RecvCol, c.YearOffset, c.FreshestAt = ev.send, ev.recv, ev.offset, ev.freshest
	c.Hints = append([]string{model.HintByValue, model.HintByTime, modeHint, model.HintNearApproxTime}, ev.hints...)
	c.Score = 60 + closenessPoints(tm.best) + ev.score
	if re1CNode.MatchString(c.Table) {
		c.Score += nodeBonus
		c.Hints = append(c.Hints, model.HintNodeTable)
	}
	send, recv := ev.sendRecvAt()
	c.Matched = &model.SampleRow{Point: label, LastSend: send, LastRecv: recv}
}

func maxTimeValue(cur *time.Time, v interface{}) *time.Time {
	t, ok := v.(time.Time)
	if !ok {
		return cur
	}
	if cur == nil || t.After(*cur) {
		return &t
	}
	return cur
}

// ListNodePoints lists the rows of all exchange-plan tables (_NodeNNN) so
// the user can pick a known point for the time-based DiscoverByValue.
// Read-only (NOLOCK), at most limit rows per table.
func ListNodePoints(ctx context.Context, st Store, opt DiscoverOptions, limit int) ([]model.NodePoint, error) {
	out, err := listNodePoints(ctx, st, opt, limit)
	if err != nil {
		return nil, ctxErr("node points", err)
	}
	return out, nil
}

// listNodePoints is ListNodePoints without the final error normalization.
func listNodePoints(ctx context.Context, st Store, opt DiscoverOptions, limit int) ([]model.NodePoint, error) {
	if limit <= 0 {
		limit = 1000
	}
	d, cancel := newDiscoverer(ctx, st, opt, time.Now())
	defer cancel()
	tables, err := d.catalog()
	if err != nil {
		return nil, err
	}
	var out []model.NodePoint
	for _, t := range nodeTables(tables, "") {
		pc, ic := pointCols1C(t)
		sel := []string{t.key1C()}
		for _, c := range []string{pc, ic} {
			if c != "" {
				sel = append(sel, c)
			}
		}
		var rows [][]interface{}
		ok, err := d.run(func(ctx context.Context) error {
			var e error
			rows, e = d.st.SampleColumns(ctx, t.ref, sel, limit)
			return e
		})
		if err != nil {
			if errors.Is(err, errBudget) {
				break
			}
			return nil, err
		}
		if !ok {
			continue
		}
		for _, r := range rows {
			np := model.NodePoint{PlanTable: t.ref.Name, PlanNo: model.PlanNoOf(t.ref.Name)}
			if b, ok := r[0].([]byte); ok {
				np.Ref = hex.EncodeToString(b)
			}
			pos := 1
			if pc != "" {
				np.Name = strings.TrimSpace(valueString(r[pos]))
				pos++
			}
			if ic != "" {
				np.Code = strings.TrimSpace(valueString(r[pos]))
			}
			if np.Name == "" {
				np.Name = np.Code
			}
			out = append(out, np)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PlanTable != out[j].PlanTable {
			return out[i].PlanTable < out[j].PlanTable
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
