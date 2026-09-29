package model

import (
	"encoding/json"
	"testing"
)

// TestCHK_SQL_01_EncryptDefaultDisableModel: phase 2d customer decision —
// the unset mode means disable, explicit modes are kept, new profiles and
// the "mssql2008" preset get "disable", strict stays forbidden with it.
func TestCHK_SQL_01_EncryptDefaultDisableModel(t *testing.T) {
	for _, c := range []struct{ in, want EncryptMode }{
		{EncryptDefault, EncryptDisable}, {EncryptDisable, EncryptDisable},
		{EncryptOptional, EncryptOptional}, {EncryptOn, EncryptOn}, {EncryptStrict, EncryptStrict},
	} {
		if got := c.in.Effective(); got != c.want {
			t.Errorf("%q.Effective() = %q, want %q", c.in, got, c.want)
		}
	}
	p := sqlTestProfile()
	p.ApplySQLDefaults()
	if p.Encrypt != EncryptDisable {
		t.Errorf("new profile: %q", p.Encrypt)
	}
	q := sqlTestProfile()
	q.Encrypt = EncryptOn
	q.ApplySQLDefaults()
	if q.Encrypt != EncryptOn {
		t.Errorf("explicit value changed: %q", q.Encrypt)
	}
	r := sqlTestProfile()
	if err := r.ApplyServerPreset(ServerPresetMSSQL2008); err != nil || r.Encrypt != EncryptDisable || r.TLSMinVersion != TLSMin10 || r.Validate() != nil {
		t.Errorf("preset: %+v %v", r, err)
	}
	r.Encrypt = EncryptStrict
	if r.Validate() == nil {
		t.Error("strict + mssql2008 must not validate")
	}
	// a document without "encrypt" reads as disable; explicit values stay
	var old, opt Profile
	if err := json.Unmarshal([]byte(`{"id":"p1","server":"s","database":"d","authMode":"sql","login":"u","mapping":{"mode":"auto"}}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Encrypt != EncryptDefault || old.EffectiveEncrypt() != EncryptDisable || !old.PlaintextLogin() {
		t.Errorf("old document: raw %q eff %q", old.Encrypt, old.EffectiveEncrypt())
	}
	if err := json.Unmarshal([]byte(`{"id":"p1","server":"s","database":"d","authMode":"sql","login":"u","encrypt":"optional","mapping":{"mode":"auto"}}`), &opt); err != nil {
		t.Fatal(err)
	}
	if opt.Encrypt != EncryptOptional || opt.EffectiveEncrypt() != EncryptOptional || opt.PlaintextLogin() {
		t.Errorf("explicit optional: raw %q eff %q", opt.Encrypt, opt.EffectiveEncrypt())
	}
	w := old
	w.AuthMode, w.Login = AuthWindows, ""
	if w.PlaintextLogin() {
		t.Error("windows auth must not be a plaintext login")
	}
}
