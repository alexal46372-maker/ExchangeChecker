package sqlcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/go-mssqldb/msdsn"

	"github.com/alexal46372-maker/ExchangeChecker/internal/config"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 2d, customer decision: SQL Server 2008 R2 RTM without SP, the
// primary connection mode is without encryption (PLAN.md §12 п. 31).

// TestCHK_SQL_01_EncryptDefaultDisable: a new profile, an unset mode and
// the "mssql2008" preset connect with encrypt=disable; explicit modes are
// kept; strict stays forbidden with the preset.
func TestCHK_SQL_01_EncryptDefaultDisable(t *testing.T) {
	if model.DefaultEncrypt != model.EncryptDisable {
		t.Fatalf("DefaultEncrypt = %q", model.DefaultEncrypt)
	}
	// unset mode (zero value) = disable on the wire, no TLS config
	p := connProfile()
	if p.EffectiveEncrypt() != model.EncryptDisable {
		t.Errorf("unset: effective %q", p.EffectiveEncrypt())
	}
	if cfg, err := driverConfig(p, "x"); err != nil || cfg.Encryption != msdsn.EncryptionDisabled || cfg.TLSConfig != nil {
		t.Errorf("unset: %v %v", cfg.Encryption, err)
	}
	// new profile defaults
	n := connProfile()
	n.ApplySQLDefaults()
	if n.Encrypt != model.EncryptDisable || n.Validate() != nil {
		t.Errorf("new profile: %q", n.Encrypt)
	}
	// explicit values are never changed by the defaults or the preset
	for _, e := range []model.EncryptMode{model.EncryptOptional, model.EncryptDisable, model.EncryptOn} {
		q := connProfile()
		q.Encrypt = e
		q.ApplySQLDefaults()
		if err := q.ApplyServerPreset(model.ServerPresetMSSQL2008); err != nil || q.Encrypt != e || q.EffectiveEncrypt() != e {
			t.Errorf("explicit %q changed to %q (%v)", e, q.Encrypt, err)
		}
		if q.Validate() != nil {
			t.Errorf("explicit %q + preset must validate", e)
		}
	}
	// the preset: unset -> disable, TLS 1.0 remains available for optional/on
	pr := connProfile()
	if err := pr.ApplyServerPreset(model.ServerPresetMSSQL2008); err != nil || pr.Encrypt != model.EncryptDisable || pr.TLSMinVersion != model.TLSMin10 {
		t.Errorf("preset: %+v %v", pr, err)
	}
	for _, c := range []struct {
		mode model.EncryptMode
		want msdsn.Encryption
	}{
		{model.EncryptDefault, msdsn.EncryptionDisabled},
		{model.EncryptDisable, msdsn.EncryptionDisabled},
		{model.EncryptOptional, msdsn.EncryptionOff},
		{model.EncryptOn, msdsn.EncryptionRequired},
	} {
		q := pr
		q.Encrypt, q.TrustServerCert = c.mode, true
		cfg, err := driverConfig(q, "x")
		if err != nil || cfg.Encryption != c.want {
			t.Errorf("preset %q: %v, want %v (%v)", c.mode, cfg.Encryption, c.want, err)
			continue
		}
		if c.want != msdsn.EncryptionDisabled && cfg.TLSConfig.MinVersion != 0x0301 {
			t.Errorf("preset %q: TLS min %x, want TLS 1.0", c.mode, cfg.TLSConfig.MinVersion)
		}
	}
	st := connProfile()
	st.Encrypt = model.EncryptStrict
	if err := st.ApplyServerPreset(model.ServerPresetMSSQL2008); err != nil || st.Encrypt != model.EncryptDisable {
		t.Errorf("strict + preset: %q %v", st.Encrypt, err)
	}
	st.Encrypt = model.EncryptStrict
	if st.Validate() == nil {
		t.Error("strict + mssql2008 must not validate")
	}
}

// TestCHK_SQL_05_EncryptOldProfilesFile: profiles.json written before phase
// 2d without "encrypt" is read as disable; explicit values survive a load
// and a save unchanged.
func TestCHK_SQL_05_EncryptOldProfilesFile(t *testing.T) {
	s := config.NewStore(t.TempDir())
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `{"version":1,"profiles":[
	  {"id":"old","name":"old","server":"srv","database":"d","authMode":"sql","login":"ec_monitor","enabled":true,
	   "mapping":{"mode":"auto"}},
	  {"id":"old2008","name":"p","server":"srv","database":"d","authMode":"sql","login":"ec_monitor","enabled":true,
	   "serverPreset":"mssql2008","tlsMinVersion":"1.0","mapping":{"mode":"auto"}},
	  {"id":"opt","name":"o","server":"srv","database":"d","authMode":"sql","login":"ec_monitor","enabled":true,
	   "encrypt":"optional","serverPreset":"mssql2008","mapping":{"mode":"auto"}},
	  {"id":"on","name":"n","server":"srv","database":"d","authMode":"windows","enabled":true,
	   "encrypt":"on","trustServerCert":true,"mapping":{"mode":"auto"}}]}`
	if err := os.WriteFile(filepath.Join(s.Dir, config.ProfilesFile), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	check := func(stage string, ps []model.Profile) {
		want := map[string]struct {
			raw, eff model.EncryptMode
			enc      msdsn.Encryption
		}{
			"old":     {model.EncryptDefault, model.EncryptDisable, msdsn.EncryptionDisabled},
			"old2008": {model.EncryptDefault, model.EncryptDisable, msdsn.EncryptionDisabled},
			"opt":     {model.EncryptOptional, model.EncryptOptional, msdsn.EncryptionOff},
			"on":      {model.EncryptOn, model.EncryptOn, msdsn.EncryptionRequired},
		}
		if len(ps) != len(want) {
			t.Fatalf("%s: %d profiles", stage, len(ps))
		}
		for _, p := range ps {
			w := want[p.ID]
			cfg, err := driverConfig(p, "x")
			if p.Encrypt != w.raw || p.EffectiveEncrypt() != w.eff || err != nil || cfg.Encryption != w.enc {
				t.Errorf("%s %s: raw %q eff %q enc %v (%v)", stage, p.ID, p.Encrypt, p.EffectiveEncrypt(), cfg.Encryption, err)
			}
		}
	}
	ps, err := s.LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	check("load", ps)
	if err := s.SaveProfiles(ps); err != nil {
		t.Fatal(err)
	}
	if ps, err = s.LoadProfiles(); err != nil {
		t.Fatal(err)
	}
	check("reload", ps)
}

// TestCHK_SQL_05_WarnPlaintextLogin: the GUI warning key for SQL
// authentication without encryption; none for Windows authentication or
// with TLS.
func TestCHK_SQL_05_WarnPlaintextLogin(t *testing.T) {
	win := model.Profile{ID: "w", Server: "srv", Database: "d", AuthMode: model.AuthWindows}
	winUser := win
	winUser.Login = `DOMAIN\ec`
	cases := []struct {
		name string
		p    model.Profile
		warn bool
	}{
		{"sql unset", connProfile(), true},
		{"sql disable", func() model.Profile { p := connProfile(); p.Encrypt = model.EncryptDisable; return p }(), true},
		{"sql preset", func() model.Profile {
			p := connProfile()
			_ = p.ApplyServerPreset(model.ServerPresetMSSQL2008)
			return p
		}(), true},
		{"sql optional", func() model.Profile { p := connProfile(); p.Encrypt = model.EncryptOptional; return p }(), false},
		{"sql on", func() model.Profile { p := connProfile(); p.Encrypt = model.EncryptOn; return p }(), false},
		{"windows unset", win, false},
		{"windows disable", func() model.Profile { p := win; p.Encrypt = model.EncryptDisable; return p }(), false},
		{"windows DOMAIN\\user disable", func() model.Profile { p := winUser; p.Encrypt = model.EncryptDisable; return p }(), false},
	}
	for _, c := range cases {
		w := Warnings(c.p)
		got := len(w) == 1 && w[0] == MsgWarnPlaintextLogin
		if got != c.warn || (!c.warn && len(w) != 0) || c.p.PlaintextLogin() != c.warn {
			t.Errorf("%s: warnings %v, want plaintext=%v", c.name, w, c.warn)
		}
	}
	ru, en := Message(MsgWarnPlaintextLogin, model.LangRU), Message(MsgWarnPlaintextLogin, model.LangEN)
	for _, sub := range []string{"пароль", "Windows", "db_datareader"} {
		if !strings.Contains(ru, sub) {
			t.Errorf("ru text lacks %q: %s", sub, ru)
		}
	}
	for _, sub := range []string{"password", "Windows", "db_datareader"} {
		if !strings.Contains(en, sub) {
			t.Errorf("en text lacks %q: %s", sub, en)
		}
	}
	found := false
	for _, k := range MessageKeys() {
		found = found || k == MsgWarnPlaintextLogin
	}
	if !found || MsgWarnPlaintextLogin != "sql.warn_plaintext_login" {
		t.Error("warning key not listed in MessageKeys")
	}
}
