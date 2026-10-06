package traysettings

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Discovery UI (phase 6d, CHK-TRAY-07; service side G2/G3, CHK-IPC-08/09,
// PLAN §6.2, §12 p.74): discover, discover_by_value, list_node_points,
// check_mapping in the administrator settings window.

// DiscoveryCapabilities must all be announced for the Discovery dialog.
var DiscoveryCapabilities = []string{model.CapDiscover, model.CapDiscoverByValue, model.CapNodePoints, model.CapCheckMapping}

// DiscoveryAvailable tells whether the Discovery dialog is offered: the
// administrator window (elevated: in-process with clientElevated or the
// --settings process) and every capability of DiscoveryCapabilities.
func DiscoveryAvailable(mode Mode, has func(string) bool) bool {
	if mode != Admin || has == nil {
		return false
	}
	for _, c := range DiscoveryCapabilities {
		if !has(c) {
			return false
		}
	}
	return true
}

// DiscoveryTimeout bounds one interactive request: the service answers
// within 100 s (hard limit, then failed + monitor.discover_timeout).
const DiscoveryTimeout = 110 * time.Second

// DefaultNodePointsLimit is the limit of list_node_points (rows to pick
// from; 0 would be "everything").
const DefaultNodePointsLimit = 1000

// Dialog fields (focus / highlight targets).
const (
	FieldDiscPoint     Field = "query.pointName"
	FieldDiscTime      Field = "query.approxTime"
	FieldDiscTolerance Field = "query.toleranceMin"
)

// InteractiveRequest is the profile part of the four commands, with the
// test_profile rules: the saved profile by id when nothing was edited,
// else the form (+ typed password).
func (e *Editor) InteractiveRequest(cmd, password string) (model.IPCRequest, *Problem) {
	req, pr := e.TestRequest(password)
	if pr != nil {
		return req, pr
	}
	req.Cmd = cmd
	return req, nil
}

// Discovery is the state of the Discovery dialog.
type Discovery struct {
	Lang            string
	Candidates      []model.DiscoveryCandidate
	Truncated       bool
	Reason          string // truncatedReason of the discovery reply
	Sel             int    // selected candidate, -1 = none
	NodePoints      []model.NodePoint
	PointsTruncated bool
	PointsReason    string
	Map             MappingForm
	Check           *model.MappingCheck
}

// NewDiscovery starts with the mapping m of the profile.
func NewDiscovery(lang string, m model.Mapping) *Discovery {
	return &Discovery{Lang: i18n.Normalize(lang), Sel: -1, Map: FromMapping(m)}
}

func (d *Discovery) t(key string) string { return i18n.T(key, d.Lang) }

// DiscoveryDone takes a discovery reply (discover / discover_by_value):
// a missing list is an empty one; the best candidate is selected.
func (d *Discovery) DiscoveryDone(m model.IPCMessage) {
	d.Candidates = append([]model.DiscoveryCandidate{}, m.Candidates...)
	d.Truncated, d.Reason = m.Truncated, m.TruncatedReason
	d.Sel = -1
	d.Check = nil
	if len(d.Candidates) > 0 {
		d.Select(0)
	}
}

// NodePointsDone takes a node_points reply.
func (d *Discovery) NodePointsDone(m model.IPCMessage) {
	d.NodePoints = append([]model.NodePoint{}, m.NodePoints...)
	d.PointsTruncated, d.PointsReason = m.Truncated, m.TruncatedReason
}

// Select takes candidate i into the mapping form.
func (d *Discovery) Select(i int) {
	if i < 0 || i >= len(d.Candidates) {
		d.Sel = -1
		return
	}
	d.Sel = i
	d.Map = FromMapping(d.Candidates[i].Mapping())
	d.Check = nil
}

// Status is the line under the result lists: count, partial result.
func (d *Discovery) Status() string {
	s := fmt.Sprintf(d.t(i18n.KeyDiscFound), len(d.Candidates))
	if len(d.Candidates) == 0 {
		s = d.t(i18n.KeyDiscNone)
	}
	if d.Truncated {
		s += " " + IncompleteText(ReplyDiscovery, d.Reason, d.Lang)
	}
	return s
}

// Reply kinds of IncompleteText.
const (
	ReplyDiscovery  = model.MsgDiscovery
	ReplyNodePoints = model.MsgNodePoints
	ReplyCheck      = model.MsgMappingCheck
)

// incompleteKeys: truncatedReason values per reply (PLAN §6.2):
// discovery — budget (retry / narrow) or size (refine; both -> budget);
// node_points — limit or size (both -> size); mapping_check — size only.
var incompleteKeys = map[string]map[string]string{
	ReplyDiscovery:  {model.TruncatedBudget: i18n.KeyDiscReasonBudget, model.TruncatedSize: i18n.KeyDiscReasonSize},
	ReplyNodePoints: {model.TruncatedLimit: i18n.KeyDiscReasonLimit, model.TruncatedSize: i18n.KeyDiscReasonPointsSize},
	ReplyCheck:      {model.TruncatedSize: i18n.KeyDiscReasonCheckSize},
}

// IncompleteText is «Результат неполный» with the truncatedReason of a
// reply of kind (Reply*); absent, unknown or not defined for the reply —
// the general text.
func IncompleteText(kind, reason, lang string) string {
	key := incompleteKeys[kind][reason]
	if key == "" {
		return i18n.T(i18n.KeyDiscIncomplete, lang)
	}
	return i18n.T(key, lang)
}

// PointsStatus describes the node_points list.
func (d *Discovery) PointsStatus() string {
	s := fmt.Sprintf(d.t(i18n.KeyDiscPointsFound), len(d.NodePoints))
	if d.PointsTruncated {
		s += " " + IncompleteText(ReplyNodePoints, d.PointsReason, d.Lang)
	}
	return s
}

// PointLine is a node point in the point box (display only).
func PointLine(p model.NodePoint) string {
	s := p.Name
	if p.Code != "" {
		s += " [" + p.Code + "]"
	}
	return s + " — " + p.PlanTable
}

// ValueForm is the input of «Поиск по точке».
type ValueForm struct {
	Point      string // typed text or the PointLine of a listed point
	PointIndex int    // index in Discovery.NodePoints, -1 = typed
	ApproxTime string // "02.01.2006 15:04", UTC+3, optional
	Tolerance  string // minutes, optional
}

// TimeLayout of the approximate time input (UTC+3).
const TimeLayout = "02.01.2006 15:04"

// Query builds the ValueQuery of discover_by_value; the empty point is
// refused here like by the service (monitor.discover_point_name_empty).
func (d *Discovery) Query(f ValueForm) (*model.ValueQuery, *Problem) {
	q := &model.ValueQuery{PointName: strings.TrimSpace(f.Point)}
	if f.PointIndex >= 0 && f.PointIndex < len(d.NodePoints) && q.PointName == PointLine(d.NodePoints[f.PointIndex]) {
		p := d.NodePoints[f.PointIndex]
		q.PointName, q.PointRef, q.PlanTable = p.Name, p.Ref, p.PlanTable
		if q.PointName == "" {
			q.PointName = p.Code
		}
	}
	if q.PointName == "" && q.PointRef == "" {
		return nil, &Problem{Text: i18n.Message(model.MsgDiscoverPointNameEmpty, d.Lang), Field: FieldDiscPoint}
	}
	if s := strings.TrimSpace(f.ApproxTime); s != "" {
		t, err := time.ParseInLocation(TimeLayout, s, model.DBZone)
		if err != nil {
			return nil, &Problem{Text: d.t(i18n.KeyDiscBadTime), Field: FieldDiscTime}
		}
		q.ApproxTime = &t
	}
	if s := strings.TrimSpace(f.Tolerance); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return nil, &Problem{Text: d.t(i18n.KeyDiscBadTolerance), Field: FieldDiscTolerance}
		}
		q.ToleranceMin = n
	}
	return q, nil
}

// MappingForm is the editable mapping of the dialog. Send/Recv edit the
// counter columns in counters mode; hidden link fields of a candidate
// (times schema, composite reference) are kept from the base mapping.
type MappingForm struct {
	Mode                              model.MappingMode
	Schema, Table, PointName, PointID string
	PointKey, TimesTable, TimesLink   string
	Send, Recv, PlanLabel, ManualSQL  string
	YearOffset2000, KeepUnlinked      bool
	base                              model.Mapping
}

// MappingModes are the items of the mode box.
var MappingModes = []model.MappingMode{model.MappingAuto, model.MappingManual, model.MappingCounters}

// MappingModeIndex is the index of m in MappingModes (none = auto).
func MappingModeIndex(m model.MappingMode) int {
	for i, x := range MappingModes {
		if x == m {
			return i
		}
	}
	return 0
}

// FromMapping fills the form.
func FromMapping(m model.Mapping) MappingForm {
	f := MappingForm{Mode: m.Mode, Schema: m.Schema, Table: m.Table, PointName: m.PointNameColumn, PointID: m.PointIDColumn,
		PointKey: m.PointKeyColumn, TimesTable: m.TimesTable, TimesLink: m.TimesLinkColumn, Send: m.LastSendColumn,
		Recv: m.LastRecvColumn, PlanLabel: m.PlanLabel, ManualSQL: m.ManualSQL, YearOffset2000: m.YearOffset == model.YearOffset1C,
		KeepUnlinked: m.KeepUnlinkedPoints, base: m}
	if f.Mode == "" {
		f.Mode = model.MappingAuto
	}
	if m.Mode == model.MappingCounters {
		f.Send, f.Recv = m.SentNoColumn, m.ReceivedNoColumn
	}
	return f
}

// Mapping is the mapping of the form.
func (f MappingForm) Mapping() model.Mapping {
	m := f.base
	tr := strings.TrimSpace
	m.Mode = f.Mode
	m.Schema, m.Table, m.PointNameColumn, m.PointIDColumn = tr(f.Schema), tr(f.Table), tr(f.PointName), tr(f.PointID)
	m.PointKeyColumn, m.TimesTable, m.TimesLinkColumn = tr(f.PointKey), tr(f.TimesTable), tr(f.TimesLink)
	m.PlanLabel, m.KeepUnlinkedPoints = tr(f.PlanLabel), f.KeepUnlinked
	m.LastSendColumn, m.LastRecvColumn, m.SentNoColumn, m.ReceivedNoColumn = tr(f.Send), tr(f.Recv), "", ""
	m.YearOffset = 0
	if f.YearOffset2000 {
		m.YearOffset = model.YearOffset1C
	}
	m.ManualSQL = ""
	switch f.Mode {
	case model.MappingCounters:
		m.SentNoColumn, m.ReceivedNoColumn = m.LastSendColumn, m.LastRecvColumn
		m.LastSendColumn, m.LastRecvColumn, m.YearOffset = "", "", 0
	case model.MappingManual:
		m.ManualSQL = strings.TrimSpace(f.ManualSQL)
	}
	if m.TimesTable == "" {
		m.TimesSchema, m.TimesLinkTypeColumn, m.TimesLinkRTRefColumn, m.TimesLinkRTRef = "", "", "", 0
	}
	return m
}

// CheckRequest is check_mapping of the form (limit 0 = service default 5).
func (e *Editor) CheckRequest(m model.Mapping, password string, limit int) (model.IPCRequest, *Problem) {
	if limit < 0 || limit > model.MaxCheckMappingLimit {
		return model.IPCRequest{}, &Problem{Text: model.FormatMessage(model.ValRange,
			[]string{"limit", "1", strconv.Itoa(model.MaxCheckMappingLimit), strconv.Itoa(limit)}, e.Lang)}
	}
	req, pr := e.InteractiveRequest(model.CmdCheckMapping, password)
	if pr != nil {
		return req, pr
	}
	req.Mapping, req.Limit = &m, limit
	if pr := e.checkSize(req); pr != nil {
		return model.IPCRequest{}, pr
	}
	return req, nil
}

// mappingLabels are the localized names of mapping.* fields.
var mappingLabels = map[string]string{
	"mapping.mode": i18n.KeyDiscMode, "mapping.schema": i18n.KeyDiscSchema, "mapping.table": i18n.KeyDiscTable,
	"mapping.pointNameColumn": i18n.KeyDiscPointName, "mapping.pointIdColumn": i18n.KeyDiscPointID,
	"mapping.pointKeyColumn": i18n.KeyDiscPointKey, "mapping.timesTable": i18n.KeyDiscTimesTable,
	"mapping.timesLinkColumn": i18n.KeyDiscTimesLink, "mapping.lastSendColumn": i18n.KeyDiscSend,
	"mapping.lastRecvColumn": i18n.KeyDiscRecv, "mapping.sentNoColumn": i18n.KeyDiscSend,
	"mapping.receivedNoColumn": i18n.KeyDiscRecv, "mapping.planLabel": i18n.KeyDiscPlanLabel,
	"mapping.manualSql": i18n.KeyDiscManualSQL, "mapping.yearOffset": i18n.KeyDiscYearOffset,
	"mapping.keepUnlinkedPoints": i18n.KeyDiscKeepUnlinked,
}

// CheckResult renders a mapping_check result: the text and the mapping.*
// field to highlight ("" = none).
func (d *Discovery) CheckResult(mc model.MappingCheck) (text, field string) {
	var b []string
	if mc.OK {
		b = append(b, d.t(i18n.KeyDiscCheckOK))
	} else {
		head := i18n.Message(mc.ErrorKey, d.Lang)
		if mc.ErrorKey == "" {
			head = d.t(i18n.KeyDiscCheckFailed)
		}
		b = append(b, head)
		if len(mc.ErrorArgs) >= 2 {
			field = mc.ErrorArgs[0]
			args := append([]string(nil), mc.ErrorArgs...)
			if k := mappingLabels[field]; k != "" {
				args[0] = strings.TrimSuffix(d.t(k), ":")
			}
			if s := model.FormatMessage(mc.ErrorArgs[1], args, d.Lang); s != "" {
				b = append(b, s)
			}
		} else if mc.Error != "" && mc.ErrorKey == "" {
			b = append(b, mc.Error)
		}
	}
	if len(mc.Violations) > 0 {
		b = append(b, d.t(i18n.KeyDiscViolations)+" "+strings.Join(mc.Violations, ", "))
	}
	for _, r := range mc.SampleRows {
		b = append(b, SampleLine(r))
	}
	if mc.Truncated {
		b = append(b, IncompleteText(ReplyCheck, mc.TruncatedReason, d.Lang))
	}
	return strings.Join(b, "\r\n"), field
}

// SampleLine is one preview row (UTC+3 wall clock).
func SampleLine(r model.SampleRow) string {
	ts := func(t *time.Time) string {
		if t == nil {
			return "—"
		}
		return model.InDBZone(*t).Format("02.01.2006 15:04")
	}
	p := r.Point
	if r.PointTruncated {
		p += "…"
	}
	return p + ": " + ts(r.LastSend) + " / " + ts(r.LastRecv)
}

// CandidateLine is a candidate in the result list.
func (d *Discovery) CandidateLine(c model.DiscoveryCandidate) string {
	tbl := c.Table
	if c.Schema != "" && c.Schema != "dbo" {
		tbl = c.Schema + "." + tbl
	}
	if c.TimesTable != "" {
		tbl += " → " + c.TimesTable
	}
	s := fmt.Sprintf("%s  [%s]  %d", tbl, c.Mode, c.Score)
	if c.Points > 0 {
		s += "  " + fmt.Sprintf(d.t(i18n.KeyDiscPoints), c.Points)
	}
	if c.FreshestAt != nil {
		s += "  " + model.InDBZone(*c.FreshestAt).Format("02.01.2006 15:04")
	}
	return s
}

// HintText localizes a discovery hint (unknown hints are shown as is).
func HintText(h, lang string) string {
	key := "hint." + h
	if i18n.Has(key) {
		return i18n.T(key, lang)
	}
	return h
}

// CandidateDetails describes the selected candidate: columns, hints,
// the found point, sample rows.
func (d *Discovery) CandidateDetails(c model.DiscoveryCandidate) string {
	var b []string
	cols := []string{c.PointCol}
	for _, x := range []string{c.SendCol, c.RecvCol} {
		if x != "" {
			cols = append(cols, x)
		}
	}
	b = append(b, d.t(i18n.KeyDiscColumns)+" "+strings.Join(cols, ", "))
	if c.YearOffset == model.YearOffset1C {
		b = append(b, d.t(i18n.KeyDiscYearOffset))
	}
	for _, h := range c.Hints {
		b = append(b, "• "+HintText(h, d.Lang))
	}
	if c.Matched != nil {
		b = append(b, d.t(i18n.KeyDiscMatched)+" "+SampleLine(*c.Matched))
	}
	for _, r := range c.SampleRows {
		b = append(b, SampleLine(r))
	}
	return strings.Join(b, "\r\n")
}

// PlanMappings is «все планы обмена»: SuggestPlanMappings of the found
// candidates without the plan table of the chosen mapping (Profile.Mappings).
func (d *Discovery) PlanMappings(chosen model.Mapping) []model.Mapping {
	var out []model.Mapping
	for _, m := range model.SuggestPlanMappings(d.Candidates) {
		if strings.EqualFold(m.Schema+"."+m.PlanTable(), chosen.Schema+"."+chosen.PlanTable()) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// InteractiveProblem renders the error of a discover / discover_by_value
// / list_node_points / check_mapping call (protocol errors; a failed
// check is a mapping_check reply, see CheckResult).
func (e *Editor) InteractiveProblem(m model.IPCMessage, err error) *Problem {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return &Problem{Text: e.t(i18n.KeyDiscCanceled)}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return &Problem{Text: i18n.Message(model.MsgDiscoverTimeout, e.Lang)}
		}
		return e.ErrorProblem(err, DiscoveryTimeout)
	}
	if m.Type != model.MsgError {
		return nil
	}
	switch m.Code {
	case model.ErrCodeCanceled:
		return &Problem{Text: e.t(i18n.KeyDiscCanceled)}
	case model.ErrCodeBusy:
		return &Problem{Text: i18n.Message(m.ErrorKey, e.Lang)}
	case model.ErrCodeFailed:
		key := m.ErrorKey
		if key == "" {
			key = model.MsgDiscoverFailed
		}
		return &Problem{Text: i18n.Message(key, e.Lang)}
	case model.ErrCodeInvalidRequest:
		if m.ErrorKey == model.MsgDiscoverPointNameEmpty {
			return &Problem{Text: i18n.Message(m.ErrorKey, e.Lang), Field: FieldDiscPoint}
		}
	}
	return e.Reply(m)
}

// TemplateTitles are the manual SQL template names in lang.
func TemplateTitles(lang string) []string {
	var out []string
	for _, t := range model.SQLManualTemplates() {
		out = append(out, t.Title.In(lang))
	}
	return out
}
