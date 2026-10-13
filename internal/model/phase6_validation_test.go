package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// CHK-CFG-03: Validate returns *FieldError with the JSON field path and a
// validation.* reason; Error() text is unchanged.
func TestCHK_CFG_03_FieldErrors(t *testing.T) {
	big := strings.Repeat("x", MaxManualSQLBytes+1)
	mapping := Mapping{Mode: MappingAuto, Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "_Fld1"}
	cases := []struct {
		name, field, key, text string
		args                   []string
		mut                    func(*Profile)
	}{
		{"empty server", "server", ValRequired, `profile "p1": server is required`, nil, func(p *Profile) { p.Server = " " }},
		// CHK-CFG-05: a leading backslash is a forbidden SQL Server address
		{"empty host", "server", ValSQLServerForbidden, "not allowed (backslash)", []string{"backslash"}, func(p *Profile) { p.Server = `\SQL` }},
		{"empty instance", "server", ValServerFormat, "instance is empty", []string{"empty_instance"}, func(p *Profile) { p.Server = `host\ ` }},
		{"second backslash", "server", ValServerFormat, "more than one backslash", []string{"backslash"}, func(p *Profile) { p.Server = `h\a\b` }},
		{"port", "port", ValRange, `profile "p1": port 70000 out of range`, []string{"0", "65535", "70000"}, func(p *Profile) { p.Port = 70000 }},
		{"database", "database", ValRequired, `profile "p1": database is required`, nil, func(p *Profile) { p.Database = "" }},
		{"auth", "authMode", ValEnum, `unknown auth mode "x"`, []string{"x"}, func(p *Profile) { p.AuthMode = "x" }},
		{"login", "login", ValRequired, `profile "p1": login is required for SQL authentication`, nil, func(p *Profile) { p.Login = "" }},
		{"encrypt", "encrypt", ValEnum, "unknown encrypt mode", []string{"bad"}, func(p *Profile) { p.Encrypt = "bad" }},
		{"preset", "encrypt", ValPresetConflict, "strict", []string{"strict", ServerPresetMSSQL2008}, func(p *Profile) {
			p.Encrypt, p.ServerPreset = EncryptStrict, ServerPresetMSSQL2008
		}},
		{"timeout", "queryTimeoutSec", ValRange, "queryTimeoutSec must be", []string{"0", itoa(MaxTimeoutSec), "9999"}, func(p *Profile) { p.QueryTimeoutSec = 9999 }},
		{"thresholds", "errorMinutes", ValGreaterThan, "errorMinutes (5) must be greater than warnMinutes (10)", []string{"warnMinutes", "10"},
			func(p *Profile) { p.WarnMinutes, p.ErrorMinutes = 10, 5 }},
		{"mapping mode", "mapping.mode", ValEnum, `mapping: unknown mode "zz"`, []string{"zz"}, func(p *Profile) { p.Mapping.Mode = "zz" }},
		{"mapping columns", "mapping.lastSendColumn", ValRequired, "last-send/last-recv", nil, func(p *Profile) {
			p.Mapping = Mapping{Mode: MappingAuto, Table: "_Node1", PointNameColumn: "_Description"}
		}},
		{"mapping yearOffset", "mapping.yearOffset", ValEnum, "yearOffset must be 0", []string{"5"}, func(p *Profile) {
			p.Mapping = mapping
			p.Mapping.YearOffset = 5
		}},
		{"manualSql too long", "mapping.manualSql", ValTooLong, "manualSql longer than 16384 bytes", []string{"16384"}, func(p *Profile) {
			p.Mapping = Mapping{Mode: MappingManual, ManualSQL: big}
		}},
		{"mappings[1].table", "mappings[1].table", ValRequired, "mappings[1]: not configured", nil, func(p *Profile) {
			p.Mapping = mapping
			p.Mappings = []Mapping{mapping, {Mode: MappingAuto}}
		}},
		{"mappings[0] manualSql", "mappings[0].manualSql", ValTooLong, "mappings[0]: mapping: manualSql longer", []string{"16384"}, func(p *Profile) {
			p.Mapping = mapping
			p.Mappings = []Mapping{{Mode: MappingManual, ManualSQL: big}}
		}},
		{"too many mappings", "mappings", ValTooMany, "more than 64 mappings", []string{"64"}, func(p *Profile) {
			p.Mapping = mapping
			for i := 0; i < MaxMappings; i++ {
				p.Mappings = append(p.Mappings, mapping)
			}
		}},
	}
	for _, c := range cases {
		p := goodProfile()
		c.mut(&p)
		err := p.Validate()
		if err == nil {
			t.Errorf("%s: accepted", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.text) {
			t.Errorf("%s: text %q, want %q", c.name, err.Error(), c.text)
		}
		fe := AsFieldError(err)
		if fe == nil {
			t.Errorf("%s: no FieldError in %v", c.name, err)
			continue
		}
		want := append([]string{c.field, c.key}, c.args...)
		if strings.Join(fe.IPCArgs(), "|") != strings.Join(want, "|") {
			t.Errorf("%s: args %q, want %q", c.name, fe.IPCArgs(), want)
		}
		if FormatMessage(fe.Key, fe.IPCArgs(), LangEN) == "" || strings.Contains(FormatMessage(fe.Key, fe.IPCArgs(), LangRU), "{") {
			t.Errorf("%s: text of %s not formatted: %q", c.name, fe.Key, FormatMessage(fe.Key, fe.IPCArgs(), LangRU))
		}
	}
	// still valid: host, host\instance, spaces around
	for _, srv := range []string{"h", `h\i`, ` h \ i `, "10.0.0.5"} {
		p := goodProfile()
		p.Server = srv
		if err := p.Validate(); err != nil {
			t.Errorf("server %q rejected: %v", srv, err)
		}
	}
	// exactly the limits are fine
	p := goodProfile()
	p.Mapping = Mapping{Mode: MappingManual, ManualSQL: big[:MaxManualSQLBytes]}
	if err := p.Validate(); err != nil {
		t.Errorf("manualSql at limit: %v", err)
	}
	p = goodProfile()
	p.Mapping = mapping
	for i := 0; i < MaxMappings-1; i++ {
		p.Mappings = append(p.Mappings, mapping)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("%d mappings: %v", MaxMappings, err)
	}
	// a profile at the limits fits into one IPC request
	p = goodProfile()
	p.Mapping = Mapping{Mode: MappingManual, ManualSQL: big[:MaxManualSQLBytes]}
	b, _ := json.Marshal(IPCRequest{ID: 1, Cmd: CmdSaveProfile, Profile: &p})
	if len(b) > IPCMaxRequest {
		t.Errorf("request %d bytes > %d", len(b), IPCMaxRequest)
	}
	// duplicate ids
	fe := AsFieldError(ValidateProfiles([]Profile{goodProfile(), goodProfile()}))
	if fe == nil || fe.Field != "id" || fe.Key != ValDuplicate {
		t.Errorf("duplicate: %+v", fe)
	}
}

func TestCHK_CFG_03_SettingsFieldErrors(t *testing.T) {
	s := DefaultSettings()
	s.Beep.WarningFreqHz = 1
	s.Language = "de"
	err := s.Validate()
	fe := AsFieldError(err)
	if fe == nil || fe.Field != "beep.warningFreqHz" || fe.Key != ValRange {
		t.Fatalf("first error: %+v (%v)", fe, err)
	}
	if !strings.Contains(err.Error(), "settings: beep.warningFreqHz must be in") || !strings.Contains(err.Error(), "unsupported language") {
		t.Errorf("text changed: %v", err)
	}
	s = DefaultSettings()
	s.WarnMinutes, s.ErrorMinutes = 50, 40
	if fe := AsFieldError(s.Validate()); fe == nil || fe.Field != "errorMinutes" || fe.Key != ValGreaterThan {
		t.Errorf("thresholds: %+v", fe)
	}
	s = DefaultSettings()
	s.PollIntervalSec = 1
	if fe := AsFieldError(s.Validate()); fe == nil || fe.Field != "pollIntervalSec" || len(fe.Args) != 3 {
		t.Errorf("interval: %+v", fe)
	}
}

func TestCHK_CFG_03_ValidationTexts(t *testing.T) {
	for _, k := range append(ValidationKeys(), MonitorMessageKeys()...) {
		if MonitorMessage(k, LangRU) == "" || MonitorMessage(k, LangEN) == "" {
			t.Errorf("no text for %s", k)
		}
	}
	got := FormatMessage(ValRange, []string{"port", ValRange, "0", "65535", "70000"}, LangEN)
	if got != `Field "port": value 70000 is out of range 0..65535.` {
		t.Errorf("format: %q", got)
	}
}

// CHK-IPC-06: the request String masks the password and shows new fields
// without content; the snapshot blocks carry no password / SQL.
func TestCHK_IPC_06_RequestStringAndSnapshotBlocks(t *testing.T) {
	p := goodProfile()
	p.Mapping = Mapping{Mode: MappingManual, ManualSQL: "SELECT secret_sql"}
	r := IPCRequest{ID: 3, Cmd: CmdSaveProfile, Profile: &p, Password: "Pa$$", ClearPassword: true, ExpectedRevision: "abc",
		Settings: &Settings{}}
	for _, s := range []string{r.String(), r.GoString()} {
		if strings.Contains(s, "Pa$$") || strings.Contains(s, "secret_sql") || !strings.Contains(s, "expectedRevision:abc") {
			t.Errorf("String: %s", s)
		}
	}
	b, _ := json.Marshal(struct {
		C *ProfileConnection
		M *MappingSummary
	}{ConnectionOf(p, true), MappingSummaryOf(p)})
	if strings.Contains(string(b), "secret_sql") || !strings.Contains(string(b), `"hasPassword":true`) || !strings.Contains(string(b), `"manual":true`) {
		t.Errorf("blocks: %s", b)
	}
	if !AdminCommand(CmdSaveProfile) || !AdminCommand(CmdGetConfig) || AdminCommand(CmdTestProfile) || AdminCommand(CmdCancel) {
		t.Error("AdminCommand")
	}
}
