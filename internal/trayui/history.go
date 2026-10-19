package trayui

import (
	"bytes"
	"html"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// History window and export of one point (phase 9b, CHK-TRAY-26 /
// CHK-TRAY-27). The service reads history only through IPC (history
// capability); pages are joined through the cursor.

// HistorySpan is a range of the history window.
type HistorySpan int

const (
	SpanDay  HistorySpan = iota // 24 h, bucket 5m
	SpanWeek                    // 7 days, bucket 1h
)

// SpanRange returns [from, to) ending at now and the bucket of the span.
func SpanRange(s HistorySpan, now time.Time) (from, to time.Time, bucket string) {
	to = now
	if s == SpanWeek {
		return to.Add(-7 * 24 * time.Hour), to, model.HistoryBucket1h
	}
	return to.Add(-24 * time.Hour), to, model.HistoryBucket5m
}

// HistoryRequest builds the history request of a point (pointKey only
// together with profileId, as the service requires).
func HistoryRequest(profileID, pointKey string, from, to time.Time, bucket string, kinds []string, cursor string) model.IPCRequest {
	f, t := from, to
	return model.IPCRequest{Cmd: model.CmdHistory, ProfileID: profileID, PointKey: pointKey, From: &f, To: &t,
		Bucket: bucket, Kinds: kinds, Cursor: cursor, Limit: model.HistoryMaxLimit}
}

// HistoryErrorText is the text of a history error reply / transport error.
func HistoryErrorText(m model.IPCMessage, err error, lang string) string {
	switch {
	case err != nil:
		return i18n.T(i18n.KeyHistFailed, lang) + ": " + err.Error()
	case m.ErrorKey != "":
		return i18n.Message(m.ErrorKey, lang)
	case m.Error != "":
		return i18n.T(i18n.KeyHistFailed, lang) + ": " + m.Error
	}
	return i18n.Message(model.MsgHistoryFailed, lang)
}

type bucketKey struct {
	start            int64
	kind, prof, pkey string
}

// BucketMerger joins the bucket pages: a bucket cut between two pages
// arrives twice with the same key and is merged (min / max, avg weighted
// by count).
type BucketMerger struct {
	m     map[bucketKey]*model.HistoryBucket
	order []bucketKey
}

func mergeAgg(a, b *model.HistoryAgg) *model.HistoryAgg {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	n := a.Count + b.Count
	r := &model.HistoryAgg{Min: a.Min, Max: a.Max, Count: n}
	if b.Min < r.Min {
		r.Min = b.Min
	}
	if b.Max > r.Max {
		r.Max = b.Max
	}
	if n > 0 {
		r.Avg = (a.Avg*float64(a.Count) + b.Avg*float64(b.Count)) / float64(n)
	}
	return r
}

// Add takes the buckets of one page.
func (bm *BucketMerger) Add(bs []model.HistoryBucket) {
	if bm.m == nil {
		bm.m = map[bucketKey]*model.HistoryBucket{}
	}
	for i := range bs {
		b := bs[i]
		k := bucketKey{b.Start.UnixNano(), b.Kind, b.ProfileID, b.PointKey}
		cur, ok := bm.m[k]
		if !ok {
			bm.m[k] = &b
			bm.order = append(bm.order, k)
			continue
		}
		cur.Records += b.Records
		cur.Errors += b.Errors
		if b.WorstLevel != nil && (cur.WorstLevel == nil || *b.WorstLevel > *cur.WorstLevel) {
			cur.WorstLevel = b.WorstLevel
		}
		cur.LagMin = mergeAgg(cur.LagMin, b.LagMin)
		cur.LatencyMs = mergeAgg(cur.LatencyMs, b.LatencyMs)
		cur.QueryMs = mergeAgg(cur.QueryMs, b.QueryMs)
	}
}

// Buckets returns the merged buckets sorted by start.
func (bm *BucketMerger) Buckets() []model.HistoryBucket {
	out := make([]model.HistoryBucket, 0, len(bm.order))
	for _, k := range bm.order {
		out = append(out, *bm.m[k])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// ChartPoint is one bucket of the lag curve.
type ChartPoint struct {
	At       time.Time
	Avg      float64
	Min, Max int64
	Worst    model.AlertLevel
}

// ChartMark is an event mark (overdue / recovery) on the time axis.
type ChartMark struct {
	At    time.Time
	Level model.AlertLevel
	Recov bool
}

// Chart is the model of the history chart of one point.
type Chart struct {
	From, To  time.Time
	Points    []ChartPoint
	Marks     []ChartMark
	Warn, Err int // thresholds, minutes (0 = unknown)
}

// BuildChart takes the merged point buckets of pointKey and the event
// records.
func BuildChart(from, to time.Time, buckets []model.HistoryBucket, events []model.HistoryRecord, pointKey string, warn, errMin int) Chart {
	c := Chart{From: from, To: to, Warn: warn, Err: errMin}
	for _, b := range buckets {
		if b.Kind != model.HistoryKindPoint || (pointKey != "" && b.PointKey != pointKey) || b.LagMin == nil || b.LagMin.Count == 0 {
			continue
		}
		p := ChartPoint{At: b.Start, Avg: b.LagMin.Avg, Min: b.LagMin.Min, Max: b.LagMin.Max}
		if b.WorstLevel != nil {
			p.Worst = *b.WorstLevel
		}
		c.Points = append(c.Points, p)
	}
	for _, r := range events {
		if r.Kind != model.HistoryKindEvent && r.Kind != model.HistoryKindRecovered {
			continue
		}
		if pointKey != "" && r.PointKey != "" && r.PointKey != pointKey {
			continue
		}
		m := ChartMark{At: r.CheckedAt, Recov: r.Kind == model.HistoryKindRecovered}
		if r.To != nil {
			m.Level = *r.To
			m.Recov = m.Recov || *r.To == model.LevelOK
		}
		c.Marks = append(c.Marks, m)
	}
	return c
}

// Pt is a pixel position.
type Pt struct{ X, Y int32 }

// ChartLayout is the chart in pixels of a plot rectangle.
type ChartLayout struct {
	Left, Top, Right, Bottom int32
	MaxY                     float64 // minutes at Top
	Avg, MinL, MaxL          [][]Pt  // polylines (a gap > 2 buckets breaks the line)
	WarnY, ErrY              int32   // -1 = not drawn
	Marks                    []struct {
		X    int32
		Mark ChartMark
	}
	YTicks []struct {
		Y   int32
		Min int
	}
	XTicks []struct {
		X    int32
		Text string
	}
}

func niceCeil(v float64) float64 {
	if v <= 10 {
		return 10
	}
	p := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 5, 10} {
		if v <= m*p {
			return m * p
		}
	}
	return 10 * p
}

// Layout maps the chart to the rectangle (l, t)-(r, b).
func (c Chart) Layout(l, t, r, b int32, gap time.Duration) ChartLayout {
	L := ChartLayout{Left: l, Top: t, Right: r, Bottom: b, WarnY: -1, ErrY: -1}
	maxV := float64(c.Err)
	if float64(c.Warn) > maxV {
		maxV = float64(c.Warn)
	}
	for _, p := range c.Points {
		if float64(p.Max) > maxV {
			maxV = float64(p.Max)
		}
	}
	L.MaxY = niceCeil(maxV * 1.1)
	span := c.To.Sub(c.From)
	if span <= 0 || r <= l || b <= t {
		return L
	}
	x := func(at time.Time) int32 {
		return l + int32(float64(r-l)*float64(at.Sub(c.From))/float64(span))
	}
	y := func(v float64) int32 {
		if v < 0 {
			v = 0
		}
		if v > L.MaxY {
			v = L.MaxY
		}
		return b - int32(float64(b-t)*v/L.MaxY)
	}
	var avg, mn, mx []Pt
	var last time.Time
	flush := func() {
		if len(avg) > 0 {
			L.Avg, L.MinL, L.MaxL = append(L.Avg, avg), append(L.MinL, mn), append(L.MaxL, mx)
		}
		avg, mn, mx = nil, nil, nil
	}
	for _, p := range c.Points {
		if p.At.Before(c.From) || !p.At.Before(c.To) {
			continue
		}
		if !last.IsZero() && gap > 0 && p.At.Sub(last) > 2*gap {
			flush()
		}
		last = p.At
		X := x(p.At.Add(gap / 2))
		avg = append(avg, Pt{X, y(p.Avg)})
		mn = append(mn, Pt{X, y(float64(p.Min))})
		mx = append(mx, Pt{X, y(float64(p.Max))})
	}
	flush()
	if c.Warn > 0 {
		L.WarnY = y(float64(c.Warn))
	}
	if c.Err > 0 {
		L.ErrY = y(float64(c.Err))
	}
	for _, m := range c.Marks {
		if m.At.Before(c.From) || m.At.After(c.To) {
			continue
		}
		L.Marks = append(L.Marks, struct {
			X    int32
			Mark ChartMark
		}{x(m.At), m})
	}
	for i := 0; i <= 4; i++ {
		v := L.MaxY * float64(i) / 4
		L.YTicks = append(L.YTicks, struct {
			Y   int32
			Min int
		}{y(v), int(v)})
	}
	layout := "15:04"
	if span > 36*time.Hour {
		layout = "02.01"
	}
	for i := 0; i <= 6; i++ {
		at := c.From.Add(span * time.Duration(i) / 6)
		L.XTicks = append(L.XTicks, struct {
			X    int32
			Text string
		}{x(at), at.In(model.DBZone).Format(layout)})
	}
	return L
}

// ---- history export (CHK-TRAY-27) ----

// HistoryColumns are the export header keys.
var HistoryColumns = []string{i18n.KeyHistColTime, i18n.KeyStDatabase, i18n.KeyStPoint, i18n.KeyHistColKind,
	i18n.KeyStState, i18n.KeyHistColLag, i18n.KeyStFresh, i18n.KeyHistColLatency, i18n.KeyHistColChange,
	i18n.KeyHistColDuration, i18n.KeyStNote}

func lvlText(l *model.AlertLevel, lang string) string {
	if l == nil {
		return ""
	}
	return levelText(*l, lang)
}

func kindText(k, lang string) string {
	switch k {
	case model.HistoryKindPoint:
		return i18n.T(i18n.KeyHistKindPoint, lang)
	case model.HistoryKindProfile:
		return i18n.T(i18n.KeyHistKindProfile, lang)
	case model.HistoryKindEvent:
		return i18n.T(i18n.KeyHistKindEvent, lang)
	case model.HistoryKindRecovered:
		return i18n.T(i18n.KeyHistKindRecovered, lang)
	}
	return k
}

// HistoryCells is one record in HistoryColumns order.
func HistoryCells(r model.HistoryRecord, lang string) []string {
	c := make([]string, len(HistoryColumns))
	c[0] = r.CheckedAt.In(model.DBZone).Format("02.01.2006 15:04:05")
	c[1], c[2], c[3] = r.ProfileName, r.PointName, kindText(r.Kind, lang)
	if c[1] == "" {
		c[1] = r.ProfileID
	}
	c[4] = lvlText(r.Level, lang)
	if r.LagMin != nil {
		c[5] = strconv.Itoa(*r.LagMin)
	}
	if r.FreshAt != nil {
		c[6] = r.FreshAt.In(model.DBZone).Format("02.01.2006 15:04")
	}
	if r.LatencyMs != nil {
		c[7] = strconv.FormatInt(*r.LatencyMs, 10)
	}
	if r.From != nil || r.To != nil {
		c[8] = lvlText(r.From, lang) + " → " + lvlText(r.To, lang)
	}
	if r.DurationMin != nil {
		c[9] = strconv.FormatInt(*r.DurationMin, 10)
	}
	switch {
	case r.ErrorKey != "":
		c[10] = i18n.Message(r.ErrorKey, lang)
	case r.Baseline:
		c[10] = i18n.T(i18n.KeyPtBaseline, lang)
	}
	return c
}

func headersOf(keys []string, lang string) []string {
	h := make([]string, len(keys))
	for i, k := range keys {
		h[i] = i18n.T(k, lang)
	}
	return h
}

// ExportHistoryCSV renders the records (same rules as ExportCSV); partial
// (a record / page limit was hit) adds a last line «показаны не все записи».
func ExportHistoryCSV(recs []model.HistoryRecord, lang string, partial bool) []byte {
	var b bytes.Buffer
	b.WriteString("\uFEFF")
	line := func(cells []string) {
		for i, c := range cells {
			if i > 0 {
				b.WriteByte(';')
			}
			b.WriteString(csvCell(c))
		}
		b.WriteString("\r\n")
	}
	line(headersOf(HistoryColumns, lang))
	for _, r := range recs {
		line(HistoryCells(r, lang))
	}
	if partial {
		line([]string{i18n.T(i18n.KeyHistPartial, lang)})
	}
	return b.Bytes()
}

// ExportHistoryHTML renders the records as a page; title names the point.
func ExportHistoryHTML(recs []model.HistoryRecord, title string, from, to time.Time, lang string, partial bool) []byte {
	var b bytes.Buffer
	e := html.EscapeString
	b.WriteString("<!DOCTYPE html>\n<html lang=\"" + e(lang) + "\"><head><meta charset=\"utf-8\"><title>" + e(title) + "</title>\n")
	b.WriteString("<style>body{font-family:Segoe UI,Arial,sans-serif;font-size:13px}table{border-collapse:collapse}" +
		"td,th{border:1px solid #999;padding:3px 6px;text-align:left}th{background:#eee}</style></head><body>\n")
	b.WriteString("<h1>" + e(title) + "</h1>\n<p>" + e(from.In(model.DBZone).Format("02.01.2006 15:04")) + " — " +
		e(to.In(model.DBZone).Format("02.01.2006 15:04")) + " (UTC+3)</p>\n")
	if partial {
		b.WriteString("<p><strong>" + e(i18n.T(i18n.KeyHistPartial, lang)) + "</strong></p>\n")
	}
	b.WriteString("<table>\n<tr>")
	for _, h := range headersOf(HistoryColumns, lang) {
		b.WriteString("<th>" + e(h) + "</th>")
	}
	b.WriteString("</tr>\n")
	for _, r := range recs {
		b.WriteString("<tr")
		lv := r.Level
		if lv == nil {
			lv = r.To
		}
		if lv != nil {
			if c, ok := htmlColors[LevelColor(*lv)]; ok {
				b.WriteString(" style=\"background:" + c + "\"")
			}
		}
		b.WriteString(">")
		for _, c := range HistoryCells(r, lang) {
			b.WriteString("<td>" + e(c) + "</td>")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</table>\n</body></html>\n")
	return b.Bytes()
}
