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
// within 100 s (hard limit, then failed + monitor.discover_timeout). A
// variable so that tests can shorten it.
var DiscoveryTimeout = 110 * time.Second

// DefaultNodePointsLimit is the limit of list_node_points (rows to pick
// from; 0 would be "everything").
const DefaultNodePointsLimit = 1000

// Dialog fields (focus / highlight targets).
const (
	FieldDiscPoint     Field = "query.pointName"
	FieldDiscTime      Field = "query.approxTime"
	FieldDiscTolerance Field = "query.toleranceMin"
	// FieldDiscLimit: limit of list_node_points / check_mapping (set by
	// the tray, no input box: the dialog focuses the button of the request).
	FieldDiscLimit Field = "limit"
	// FieldDiscoverButton: «Таблицы обмена…» of the settings window, the
	// focus target of mapping.* / mappings[i].* errors (the mapping is
	// edited only in the dialog).
	FieldDiscoverButton Field = "discoverButton"
)

// discoveryFocus maps a JSON path of the Discovery commands / the mapping
// to its focus target; ok = the path belongs to Discovery.
func discoveryFocus(path string) (Field, bool) {
	switch {
	case path == "mapping" || path == "mappings" || strings.HasPrefix(path, "mapping.") || strings.HasPrefix(path, "mappings["):
		return FieldDiscoverButton, true
	case path == "query" || path == "query.pointName" || path == "query.pointRef" || path == "query.planTable":
		return FieldDiscPoint, true
	case path == "query.approxTime":
		return FieldDiscTime, true
	case path == "query.toleranceMin":
		return FieldDiscTolerance, true
	case path == "limit":
		return FieldDiscLimit, true
	}
	return FieldNone, false
}

// mappingLabel is the localized name of a mapping field ("table",
// "timesLinkRTRef", ...; "" for an unknown one).
func mappingLabel(field, lang string) string {
	k := mappingLabels["mapping."+field]
	if k == "" {
		return ""
	}
	return strings.TrimSuffix(i18n.T(k, lang), ":")
}

// discoveryFieldLabel is the label of a Discovery path for validation
// texts of the settings window: «Таблицы обмена — Ручной SQL»,
// «Доп. план обмена 2 — Таблица точек», «Точка», «Число строк (limit)».
func discoveryFieldLabel(path, lang string) (string, bool) {
	sub := func(rest string) string {
		if l := mappingLabel(rest, lang); l != "" {
			return l
		}
		return rest
	}
	switch {
	case path == "mapping":
		return i18n.T(i18n.KeyDiscTitle, lang), true
	case path == "mappings":
		return i18n.T(i18n.KeyDiscExtraPlans, lang), true
	case strings.HasPrefix(path, "mapping."):
		return fmt.Sprintf(i18n.T(i18n.KeyDiscFieldMain, lang), sub(strings.TrimPrefix(path, "mapping."))), true
	case strings.HasPrefix(path, "mappings["):
		rest := strings.TrimPrefix(path, "mappings[")
		if j := strings.Index(rest, "]."); j > 0 {
			if n, err := strconv.Atoi(rest[:j]); err == nil {
				return fmt.Sprintf(i18n.T(i18n.KeyDiscFieldExtra, lang), n+1, sub(rest[j+2:])), true
			}
		}
		return path, true
	}
	key := map[string]string{"query": i18n.KeyDiscPoint, "query.pointName": i18n.KeyDiscPoint, "query.pointRef": i18n.KeyDiscPoint,
		"query.planTable": i18n.KeyDiscPoint, "query.approxTime": i18n.KeyDiscTime, "query.toleranceMin": i18n.KeyDiscTolerance,
		"limit": i18n.KeyDiscLimit}[path]
	if key == "" {
		return "", false
	}
	return strings.TrimSuffix(i18n.T(key, lang), ":"), true
}

// InteractiveRequest is the profile part of the four commands, with the
// test_profile rules: the saved profile by id when the connection form
// was not edited, else the form (+ typed password). The mapping edit of
// the dialog is not part of it and never blocks a request (TestRequest).
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
	// initial: the mapping of the profile at opening; loaded: the mapping
	// last put into the form (opening or a candidate). Unapplied / Edited
	// compare the form with them (questions on close / a new search).
	initial, loaded model.Mapping
}

// NewDiscovery starts with the mapping m of the profile.
func NewDiscovery(lang string, m model.Mapping) *Discovery {
	d := &Discovery{Lang: i18n.Normalize(lang), Sel: -1, Map: FromMapping(m)}
	d.initial = d.Map.Mapping()
	d.loaded = d.initial
	return d
}

// Unapplied: the form differs from the profile's mapping (closing the
// dialog would lose it — ask, like the settings window, §12 p.67).
func (d *Discovery) Unapplied() bool { return d.Map.Mapping() != d.initial }

// Edited: the form was changed by hand since it was last loaded (a new
// search or another candidate would overwrite it — ask).
func (d *Discovery) Edited() bool { return d.Map.Mapping() != d.loaded }

// AcceptOverwrite: the user agreed to replace the hand edits (a new
// search); the next result fills the form again.
func (d *Discovery) AcceptOverwrite() { d.loaded = d.Map.Mapping() }

func (d *Discovery) t(key string) string { return i18n.T(key, d.Lang) }

// DiscoveryDone takes a discovery reply (discover / discover_by_value):
// a missing list is an empty one; the best candidate is selected unless
// the form was edited by hand meanwhile (the edits are kept, nothing is
// selected).
func (d *Discovery) DiscoveryDone(m model.IPCMessage) {
	edited := d.Edited()
	d.Candidates = append([]model.DiscoveryCandidate{}, m.Candidates...)
	d.Truncated, d.Reason = m.Truncated, m.TruncatedReason
	d.Sel = -1
	d.Check = nil
	if len(d.Candidates) > 0 && !edited {
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
	d.loaded = d.Map.Mapping()
	d.Check = nil
}

// CountText is the short result line of discovery (count only).
func (d *Discovery) CountText() string {
	if len(d.Candidates) == 0 {
		return d.t(i18n.KeyDiscNone)
	}
	return fmt.Sprintf(d.t(i18n.KeyDiscFound), len(d.Candidates))
}

// IncompleteNote is the truncatedReason text of the last discovery ("" =
// complete).
func (d *Discovery) IncompleteNote() string {
	if !d.Truncated {
		return ""
	}
	return IncompleteText(ReplyDiscovery, d.Reason, d.Lang)
}

// PointsNote is the truncatedReason text of the last node_points reply.
func (d *Discovery) PointsNote() string {
	if !d.PointsTruncated {
		return ""
	}
	return IncompleteText(ReplyNodePoints, d.PointsReason, d.Lang)
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

// MappingKind is what the mapping editor edits: tables (a Discovery
// candidate or columns typed by hand), a manual SQL query, or the message
// counters of an exchange plan.
type MappingKind int

// Mapping kinds (items of the mode box).
const (
	KindTables MappingKind = iota
	KindSQL
	KindCounters
)

// MappingKinds are the items of the mode box.
var MappingKinds = []MappingKind{KindTables, KindSQL, KindCounters}

// KindOf is the editor kind of a mapping.
func KindOf(m model.Mapping) MappingKind {
	switch {
	case m.Mode == model.MappingCounters:
		return KindCounters
	case strings.TrimSpace(m.ManualSQL) != "" && strings.TrimSpace(m.Table) == "":
		return KindSQL
	}
	return KindTables
}

// MappingForm is the editable mapping of the dialog. Send/Recv edit the
// counter columns in counters mode. The mapping is always built from the
// visible fields only (Mapping), so no hidden field of another kind is
// validated or checked; the hidden link fields of a candidate (times
// schema, composite reference _TYPE/_RTRef) are kept only while the
// tables they belong to are unchanged.
type MappingForm struct {
	Kind                              MappingKind
	Schema, Table, PointName, PointID string
	PointKey, TimesTable, TimesLink   string
	Send, Recv, PlanLabel, ManualSQL  string
	YearOffset2000, KeepUnlinked      bool
	base                              model.Mapping
}

// FromMapping fills the form.
func FromMapping(m model.Mapping) MappingForm {
	f := MappingForm{Kind: KindOf(m), PlanLabel: m.PlanLabel, YearOffset2000: m.YearOffset == model.YearOffset1C, base: m}
	switch f.Kind {
	case KindSQL:
		f.ManualSQL = m.ManualSQL
	case KindCounters:
		f.Schema, f.Table, f.PointName, f.PointID, f.PointKey = m.Schema, m.Table, m.PointNameColumn, m.PointIDColumn, m.PointKeyColumn
		f.Send, f.Recv, f.YearOffset2000 = m.SentNoColumn, m.ReceivedNoColumn, false
	default:
		f.Schema, f.Table, f.PointName, f.PointID, f.PointKey = m.Schema, m.Table, m.PointNameColumn, m.PointIDColumn, m.PointKeyColumn
		f.TimesTable, f.TimesLink, f.Send, f.Recv = m.TimesTable, m.TimesLinkColumn, m.LastSendColumn, m.LastRecvColumn
		f.KeepUnlinked = m.KeepUnlinkedPoints
	}
	return f
}

// SetKind switches the editor kind and clears the fields the new kind
// does not use (they could not be edited, but would be validated).
func (f *MappingForm) SetKind(k MappingKind) {
	if f.Kind == k {
		return
	}
	f.Kind = k
	switch k {
	case KindSQL:
		f.Schema, f.Table, f.PointName, f.PointID, f.PointKey = "", "", "", "", ""
		f.TimesTable, f.TimesLink, f.Send, f.Recv, f.KeepUnlinked = "", "", "", "", false
	case KindCounters:
		f.TimesTable, f.TimesLink, f.ManualSQL, f.YearOffset2000, f.KeepUnlinked = "", "", "", false, false
		f.Send, f.Recv = "", ""
	default:
		f.ManualSQL = ""
	}
}

// FieldEditable tells whether a mapping field (JSON name) has an enabled
// box in kind k.
func FieldEditable(k MappingKind, field string) bool {
	switch field {
	case "planLabel":
		return true
	case "manualSql":
		return k == KindSQL
	case "yearOffset":
		return k != KindCounters
	case "timesTable", "timesLinkColumn", "keepUnlinkedPoints":
		return k == KindTables
	}
	return k != KindSQL
}

// SchemaKey normalizes a schema name for comparisons ("" = dbo,
// case-insensitive).
func SchemaKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "dbo"
	}
	return s
}

func sameName(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

// sameTables: the points and times tables of a and b are the same (the
// hidden link fields of b still apply to a).
func sameTables(a, b model.Mapping) bool {
	return SchemaKey(a.Schema) == SchemaKey(b.Schema) && sameName(a.Table, b.Table) && sameName(a.TimesTable, b.TimesTable) &&
		sameName(a.TimesLinkColumn, b.TimesLinkColumn)
}

// sameColumns: a table mapping a has the tables and columns of b.
func sameColumns(a, b model.Mapping) bool {
	return sameTables(a, b) && sameName(a.PointNameColumn, b.PointNameColumn) && sameName(a.PointIDColumn, b.PointIDColumn) &&
		sameName(a.PointKeyColumn, b.PointKeyColumn) && sameName(a.LastSendColumn, b.LastSendColumn) &&
		sameName(a.LastRecvColumn, b.LastRecvColumn) && a.YearOffset == b.YearOffset
}

// Mapping is the mapping of the form, built from the fields of its kind
// only. Source label (model.Mapping.Mode, profile.go): counters; manual
// for a manual SQL and for columns edited by hand; auto only while the
// tables and columns of a confirmed Discovery candidate are unchanged.
func (f MappingForm) Mapping() model.Mapping {
	tr := strings.TrimSpace
	b := f.base
	m := model.Mapping{PlanLabel: tr(f.PlanLabel)}
	switch f.Kind {
	case KindSQL:
		m.Mode, m.ManualSQL = model.MappingManual, tr(f.ManualSQL)
		if f.YearOffset2000 {
			m.YearOffset = model.YearOffset1C
		}
		return m
	case KindCounters:
		m.Mode = model.MappingCounters
		m.Schema, m.Table, m.PointNameColumn, m.PointIDColumn, m.PointKeyColumn = tr(f.Schema), tr(f.Table), tr(f.PointName), tr(f.PointID), tr(f.PointKey)
		m.SentNoColumn, m.ReceivedNoColumn = tr(f.Send), tr(f.Recv)
		return m
	}
	m.Schema, m.Table, m.PointNameColumn, m.PointIDColumn, m.PointKeyColumn = tr(f.Schema), tr(f.Table), tr(f.PointName), tr(f.PointID), tr(f.PointKey)
	m.TimesTable, m.TimesLinkColumn, m.LastSendColumn, m.LastRecvColumn = tr(f.TimesTable), tr(f.TimesLink), tr(f.Send), tr(f.Recv)
	m.KeepUnlinkedPoints = f.KeepUnlinked
	if f.YearOffset2000 {
		m.YearOffset = model.YearOffset1C
	}
	if m.TimesTable != "" && sameTables(m, b) {
		m.TimesSchema, m.TimesLinkTypeColumn, m.TimesLinkRTRefColumn, m.TimesLinkRTRef = b.TimesSchema, b.TimesLinkTypeColumn, b.TimesLinkRTRefColumn, b.TimesLinkRTRef
	}
	switch {
	case !m.Configured() && m.PointNameColumn == "" && m.PointIDColumn == "" && m.LastSendColumn == "" && m.LastRecvColumn == "":
		m.Mode = model.MappingNone
	case (b.Mode == model.MappingAuto || b.Mode == model.MappingManual) && KindOf(b) == KindTables && sameColumns(m, b):
		m.Mode = b.Mode
	default:
		m.Mode = model.MappingManual
	}
	return m
}

// ValidateMappings checks the mapping and the additional mappings the
// dialog would apply (model rules of Profile.Validate); the field path is
// "mapping.<field>" / "mappings[i].<field>".
func ValidateMappings(m model.Mapping, extra []model.Mapping) *model.FieldError {
	prefix := func(err error, pre string) *model.FieldError {
		fe := model.AsFieldError(err)
		if fe == nil {
			return &model.FieldError{Field: "", Key: model.ValMapping, Msg: err.Error()}
		}
		cp := *fe
		cp.Field = pre + cp.Field
		return &cp
	}
	if err := m.Validate(); err != nil {
		return prefix(err, "mapping.")
	}
	if len(extra) == 0 {
		return nil
	}
	if !m.Configured() {
		return &model.FieldError{Field: "mapping.table", Key: model.ValRequired}
	}
	if len(extra)+1 > model.MaxMappings {
		return &model.FieldError{Field: "mappings", Key: model.ValTooMany, Args: []string{strconv.Itoa(model.MaxMappings)}}
	}
	for i, x := range extra {
		pre := fmt.Sprintf("mappings[%d].", i)
		if !x.Configured() {
			return &model.FieldError{Field: pre + "table", Key: model.ValRequired}
		}
		if err := x.Validate(); err != nil {
			return prefix(err, pre)
		}
	}
	return nil
}

// ValidationProblem renders a mapping validation error in the dialog: the
// text with the dialog's field label and the mapping field to highlight;
// a field the current kind cannot edit is moved to the box that causes it
// (manual SQL / the points table).
func (d *Discovery) ValidationProblem(fe *model.FieldError) *Problem {
	if fe.Field == "" {
		return &Problem{Text: fmt.Sprintf(d.t(i18n.KeySetFailed), fe.Msg)}
	}
	field := fe.Field
	args := fe.IPCArgs()
	if strings.HasPrefix(field, "mapping.") {
		f := strings.TrimPrefix(field, "mapping.")
		if l := mappingLabel(f, d.Lang); l != "" {
			args[0] = l
		}
		if !FieldEditable(d.Map.Kind, f) {
			f = "table"
			if d.Map.Kind == KindSQL {
				f = "manualSql"
			}
			field = "mapping." + f
		}
	} else if l, ok := discoveryFieldLabel(field, d.Lang); ok {
		args[0] = l
	}
	return &Problem{Text: model.FormatMessage(fe.Key, args, d.Lang), Field: Field(field)}
}

// PlanKey identifies the exchange plan of a mapping for merging:
// normalized schema ("" = dbo, case-insensitive) + plan table (else the
// points table); "" when it has no table (manual SQL) — never merged.
func PlanKey(m model.Mapping) string {
	t := m.PlanTable()
	if t == "" {
		t = strings.TrimSpace(m.Table)
	}
	if t == "" {
		return ""
	}
	return SchemaKey(m.Schema) + "." + strings.ToLower(t)
}

// MappingName is a mapping for messages: label, schema.table or «SQL».
func MappingName(m model.Mapping) string {
	name := strings.TrimSpace(m.Table)
	if name == "" {
		name = "SQL"
	} else if SchemaKey(m.Schema) != "dbo" {
		name = strings.TrimSpace(m.Schema) + "." + name
	}
	if l := strings.TrimSpace(m.PlanLabel); l != "" {
		name = l + " (" + name + ")"
	}
	return name
}

// MergeMappings merges the profile's additional mappings with the chosen
// main mapping and (with «все планы обмена») the suggested plans: existing
// mappings are kept, a suggestion joins only when its plan is not there
// yet, nothing is duplicated (PlanKey). removed lists what would be lost
// and needs a confirmation: an additional mapping of the chosen plan
// (it would duplicate the main one) and the old main table mapping of
// another plan (it is replaced; editing a manual SQL is no removal).
func MergeMappings(oldMain model.Mapping, existing []model.Mapping, chosen model.Mapping, suggested []model.Mapping) (merged []model.Mapping, removed []string) {
	ck := PlanKey(chosen)
	seen := map[string]bool{}
	if ck != "" {
		seen[ck] = true
	}
	if ok := PlanKey(oldMain); ok != "" && ok != ck {
		removed = append(removed, MappingName(oldMain))
	}
	for _, x := range existing {
		k := PlanKey(x)
		if k != "" && seen[k] {
			removed = append(removed, MappingName(x))
			continue
		}
		if k != "" {
			seen[k] = true
		}
		merged = append(merged, x)
	}
	for _, x := range suggested {
		k := PlanKey(x)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		merged = append(merged, x)
	}
	return merged, removed
}

// CheckRequest is check_mapping of the form (limit 0 = service default 5).
func (e *Editor) CheckRequest(m model.Mapping, password string, limit int) (model.IPCRequest, *Problem) {
	if limit < 0 || limit > model.MaxCheckMappingLimit {
		return model.IPCRequest{}, &Problem{Text: e.validationText([]string{"limit", model.ValRange, "1",
			strconv.Itoa(model.MaxCheckMappingLimit), strconv.Itoa(limit)}), Field: FieldDiscLimit}
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
	"mapping.keepUnlinkedPoints": i18n.KeyDiscKeepUnlinked, "mapping.timesSchema": i18n.KeyDiscTimesSchema,
	"mapping.timesLinkTypeColumn": i18n.KeyDiscLinkType, "mapping.timesLinkRTRefColumn": i18n.KeyDiscLinkRTRefCol,
	"mapping.timesLinkRTRef": i18n.KeyDiscLinkRTRef,
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
	s := tbl + "  [" + CandidateModeText(c.Mode, d.Lang) + "]  " + fmt.Sprintf(d.t(i18n.KeyDiscScore), c.Score)
	if c.Points > 0 {
		s += "  " + fmt.Sprintf(d.t(i18n.KeyDiscPoints), c.Points)
	}
	if c.FreshestAt != nil {
		s += "  " + model.InDBZone(*c.FreshestAt).Format("02.01.2006 15:04")
	}
	return s
}

// CandidateModeText localizes a candidate mode (unknown: as is).
func CandidateModeText(m model.CandidateMode, lang string) string {
	key := map[model.CandidateMode]string{model.CandidateSingle: i18n.KeyDiscCandSingle, model.CandidateLinked: i18n.KeyDiscCandLinked,
		model.CandidateCounters: i18n.KeyDiscCandCounters}[m]
	if key == "" {
		return string(m)
	}
	return i18n.T(key, lang)
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
	ck := PlanKey(chosen)
	seen := map[string]bool{}
	for _, m := range model.SuggestPlanMappings(d.Candidates) {
		k := PlanKey(m)
		if k == "" || k == ck || seen[k] {
			continue
		}
		seen[k] = true
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
	return e.Reply(m) // invalid_request query.* / limit: label + field (fieldLabel, FocusField)
}

// TemplateTitles are the manual SQL template names in lang.
func TemplateTitles(lang string) []string {
	var out []string
	for _, t := range model.SQLManualTemplates() {
		out = append(out, t.Title.In(lang))
	}
	return out
}
