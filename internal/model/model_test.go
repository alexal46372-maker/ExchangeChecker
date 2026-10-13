package model

import (
	"encoding/json"
	"testing"
	"time"
)

func tp(t time.Time) *time.Time { return &t }

func TestCHK_CFG_01_AlertLevelString(t *testing.T) {
	cases := map[AlertLevel]string{LevelOK: "OK", LevelWarning: "Warning", LevelError: "Error"}
	for l, want := range cases {
		if got := l.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int(l), got, want)
		}
	}
	if got := AlertLevel(42).String(); got != "AlertLevel(42)" {
		t.Errorf("invalid level String() = %q", got)
	}
}

func TestCHK_CFG_01_AlertLevelJSON(t *testing.T) {
	for _, l := range []AlertLevel{LevelOK, LevelWarning, LevelError} {
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal %v: %v", l, err)
		}
		var back AlertLevel
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if back != l {
			t.Errorf("roundtrip %v -> %s -> %v", l, b, back)
		}
	}
	if b, _ := json.Marshal(LevelWarning); string(b) != `"warning"` {
		t.Errorf("LevelWarning JSON = %s", b)
	}
	var x AlertLevel
	if err := json.Unmarshal([]byte(`"bogus"`), &x); err == nil {
		t.Error("expected error for unknown level")
	}
	if _, err := json.Marshal(AlertLevel(9)); err == nil {
		t.Error("expected error marshaling invalid level")
	}
}

func TestCHK_CFG_01_Worst(t *testing.T) {
	if got := Worst(); got != LevelOK {
		t.Errorf("Worst() = %v", got)
	}
	if got := Worst(LevelOK, LevelWarning, LevelOK); got != LevelWarning {
		t.Errorf("Worst(ok,warn,ok) = %v", got)
	}
	if got := Worst(LevelWarning, LevelError, LevelOK); got != LevelError {
		t.Errorf("Worst(warn,err,ok) = %v", got)
	}
}

func TestCHK_CFG_01_DBZone(t *testing.T) {
	name, off := time.Date(2026, 1, 1, 0, 0, 0, 0, DBZone).Zone()
	if name != "UTC+3" || off != 3*3600 {
		t.Fatalf("DBZone = %s %d", name, off)
	}
	want := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	for _, loc := range []*time.Location{time.UTC, time.FixedZone("X", -5*3600), time.Local} {
		naive := time.Date(2026, 9, 28, 12, 0, 0, 0, loc)
		got := FromDBNaive(naive)
		if !got.Equal(want) {
			t.Errorf("FromDBNaive(12:00 %v) = %v, want instant %v", loc, got, want)
		}
		if got.Hour() != 12 || got.Location() != DBZone {
			t.Errorf("FromDBNaive wall clock/zone = %v", got)
		}
	}
	u := time.Date(2026, 9, 28, 18, 37, 0, 0, time.UTC)
	if h := InDBZone(u).Hour(); h != 21 {
		t.Errorf("InDBZone hour = %d, want 21", h)
	}
	if NowDB().Location() != DBZone {
		t.Error("NowDB not in DBZone")
	}
}

func TestCHK_CFG_01_FreshAt(t *testing.T) {
	send := FromDBNaive(time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC))
	recv := FromDBNaive(time.Date(2026, 9, 28, 11, 30, 0, 0, time.UTC))
	cases := []struct {
		name       string
		send, recv *time.Time
		want       time.Time
		ok         bool
	}{
		{"both nil", nil, nil, time.Time{}, false},
		{"send only", tp(send), nil, send, true},
		{"recv only", nil, tp(recv), recv, true},
		{"recv newer", tp(send), tp(recv), recv, true},
		{"send newer", tp(recv), tp(send), recv, true},
		{"equal", tp(send), tp(send), send, true},
	}
	for _, c := range cases {
		p := ExchangePoint{Name: c.name, LastSend: c.send, LastRecv: c.recv}
		got, ok := p.FreshAt()
		if ok != c.ok || !got.Equal(c.want) {
			t.Errorf("%s: FreshAt = %v,%v want %v,%v", c.name, got, ok, c.want, c.ok)
		}
		if ok && got.Location() != DBZone {
			t.Errorf("%s: FreshAt not in DBZone", c.name)
		}
	}
	p := ExchangePoint{LastSend: tp(send.UTC()), LastRecv: tp(recv.Add(-2 * time.Hour).UTC())}
	if got, _ := p.FreshAt(); !got.Equal(send) {
		t.Errorf("mixed zones FreshAt = %v, want %v", got, send)
	}
}

func TestCHK_CFG_01_Thresholds(t *testing.T) {
	s := DefaultSettings() // warn 30, error 60
	now := FromDBNaive(time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	at := func(d time.Duration) *time.Time { x := now.Add(-d); return &x }
	cases := []struct {
		name       string
		send, recv *time.Time
		want       AlertLevel
	}{
		{"fresh", at(0), nil, LevelOK},
		{"29m59s", at(29*time.Minute + 59*time.Second), nil, LevelOK},
		{"30m exact", at(30 * time.Minute), nil, LevelWarning},
		{"59m recv", nil, at(59 * time.Minute), LevelWarning},
		{"60m exact", at(60 * time.Minute), nil, LevelError},
		{"3h/2h", at(3 * time.Hour), at(2 * time.Hour), LevelError},
		{"fresh recv rescues old send", at(5 * time.Hour), at(time.Minute), LevelOK},
		{"future clock skew", at(-10 * time.Minute), nil, LevelOK},
		{"both nil", nil, nil, LevelError},
	}
	for _, c := range cases {
		p := ExchangePoint{Name: c.name, LastSend: c.send, LastRecv: c.recv}
		if got := Evaluate(p, s, now); got != c.want {
			t.Errorf("%s: Evaluate = %v, want %v", c.name, got, c.want)
		}
		if got := Evaluate(p, s, now.UTC()); got != c.want {
			t.Errorf("%s (now UTC): Evaluate = %v, want %v", c.name, got, c.want)
		}
	}
	p := ExchangePoint{LastSend: at(45 * time.Minute)}
	if lag, ok := p.Lag(now); !ok || lag != 45*time.Minute {
		t.Errorf("Lag = %v,%v", lag, ok)
	}
	if lag, ok := (ExchangePoint{LastRecv: at(-time.Hour)}).Lag(now); !ok || lag != 0 {
		t.Errorf("negative lag not clamped: %v,%v", lag, ok)
	}
	s.WarnMinutes, s.ErrorMinutes = 5, 10
	if got := LevelForLag(7*time.Minute, s); got != LevelWarning {
		t.Errorf("custom thresholds 7m = %v", got)
	}
}

func TestCHK_CFG_01_Refresh(t *testing.T) {
	s := DefaultSettings()
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	p := ExchangePoint{Name: "empty"}
	p.Refresh(s, now)
	if p.Level != LevelError || p.Message != ReasonNoTimestamps || p.CheckedAt.Location() != DBZone {
		t.Errorf("Refresh(nil,nil) = %+v", p)
	}
	old := now.Add(-40 * time.Minute)
	p.LastSend = &old
	p.Refresh(s, now)
	if p.Level != LevelWarning || p.Message != "" {
		t.Errorf("Refresh(40m) = %+v", p)
	}
}

func TestCHK_CFG_01_SettingsValidate(t *testing.T) {
	d := DefaultSettings()
	if err := d.Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	if d.PollIntervalSec != 60 || d.WarnMinutes != 30 || d.ErrorMinutes != 60 || !d.TrayAutostart || d.Language != LangAuto {
		t.Errorf("unexpected defaults: %+v", d)
	}
	bad := map[string]func(*Settings){
		"warn==error":  func(s *Settings) { s.WarnMinutes, s.ErrorMinutes = 60, 60 },
		"warn>error":   func(s *Settings) { s.WarnMinutes, s.ErrorMinutes = 90, 60 },
		"warn=0":       func(s *Settings) { s.WarnMinutes = 0 },
		"poll too low": func(s *Settings) { s.PollIntervalSec = 1 },
		"poll too big": func(s *Settings) { s.PollIntervalSec = MaxPollIntervalSec + 1 },
		"lang de":      func(s *Settings) { s.Language = "de" },
		"freq low":     func(s *Settings) { s.Beep.WarningFreqHz = 10 },
		"freq high":    func(s *Settings) { s.Beep.ErrorFreqHz = 40000 },
		"dur 0":        func(s *Settings) { s.Beep.ErrorDurMs = 0 },
		"gap<0":        func(s *Settings) { s.Beep.GapMs = -1 },
		"error huge":   func(s *Settings) { s.ErrorMinutes = MaxThresholdMinutes + 1 },
	}
	for name, mut := range bad {
		s := DefaultSettings()
		mut(&s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
	for _, lang := range []string{LangAuto, LangRU, LangEN} {
		s := DefaultSettings()
		s.Language = lang
		if err := s.Validate(); err != nil {
			t.Errorf("lang %q: %v", lang, err)
		}
	}
	if d.WarnAfter() != 30*time.Minute || d.ErrorAfter() != time.Hour || d.PollInterval() != time.Minute {
		t.Error("duration helpers wrong")
	}
}

func goodProfile() Profile {
	return Profile{
		ID:       "p1",
		Name:     "Главная база",
		Server:   `10.0.0.5\SQL2008`,
		Database: "buh",
		AuthMode: AuthSQL,
		Login:    "sa",
		Enabled:  true,
	}
}

func TestCHK_CFG_01_ProfileValidate(t *testing.T) {
	if err := goodProfile().Validate(); err != nil {
		t.Fatalf("good profile: %v", err)
	}
	okMut := map[string]func(*Profile){
		"windows no login": func(p *Profile) { p.AuthMode, p.Login = AuthWindows, "" },
		"port 1500":        func(p *Profile) { p.Port = 1500 },
		"auto mapping": func(p *Profile) {
			p.Mapping = Mapping{Mode: MappingAuto, Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "_Fld1", LastRecvColumn: "_Fld2"}
		},
		"manual sql": func(p *Profile) { p.Mapping = Mapping{Mode: MappingManual, ManualSQL: "SELECT 1"} },
	}
	for name, mut := range okMut {
		p := goodProfile()
		mut(&p)
		if err := p.Validate(); err != nil {
			t.Errorf("%s: unexpected error %v", name, err)
		}
	}
	badMut := map[string]func(*Profile){
		"empty id":        func(p *Profile) { p.ID = "" },
		"empty server":    func(p *Profile) { p.Server = "  " },
		"empty database":  func(p *Profile) { p.Database = "" },
		"bad authmode":    func(p *Profile) { p.AuthMode = "kerberos" },
		"empty authmode":  func(p *Profile) { p.AuthMode = "" },
		"sql no login":    func(p *Profile) { p.Login = "" },
		"port too big":    func(p *Profile) { p.Port = 70000 },
		"bad map mode":    func(p *Profile) { p.Mapping.Mode = "weird" },
		"table no cols":   func(p *Profile) { p.Mapping = Mapping{Mode: MappingAuto, Table: "_Node1"} },
		"table no times":  func(p *Profile) { p.Mapping = Mapping{Table: "_Node1", PointNameColumn: "_Code"} },
		"manual no query": func(p *Profile) { p.Mapping = Mapping{Mode: MappingManual} },
	}
	for name, mut := range badMut {
		p := goodProfile()
		mut(&p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	p := goodProfile()
	if p.EffectivePort() != 0 { // 10.0.0.5\SQL2008: SQL Browser
		t.Errorf("EffectivePort with an instance = %d", p.EffectivePort())
	}
	if p.DisplayName() != "Главная база" {
		t.Error("DisplayName")
	}
	p.Name = ""
	if p.DisplayName() != `10.0.0.5\SQL2008/buh` {
		t.Errorf("DisplayName fallback = %q", p.DisplayName())
	}
	a, b := goodProfile(), goodProfile()
	if err := ValidateProfiles([]Profile{a, b}); err == nil {
		t.Error("duplicate ids accepted")
	}
	b.ID = "p2"
	if err := ValidateProfiles([]Profile{a, b}); err != nil {
		t.Errorf("unique ids rejected: %v", err)
	}
}

func TestCHK_CFG_01_ProfileStatusLevel(t *testing.T) {
	ps := ProfileStatus{ProfileID: "p1"}
	if ps.Level() != LevelOK {
		t.Error("empty profile should be OK")
	}
	ps.Points = []ExchangePoint{{Level: LevelOK}, {Level: LevelWarning}, {Level: LevelOK}}
	if ps.Level() != LevelWarning {
		t.Errorf("worst = %v, want Warning (yellow)", ps.Level())
	}
	ps.Points = append(ps.Points, ExchangePoint{Level: LevelError})
	if ps.Level() != LevelError {
		t.Errorf("worst = %v, want Error (red)", ps.Level())
	}
	ps.Points = []ExchangePoint{{Level: LevelOK}}
	ps.Error = "login failed"
	if ps.Level() != LevelError {
		t.Error("connection error must be Error")
	}
}

// TestCHK_MODEL_01_EffectivePort — the port the driver connects to:
// Port when set, 1433 for a plain host, 0 (SQL Browser) for an instance
// (phase 8; same rule as sqlcheck.driverConfig and SameServerIdentity).
func TestCHK_MODEL_01_EffectivePort(t *testing.T) {
	cases := []struct {
		server string
		port   int
		want   int
	}{
		{"srv", 0, DefaultSQLPort},
		{"srv", 1500, 1500},
		{`srv\SQLEXPRESS`, 0, 0},
		{`srv\SQLEXPRESS`, 1500, 1500},
		{` srv \ SQL1 `, 0, 0},
	}
	for _, c := range cases {
		if got := (Profile{Server: c.server, Port: c.port}).EffectivePort(); got != c.want {
			t.Errorf("%q:%d -> %d, want %d", c.server, c.port, got, c.want)
		}
	}
}
