package traysettings

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_40_UIFields — every 0.8/0.9 field is in the inventory
// once, in a known section; simple mode is a subset; Viber needs its
// capability.
func TestCHK_TRAY_40_UIFields(t *testing.T) {
	sec := map[Section]bool{}
	for _, s := range Sections {
		sec[s] = true
	}
	seen := map[Field]bool{}
	for _, f := range UIFields {
		if seen[f.ID] || !sec[f.Section] || f.ID == "" {
			t.Errorf("field %+v", f)
		}
		seen[f.ID] = true
	}
	all := []Field{FieldName, FieldHost, FieldInstance, FieldPort, FieldDatabase, FieldAuth, FieldLogin, FieldPassword,
		FieldClearPassword, FieldEncrypt, FieldTLS10, FieldTrust, FieldEnabled, FieldPreset, FieldProfWarn, FieldProfError,
		FieldConnTimeout, FieldQueryTimeout, FieldPollInterval, FieldWarnMinutes, FieldErrorMinutes, FieldBeepEnabled,
		FieldWarnFreq, FieldWarnDur, FieldErrorFreq, FieldErrorDur, FieldGap, FieldLanguage, FieldAutostart,
		FieldDiscPoint, FieldDiscTime, FieldDiscTolerance, FieldDiscLimit}
	for _, id := range all {
		if !seen[id] {
			t.Errorf("%s missing from UIFields", id)
		}
	}
	expert, simple := FieldsOf(UIExpert, nil), FieldsOf(UISimple, nil)
	if len(simple) >= len(expert) {
		t.Error("simple must be a subset")
	}
	for _, f := range expert {
		if f.Capability == CapViber {
			t.Error("viber without capability")
		}
	}
	if n := len(FieldsOf(UIExpert, []string{CapViber})); n != len(expert)+8 {
		t.Errorf("viber fields %d", n-len(expert))
	}
	if NormalizeUIMode("") != UISimple || NormalizeUIMode(" Expert ") != UIExpert || NormalizeUIMode("x") != UISimple {
		t.Error("mode")
	}
}

// TestCHK_TRAY_41_Required — simple: server, database, login method, and
// login / password only for SQL login; expert: what the model requires.
func TestCHK_TRAY_41_Required(t *testing.T) {
	f := Form{Auth: model.AuthSQL}
	if m := Missing(UISimple, f, false); len(m) != 4 {
		t.Errorf("simple sql %v", m)
	}
	if m := Missing(UIExpert, f, false); len(m) != 3 || m[2] != FieldLogin {
		t.Errorf("expert sql %v", m)
	}
	f = Form{Auth: model.AuthWindows, Host: "srv", Database: "db"}
	if m := Missing(UISimple, f, false); len(m) != 0 {
		t.Errorf("windows auth %v", m)
	}
	f = Form{Auth: model.AuthSQL, Host: "srv", Database: "db", Login: "mon"}
	if m := Missing(UISimple, f, true); len(m) != 0 {
		t.Errorf("stored password %v", m)
	}
	for _, id := range RequiredFields(UIExpert, Form{Auth: model.AuthSQL}, false) {
		for _, u := range UIFields {
			if u.ID == id && u.HasDefault {
				t.Errorf("%s has a default and must not be required", id)
			}
		}
	}
}
