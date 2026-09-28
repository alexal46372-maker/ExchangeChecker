package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// fakeProtector is a reversible non-DPAPI protector for cross-platform tests.
type fakeProtector struct{}

func (fakeProtector) Protect(b []byte) ([]byte, error) {
	out := make([]byte, 4+len(b))
	copy(out, "FAKE")
	for i, c := range b {
		out[4+i] = c ^ 0x5a
	}
	return out, nil
}

func (fakeProtector) Unprotect(b []byte) ([]byte, error) {
	if len(b) < 4 || string(b[:4]) != "FAKE" {
		return nil, errors.New("fake: bad blob")
	}
	out := make([]byte, len(b)-4)
	for i, c := range b[4:] {
		out[i] = c ^ 0x5a
	}
	return out, nil
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: filepath.Join(t.TempDir(), "ExchangeChecker"), Protector: fakeProtector{}}
}

func assertOnlyFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	for _, e := range ents {
		if !want[e.Name()] {
			t.Errorf("unexpected file left in config dir: %s", e.Name())
		}
	}
}

func sampleProfiles() []model.Profile {
	return []model.Profile{
		{
			ID: "p1", Name: "База Москва", Server: `192.168.1.10\SQL2008`, Database: "buh_msk",
			AuthMode: model.AuthSQL, Login: "sa", HasPassword: true, Enabled: true,
			Mapping: model.Mapping{Mode: model.MappingAuto, Table: "_Node123", PointNameColumn: "_Description",
				LastSendColumn: "_Fld1", LastRecvColumn: "_Fld2"},
		},
		{
			ID: "p2", Name: "Filial", Server: "srv-sql", Port: 1500, Database: "trade",
			AuthMode: model.AuthWindows, Enabled: false,
			Mapping: model.Mapping{Mode: model.MappingManual, ManualSQL: "SELECT a, b, c FROM t WHERE x < 5 & y > 1"},
		},
	}
}

func TestCHK_CFG_01_DefaultDir(t *testing.T) {
	t.Setenv("ProgramData", filepath.Join("X", "PD"))
	if got, want := DefaultDir(), filepath.Join("X", "PD", "ExchangeChecker"); got != want {
		t.Errorf("DefaultDir = %q, want %q", got, want)
	}
	t.Setenv("ProgramData", "")
	if got, want := DefaultDir(), filepath.Join(`C:\ProgramData`, "ExchangeChecker"); got != want {
		t.Errorf("DefaultDir fallback = %q, want %q", got, want)
	}
	if s := NewStore(""); s.Dir != DefaultDir() || s.Protector == nil {
		t.Errorf("NewStore(\"\") = %+v", s)
	}
	if s := NewStore("custom"); s.Dir != "custom" {
		t.Errorf("NewStore override dir = %q", s.Dir)
	}
}

func TestCHK_CFG_01_DefaultsWhenMissing(t *testing.T) {
	s := newTestStore(t)
	st, err := s.LoadSettings()
	if err != nil || !reflect.DeepEqual(st, model.DefaultSettings()) {
		t.Errorf("LoadSettings missing = %+v, %v", st, err)
	}
	ps, err := s.LoadProfiles()
	if err != nil || ps == nil || len(ps) != 0 {
		t.Errorf("LoadProfiles missing = %v, %v", ps, err)
	}
	sec, err := s.LoadSecrets()
	if err != nil || sec == nil || len(sec) != 0 {
		t.Errorf("LoadSecrets missing = %v, %v", sec, err)
	}
}

func TestCHK_CFG_01_SettingsRoundtrip(t *testing.T) {
	s := newTestStore(t)
	st := model.DefaultSettings()
	st.PollIntervalSec = 120
	st.WarnMinutes = 15
	st.ErrorMinutes = 45
	st.Beep.WarningFreqHz = 1000
	st.Beep.GapMs = 0
	st.Language = model.LangEN
	st.TrayAutostart = false
	if err := s.SaveSettings(st); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	st.WarnMinutes = 20
	if err := s.SaveSettings(st); err != nil {
		t.Fatalf("SaveSettings (overwrite): %v", err)
	}
	got, err := s.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if !reflect.DeepEqual(got, st) {
		t.Errorf("roundtrip:\n got %+v\nwant %+v", got, st)
	}
	raw, _ := os.ReadFile(s.Path(SettingsFile))
	if !bytes.Contains(raw, []byte("\n  \"pollIntervalSec\": 120")) || !bytes.Contains(raw, []byte(`"version": 1`)) {
		t.Errorf("settings.json not indented / versioned:\n%s", raw)
	}
	assertOnlyFiles(t, s.Dir, SettingsFile)
}

func TestCHK_CFG_01_SettingsPartialFileKeepsDefaults(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := "\xef\xbb\xbf{\"version\":1,\"warnMinutes\":10,\"beep\":{\"errorFreqHz\":2000}}"
	if err := os.WriteFile(s.Path(SettingsFile), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	d := model.DefaultSettings()
	if got.WarnMinutes != 10 || got.ErrorMinutes != d.ErrorMinutes || got.PollIntervalSec != d.PollIntervalSec ||
		got.Beep.ErrorFreqHz != 2000 || got.Beep.WarningFreqHz != d.Beep.WarningFreqHz || !got.TrayAutostart {
		t.Errorf("partial merge = %+v", got)
	}
}

func TestCHK_CFG_01_SettingsValidation(t *testing.T) {
	s := newTestStore(t)
	st := model.DefaultSettings()
	st.WarnMinutes, st.ErrorMinutes = 60, 60
	if err := s.SaveSettings(st); err == nil {
		t.Fatal("SaveSettings accepted warn >= error")
	}
	if _, err := os.Stat(s.Path(SettingsFile)); !os.IsNotExist(err) {
		t.Error("invalid settings were written")
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(s.Path(SettingsFile), []byte(`{"warnMinutes":90,"errorMinutes":60}`), 0o644)
	if _, err := s.LoadSettings(); err == nil {
		t.Error("LoadSettings accepted warn > error")
	}
	_ = os.WriteFile(s.Path(SettingsFile), []byte(`{not json`), 0o644)
	if _, err := s.LoadSettings(); err == nil {
		t.Error("LoadSettings accepted broken JSON")
	}
	_ = os.WriteFile(s.Path(SettingsFile), []byte(`{"version":99}`), 0o644)
	if _, err := s.LoadSettings(); err == nil {
		t.Error("LoadSettings accepted future version")
	}
}

func TestCHK_CFG_01_ProfilesRoundtrip(t *testing.T) {
	s := newTestStore(t)
	ps := sampleProfiles()
	if err := s.SaveProfiles(ps); err != nil {
		t.Fatalf("SaveProfiles: %v", err)
	}
	got, err := s.LoadProfiles()
	if err != nil {
		t.Fatalf("LoadProfiles: %v", err)
	}
	if !reflect.DeepEqual(got, ps) {
		t.Errorf("roundtrip:\n got %+v\nwant %+v", got, ps)
	}
	raw, _ := os.ReadFile(s.Path(ProfilesFile))
	if !bytes.Contains(raw, []byte("База Москва")) {
		t.Error("profiles.json must contain raw UTF-8 names")
	}
	if !bytes.Contains(raw, []byte("x < 5 & y > 1")) {
		t.Error("manual SQL must not be HTML-escaped")
	}
	if bytes.Contains(bytes.ToLower(raw), []byte("\"password\"")) {
		t.Error("profiles.json must not contain a password field")
	}
	if err := s.SaveProfiles(nil); err != nil {
		t.Fatalf("SaveProfiles(nil): %v", err)
	}
	got, err = s.LoadProfiles()
	if err != nil || len(got) != 0 {
		t.Errorf("empty roundtrip = %v, %v", got, err)
	}
	assertOnlyFiles(t, s.Dir, ProfilesFile)
}

func TestCHK_CFG_01_ProfilesValidation(t *testing.T) {
	s := newTestStore(t)
	cases := map[string]func(p *model.Profile){
		"empty server": func(p *model.Profile) { p.Server = "" },
		"bad authmode": func(p *model.Profile) { p.AuthMode = "ntlm" },
		"sql no login": func(p *model.Profile) { p.Login = "" },
	}
	for name, mut := range cases {
		ps := sampleProfiles()
		mut(&ps[0])
		if err := s.SaveProfiles(ps); err == nil {
			t.Errorf("%s: SaveProfiles accepted invalid profile", name)
		}
	}
	ps := sampleProfiles()
	ps[1].ID = ps[0].ID
	if err := s.SaveProfiles(ps); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("duplicate id: err = %v", err)
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(s.Path(ProfilesFile), []byte(`{"version":1,"profiles":[{"id":"x","server":"","database":"d","authMode":"sql"}]}`), 0o644)
	if _, err := s.LoadProfiles(); err == nil {
		t.Error("LoadProfiles accepted empty server")
	}
}

func TestCHK_CFG_01_SecretsRoundtripFakeProtector(t *testing.T) {
	s := newTestStore(t)
	sec := Secrets{"p1": "S3cret!", "p2": "пароль"}
	if err := s.SaveSecrets(sec); err != nil {
		t.Fatalf("SaveSecrets: %v", err)
	}
	raw, _ := os.ReadFile(s.Path(SecretsFile))
	if bytes.Contains(raw, []byte("S3cret!")) || !bytes.HasPrefix(raw, []byte("FAKE")) {
		t.Error("secrets file not protected by Protector")
	}
	got, err := s.LoadSecrets()
	if err != nil || !reflect.DeepEqual(got, sec) {
		t.Fatalf("LoadSecrets = %v, %v", got, err)
	}
	if err := s.SetPassword("p3", "new"); err != nil {
		t.Fatal(err)
	}
	if pw, ok, err := s.Password("p3"); err != nil || !ok || pw != "new" {
		t.Errorf("Password(p3) = %q %v %v", pw, ok, err)
	}
	if err := s.SetPassword("p3", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Password("p3"); ok {
		t.Error("SetPassword(\"\") must delete")
	}
	if err := s.SetPassword("", "x"); err == nil {
		t.Error("empty profile id accepted")
	}
	got, _ = s.LoadSecrets()
	if n := got.Prune(sampleProfiles()[:1]); n != 1 || len(got) != 1 || got["p1"] != "S3cret!" {
		t.Errorf("Prune = %d, %v", n, got)
	}
	assertOnlyFiles(t, s.Dir, SecretsFile)
}

func TestCHK_CFG_01_SecretsErrors(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(s.Path(SecretsFile), []byte("garbage"), 0o600)
	if _, err := s.LoadSecrets(); err == nil {
		t.Error("corrupt secrets accepted")
	}
	s.Protector = nil
	if err := s.SaveSecrets(Secrets{"a": "b"}); !errors.Is(err, ErrNoProtector) {
		t.Errorf("nil protector err = %v", err)
	}
	if _, err := s.LoadSecrets(); !errors.Is(err, ErrNoProtector) {
		t.Errorf("nil protector load err = %v", err)
	}
}

func TestCHK_CFG_01_EffectiveLanguage(t *testing.T) {
	st := model.DefaultSettings()
	st.Language = model.LangEN
	if got := EffectiveLanguage(st); got != "en" {
		t.Errorf("explicit en -> %q", got)
	}
	st.Language = model.LangRU
	if got := EffectiveLanguage(st); got != "ru" {
		t.Errorf("explicit ru -> %q", got)
	}
	st.Language = model.LangAuto
	if got := EffectiveLanguage(st); got != "ru" && got != "en" {
		t.Errorf("auto -> %q", got)
	}
}
