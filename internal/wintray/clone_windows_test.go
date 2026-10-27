//go:build windows

package wintray

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
	"github.com/alexal46372-maker/ExchangeChecker/internal/traysettings"
)

// TestCHK_TRAY_58_CloneButton — CapCloneProfile gating, clone_profile request
// shape, and needPassword → FieldPassword focus path (CHK-TRAY-58).
// Pure logic: startCfgApp GUI was flaky on Windows CI (clientElevated /
// cloned timeouts). Cross-package Editor coverage is in traysettings.
func TestCHK_TRAY_58_CloneButton(t *testing.T) {
	if model.CapCloneProfile != "clone_profile" || model.CmdCloneProfile != "clone_profile" {
		t.Fatalf("cap/cmd %q %q", model.CapCloneProfile, model.CmdCloneProfile)
	}
	if !cloneButtonVisible(traysettings.Admin, true) {
		t.Fatal("admin+cap must show clone")
	}
	e := &traysettings.Editor{Mode: traysettings.Admin, Lang: i18n.RU, Revision: "r3",
		Profiles: []model.Profile{{ID: "p1", Name: "Склад", Server: "s", Database: "d", AuthMode: model.AuthSQL, Login: "u", HasPassword: true}}}
	e.Select(0)
	req, pr := e.CloneRequest()
	if pr != nil || req.Cmd != model.CmdCloneProfile || req.ProfileID != "p1" || req.ExpectedRevision != "r3" || req.Password != "" {
		t.Fatalf("req %+v pr %v", req, pr)
	}
	clone := model.Profile{ID: "c1", Name: "Склад (копия)", Server: "s", Database: "d", AuthMode: model.AuthSQL}
	msg := e.CloneDone(model.IPCMessage{Type: model.MsgOK, Result: model.ResultCloned, ProfileID: "c1",
		Profile: &clone, Revision: "r4", NeedPassword: true})
	if e.Base.ID != "c1" || e.Revision != "r4" || e.Base.HasPassword || msg == "" {
		t.Fatalf("done id=%s rev=%s hasPw=%v msg=%q", e.Base.ID, e.Revision, e.Base.HasPassword, msg)
	}
	if cloneFocusField(true) != traysettings.FieldPassword {
		t.Fatal("needPassword must focus password")
	}
	if cloneFocusField(false) != traysettings.FieldNone {
		t.Fatal("no needPassword → no forced focus")
	}
	ru, en := i18n.T(i18n.KeySetClone, i18n.RU), i18n.T(i18n.KeySetClone, i18n.EN)
	if ru == "" || en == "" || ru == i18n.KeySetClone {
		t.Fatalf("labels %q %q", ru, en)
	}
}

// TestCHK_TRAY_58_CloneHiddenReadOnly — hide without cap / read-only / unsaved.
func TestCHK_TRAY_58_CloneHiddenReadOnly(t *testing.T) {
	if cloneButtonVisible(traysettings.ReadOnly, true) {
		t.Fatal("read-only must hide")
	}
	if cloneButtonVisible(traysettings.Admin, false) {
		t.Fatal("no cap must hide")
	}
	e := &traysettings.Editor{Mode: traysettings.ReadOnly, Lang: i18n.RU, Revision: "r1",
		Profiles: []model.Profile{{ID: "p1", Name: "Склад", Server: "s", Database: "d", AuthMode: model.AuthSQL}}}
	e.Select(0)
	if _, pr := e.CloneRequest(); pr == nil {
		t.Fatal("read-only CloneRequest must refuse")
	}
	e.Mode = traysettings.Admin
	e.New()
	if _, pr := e.CloneRequest(); pr == nil {
		t.Fatal("unsaved New must refuse")
	}
}

// TestCHK_TRAY_58_CloneStrings — RU+EN.
func TestCHK_TRAY_58_CloneStrings(t *testing.T) {
	for _, lang := range []string{i18n.RU, i18n.EN} {
		for _, k := range []string{i18n.KeySetClone, i18n.KeySetCloneConfirm, i18n.KeySetClonedNeedPw, i18n.KeyTipSetClone} {
			if i18n.T(k, lang) == k || i18n.T(k, lang) == "" {
				t.Errorf("%s %s missing", k, lang)
			}
		}
	}
}
