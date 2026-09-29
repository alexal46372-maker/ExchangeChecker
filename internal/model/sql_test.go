package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sqlTestProfile() Profile {
	return Profile{ID: "p1", Server: `srv\SQL2005`, Database: "db", AuthMode: AuthSQL, Login: "sa", Enabled: true}
}

func TestCHK_SQL_01_ProfileConnOptions(t *testing.T) {
	p := sqlTestProfile()
	if p.Validate() != nil || p.ConnectTimeout() != 15*time.Second || p.QueryTimeout() != 30*time.Second {
		t.Fatal("defaults")
	}
	p.ConnectTimeoutSec, p.QueryTimeoutSec = 5, 120
	if p.ConnectTimeout() != 5*time.Second || p.QueryTimeout() != 2*time.Minute {
		t.Error("custom timeouts")
	}
	for _, e := range []EncryptMode{EncryptDefault, EncryptOptional, EncryptDisable, EncryptOn, EncryptStrict} {
		for _, v := range []string{"", "1.0", "1.1", "1.2"} {
			q := sqlTestProfile()
			q.Encrypt, q.TLSMinVersion = e, v
			if err := q.Validate(); err != nil {
				t.Errorf("%q/%q: %v", e, v, err)
			}
		}
	}
	bad := map[string]func(*Profile){
		"encrypt": func(p *Profile) { p.Encrypt = "yes" },
		"tls":     func(p *Profile) { p.TLSMinVersion = "1.3" },
		"connect": func(p *Profile) { p.ConnectTimeoutSec = -1 },
		"query":   func(p *Profile) { p.QueryTimeoutSec = MaxTimeoutSec + 1 },
		"yearOff": func(p *Profile) {
			p.Mapping = Mapping{Table: "t", PointNameColumn: "n", LastSendColumn: "s", YearOffset: 1}
		},
	}
	for name, f := range bad {
		q := sqlTestProfile()
		f(&q)
		if err := q.Validate(); err == nil || !strings.Contains(err.Error(), `"p1"`) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// phase-1 JSON still loads and validates
	var old Profile
	js := `{"id":"p1","name":"x","server":"s","database":"d","authMode":"windows","enabled":true,
	        "mapping":{"mode":"auto","table":"_Node1","pointNameColumn":"_Description","lastSendColumn":"_Fld1"}}`
	if err := json.Unmarshal([]byte(js), &old); err != nil || old.Validate() != nil || old.Encrypt != EncryptDefault || old.Mapping.YearOffset != 0 {
		t.Errorf("old profile: %+v %v", old, err)
	}
	b, _ := json.Marshal(sqlTestProfile())
	for _, k := range []string{"encrypt", "trustServerCert", "tlsMinVersion", "Timeout", "yearOffset", "timesTable", "sentNoColumn"} {
		if strings.Contains(string(b), k) {
			t.Errorf("zero field %s must be omitted: %s", k, b)
		}
	}
}

func TestCHK_SQL_04_MappingExtendedValidation(t *testing.T) {
	linked := Mapping{Mode: MappingAuto, Table: "_Reference77", PointNameColumn: "_Description", PointKeyColumn: "_IDRRef",
		TimesTable: "_InfoRg88", TimesLinkColumn: "_Fld89RRef", LastSendColumn: "_Fld90", YearOffset: YearOffset1C}
	counters := Mapping{Mode: MappingCounters, Table: "_Node123", PointNameColumn: "_Description", PointKeyColumn: "_IDRRef"}
	good := []Mapping{linked, counters,
		{Mode: MappingCounters, Table: "_Node123", PointIDColumn: "_Code", SentNoColumn: "_S", ReceivedNoColumn: "_R"},
		{Mode: MappingManual, Schema: "exch", Table: "t", PointNameColumn: "n", LastRecvColumn: "r"},
	}
	for i, m := range good {
		if err := m.Validate(); err != nil {
			t.Errorf("good[%d]: %v", i, err)
		}
	}
	if !linked.Linked() || counters.Linked() || counters.SentNoCol() != DefaultSentNoColumn || good[2].ReceivedNoCol() != "_R" {
		t.Error("helpers")
	}
	mut := func(m Mapping, f func(*Mapping)) Mapping { f(&m); return m }
	bad := map[string]Mapping{
		"linked no key":        mut(linked, func(m *Mapping) { m.PointKeyColumn = "" }),
		"linked no link":       mut(linked, func(m *Mapping) { m.TimesLinkColumn = "" }),
		"linked no table":      mut(linked, func(m *Mapping) { m.Table = "" }),
		"link without times":   mut(linked, func(m *Mapping) { m.TimesTable = "" }),
		"timesSchema only":     {Table: "t", PointNameColumn: "n", LastSendColumn: "s", TimesSchema: "x"},
		"year offset":          mut(linked, func(m *Mapping) { m.YearOffset = 1900 }),
		"counters no table":    {Mode: MappingCounters, ManualSQL: "SELECT 1, 2, 3"},
		"counters linked":      mut(counters, func(m *Mapping) { m.TimesTable, m.TimesLinkColumn = "x", "y" }),
		"counters manual":      mut(counters, func(m *Mapping) { m.ManualSQL = "SELECT a, b, c FROM t" }),
		"counters no name":     {Mode: MappingCounters, Table: "_Node1"},
		"counter cols in auto": {Mode: MappingAuto, Table: "t", PointNameColumn: "n", LastSendColumn: "s", SentNoColumn: "_SentNo"},
		"unknown mode":         {Mode: "count"},
	}
	for name, m := range bad {
		if err := m.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// JSON roundtrip of the extended mapping
	b, _ := json.Marshal(linked)
	var back Mapping
	if err := json.Unmarshal(b, &back); err != nil || back != linked {
		t.Errorf("roundtrip %s -> %+v", b, back)
	}
	if !strings.Contains(string(b), `"timesLinkColumn":"_Fld89RRef"`) || !strings.Contains(string(b), `"yearOffset":2000`) {
		t.Errorf("json names: %s", b)
	}
}

func TestCHK_SQL_02_CandidateMappingAndValueQuery(t *testing.T) {
	lc := DiscoveryCandidate{Mode: CandidateLinked, Table: "_Reference77", PointCol: "_Description", PointIDCol: "_Code",
		PointKeyCol: "_IDRRef", TimesTable: "_InfoRg88", LinkCol: "_Fld89RRef", SendCol: "_Fld90", RecvCol: "_Fld91",
		YearOffset: 2000, Hints: []string{HintLinked}}
	m := lc.Mapping()
	if m.Mode != MappingAuto || m.TimesTable != "_InfoRg88" || m.TimesLinkColumn != "_Fld89RRef" || m.PointKeyColumn != "_IDRRef" ||
		m.YearOffset != 2000 || m.Validate() != nil || !lc.HasHint(HintLinked) || lc.HasHint(HintCounters) {
		t.Errorf("linked mapping %+v", m)
	}
	sc := DiscoveryCandidate{Mode: CandidateSingle, Table: "_Reference55", PointCol: "_Description", SendCol: "_Fld56", RecvCol: "_Fld57"}
	if m := sc.Mapping(); m.Linked() || m.PointKeyColumn != "" || m.Validate() != nil {
		t.Errorf("single mapping %+v", m)
	}
	cc := DiscoveryCandidate{Mode: CandidateCounters, Table: "_Node123", PointCol: "_Description", PointKeyCol: "_IDRRef",
		SendCol: "_SentNo", RecvCol: "_ReceivedNo", YearOffset: 2000}
	if m := cc.Mapping(); m.Mode != MappingCounters || m.LastSendColumn != "" || m.SentNoColumn != "_SentNo" || m.YearOffset != 0 || m.Validate() != nil {
		t.Errorf("counters mapping %+v", m)
	}
	if lc.Key() == sc.Key() || lc.Key() != lc.Key() {
		t.Error("Key")
	}
	b, _ := json.Marshal(lc)
	var back DiscoveryCandidate
	if err := json.Unmarshal(b, &back); err != nil || back.Key() != lc.Key() || !strings.Contains(string(b), `"mode":"linked"`) {
		t.Errorf("candidate JSON %s %v", b, err)
	}
	if (ValueQuery{}).Tolerance() != DefaultValueToleranceMin*time.Minute || (ValueQuery{ToleranceMin: 5}).Tolerance() != 5*time.Minute {
		t.Error("Tolerance")
	}
}
