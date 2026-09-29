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
	// phase 4b: every axis separately — mode, TLS floor, certificate
	// validation; a stronger mode does not compensate the others
	sv := func(e EncryptMode, trust bool, tls string) Profile {
		p := Profile{ID: "s", Server: "srv", Database: "db", AuthMode: AuthSQL, Login: "u", Encrypt: e, TrustServerCert: trust, TLSMinVersion: tls}
		return p
	}
	axes := []struct {
		name       string
		req, saved Profile
		want       bool
	}{
		{"on+trust+1.0 vs optional+1.2", sv(EncryptOn, true, TLSMin10), sv(EncryptOptional, false, TLSMin12), false},
		{"on+1.0 vs optional+1.2", sv(EncryptOn, false, TLSMin10), sv(EncryptOptional, false, TLSMin12), false},
		{"strict+1.1 vs on+1.2", sv(EncryptStrict, false, TLSMin11), sv(EncryptOn, false, TLSMin12), false},
		{"on+1.2 vs optional+1.2", sv(EncryptOn, false, TLSMin12), sv(EncryptOptional, false, TLSMin12), true},
		{"on+trust vs on (cert validation lost)", sv(EncryptOn, true, ""), sv(EncryptOn, false, ""), false},
		{"on+trust+1.2 vs optional+1.2 (optional does not validate)", sv(EncryptOn, true, TLSMin12), sv(EncryptOptional, false, TLSMin12), true},
		{"strict vs on", sv(EncryptStrict, false, ""), sv(EncryptOn, false, ""), true},
		{"on vs on+trust", sv(EncryptOn, false, ""), sv(EncryptOn, true, ""), true},
		{"optional+1.0 vs disable", sv(EncryptOptional, false, TLSMin10), sv(EncryptDisable, false, ""), true},
		{"optional vs on+trust", sv(EncryptOptional, false, ""), sv(EncryptOn, true, ""), false},
	}
	for _, c := range axes {
		if got := EncryptNotWeaker(c.req, c.saved); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
		if got := CanReuseSavedPassword(c.req, c.saved); got != c.want {
			t.Errorf("reuse %s: %v, want %v", c.name, got, c.want)
		}
	}
	// host: ASCII case-insensitive only (U+212A KELVIN SIGN is not "k");
	// port 0 = 1433 only without an instance
	plain := sv(EncryptOptional, false, "")
	idc := []struct {
		name   string
		server string
		port   int
		want   bool
	}{
		{"ascii case", "SRV", 0, true},
		{"kelvin sign", "\u212Asrv", 0, false},
		{"port 0 = 1433 without instance", "srv", 1433, true},
		{"port 1434", "srv", 1434, false},
	}
	plain.Server = "ksrv"
	for _, c := range idc {
		r := plain
		r.Server, r.Port = c.server, c.port
		saved := plain
		if c.name != "kelvin sign" {
			saved.Server = "srv"
		}
		if got := SameServerIdentity(r, saved); got != c.want {
			t.Errorf("identity %s: %v, want %v", c.name, got, c.want)
		}
	}
	if SameServerIdentity(mut(func(p *Profile) { p.Port = 1433 }), base) {
		t.Error("with an instance port 0 (SQL Browser) != 1433")
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
	// phase 4b: pollerHung is explicit false in the v2 heartbeat and in
	// the snapshot; serviceStartedAt stays in hello.heartbeat
	b, _ = json.Marshal(Heartbeat{Protocol: 2})
	m = nil
	json.Unmarshal(b, &m)
	if v, ok := m["pollerHung"]; !ok || v != false {
		t.Errorf("heartbeat pollerHung not explicit false: %s", b)
	}
	if _, ok := m["serviceStartedAt"]; !ok {
		t.Errorf("heartbeat without serviceStartedAt: %s", b)
	}
	b, _ = json.Marshal(StatusSnapshot{})
	m = nil
	json.Unmarshal(b, &m)
	if v, ok := m["pollerHung"]; !ok || v != false {
		t.Errorf("snapshot pollerHung not explicit false: %s", b)
	}
	e := &IPCError{Code: ErrCodeBusy, Key: MsgTestBusy, Message: "busy"}
	if e.Error() != "busy" {
		t.Error("IPCError text")
	}
}
