package model

import (
	"encoding/json"
	"testing"
)

// TestCHK_IPC_03_PasswordReuseRules — the pure rules behind test_profile
// with an empty password: encryption strength order, TLS floor, server
// identity (host / instance case-insensitive, port, auth mode, login).
func TestCHK_IPC_03_PasswordReuseRules(t *testing.T) {
	base := Profile{ID: "s", Server: `srv\INST`, Database: "db", AuthMode: AuthSQL, Login: "u", Encrypt: EncryptOptional, TLSMinVersion: TLSMin11}
	order := []Profile{
		{Encrypt: EncryptDisable}, {Encrypt: EncryptOptional}, {Encrypt: EncryptOn, TrustServerCert: true}, {Encrypt: EncryptOn}, {Encrypt: EncryptStrict},
	}
	for i := range order {
		if EncryptStrength(order[i]) != i {
			t.Errorf("strength of %+v = %d", order[i], EncryptStrength(order[i]))
		}
	}
	if EncryptStrength(Profile{}) != 0 {
		t.Error("unset encrypt = disable")
	}
	mut := func(f func(*Profile)) Profile { p := base; f(&p); return p }
	cases := []struct {
		name string
		p    Profile
		want bool
	}{
		{"identical", base, true},
		{"host case", mut(func(p *Profile) { p.Server = ` SRV\inst ` }), true},
		{"database", mut(func(p *Profile) { p.Database = "x" }), true},
		{"stronger", mut(func(p *Profile) { p.Encrypt = EncryptOn }), true},
		{"tls 1.2", mut(func(p *Profile) { p.TLSMinVersion = TLSMin12 }), true},
		{"tls default = 1.2", mut(func(p *Profile) { p.TLSMinVersion = "" }), true},
		{"host", mut(func(p *Profile) { p.Server = `other\INST` }), false},
		{"instance", mut(func(p *Profile) { p.Server = `srv\X` }), false},
		{"port", mut(func(p *Profile) { p.Port = 1433 }), false},
		{"login", mut(func(p *Profile) { p.Login = "U" }), false},
		{"auth", mut(func(p *Profile) { p.AuthMode = AuthWindows }), false},
		{"disable", mut(func(p *Profile) { p.Encrypt = EncryptDisable }), false},
		{"tls 1.0", mut(func(p *Profile) { p.TLSMinVersion = TLSMin10 }), false},
		{"2008 preset floor", mut(func(p *Profile) { p.TLSMinVersion = ""; p.ServerPreset = ServerPresetMSSQL2008 }), false},
	}
	for _, c := range cases {
		if got := CanReuseSavedPassword(c.p, base); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if !SameConnection(base, base) || SameConnection(mut(func(p *Profile) { p.Database = "x" }), base) {
		t.Error("SameConnection")
	}
	for _, cat := range []string{TestOK, TestNetwork, TestLogin, TestDatabase, TestTLS, TestTimeout, TestConfig, TestOther} {
		if MonitorMessage(TestCategoryKey(cat), LangEN) == "" {
			t.Errorf("no message for category %s", cat)
		}
	}
}

// TestCHK_IPC_05_WireCompat — version-2 fields are omitempty: a
// version-1 style message / request marshals exactly as before; the
// password is never in the formatted request.
func TestCHK_IPC_05_WireCompat(t *testing.T) {
	b, _ := json.Marshal(IPCMessage{ID: 3, Type: MsgOK, Result: ResultAccepted})
	if string(b) != `{"id":3,"type":"ok","result":"accepted"}` {
		t.Errorf("v1 reply changed: %s", b)
	}
	b, _ = json.Marshal(IPCRequest{ID: 1, Cmd: CmdPing})
	if string(b) != `{"id":1,"cmd":"ping"}` {
		t.Errorf("v1 request changed: %s", b)
	}
	b, _ = json.Marshal(PointStatus{LagMinutes: 1})
	var m map[string]interface{}
	json.Unmarshal(b, &m)
	if _, ok := m["acknowledged"]; ok {
		t.Errorf("acknowledged not omitempty: %s", b)
	}
	e := &IPCError{Code: ErrCodeBusy, Key: MsgTestBusy, Message: "busy"}
	if e.Error() != "busy" {
		t.Error("IPCError text")
	}
}
