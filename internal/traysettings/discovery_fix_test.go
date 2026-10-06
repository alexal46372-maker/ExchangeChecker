package traysettings

import (
	"context"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_07_ApplyValidation — the dialog applies only a valid
// mapping: an empty manual SQL / columns without a table / a broken
// additional plan are refused with the dialog's label and the field to
// highlight (a field the kind cannot edit moves to the box causing it).
func TestCHK_TRAY_07_ApplyValidation(t *testing.T) {
	d := NewDiscovery(i18n.RU, model.Mapping{})
	d.Map.SetKind(KindSQL)
	fe := ValidateMappings(d.Map.Mapping(), nil)
	if fe == nil || fe.Field != "mapping.manualSql" {
		t.Fatalf("empty manual SQL: %+v", fe)
	}
	pr := d.ValidationProblem(fe)
	if pr.Field != "mapping.manualSql" || !strings.Contains(pr.Text, "Ручной SQL") || strings.Contains(pr.Text, "manualSql") {
		t.Errorf("problem %+v", pr)
	}

	d.Map.SetKind(KindTables)
	d.Map.Send = "_Fld90" // a column without a table
	m := d.Map.Mapping()
	if m.Mode != model.MappingManual {
		t.Errorf("hand-typed columns are manual: %+v", m)
	}
	fe = ValidateMappings(m, nil)
	if fe == nil {
		t.Fatal("columns without a table must be refused")
	}
	if pr = d.ValidationProblem(fe); pr.Field != "mapping.table" || pr.Text == "" {
		t.Errorf("tables kind: %+v (from %+v)", pr, fe)
	}

	// additional plans: an unconfigured one names «Доп. план обмена 1»
	d.Select(-1)
	good := cand("_Node10", "_Node10", 9).Mapping()
	fe = ValidateMappings(good, []model.Mapping{{Mode: model.MappingAuto}})
	if fe == nil || fe.Field != "mappings[0].table" {
		t.Fatalf("extra: %+v", fe)
	}
	if pr = d.ValidationProblem(fe); !strings.Contains(pr.Text, "Доп. план обмена 1") {
		t.Errorf("extra label %+v", pr)
	}
	if fe = ValidateMappings(good, nil); fe != nil {
		t.Errorf("valid: %+v", fe)
	}
	if fe = ValidateMappings(model.Mapping{}, []model.Mapping{good}); fe == nil || fe.Field != "mapping.table" {
		t.Errorf("extras need a main mapping: %+v", fe)
	}
}

// TestCHK_TRAY_07_RequestsWithoutMapping — an invalid mapping edit never
// blocks test_profile / the Discovery commands (they are validated
// without the mapping); save names the mapping field with a label and
// focuses «Таблицы обмена…».
func TestCHK_TRAY_07_RequestsWithoutMapping(t *testing.T) {
	e := adminEditor(savedSQL())
	e.SetMapping(model.Mapping{Mode: model.MappingManual}, nil) // manual SQL empty
	for _, cmd := range []string{model.CmdTestProfile, model.CmdDiscover, model.CmdListNodePoints} {
		req, pr := e.InteractiveRequest(cmd, "")
		if pr != nil || req.ProfileID != "p1" || req.Profile != nil {
			t.Errorf("%s by id: %+v %+v", cmd, req, pr)
		}
	}
	e.Form.Database, e.Form.PasswordTyped = "db2", true
	req, pr := e.TestRequest("pw")
	if pr != nil || req.Profile == nil || req.Profile.Database != "db2" || req.Profile.Mapping.Configured() || len(req.Profile.Mappings) != 0 {
		t.Errorf("edited connection: %+v %+v", req, pr)
	}
	if _, pr = e.CheckRequest(model.Mapping{Mode: model.MappingManual, ManualSQL: "SELECT 1"}, "pw", 0); pr != nil {
		t.Errorf("check_mapping: %+v", pr)
	}
	_, pr = e.SaveRequest("pw")
	if pr == nil || pr.Field != FieldDiscoverButton || !strings.Contains(pr.Text, "Таблицы обмена — Ручной SQL") {
		t.Errorf("save: %+v", pr)
	}
}

// TestCHK_TRAY_07_FieldLabels — labels and focus of mapping.*,
// mappings[i].*, query.* and limit paths (validation / invalid_request).
func TestCHK_TRAY_07_FieldLabels(t *testing.T) {
	focus := map[string]Field{"mapping": FieldDiscoverButton, "mapping.manualSql": FieldDiscoverButton,
		"mappings": FieldDiscoverButton, "mappings[2].table": FieldDiscoverButton, "query.pointName": FieldDiscPoint,
		"query": FieldDiscPoint, "query.approxTime": FieldDiscTime, "query.toleranceMin": FieldDiscTolerance, "limit": FieldDiscLimit}
	for p, f := range focus {
		if got := FocusField(p, nil); got != f {
			t.Errorf("FocusField(%s) = %q", p, got)
		}
	}
	e := adminEditor(savedSQL())
	labels := map[string]string{"mapping.manualSql": "Таблицы обмена — Ручной SQL", "mappings[1].timesLinkRTRef": "Доп. план обмена 2 — Номер таблицы плана обмена",
		"mapping.timesSchema": "Схема таблицы времён", "query.approxTime": "Время обмена (UTC+3)", "query.toleranceMin": "Допуск, мин",
		"limit": "Число строк (limit)", "mappings": "Доп. планы обмена"}
	for p, want := range labels {
		if got := e.fieldLabel(p); !strings.Contains(got, want) {
			t.Errorf("label %s = %q", p, got)
		}
	}
	for _, p := range []string{"query.approxTime", "query.toleranceMin", "limit", "mappings[0].table"} {
		pr := e.InteractiveProblem(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorBadRequest,
			ErrorArgs: []string{p, model.ValRange, "1", "1000", "5000"}}, nil)
		if pr == nil || pr.Field != FocusField(p, nil) || strings.Contains(pr.Text, "«"+p+"»") {
			t.Errorf("invalid_request %s: %+v", p, pr)
		}
	}
	// check_mapping limit out of range: label + focus
	if _, pr := e.CheckRequest(model.Mapping{}, "", 99); pr == nil || pr.Field != FieldDiscLimit || !strings.Contains(pr.Text, "Число строк") {
		t.Errorf("limit %+v", pr)
	}
}

// TestCHK_TRAY_07_MergePlans — «все планы обмена» merges without
// duplicates (schema "" = dbo, case-insensitive) and lists what would be
// removed (confirmation).
func TestCHK_TRAY_07_MergePlans(t *testing.T) {
	chosen := model.Mapping{Mode: model.MappingAuto, Schema: "dbo", Table: "_Node10", PointNameColumn: "_Description", LastSendColumn: "_Fld1"}
	a := model.Mapping{Mode: model.MappingManual, Table: "_Node20", PointNameColumn: "_Description", LastSendColumn: "_Fld2", PlanLabel: "Магазины"}
	sugg := []model.Mapping{{Mode: model.MappingAuto, Schema: "DBO", Table: "_NODE20", PointNameColumn: "x", LastSendColumn: "y"},
		{Mode: model.MappingAuto, Table: "_node10", PointNameColumn: "x", LastSendColumn: "y"},
		{Mode: model.MappingAuto, Table: "_Node30", PointNameColumn: "x", LastSendColumn: "y"},
		{Mode: model.MappingAuto, Schema: "", Table: "_Node30", PointNameColumn: "z", LastSendColumn: "y"}}
	merged, removed := MergeMappings(chosen, []model.Mapping{a}, chosen, sugg)
	if len(removed) != 0 || len(merged) != 2 || merged[0] != a || merged[1].Table != "_Node30" {
		t.Errorf("merge %+v removed %v", merged, removed)
	}
	// an additional mapping of the chosen plan and an old main of another plan are removals
	dup := model.Mapping{Mode: model.MappingManual, Schema: "DBO", Table: "_NODE10", PointNameColumn: "p", LastSendColumn: "s"}
	merged, removed = MergeMappings(a, []model.Mapping{dup}, chosen, nil)
	if len(merged) != 0 || len(removed) != 2 || !strings.Contains(strings.Join(removed, ","), "Магазины") {
		t.Errorf("removals %+v %v", merged, removed)
	}
	// editing a manual SQL main is no removal
	sql := model.Mapping{Mode: model.MappingManual, ManualSQL: "SELECT 1"}
	if _, removed = MergeMappings(sql, nil, model.Mapping{Mode: model.MappingManual, ManualSQL: "SELECT 2"}, nil); len(removed) != 0 {
		t.Errorf("sql edit %v", removed)
	}
	if PlanKey(model.Mapping{Table: "_Node10"}) != PlanKey(model.Mapping{Schema: " DBO ", Table: "_NODE10"}) || PlanKey(sql) != "" {
		t.Error("PlanKey")
	}
	d := NewDiscovery(i18n.RU, model.Mapping{})
	d.DiscoveryDone(model.IPCMessage{Type: model.MsgDiscovery, Candidates: []model.DiscoveryCandidate{cand("_Node10", "_Node10", 9), cand("_NODE20", "_NODE20", 5), cand("_Node20", "_Node20", 4)}})
	if pm := d.PlanMappings(d.Map.Mapping()); len(pm) != 1 {
		t.Errorf("plan mappings %+v", pm)
	}
}

// TestCHK_TRAY_07_ManualColumns — manual mode: only the fields of the
// kind are part of the mapping; hidden link fields are reset when the
// table is changed by hand; hand-edited columns are saved as manual.
func TestCHK_TRAY_07_ManualColumns(t *testing.T) {
	d := NewDiscovery(i18n.RU, model.Mapping{})
	d.DiscoveryDone(model.IPCMessage{Type: model.MsgDiscovery, Candidates: []model.DiscoveryCandidate{cand("_Node10", "_Node10", 9)}})
	if d.Edited() || !d.Unapplied() {
		t.Error("a candidate is loaded, not applied")
	}
	if m := d.Map.Mapping(); m.Mode != model.MappingAuto || m.TimesLinkRTRef != 10 {
		t.Fatalf("candidate %+v", m)
	}
	d.Map.Send = "_Fld95"
	m := d.Map.Mapping()
	if m.Mode != model.MappingManual || m.TimesLinkRTRef != 10 || m.TimesLinkTypeColumn != "_Fld89_TYPE" || !d.Edited() {
		t.Errorf("edited column: %+v", m)
	}
	d.Map.Send = "_Fld90"
	if d.Map.Mapping().Mode != model.MappingAuto || d.Edited() {
		t.Error("back to the candidate columns = auto again")
	}
	d.Map.Table = "_Node11"
	m = d.Map.Mapping()
	if m.Mode != model.MappingManual || m.TimesLinkRTRef != 0 || m.TimesLinkTypeColumn != "" || m.TimesLinkRTRefColumn != "" || m.TimesSchema != "" {
		t.Errorf("table changed by hand: hidden link fields must be reset: %+v", m)
	}
	d.Map.SetKind(KindSQL)
	d.Map.ManualSQL = "SELECT 1"
	m = d.Map.Mapping()
	if m.Table != "" || m.LastSendColumn != "" || m.TimesTable != "" || m.Mode != model.MappingManual {
		t.Errorf("SQL kind keeps no hidden table fields: %+v", m)
	}
	d.Map.SetKind(KindTables)
	if d.Map.ManualSQL != "" || d.Map.Mapping().ManualSQL != "" {
		t.Error("tables kind keeps no hidden SQL")
	}
	for _, c := range []struct {
		k  MappingKind
		f  string
		ok bool
	}{{KindSQL, "table", false}, {KindSQL, "manualSql", true}, {KindSQL, "yearOffset", true}, {KindTables, "manualSql", false},
		{KindCounters, "timesTable", false}, {KindCounters, "sentNoColumn", true}, {KindTables, "timesLinkColumn", true}} {
		if FieldEditable(c.k, c.f) != c.ok {
			t.Errorf("FieldEditable(%d, %s)", c.k, c.f)
		}
	}
	// a discovery reply over hand edits keeps them (nothing selected);
	// after AcceptOverwrite the best candidate fills the form again
	d.Map.PlanLabel = "typed"
	d.DiscoveryDone(model.IPCMessage{Type: model.MsgDiscovery, Candidates: []model.DiscoveryCandidate{cand("_Node10", "_Node10", 9)}})
	if d.Sel != -1 || d.Map.PlanLabel != "typed" {
		t.Errorf("reply over edits: sel %d label %q", d.Sel, d.Map.PlanLabel)
	}
	d.AcceptOverwrite()
	d.DiscoveryDone(model.IPCMessage{Type: model.MsgDiscovery, Candidates: []model.DiscoveryCandidate{cand("_Node10", "_Node10", 9)}})
	if d.Sel != 0 || d.Map.Table != "_Node10" {
		t.Errorf("after AcceptOverwrite: sel %d", d.Sel)
	}
	// the profile's own manual mapping keeps its label
	own := model.Mapping{Mode: model.MappingManual, Table: "_T", PointNameColumn: "p", LastSendColumn: "s"}
	d2 := NewDiscovery(i18n.EN, own)
	if d2.Unapplied() || d2.Map.Mapping() != own {
		t.Errorf("own mapping %+v", d2.Map.Mapping())
	}
	if KindOf(model.Mapping{Mode: model.MappingManual, Table: "_T", ManualSQL: "SELECT 1"}) != KindSQL ||
		KindOf(model.Mapping{Mode: model.MappingAuto, Table: "_T", ManualSQL: " SELECT 1 "}) != KindSQL {
		t.Error("table + SQL is a manual SQL mapping (the service polls the SQL)")
	}
}

// TestCHK_TRAY_07_ToleranceAndTexts — the tolerance hint says 0 = 1 day
// (model.DefaultValueToleranceMin); validation.sql_* texts are localized;
// the timeout of the commands is DiscoveryTimeout.
func TestCHK_TRAY_07_ToleranceAndTexts(t *testing.T) {
	if model.DefaultValueToleranceMin != 1440 {
		t.Fatal("DefaultValueToleranceMin changed: update the hint")
	}
	for _, lang := range []string{i18n.RU, i18n.EN} {
		h := i18n.T(i18n.KeyDiscToleranceHint, lang)
		if !strings.Contains(h, "0") || !strings.Contains(h, "1440") {
			t.Errorf("hint %s %q", lang, h)
		}
	}
	d := NewDiscovery(i18n.RU, model.Mapping{})
	q, pr := d.Query(ValueForm{Point: "Склад", PointIndex: -1, ApproxTime: "06.10.2026 09:00", Tolerance: "0"})
	if pr != nil || q.ToleranceMin > 0 {
		t.Errorf("tolerance 0 = service default: %+v %+v", q, pr)
	}
	e := adminEditor(savedSQL())
	for _, k := range []string{model.ValSQLComment, model.ValSQLManualHint, model.ValSQLManualOption, model.ValSQLManualLiteral, model.ValSQLManualForbidden} {
		var texts []string
		for _, lang := range []string{i18n.RU, i18n.EN} {
			e.Lang = lang
			pr := e.Reply(model.IPCMessage{Type: model.MsgError, Code: model.ErrCodeInvalidRequest, ErrorKey: model.MsgMonitorBadRequest,
				ErrorArgs: []string{"mapping.manualSql", k, "x"}})
			if pr == nil || pr.Text == "" || strings.Contains(pr.Text, "validation.") || strings.Contains(pr.Text, "{") || pr.Field != FieldDiscoverButton {
				t.Errorf("%s/%s: %+v", k, lang, pr)
				continue
			}
			texts = append(texts, pr.Text)
		}
		if len(texts) == 2 && texts[0] == texts[1] {
			t.Errorf("%s not translated: %q", k, texts[0])
		}
	}
	e.Lang = i18n.RU
	if pr := e.InteractiveProblem(model.IPCMessage{}, context.DeadlineExceeded); pr == nil || pr.Text != i18n.Message(model.MsgDiscoverTimeout, i18n.RU) {
		t.Errorf("timeout %+v", pr)
	}
}

// usesSQL is what the service polls (sqlcheck usesManualSQL / ReaderFor).
func usesSQL(m model.Mapping) bool { return strings.TrimSpace(m.ManualSQL) != "" }

// TestCHK_TRAY_07_MappingRoundTrip — FromMapping -> Mapping() keeps every
// kind (tables with hidden link fields, manual SQL, counters); a table +
// manual SQL mapping opens as manual SQL with its SQL; Apply without
// edits returns the profile's own mapping unchanged (the service's
// effective mode never changes, no edit is made).
func TestCHK_TRAY_07_MappingRoundTrip(t *testing.T) {
	tables := cand("_Node10", "_Node10", 9).Mapping()
	tables.PlanLabel, tables.KeepUnlinkedPoints = "Магазины", true
	hand := model.Mapping{Mode: model.MappingManual, Schema: "s1", Table: "_T", PointNameColumn: "_Description", PointIDColumn: "_Code",
		LastSendColumn: "_Fld1", LastRecvColumn: "_Fld2"}
	sql := model.Mapping{Mode: model.MappingManual, ManualSQL: "SELECT a, b, c FROM t", YearOffset: model.YearOffset1C, PlanLabel: "SQL"}
	counters := model.Mapping{Mode: model.MappingCounters, Schema: "dbo", Table: "_Node10", PointNameColumn: "_Description",
		PointKeyColumn: "_IDRRef", SentNoColumn: "_SentNo", ReceivedNoColumn: "_ReceivedNo", PlanLabel: "Счётчики"}
	for name, m := range map[string]model.Mapping{"tables": tables, "hand": hand, "sql": sql, "counters": counters} {
		f := FromMapping(m)
		if got := f.Mapping(); got != m {
			t.Errorf("%s round trip:\n got %+v\nwant %+v", name, got, m)
		}
		if KindOf(m) != f.Kind {
			t.Errorf("%s kind", name)
		}
	}
	if FromMapping(sql).Kind != KindSQL || FromMapping(counters).Kind != KindCounters || FromMapping(tables).Kind != KindTables {
		t.Error("kinds")
	}

	// table + manual SQL: the service polls the SQL
	both := hand
	both.ManualSQL, both.YearOffset = "SELECT a, b, c FROM t", model.YearOffset1C
	f := FromMapping(both)
	if f.Kind != KindSQL || f.ManualSQL != both.ManualSQL {
		t.Fatalf("table + SQL opens as %d with SQL %q", f.Kind, f.ManualSQL)
	}
	if got := f.Mapping(); !usesSQL(got) || got.ManualSQL != both.ManualSQL || got.YearOffset != both.YearOffset {
		t.Errorf("table + SQL form keeps the SQL: %+v", got)
	}
	for name, m := range map[string]model.Mapping{"tables": tables, "hand": hand, "sql": sql, "counters": counters, "both": both, "none": {}} {
		d := NewDiscovery(i18n.RU, m)
		if d.Unapplied() {
			t.Errorf("%s: unapplied without edits", name)
		}
		r, edited := d.Result()
		if edited || r != m || usesSQL(r) != usesSQL(m) {
			t.Errorf("%s: Apply without edits changed the mapping: %+v", name, r)
		}
	}
	// switching table + SQL to tables brings its table part back; the SQL goes
	f.SetKind(KindTables)
	if got := f.Mapping(); usesSQL(got) || got.Table != "_T" || got.LastSendColumn != "_Fld1" || got.Mode != model.MappingManual {
		t.Errorf("table + SQL switched to tables: %+v", got)
	}
	// an edited SQL is an edit (SQL kind: no hidden table part)
	d := NewDiscovery(i18n.RU, both)
	d.Map.ManualSQL = "SELECT x, y, z FROM t"
	if r, edited := d.Result(); !edited || r.ManualSQL != "SELECT x, y, z FROM t" || r.Table != "" {
		t.Errorf("edited SQL: %+v", r)
	}
	// a new candidate list bumps the generation
	g := d.Gen
	d.DiscoveryDone(model.IPCMessage{Type: model.MsgDiscovery})
	if d.Gen != g+1 {
		t.Error("Gen")
	}
}
