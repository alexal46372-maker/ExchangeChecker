package sqlcheck

import (
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// 1C 8.x storage names are not descriptive: _Node123, _Reference77,
// _Document5, _InfoRg88, tabular sections _Reference77_VT80, columns
// _FldNNN, references binary(16) _IDRRef / _FldNNNRRef.
var (
	re1CStorage = regexp.MustCompile(`^_[A-Za-z]+[0-9]+`)
	// re1CDirectory: tables that can hold a points directory.
	re1CDirectory = regexp.MustCompile(`(?i)^_(Reference|Node|Document|InfoRg)[0-9]+$`)
	re1CNode      = regexp.MustCompile(`(?i)^_Node[0-9]+$`)
	re1CInfoRg    = regexp.MustCompile(`(?i)^_InfoRg[0-9]+`)
	// re1CTableNo: object tables whose number is the RTRef type number.
	re1CTableNo = regexp.MustCompile(`(?i)^_(Node|Reference|Document)([0-9]+)$`)
)

// nodeBonus is added to every candidate whose points table is a 1C
// exchange plan _NodeNNN: the plans are the PRIMARY source of points
// (customer clarification, PLAN.md §12), so their candidates must clearly
// outrank generic / currency-like distractors.
const nodeBonus = 40

// tableNo returns the type number NNN of _NodeNNN / _ReferenceNNN /
// _DocumentNNN (the value stored in composite _RTRef columns), else 0.
func tableNo(name string) int {
	m := re1CTableNo.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	n := 0
	for _, r := range m[2] {
		n = n*10 + int(r-'0')
		if n > model.MaxRTRef {
			return 0
		}
	}
	return n
}

// compositeCols are the sibling columns of a composite _FldNNN_RRRef.
type compositeCols struct {
	typeCol  string // _FldNNN_TYPE, binary(1)
	rtrefCol string // _FldNNN_RTRef, binary(4)
}

// compositeOf reports whether binary(16) column c is the _RRRef part of a
// 1C 8.1 composite reference with _TYPE (binary(1)) and _RTRef
// (binary(4)) siblings.
func (t *tableInfo) compositeOf(c Column) (compositeCols, bool) {
	const suffix = "_rrref"
	if !c.IsRef16() || len(c.Name) <= len(suffix) || strings.ToLower(c.Name[len(c.Name)-len(suffix):]) != suffix {
		return compositeCols{}, false
	}
	prefix := c.Name[:len(c.Name)-len(suffix)]
	tc, ok1 := t.find(prefix + "_TYPE")
	rc, ok2 := t.find(prefix + "_RTRef")
	if !ok1 || !ok2 || tc.typ() != "binary" || tc.MaxLength != 1 || rc.typ() != "binary" || rc.MaxLength != 4 {
		return compositeCols{}, false
	}
	return compositeCols{typeCol: tc.Name, rtrefCol: rc.Name}, true
}

// compositeMatch reports whether a sampled (_TYPE, _RTRef) pair is a
// reference to table number no.
func compositeMatch(typ, rtref interface{}, no int) bool {
	tb, ok := typ.([]byte)
	if !ok || len(tb) != 1 || tb[0] != model.RefTypeReference || no <= 0 {
		return false
	}
	rb, _ := rtref.([]byte)
	n, ok := model.RTRefValue(rb)
	return ok && n == no
}

// 1C storage column names.
const (
	col1CKey         = "_IDRRef"
	col1CDescription = "_Description"
	col1CCode        = "_Code"
)

type tableInfo struct {
	ref  TableRef
	cols []Column
}

func groupTables(cols []Column) []*tableInfo {
	idx := map[TableRef]*tableInfo{}
	var out []*tableInfo
	for _, c := range cols {
		t := idx[c.Table]
		if t == nil {
			t = &tableInfo{ref: c.Table}
			idx[c.Table] = t
			out = append(out, t)
		}
		t.cols = append(t.cols, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ref.Schema != out[j].ref.Schema {
			return out[i].ref.Schema < out[j].ref.Schema
		}
		return out[i].ref.Name < out[j].ref.Name
	})
	for _, t := range out {
		sort.SliceStable(t.cols, func(i, j int) bool { return t.cols[i].Ordinal < t.cols[j].Ordinal })
	}
	return out
}

func (t *tableInfo) find(name string) (Column, bool) {
	for _, c := range t.cols {
		if strings.EqualFold(c.Name, name) {
			return c, true
		}
	}
	return Column{}, false
}

func (t *tableInfo) dateCols(max int) []Column {
	var out []Column
	for _, c := range t.cols {
		if c.IsDateTime() && len(out) < max {
			out = append(out, c)
		}
	}
	return out
}

// refCols: binary(16) columns except the table's own _IDRRef key.
func (t *tableInfo) refCols() []Column {
	var out []Column
	for _, c := range t.cols {
		if c.IsRef16() && !strings.EqualFold(c.Name, col1CKey) {
			out = append(out, c)
		}
	}
	return out
}

// textCols: bounded string columns up to maxLen characters, at most max.
func (t *tableInfo) textCols(maxLen, max int) []Column {
	var out []Column
	for _, c := range t.cols {
		if c.IsText() && c.MaxLength <= maxLen && len(out) < max {
			out = append(out, c)
		}
	}
	return out
}

// key1C returns the _IDRRef binary(16) key column name, or "".
func (t *tableInfo) key1C() string {
	if c, ok := t.find(col1CKey); ok && c.IsRef16() {
		return c.Name
	}
	return ""
}

func colNames(cols []Column) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Name
	}
	return out
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// pickPointCol chooses the point name column: a bounded string column,
// preferring name/description, then node/point, then code, then the first.
func pickPointCol(t *tableInfo) string {
	best, bestScore := "", -1
	for _, c := range t.cols {
		if !c.IsText() {
			continue
		}
		l := strings.ToLower(c.Name)
		s := 0
		switch {
		case containsAny(l, "name", "descr", "наимен", "имя", "назв"):
			s = 3
		case containsAny(l, "node", "point", "узел", "узл", "точк"):
			s = 2
		case containsAny(l, "code", "код"):
			s = 1
		}
		if s > bestScore {
			best, bestScore = c.Name, s
		}
	}
	return best
}

// pointCols1C picks name/code columns of a 1C table (_Description, _Code,
// else a string column).
func pointCols1C(t *tableInfo) (point, id string) {
	name, code := "", ""
	if c, ok := t.find(col1CDescription); ok && c.IsText() {
		name = c.Name
	}
	if c, ok := t.find(col1CCode); ok && (c.IsText() || c.IsNumeric()) {
		code = c.Name
	}
	if name == "" && code == "" {
		name = pickPointCol(t)
	}
	if name == "" {
		return code, ""
	}
	return name, code
}

// Generic markers (lower case) searched in table and column names.
var genericMarkers = []string{"node", "exchange", "msg", "message", "обмен", "узел", "узл"}

func hasMarker(t *tableInfo) bool {
	names := []string{t.ref.Name}
	for _, c := range t.cols {
		names = append(names, c.Name)
	}
	for _, n := range names {
		if containsAny(strings.ToLower(n), genericMarkers...) {
			return true
		}
	}
	return false
}

// Name hints for send/recv columns.
const (
	hintNone = iota
	hintSend
	hintRecv
)

var (
	sendTokens = map[string]bool{"send": true, "sent": true, "sender": true, "out": true, "outgoing": true,
		"outbound": true, "export": true, "upload": true, "tx": true}
	recvTokens = map[string]bool{"recv": true, "rcv": true, "receive": true, "received": true, "in": true,
		"incoming": true, "inbound": true, "import": true, "download": true, "rx": true}
	sendSubs = []string{"send", "export", "upload", "выгруз", "отправ"}
	recvSubs = []string{"recv", "receiv", "import", "download", "загруз", "получ", "прием", "приём"}
)

// splitTokens splits an identifier on non-alphanumerics, lower->upper and
// letter<->digit boundaries, lower-cased: "LastRecvDate" -> last recv date.
func splitTokens(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	var prev rune
	for _, r := range s {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
		case len(cur) > 0 && (unicode.IsUpper(r) && unicode.IsLower(prev) ||
			unicode.IsDigit(r) != unicode.IsDigit(prev)):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
		prev = r
	}
	flush()
	return out
}

func nameHint(name string) int {
	l := strings.ToLower(name)
	isSend, isRecv := containsAny(l, sendSubs...), containsAny(l, recvSubs...)
	for _, t := range splitTokens(name) {
		isSend = isSend || sendTokens[t]
		isRecv = isRecv || recvTokens[t]
	}
	switch {
	case isSend && !isRecv:
		return hintSend
	case isRecv && !isSend:
		return hintRecv
	}
	return hintNone
}

// DetectYearOffset returns model.YearOffset1C when timestamp values show
// the 1C +2000 year offset (a year >= 3000 that is plausible after
// subtracting 2000, or only 1C "empty dates" 2001-01-01), otherwise 0.
func DetectYearOffset(values []*time.Time, now time.Time) int {
	ny := now.Year()
	empty1C, plain := false, false
	for _, m := range values {
		if m == nil {
			continue
		}
		y := m.Year()
		if y >= 3000 && y-model.YearOffset1C <= ny+1 {
			return model.YearOffset1C
		}
		h, mi, s := m.Clock()
		if y == 2001 && m.Month() == time.January && m.Day() == 1 && h == 0 && mi == 0 && s == 0 {
			empty1C = true
		} else {
			plain = true
		}
	}
	if empty1C && !plain {
		return model.YearOffset1C
	}
	return 0
}

// seriesEval is the choice of send/recv columns among datetime columns.
type seriesEval struct {
	send, recv       string
	sendIdx, recvIdx int
	offset           int
	freshest         *time.Time // instant, offset removed
	at               []*time.Time
	score            int // name hints (+5/+2) + two series (+10); freshness excluded
	fresh            int // freshness points (0..30), -10 for future dates
	hints            []string
	note             string
}

func freshnessPoints(age time.Duration) int {
	switch {
	case age <= time.Hour:
		return 30
	case age <= 24*time.Hour:
		return 25
	case age <= 7*24*time.Hour:
		return 18
	case age <= 30*24*time.Hour:
		return 10
	case age <= 365*24*time.Hour:
		return 4
	}
	return 1
}

// evaluateSeries picks send/recv columns from datetime columns and their
// naive maxima: name hints first, otherwise the two freshest series (the
// lower ordinal becomes "send"). It also detects YearOffset and scores
// freshness against now.
func evaluateSeries(dates []Column, maxes []*time.Time, now time.Time) seriesEval {
	ev := seriesEval{offset: DetectYearOffset(maxes, now), sendIdx: -1, recvIdx: -1}
	ev.at = make([]*time.Time, len(dates))
	var valid []int
	for i := range dates {
		if i < len(maxes) {
			ev.at[i] = ConvertDBTime(maxes[i], ev.offset)
		}
		if ev.at[i] != nil {
			valid = append(valid, i)
		}
	}
	pool := valid
	if len(pool) == 0 {
		ev.hints = append(ev.hints, model.HintNoTimestamps)
		pool = make([]int, len(dates))
		for i := range dates {
			pool[i] = i
		}
	}
	sendIdx, recvIdx := -1, -1
	for _, i := range pool {
		switch nameHint(dates[i].Name) {
		case hintSend:
			if sendIdx < 0 {
				sendIdx = i
			}
		case hintRecv:
			if recvIdx < 0 {
				recvIdx = i
			}
		}
	}
	order := append([]int(nil), pool...)
	sort.SliceStable(order, func(a, b int) bool {
		x, y := ev.at[order[a]], ev.at[order[b]]
		if x != nil && y != nil && !x.Equal(*y) {
			return x.After(*y)
		}
		return dates[order[a]].Ordinal < dates[order[b]].Ordinal
	})
	switch {
	case sendIdx >= 0 && recvIdx >= 0:
		ev.hints = append(ev.hints, model.HintSendRecvByName)
		ev.note = "send/recv by column names"
		ev.score += 5
	case sendIdx >= 0 || recvIdx >= 0:
		for _, i := range order {
			if i != sendIdx && i != recvIdx {
				if sendIdx < 0 {
					sendIdx = i
				} else {
					recvIdx = i
				}
				break
			}
		}
		ev.note = "send/recv partly by column names"
		ev.score += 2
	default:
		if len(order) > 0 {
			sendIdx = order[0]
		}
		if len(order) > 1 {
			recvIdx = order[1]
			if dates[recvIdx].Ordinal < dates[sendIdx].Ordinal {
				sendIdx, recvIdx = recvIdx, sendIdx
			}
			ev.hints = append(ev.hints, model.HintSendRecvByOrder)
			ev.note = "send/recv = two freshest series by column order (confirm)"
		}
	}
	ev.sendIdx, ev.recvIdx = sendIdx, recvIdx
	if sendIdx >= 0 {
		ev.send = dates[sendIdx].Name
	}
	if recvIdx >= 0 {
		ev.recv = dates[recvIdx].Name
	}
	if ev.send == "" || ev.recv == "" {
		ev.hints = append(ev.hints, model.HintSingleSeries)
		if ev.note == "" {
			ev.note = "single timestamp column"
		}
	}
	nSeries := 0
	for _, i := range []int{sendIdx, recvIdx} {
		if i < 0 || ev.at[i] == nil {
			continue
		}
		nSeries++
		if ev.freshest == nil || ev.at[i].After(*ev.freshest) {
			t := *ev.at[i]
			ev.freshest = &t
		}
	}
	if nSeries == 2 {
		ev.score += 10 // two timestamp series per point: send + recv
	}
	if ev.offset != 0 {
		ev.hints = append(ev.hints, model.HintYearOffset2000)
		ev.note += "; 1C year offset 2000"
	}
	if ev.freshest != nil {
		age := now.Sub(*ev.freshest)
		if age < -24*time.Hour {
			ev.hints = append(ev.hints, model.HintFutureDates)
			ev.fresh = -10
		} else {
			ev.fresh = freshnessPoints(age)
		}
		ev.note += "; freshest " + ev.freshest.In(model.DBZone).Format("2006-01-02 15:04:05") + " UTC+3"
	}
	return ev
}

// sendRecvAt returns the converted values of the chosen send/recv columns.
func (ev seriesEval) sendRecvAt() (send, recv *time.Time) {
	if ev.sendIdx >= 0 {
		send = ev.at[ev.sendIdx]
	}
	if ev.recvIdx >= 0 {
		recv = ev.at[ev.recvIdx]
	}
	return send, recv
}
