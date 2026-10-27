package traysettings

import (
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/i18n"
	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_TRAY_58_CloneRequest — clone_profile carries profileId +
// expectedRevision; admin + saved profile only; password never sent.
func TestCHK_TRAY_58_CloneRequest(t *testing.T) {
	e := &Editor{Mode: Admin, Lang: i18n.RU, Revision: "r3",
		Profiles: []model.Profile{{ID: "p1", Name: "Склад", Server: "s", Database: "d", AuthMode: model.AuthSQL, Login: "u", HasPassword: true}}}
	e.Select(0)
	req, pr := e.CloneRequest()
	if pr != nil {
		t.Fatal(pr.Text)
	}
	if req.Cmd != model.CmdCloneProfile || req.ProfileID != "p1" || req.ExpectedRevision != "r3" || req.Password != "" || req.NewID != "" {
		t.Fatalf("%+v", req)
	}
	e.Mode = ReadOnly
	if _, pr := e.CloneRequest(); pr == nil {
		t.Fatal("read-only must refuse")
	}
	e.Mode = Admin
	e.New()
	if _, pr := e.CloneRequest(); pr == nil {
		t.Fatal("new unsaved must refuse")
	}
}

// TestCHK_TRAY_58_CloneDone — selects the clone and reports needPassword.
func TestCHK_TRAY_58_CloneDone(t *testing.T) {
	e := &Editor{Mode: Admin, Lang: i18n.RU, Revision: "r1",
		Profiles: []model.Profile{{ID: "p1", Name: "Склад", Server: "s", Database: "d", AuthMode: model.AuthSQL}}}
	e.Select(0)
	clone := model.Profile{ID: "c1", Name: "Склад (копия)", Server: "s", Database: "d", AuthMode: model.AuthSQL}
	msg := e.CloneDone(model.IPCMessage{Type: model.MsgOK, Result: model.ResultCloned, ProfileID: "c1",
		Profile: &clone, Revision: "r2", NeedPassword: true})
	if e.Revision != "r2" || e.Base.ID != "c1" || len(e.Profiles) != 2 {
		t.Fatalf("sel=%s rev=%s n=%d", e.Base.ID, e.Revision, len(e.Profiles))
	}
	if msg == "" || e.Base.HasPassword {
		t.Fatalf("status %q hasPassword %v", msg, e.Base.HasPassword)
	}
	for _, lang := range []string{i18n.RU, i18n.EN} {
		if i18n.T(i18n.KeySetClone, lang) == i18n.KeySetClone {
			t.Errorf("missing key %s %s", i18n.KeySetClone, lang)
		}
		if i18n.T(i18n.KeySetCloneConfirm, lang) == i18n.KeySetCloneConfirm {
			t.Errorf("missing confirm %s", lang)
		}
	}
}
