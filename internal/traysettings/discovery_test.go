package traysettings

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_07_HiddenWithoutCaps — the dialog needs the administrator
// window and all four capabilities.
func TestCHK_TRAY_07_HiddenWithoutCaps(t *testing.T) {
	all := map[string]bool{}
	for _, c := range DiscoveryCapabilities {
		all[c] = true
	}
	has := func(m map[string]bool) func(string) bool { return func(c string) bool { return m[c] } }
	if !DiscoveryAvailable(Admin, has(all)) || DiscoveryAvailable(ReadOnly, has(all)) || DiscoveryAvailable(Admin, nil) {
		t.Error("admin + all capabilities only")
	}
	for _, c := range DiscoveryCapabilities {
		m := map[string]bool{}
		for k := range all {
			m[k] = k != c
		}
		if DiscoveryAvailable(Admin, has(m)) {
			t.Errorf("without %s", c)
		}
	}
}

// TestCHK_TRAY_07_ProfileRules — the profile of the four commands follows
// test_profile: saved and unchanged -> profileId, edited -> profile +
// password; check_mapping carries the mapping and a valid limit.
func TestCHK_TRAY_07_ProfileRules(t *testing.T) {
	e := adminEditor(savedSQL())
	req, pr := e.InteractiveRequest(model.CmdDiscover, "")
	if pr != nil || req.Cmd != model.CmdDiscover || req.ProfileID != "p1" || req.Profile != nil {
		t.Fatalf("saved: %+v %+v", req, pr)
	}
	e.Form.Database, e.Form.PasswordTyped = "db2", true
	req, pr = e.InteractiveRequest(model.CmdListNodePoints, "pw")
	if pr != nil || req.Profile == nil || req.Profile.Database != "db2" || req.Password != "pw" {
		t.Fatalf("edited: %+v %+v", req, pr)
	}
	m := model.Mapping{Mode: model.MappingManual, ManualSQL: "SELECT 1"}
	req, pr = e.CheckRequest(m, "pw", 5)
	if pr != nil || req.Cmd != model.CmdCheckMapping || req.Mapping == nil || req.Mapping.ManualSQL != "SELECT 1" || req.Limit != 5 {
		t.Fatalf("check: %+v %+v", req, pr)
	}
	if _, pr = e.CheckRequest(m, "pw", 21); pr == nil {
		t.Error("limit 21 must be refused")
	}
}

func cand(table string, plan string, score int) model.DiscoveryCandidate {
	fresh := time.Date(2026, 10, 6, 9, 0, 0, 0, model.DBZone)
	return model.DiscoveryCandidate{Mode: model.CandidateLinked, Schema: "dbo", Table: table, PointCol: "_Description",
		PointKeyCol: "_IDRRef", TimesTable: "_InfoRg88", LinkCol: "_Fld89RRef", LinkTypeCol: "_Fld89_TYPE", LinkRTRefCol: "_Fld89_RTRef", LinkRTRef: 10,
		SendCol: "_Fld90", RecvCol: "_Fld91", YearOffset: model.YearOffset1C, PlanTable: plan, Points: 3, FreshestAt: &fresh,
		Score: score, Hints: []string{model.HintLinked, model.HintYearOffset2000, "new_hint"},
		SampleRows: []model.SampleRow{{Point: "Склад", LastSend: &fresh}}}
}

// TestCHK_TRAY_07_CandidateToMapping — candidates (missing list = empty,
// truncated = incomplete), selection into the editable mapping, hidden
// link fields kept, all plans, applying into the profile (save_profile).
func TestCHK_TRAY_07_CandidateToMapping(t *testing.T) {
	d := NewDiscovery(i18n.RU, model.Mapping{})
	d.DiscoveryDone(model.IPCMessage{Type: model.MsgDiscovery})
	if d.Candidates == nil || len(d.Candidates) != 0 || d.Sel != -1 || d.Status() != i18n.T(i18n.KeyDiscNone, i18n.RU) {
		t.Fatalf("empty: %+v %q", d.Candidates, d.Status())
	}
	d.DiscoveryDone(model.IPCMessage{Type: model.MsgDiscovery, Truncated: true,
		Candidates: []model.DiscoveryCandidate{cand("_Node10", "_Node10", 90), cand("_Node20", "_Node20", 80)}})
	if d.Sel != 0 || !strings.Contains(d.Status(), i18n.T(i18n.KeyDiscIncomplete, i18n.RU)) {
		t.Errorf("status %q", d.Status())
	}
	if d.Map.Table != "_Node10" || !d.Map.YearOffset2000 || d.Map.TimesTable != "_InfoRg88" {
		t.Errorf("form %+v", d.Map)
	}
	d.Map.PlanLabel = "Магазины"
	m := d.Map.Mapping()
	if m.TimesLinkTypeColumn != "_Fld89_TYPE" || m.TimesLinkRTRef != 10 || m.PlanLabel != "Магазины" || m.YearOffset != model.YearOffset1C || m.Mode != model.MappingAuto {
		t.Errorf("mapping %+v", m)
	}
	others := d.PlanMappings(m)
	if len(others) != 1 || others[0].Table != "_Node20" {
		t.Errorf("other plans %+v", others)
	}
	if !strings.Contains(d.CandidateLine(d.Candidates[0]), "_Node10 → _InfoRg88") {
		t.Errorf("line %q", d.CandidateLine(d.Candidates[0]))
	}

	e := adminEditor(savedSQL())
	e.SetMapping(m, others, true)
	if !e.Unsaved() || e.EffectiveMapping().PlanLabel != "Магазины" {
		t.Error("applied mapping must be an unsaved edit")
	}
	req, pr := e.SaveRequest("")
	if pr != nil || req.Profile.Mapping.Table != "_Node10" || len(req.Profile.Mappings) != 1 || req.ExpectedRevision != "rev1" {
		t.Fatalf("save_profile with mapping: %+v %+v", req.Profile, pr)
	}
	e.Select(0)
	if e.MapEdit != nil || e.Unsaved() {
		t.Error("selecting a profile drops the chosen mapping")
	}

	// manual SQL and counters modes
	d.Map.Mode, d.Map.ManualSQL = model.MappingManual, " SELECT 1 "
	if mm := d.Map.Mapping(); mm.Mode != model.MappingManual || mm.ManualSQL != "SELECT 1" {
		t.Errorf("manual %+v", mm)
	}
	d.Map.Mode = model.MappingCounters
	if mc := d.Map.Mapping(); mc.SentNoColumn != "_Fld90" || mc.LastSendColumn != "" || mc.YearOffset != 0 || mc.ManualSQL != "" {
		t.Errorf("counters %+v", mc)
	}
	if len(TemplateTitles(i18n.EN)) == 0 || len(TemplateTitles(i18n.EN)) != len(model.SQLManualTemplates()) {
		t.Error("templates")
	}
}

// TestCHK_TRAY_07_HintsLocalized — known hints are localized, unknown
// ones shown as is; details carry the sample rows in UTC+3.
func TestCHK_TRAY_07_HintsLocalized(t *testing.T) {
	for _, h := range []string{model.HintLinked, model.HintSameTable, model.HintNodeTable, model.HintGeneric, model.HintByValue,
		model.HintExactMatch, model.HintNearApproxTime, model.HintAllPointsFresh, model.HintYearOffset2000, model.HintSendRecvByName,
		model.HintSendRecvByOrder, model.HintSingleSeries, model.HintNoTimestamps, model.HintFutureDates, model.HintSampled,
		model.HintSampleFailed, model.HintTruncated, model.HintCounters, model.HintCompositeRef, model.HintByTime} {
		ru, en := HintText(h, i18n.RU), HintText(h, i18n.EN)
		if ru == h || en == h || ru == en {
			t.Errorf("hint %s: %q / %q", h, ru, en)
		}
	}
	d := NewDiscovery(i18n.RU, model.Mapping{})
	det := d.CandidateDetails(cand("_Node10", "_Node10", 1))
	if !strings.Contains(det, HintText(model.HintLinked, i18n.RU)) || !strings.Contains(det, "new_hint") ||
		!strings.Contains(det, "Склад: 06.10.2026 09:00 / —") {
		t.Errorf("details %q", det)
	}
}

// TestCHK_TRAY_07_TruncatedReason — «Результат неполный» names the
// truncatedReason of each reply with its hint: discovery budget / size,
// node_points limit / size, mapping_check size; missing, unknown or
// foreign to the reply — the general text.
func TestCHK_TRAY_07_TruncatedReason(t *testing.T) {
	for _, c := range []struct{ kind, reason, key string }{
		{ReplyDiscovery, model.TruncatedBudget, i18n.KeyDiscReasonBudget},
		{ReplyDiscovery, model.TruncatedSize, i18n.KeyDiscReasonSize},
		{ReplyDiscovery, model.TruncatedLimit, i18n.KeyDiscIncomplete},
		{ReplyDiscovery, "", i18n.KeyDiscIncomplete},
		{ReplyDiscovery, "future", i18n.KeyDiscIncomplete},
		{ReplyNodePoints, model.TruncatedLimit, i18n.KeyDiscReasonLimit},
		{ReplyNodePoints, model.TruncatedSize, i18n.KeyDiscReasonPointsSize},
		{ReplyNodePoints, model.TruncatedBudget, i18n.KeyDiscIncomplete},
		{ReplyNodePoints, "future", i18n.KeyDiscIncomplete},
		{ReplyCheck, model.TruncatedSize, i18n.KeyDiscReasonCheckSize},
		{ReplyCheck, model.TruncatedLimit, i18n.KeyDiscIncomplete},
		{ReplyCheck, "", i18n.KeyDiscIncomplete},
	} {
		for _, lang := range []string{i18n.RU, i18n.EN} {
			if got := IncompleteText(c.kind, c.reason, lang); got != i18n.T(c.key, lang) {
				t.Errorf("%s/%q/%s: %q", c.kind, c.reason, lang, got)
			}
		}
	}
	if ru := i18n.T(i18n.KeyDiscReasonBudget, i18n.RU); !strings.Contains(ru, "Повторите") || !strings.Contains(i18n.T(i18n.KeyDiscReasonSize, i18n.EN), "Refine") {
		t.Error("hints: retry / narrow, refine")
	}
	d := NewDiscovery(i18n.EN, model.Mapping{})
	d.DiscoveryDone(model.IPCMessage{Type: model.MsgDiscovery, Truncated: true, TruncatedReason: model.TruncatedBudget})
	if !strings.Contains(d.Status(), i18n.T(i18n.KeyDiscReasonBudget, i18n.EN)) {
		t.Errorf("discovery %q", d.Status())
	}
	d.DiscoveryDone(model.IPCMessage{Type: model.MsgDiscovery, Truncated: true, TruncatedReason: "future"})
	if !strings.HasSuffix(d.Status(), i18n.T(i18n.KeyDiscIncomplete, i18n.EN)) {
		t.Errorf("unknown reason %q", d.Status())
	}
	d.NodePointsDone(model.IPCMessage{Type: model.MsgNodePoints, Truncated: true, TruncatedReason: model.TruncatedLimit,
		NodePoints: []model.NodePoint{{Name: "A"}}})
	if !strings.Contains(d.PointsStatus(), i18n.T(i18n.KeyDiscReasonLimit, i18n.EN)) {
		t.Errorf("points %q", d.PointsStatus())
	}
	if txt, _ := d.CheckResult(model.MappingCheck{OK: true, Truncated: true, TruncatedReason: model.TruncatedSize}); !strings.Contains(txt, i18n.T(i18n.KeyDiscReasonCheckSize, i18n.EN)) {
		t.Errorf("check %q", txt)
	}
	// sampleRows cut to the requested limit is not truncated
	if txt, _ := d.CheckResult(model.MappingCheck{OK: true, SampleRows: []model.SampleRow{{Point: "A"}}}); strings.Contains(txt, i18n.T(i18n.KeyDiscIncomplete, i18n.EN)) {
		t.Errorf("complete preview %q", txt)
	}
	d.DiscoveryDone(model.IPCMessage{Type: model.MsgDiscovery})
	if strings.Contains(d.Status(), i18n.T(i18n.KeyDiscIncomplete, i18n.EN)) {
		t.Error("complete result")
	}
}

// TestCHK_TRAY_07_ByValueQuery — empty point refused locally (same key as
// the service), a listed point goes as pointRef, the approximate time is
// read in UTC+3, tolerance checked.
func TestCHK_TRAY_07_ByValueQuery(t *testing.T) {
	d := NewDiscovery(i18n.RU, model.Mapping{})
	if _, pr := d.Query(ValueForm{Point: "  ", PointIndex: -1}); pr == nil || pr.Field != FieldDiscPoint ||
		pr.Text != model.MonitorMessage(model.MsgDiscoverPointNameEmpty, i18n.RU) {
		t.Errorf("empty point %+v", pr)
	}
	d.NodePointsDone(model.IPCMessage{Type: model.MsgNodePoints, NodePoints: []model.NodePoint{{PlanTable: "_Node10", Ref: "ab01", Code: "001", Name: "Склад"}}})
	q, pr := d.Query(ValueForm{Point: PointLine(d.NodePoints[0]), PointIndex: 0, ApproxTime: "06.10.2026 09:30", Tolerance: "60"})
	if pr != nil || q.PointRef != "ab01" || q.PointName != "Склад" || q.PlanTable != "_Node10" || q.ToleranceMin != 60 ||
		!q.ApproxTime.Equal(time.Date(2026, 10, 6, 6, 30, 0, 0, time.UTC)) {
		t.Fatalf("listed point %+v %+v", q, pr)
	}
	q, pr = d.Query(ValueForm{Point: "Магазин 5", PointIndex: 0})
	if pr != nil || q.PointRef != "" || q.PointName != "Магазин 5" {
		t.Errorf("typed point %+v", q)
	}
	if _, pr = d.Query(ValueForm{Point: "x", PointIndex: -1, ApproxTime: "2026-10-06"}); pr == nil || pr.Field != FieldDiscTime {
		t.Errorf("bad time %+v", pr)
	}
	if _, pr = d.Query(ValueForm{Point: "x", PointIndex: -1, Tolerance: "-1"}); pr == nil || pr.Field != FieldDiscTolerance {
		t.Errorf("bad tolerance %+v", pr)
	}
	if !strings.Contains(d.PointsStatus(), "1") {
		t.Error("points status")
	}
}

// TestCHK_TRAY_07_ManualSQLFlow — mapping_check results: ok with sample
// rows, a field error highlighted and localized, 2012+ violations,
// a connection error key, truncated preview.
func TestCHK_TRAY_07_ManualSQLFlow(t *testing.T) {
	d := NewDiscovery(i18n.RU, model.Mapping{})
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, model.DBZone)
	txt, f := d.CheckResult(model.MappingCheck{OK: true, SampleRows: []model.SampleRow{{Point: "A", LastSend: &at, LastRecv: &at}}, Truncated: true})
	if f != "" || !strings.HasPrefix(txt, i18n.T(i18n.KeyDiscCheckOK, i18n.RU)) || !strings.Contains(txt, "A: 06.10.2026 09:00 / 06.10.2026 09:00") ||
		!strings.Contains(txt, i18n.T(i18n.KeyDiscIncomplete, i18n.RU)) {
		t.Errorf("ok: %q", txt)
	}
	txt, f = d.CheckResult(model.MappingCheck{ErrorKey: model.MsgDiscoverMappingInvalid,
		ErrorArgs: []string{"mapping.lastSendColumn", model.ValRequired}})
	if f != "mapping.lastSendColumn" || !strings.Contains(txt, "Колонка отправки") ||
		!strings.Contains(txt, model.MonitorMessage(model.MsgDiscoverMappingInvalid, i18n.RU)) {
		t.Errorf("field error: %q %q", txt, f)
	}
	txt, _ = d.CheckResult(model.MappingCheck{ErrorKey: model.MsgDiscoverMappingInvalid, Violations: []string{"OFFSET/FETCH", "IIF"}})
	if !strings.Contains(txt, "OFFSET/FETCH, IIF") || !strings.Contains(txt, i18n.T(i18n.KeyDiscViolations, i18n.RU)) {
		t.Errorf("violations: %q", txt)
	}
	keys := model.SQLMessageKeys()
	txt, _ = d.CheckResult(model.MappingCheck{ErrorKey: keys[0], Error: "tech"})
	if txt != model.SQLMessage(keys[0], i18n.RU) {
		t.Errorf("sql key: %q", txt)
	}
}

// TestCHK_TRAY_07_ProtocolErrors — busy, rate_limited, failed (sql.* /
// monitor.* texts), canceled, empty point, transport errors.
func TestCHK_TRAY_07_ProtocolErrors(t *testing.T) {
	e := adminEditor(savedSQL())
	msg := func(code, key string) model.IPCMessage {
		return model.IPCMessage{Type: model.MsgError, Code: code, ErrorKey: key, Error: "tech"}
	}
	sqlKey := model.SQLMessageKeys()[0]
	for _, c := range []struct {
		m    model.IPCMessage
		want string
	}{
		{msg(model.ErrCodeBusy, model.MsgDiscoverBusy), model.MonitorMessage(model.MsgDiscoverBusy, i18n.RU)},
		{msg(model.ErrCodeBusy, model.MsgDiscoverProfileBusy), model.MonitorMessage(model.MsgDiscoverProfileBusy, i18n.RU)},
		{msg(model.ErrCodeFailed, sqlKey), model.SQLMessage(sqlKey, i18n.RU)},
		{msg(model.ErrCodeFailed, model.MsgDiscoverTimeout), model.MonitorMessage(model.MsgDiscoverTimeout, i18n.RU)},
		{msg(model.ErrCodeFailed, ""), model.MonitorMessage(model.MsgDiscoverFailed, i18n.RU)},
		{msg(model.ErrCodeCanceled, model.MsgMonitorCanceled), i18n.T(i18n.KeyDiscCanceled, i18n.RU)},
	} {
		if pr := e.InteractiveProblem(c.m, nil); pr == nil || pr.Text != c.want {
			t.Errorf("%s/%s: %+v", c.m.Code, c.m.ErrorKey, pr)
		}
	}
	// code internal (a recovered panic of the service handler): the
	// general text of monitor.internal, never the technical one
	if pr := e.InteractiveProblem(msg(model.ErrCodeInternal, model.MsgMonitorInternal), nil); pr == nil ||
		!strings.Contains(pr.Text, model.MonitorMessage(model.MsgMonitorInternal, i18n.RU)) || strings.Contains(pr.Text, "tech") {
		t.Errorf("internal %+v", pr)
	}
	if pr := e.InteractiveProblem(msg(model.ErrCodeRateLimited, model.MsgMonitorRateLimited), nil); pr == nil || !strings.Contains(pr.Text, "10") {
		t.Errorf("rate limited %+v", pr)
	}
	pr := e.InteractiveProblem(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgDiscoverPointNameEmpty,
		ErrorArgs: []string{"query.pointName", model.ValRequired}}, nil)
	if pr == nil || pr.Field != FieldDiscPoint {
		t.Errorf("point empty %+v", pr)
	}
	if pr := e.InteractiveProblem(model.IPCMessage{}, context.Canceled); pr == nil || pr.Text != i18n.T(i18n.KeyDiscCanceled, i18n.RU) {
		t.Errorf("ctx canceled %+v", pr)
	}
	if pr := e.InteractiveProblem(model.IPCMessage{Type: model.MsgDiscovery}, nil); pr != nil {
		t.Error("a reply is no problem")
	}
}

// TestCHK_TRAY_07_FailedKeys — every errorKey of code failed (PLAN §6.2)
// is localized; for check_mapping the same keys come in ok:false; an
// unknown error code is a general error.
func TestCHK_TRAY_07_FailedKeys(t *testing.T) {
	e := adminEditor(savedSQL())
	d := NewDiscovery(i18n.RU, model.Mapping{})
	keys := []string{"monitor.test_network", "monitor.test_login", "monitor.test_database", "monitor.test_tls",
		"monitor.conn_error", "monitor.query_error", "monitor.mapping", "monitor.discover_timeout", "monitor.discover_failed",
		model.SQLMessageKeys()[0]}
	for _, k := range keys {
		for _, lang := range []string{i18n.RU, i18n.EN} {
			want := i18n.Message(k, lang)
			if want == "" || strings.Contains(want, k) {
				t.Errorf("%s/%s not localized: %q", k, lang, want)
			}
		}
		pr := e.InteractiveProblem(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeFailed, ErrorKey: k, Error: "tech"}, nil)
		if pr == nil || pr.Text != i18n.Message(k, i18n.RU) {
			t.Errorf("failed %s: %+v", k, pr)
		}
		if txt, _ := d.CheckResult(model.MappingCheck{ErrorKey: k, Error: "tech"}); txt != i18n.Message(k, i18n.RU) {
			t.Errorf("ok:false %s: %q", k, txt)
		}
	}
	if pr := e.InteractiveProblem(model.IPCMessage{Type: model.MsgError, Code: "future_code", ErrorKey: "monitor.future", Error: "tech"}, nil); pr == nil || pr.Text == "" {
		t.Errorf("unknown code: %+v", pr)
	}
}
