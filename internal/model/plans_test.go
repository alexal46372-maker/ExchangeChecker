package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func compositeTestMapping() Mapping {
	return Mapping{Mode: MappingAuto, Table: "_Node124", PointNameColumn: "_Description", PointKeyColumn: "_IDRRef",
		TimesTable: "_InfoRg600", TimesLinkColumn: "_Fld601_RRRef", TimesLinkTypeColumn: "_Fld601_TYPE",
		TimesLinkRTRefColumn: "_Fld601_RTRef", TimesLinkRTRef: 124, LastSendColumn: "_Fld602", PlanLabel: "Магазин"}
}

func TestCHK_SQL_03_PlanHelpers(t *testing.T) {
	for in, want := range map[string]int{"_Node123": 123, "_node7": 7, "_Node": 0, "_Node12x": 0, "_Reference10": 0, "Node1": 0, "": 0} {
		if got := PlanNoOf(in); got != want {
			t.Errorf("PlanNoOf(%q) = %d, want %d", in, got, want)
		}
		if (PlanTableOf(in) != "") != (want != 0) {
			t.Errorf("PlanTableOf(%q) = %q", in, PlanTableOf(in))
		}
	}
	for _, n := range []int{0, 1, 124, 65535, 0x01020304, MaxRTRef} {
		b := RTRefBytes(n)
		if v, ok := RTRefValue(b); len(b) != 4 || !ok || v != n {
			t.Errorf("RTRef %d -> %x -> %d %v", n, b, v, ok)
		}
	}
	if b := RTRefBytes(124); b[0] != 0 || b[3] != 0x7c {
		t.Errorf("big endian: %x", b)
	}
	if _, ok := RTRefValue([]byte{1, 2}); ok {
		t.Error("short RTRef")
	}
	m := compositeTestMapping()
	if !m.Composite() || m.PlanTable() != "_Node124" || m.PlanNo() != 124 || m.Validate() != nil {
		t.Errorf("composite mapping %+v: %v", m, m.Validate())
	}
	if (Mapping{Table: "_Reference7"}).PlanTable() != "" {
		t.Error("non-plan table")
	}
}

func TestCHK_SQL_03_ValidateComposite(t *testing.T) {
	base := compositeTestMapping()
	mut := func(f func(*Mapping)) Mapping { m := base; f(&m); return m }
	for name, m := range map[string]Mapping{
		"not linked":     mut(func(m *Mapping) { m.TimesTable, m.TimesLinkColumn, m.PointKeyColumn = "", "", "" }),
		"no rtref col":   mut(func(m *Mapping) { m.TimesLinkRTRefColumn = "" }),
		"no type col":    mut(func(m *Mapping) { m.TimesLinkTypeColumn = "" }),
		"rtref 0":        mut(func(m *Mapping) { m.TimesLinkRTRef = 0 }),
		"rtref too big":  mut(func(m *Mapping) { m.TimesLinkRTRef = MaxRTRef + 1 }),
		"rtref no cols":  mut(func(m *Mapping) { m.TimesLinkTypeColumn, m.TimesLinkRTRefColumn = "", "" }),
		"negative rtref": mut(func(m *Mapping) { m.TimesLinkRTRef = -1 }),
	} {
		if err := m.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCHK_SQL_03_ProfileMappings(t *testing.T) {
	p := sqlTestProfile()
	if p.AllMappings() != nil {
		t.Error("unconfigured primary -> nil")
	}
	p.Mapping = Mapping{Mode: MappingAuto, Table: "_Node123", PointNameColumn: "_Description", LastSendColumn: "_Fld1", PlanLabel: "Основной"}
	p.Mappings = []Mapping{compositeTestMapping()}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if all := p.AllMappings(); len(all) != 2 || all[0].Table != "_Node123" || all[1].Table != "_Node124" {
		t.Errorf("AllMappings %+v", all)
	}
	bad := map[string]func(*Profile){
		"extra without primary": func(p *Profile) { p.Mapping = Mapping{} },
		"extra unconfigured":    func(p *Profile) { p.Mappings = append(p.Mappings, Mapping{Mode: MappingAuto}) },
		"extra invalid":         func(p *Profile) { p.Mappings[0].TimesLinkRTRef = 0 },
	}
	for name, f := range bad {
		q := p
		q.Mappings = append([]Mapping(nil), p.Mappings...)
		f(&q)
		if err := q.Validate(); err == nil || !strings.Contains(err.Error(), "mapping") {
			t.Errorf("%s: %v", name, err)
		}
	}
	// JSON: new fields omitted when empty; old documents load unchanged
	old := sqlTestProfile()
	old.Mapping = Mapping{Mode: MappingAuto, Table: "t", PointNameColumn: "n", LastSendColumn: "s"}
	b, _ := json.Marshal(old)
	for _, k := range []string{"mappings", "timesLinkTypeColumn", "timesLinkRTRefColumn", "timesLinkRTRef", "planLabel", "serverPreset"} {
		if strings.Contains(string(b), k) {
			t.Errorf("%s not omitted: %s", k, b)
		}
	}
	b, _ = json.Marshal(p)
	var back Profile
	if err := json.Unmarshal(b, &back); err != nil || len(back.Mappings) != 1 || back.Mappings[0].TimesLinkRTRef != 124 ||
		back.Mappings[0].TimesLinkTypeColumn != "_Fld601_TYPE" || back.Mapping.PlanLabel != "Основной" {
		t.Errorf("roundtrip %s -> %+v %v", b, back, err)
	}
	for _, k := range []string{`"mappings":[`, `"timesLinkRTRef":124`, `"planLabel":"Магазин"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("json missing %s: %s", k, b)
		}
	}
}

func TestCHK_SQL_02_SuggestPlanMappings(t *testing.T) {
	c := func(table, times string, mode CandidateMode) DiscoveryCandidate {
		return DiscoveryCandidate{Mode: mode, Schema: "dbo", Table: table, PointCol: "_Description", PointKeyCol: "_IDRRef",
			TimesTable: times, LinkCol: "_Fld1RRef", SendCol: "_Fld2"}
	}
	ms := SuggestPlanMappings([]DiscoveryCandidate{
		c("_Node124", "_InfoRg600", CandidateLinked), c("_Node123", "_InfoRg456", CandidateLinked),
		c("_Node123", "_InfoRg500", CandidateLinked), c("_Reference10", "_InfoRg11", CandidateLinked),
		c("_Node125", "", CandidateCounters),
	})
	if len(ms) != 2 || ms[0].Table != "_Node124" || ms[1].TimesTable != "_InfoRg456" {
		t.Errorf("suggested %+v", ms)
	}
}

func TestCHK_SQL_05_ServerPreset(t *testing.T) {
	p := sqlTestProfile()
	if err := p.ApplyServerPreset(ServerPresetMSSQL2008); err != nil || p.TLSMinVersion != TLSMin10 || p.EffectiveTLSMinVersion() != "1.0" {
		t.Fatalf("%+v %v", p, err)
	}
	if err := p.ApplyServerPreset(""); err != nil || p.ServerPreset != "" || p.TLSMinVersion != TLSMin10 {
		t.Errorf("clear preset keeps explicit fields: %+v", p)
	}
	q := sqlTestProfile()
	if q.EffectiveTLSMinVersion() != "" {
		t.Error("default TLS")
	}
	q.ServerPreset = ServerPresetMSSQL2008
	if q.EffectiveTLSMinVersion() != TLSMin10 || q.Validate() != nil {
		t.Error("implicit TLS 1.0")
	}
	q.TLSMinVersion = TLSMin12
	if q.EffectiveTLSMinVersion() != TLSMin12 {
		t.Error("explicit TLS wins")
	}
}
