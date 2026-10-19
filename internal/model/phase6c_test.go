package model

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestCHK_CFG_03_BeepEnabledJSON — beep.enabled (PLAN §12 p.65): OptBool
// keeps "not given" apart from false; default true; omitted when unset,
// explicit false / true kept; null = unset; anything else is an error;
// Settings stays comparable.
func TestCHK_CFG_03_BeepEnabledJSON(t *testing.T) {
	d := DefaultSettings()
	if d.Beep.Enabled != OptTrue || !d.Beep.BeepEnabled() {
		t.Fatalf("default: %v", d.Beep.Enabled)
	}
	if !d.Equal(DefaultSettings()) {
		t.Fatal("Settings.Equal: defaults differ")
	}
	for _, c := range []struct {
		in   OptBool
		json string
	}{{OptTrue, `"enabled":true`}, {OptFalse, `"enabled":false`}} {
		b, _ := json.Marshal(BeepSettings{Enabled: c.in})
		if !strings.Contains(string(b), c.json) {
			t.Errorf("%v -> %s", c.in, b)
		}
	}
	if b, _ := json.Marshal(BeepSettings{}); strings.Contains(string(b), "enabled") {
		t.Errorf("unset not omitted: %s", b)
	}
	for in, want := range map[string]OptBool{`{}`: OptUnset, `{"enabled":null}`: OptUnset, `{"enabled":false}`: OptFalse, `{"enabled":true}`: OptTrue} {
		var b BeepSettings
		if err := json.Unmarshal([]byte(in), &b); err != nil || b.Enabled != want {
			t.Errorf("%s -> %v %v", in, b.Enabled, err)
		}
	}
	var b BeepSettings
	if err := json.Unmarshal([]byte(`{"enabled":"yes"}`), &b); err == nil {
		t.Error(`"yes" accepted`)
	}
	if !(BeepSettings{}).BeepEnabled() || (BeepSettings{Enabled: OptFalse}).BeepEnabled() {
		t.Error("BeepEnabled")
	}
	if s := (Settings{}).Normalized(); s.Beep.Enabled != OptTrue {
		t.Error("Normalized unset")
	}
	if s := (Settings{Beep: BeepSettings{Enabled: OptFalse}}).Normalized(); s.Beep.Enabled != OptFalse {
		t.Error("Normalized false")
	}
	if OptBoolOf(true) != OptTrue || OptBoolOf(false) != OptFalse {
		t.Error("OptBoolOf")
	}
}

// TestCHK_IPC_06_ValidateProfilesProfileID — ValidateProfiles tags the
// FieldError with the id of the profile it belongs to (invalid field and
// duplicate id); the text and errorArgs are unchanged.
func TestCHK_IPC_06_ValidateProfilesProfileID(t *testing.T) {
	good := NewProfile("a")
	good.Name, good.Server, good.Database, good.AuthMode = "A", "srv", "db", AuthWindows
	good.Mapping = Mapping{Table: "_Node1", PointNameColumn: "_Description", LastSendColumn: "s"}
	bad := good
	bad.ID, bad.Server = "bad", `host\`
	err := ValidateProfiles([]Profile{good, bad})
	fe := AsFieldError(err)
	if fe == nil || fe.ProfileID != "bad" || fe.Field != "server" {
		t.Fatalf("invalid: %+v", fe)
	}
	if want := bad.Validate().Error(); err.Error() != want {
		t.Errorf("text changed: %q, want %q", err.Error(), want)
	}
	if args := fe.IPCArgs(); args[0] != "server" || args[1] != ValServerFormat {
		t.Errorf("errorArgs %q", args)
	}
	dup := good
	fe = AsFieldError(ValidateProfiles([]Profile{good, dup}))
	if fe == nil || fe.ProfileID != "a" || fe.Key != ValDuplicate {
		t.Fatalf("duplicate: %+v", fe)
	}
	// a direct Validate is not tagged
	if fe := AsFieldError(bad.Validate()); fe == nil || fe.ProfileID != "" {
		t.Errorf("Validate tagged: %+v", fe)
	}
	var target *FieldError
	if !errors.As(err, &target) {
		t.Error("chain")
	}
}
